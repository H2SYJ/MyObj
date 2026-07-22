package service

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/config"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const downloadLeaseDuration = 45 * time.Second

// DownloadManager 使用数据库任务记录驱动单机可靠下载调度。
type DownloadManager struct {
	factory       *impl.RepositoryFactory
	tempDir       string
	config        config.Download
	workerID      string
	networkPolicy *download.NetworkPolicy

	notify chan struct{}
	stop   chan struct{}

	mu            sync.Mutex
	active        map[string]context.CancelFunc
	activeUsers   map[string]int
	activeBatches map[string]bool
	secrets       map[string]string
	torrentSem    chan struct{}
}

func NewDownloadManager(factory *impl.RepositoryFactory, tempDir string, policies ...*download.NetworkPolicy) *DownloadManager {
	downloadConfig := config.Download{
		MaxActiveTasks:            4,
		MaxActiveTasksPerUser:     2,
		HTTPMaxConnectionsPerTask: 4,
		HTTPChunkSizeMB:           10,
		TorrentMaxActiveSessions:  2,
		MaxRetries:                3,
	}
	if config.CONFIG != nil {
		downloadConfig = config.CONFIG.Download
	}
	hostname, _ := os.Hostname()
	networkPolicy := download.NewNetworkPolicy()
	if len(policies) > 0 && policies[0] != nil {
		networkPolicy = policies[0]
	}
	return &DownloadManager{
		factory:       factory,
		tempDir:       tempDir,
		config:        downloadConfig,
		workerID:      fmt.Sprintf("%s-%s", hostname, uuid.NewString()),
		networkPolicy: networkPolicy,
		notify:        make(chan struct{}, 1),
		stop:          make(chan struct{}),
		active:        make(map[string]context.CancelFunc),
		activeUsers:   make(map[string]int),
		activeBatches: make(map[string]bool),
		secrets:       make(map[string]string),
		torrentSem:    make(chan struct{}, downloadConfig.TorrentMaxActiveSessions),
	}
}

func (m *DownloadManager) Start() {
	if err := m.recoverInterruptedTasks(); err != nil {
		logger.LOG.Error("恢复离线下载任务失败", "error", err)
	}
	go m.loop()
}

func (m *DownloadManager) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-m.notify:
			m.dispatch()
		case <-ticker.C:
			m.dispatch()
		}
	}
}

func (m *DownloadManager) Notify(taskID, filePassword string) {
	if filePassword != "" {
		m.mu.Lock()
		m.secrets[taskID] = filePassword
		m.mu.Unlock()
	}
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *DownloadManager) dispatch() {
	m.mu.Lock()
	available := m.config.MaxActiveTasks - len(m.active)
	m.mu.Unlock()
	if available <= 0 {
		return
	}
	tasks, err := m.factory.DownloadTask().ListRunnable(context.Background(), time.Now(), available*4)
	if err != nil {
		logger.LOG.Error("查询待执行下载任务失败", "error", err)
		return
	}
	for _, task := range tasks {
		if !m.startTask(task) {
			continue
		}
		available--
		if available == 0 {
			return
		}
	}
}

func (m *DownloadManager) startTask(task *models.DownloadTask) bool {
	if task.EnableEncryption {
		m.mu.Lock()
		_, hasSecret := m.secrets[task.ID]
		m.mu.Unlock()
		if !hasSecret {
			return false
		}
	}
	isTorrent := task.Type == enum.DownloadTaskTypeBtp.Value() || task.Type == enum.DownloadTaskTypeMagnet.Value()
	if isTorrent {
		select {
		case m.torrentSem <- struct{}{}:
		default:
			return false
		}
	}

	m.mu.Lock()
	if (task.BatchID != "" && m.activeBatches[task.BatchID]) || len(m.active) >= m.config.MaxActiveTasks || m.activeUsers[task.UserID] >= m.config.MaxActiveTasksPerUser {
		m.mu.Unlock()
		if isTorrent {
			<-m.torrentSem
		}
		return false
	}
	runToken := uuid.NewString()
	claimed, err := m.factory.DownloadTask().Claim(context.Background(), task.ID, m.workerID, runToken, time.Now().Add(downloadLeaseDuration))
	if err != nil || !claimed {
		m.mu.Unlock()
		if isTorrent {
			<-m.torrentSem
		}
		if err != nil {
			logger.LOG.Error("认领下载任务失败", "taskID", task.ID, "error", err)
		}
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active[task.ID] = cancel
	m.activeUsers[task.UserID]++
	if task.BatchID != "" {
		m.activeBatches[task.BatchID] = true
	}
	filePassword := m.secrets[task.ID]
	m.mu.Unlock()

	task.RunToken = runToken
	go m.runTask(ctx, task, filePassword, isTorrent)
	return true
}

func (m *DownloadManager) runTask(ctx context.Context, task *models.DownloadTask, filePassword string, isTorrent bool) {
	heartbeatDone := make(chan struct{})
	go m.heartbeat(ctx, task.ID, task.RunToken, heartbeatDone)

	var fileID string
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("下载任务发生异常: %v", recovered)
		}
		close(heartbeatDone)
		m.finishTask(task, fileID, err)
		if isTorrent {
			<-m.torrentSem
		}
		m.releaseActive(task)
		m.Notify("", "")
	}()
	reservedSize, reserveErr := m.ensureReservation(task, task.FileSize)
	if reserveErr != nil {
		err = reserveErr
		return
	}
	task.ReservedSize = reservedSize
	secret := ""
	if config.CONFIG != nil {
		secret = config.CONFIG.Auth.Secret
	}
	requestHeaders, decryptErr := download.DecryptRequestHeaders(secret, task.ID, task.UserID, task.RequestHeadersEncrypted)
	if decryptErr != nil {
		err = &download.CredentialsRequiredError{Reason: "已保存的请求头无法解密，请重新输入请求头后恢复任务"}
		return
	}
	headerHosts, decodeErr := download.DecodeHeaderHosts(task.HeaderHostsJSON)
	if decodeErr != nil {
		err = &download.CredentialsRequiredError{Reason: "已保存的请求头主机无效，请重新输入请求头后恢复任务"}
		return
	}
	if isTorrent {
		opts := &download.TorrentSingleFileDownloadOptions{
			MaxConcurrentPeers: 200,
			EnableEncryption:   task.EnableEncryption,
			VirtualPath:        task.VirtualPath,
			TorrentName:        task.TorrentName,
			InfoHash:           task.InfoHash,
			FilePassword:       filePassword,
			RunToken:           task.RunToken,
			ReservedSize:       task.ReservedSize,
			SessionID:          task.BatchID,
			DownloadLimiter:    m.networkPolicy.DownloadLimiter(),
			UploadLimiter:      m.networkPolicy.BTUploadLimiter(),
		}
		fileID, err = download.DownloadTorrentSingleFile(ctx, task.ID, task.URL, task.FileIndex, task.UserID, m.tempDir, m.factory, opts)
	} else if task.Type == enum.DownloadTaskTypeHLS.Value() {
		opts := &download.HLSDownloadOptions{
			EnableEncryption: task.EnableEncryption,
			VirtualPath:      task.VirtualPath,
			MaxRetries:       m.config.MaxRetries,
			MaxConcurrent:    m.config.HTTPMaxConnectionsPerTask,
			FilePassword:     filePassword,
			RunToken:         task.RunToken,
			ReservedSize:     task.ReservedSize,
			ProxyURL:         m.networkPolicy.ProxyURL(),
			DownloadLimiter:  m.networkPolicy.DownloadLimiter(),
			RequestHeaders:   requestHeaders,
			HeaderHosts:      headerHosts,
			OutputFileName:   task.FileName,
			ReserveSpace: func(size int64) (int64, error) {
				reserved, reserveErr := m.ensureReservation(task, size)
				if reserveErr == nil {
					task.ReservedSize = reserved
				}
				return reserved, reserveErr
			},
		}
		result, downloadErr := download.DownloadHLSWithContext(ctx, task.ID, task.URL, task.UserID, m.tempDir, m.factory, opts)
		err = downloadErr
		if result != nil {
			fileID = result.FileID
			task.FileID = result.FileID
			task.FileName = result.FileName
			task.FileSize = result.FileSize
		}
	} else {
		opts := &download.HTTPDownloadOptions{
			EnableEncryption: task.EnableEncryption,
			VirtualPath:      task.VirtualPath,
			MaxRetries:       m.config.MaxRetries,
			ChunkSize:        int64(m.config.HTTPChunkSizeMB) * 1024 * 1024,
			MaxConcurrent:    m.config.HTTPMaxConnectionsPerTask,
			Timeout:          300,
			FilePassword:     filePassword,
			RunToken:         task.RunToken,
			ReservedSize:     task.ReservedSize,
			ProxyURL:         m.networkPolicy.ProxyURL(),
			DownloadLimiter:  m.networkPolicy.DownloadLimiter(),
			RequestHeaders:   requestHeaders,
			HeaderHosts:      headerHosts,
			OutputFileName:   task.FileName,
			ReserveSpace: func(size int64) (int64, error) {
				reserved, reserveErr := m.ensureReservation(task, size)
				if reserveErr == nil {
					task.ReservedSize = reserved
				}
				return reserved, reserveErr
			},
		}
		result, downloadErr := download.DownloadHTTPWithContext(ctx, task.ID, task.URL, task.UserID, m.tempDir, m.factory, opts)
		err = downloadErr
		if result != nil {
			fileID = result.FileID
			task.FileID = result.FileID
			task.FileName = result.FileName
			task.FileSize = result.FileSize
		}
	}
}

func (m *DownloadManager) heartbeat(ctx context.Context, taskID, runToken string, done <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_, err := m.factory.DownloadTask().Heartbeat(context.Background(), taskID, runToken, time.Now().Add(downloadLeaseDuration))
			if err != nil {
				logger.LOG.Warn("更新下载任务心跳失败", "taskID", taskID, "error", err)
			}
		}
	}
}

func (m *DownloadManager) finishTask(task *models.DownloadTask, fileID string, runErr error) {
	ctx := context.Background()
	if runErr == nil && fileID == "" {
		runErr = fmt.Errorf("下载执行器未返回文件ID")
	}
	if runErr == nil {
		updated, err := m.factory.DownloadTask().UpdateIfRunToken(ctx, task.ID, task.RunToken, map[string]interface{}{
			"state":            enum.DownloadTaskStateFinished.Value(),
			"file_id":          fileID,
			"file_name":        task.FileName,
			"file_size":        task.FileSize,
			"progress":         100,
			"downloaded_size":  task.FileSize,
			"speed":            0,
			"finish_time":      custom_type.Now(),
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"next_retry_at":    nil,
			"error_msg":        "",
			"requires_headers": false,
			"reserved_size":    0,
		})
		if err != nil {
			logger.LOG.Error("提交下载完成状态失败", "taskID", task.ID, "error", err)
		}
		if !updated {
			// 文件已经成功入库，预留空间已在入库事务中结算，只清除任务标记。
			_ = m.factory.DB().Model(&models.DownloadTask{}).Where("id = ?", task.ID).Update("reserved_size", 0).Error
		}
		if isTorrentTask(task.Type) {
			m.cleanupTaskTemp(task)
		}
		m.deleteSecret(task.ID)
		return
	}

	if errors.Is(runErr, context.Canceled) {
		latest, getErr := m.factory.DownloadTask().GetByID(ctx, task.ID)
		if getErr == nil && latest.State == enum.DownloadTaskStateCanceled.Value() {
			m.cleanupTaskTemp(latest)
			m.releaseReservation(latest.ID)
		}
		return
	}
	if download.IsCredentialsRequired(runErr) {
		_, updateErr := m.factory.DownloadTask().UpdateIfRunToken(ctx, task.ID, task.RunToken, map[string]interface{}{
			"state":            enum.DownloadTaskStatePaused.Value(),
			"speed":            0,
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"next_retry_at":    nil,
			"requires_headers": true,
			"error_msg":        runErr.Error(),
		})
		if updateErr != nil {
			logger.LOG.Error("暂停凭据失效的HTTP/HLS任务失败", "taskID", task.ID, "error", updateErr)
		}
		m.deleteSecret(task.ID)
		return
	}
	if m.isRetryable(runErr) && task.RetryCount < m.config.MaxRetries {
		retryCount := task.RetryCount + 1
		backoff := []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}
		delay := backoff[len(backoff)-1]
		if retryCount <= len(backoff) {
			delay = backoff[retryCount-1]
		}
		_, _ = m.factory.DownloadTask().UpdateIfRunToken(ctx, task.ID, task.RunToken, map[string]interface{}{
			"state":            enum.DownloadTaskStateInit.Value(),
			"retry_count":      retryCount,
			"next_retry_at":    time.Now().Add(delay),
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"speed":            0,
			"error_msg":        fmt.Sprintf("第%d次重试等待中: %s", retryCount, download.RedactErrorForLog(runErr)),
		})
		return
	}
	_, err := m.factory.DownloadTask().UpdateIfRunToken(ctx, task.ID, task.RunToken, map[string]interface{}{
		"state":            enum.DownloadTaskStateFailed.Value(),
		"speed":            0,
		"run_token":        "",
		"worker_id":        "",
		"lease_expires_at": nil,
		"next_retry_at":    nil,
		"error_msg":        download.RedactErrorForLog(runErr),
	})
	if err != nil {
		logger.LOG.Error("提交下载失败状态失败", "taskID", task.ID, "error", err)
	}
	m.releaseReservation(task.ID)
	m.cleanupTaskTemp(task)
	m.deleteSecret(task.ID)
}

func (m *DownloadManager) isRetryable(err error) bool {
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") || strings.Contains(message, "超时") ||
		strings.Contains(message, "状态码: 408") || strings.Contains(message, "状态码: 429") ||
		strings.Contains(message, "状态码: 500") || strings.Contains(message, "状态码: 502") ||
		strings.Contains(message, "状态码: 503") || strings.Contains(message, "状态码: 504")
}

func (m *DownloadManager) Pause(taskID string) error {
	transitioned, err := m.factory.DownloadTask().Transition(context.Background(), taskID,
		[]int{enum.DownloadTaskStateInit.Value(), enum.DownloadTaskStateDownloading.Value()},
		enum.DownloadTaskStatePaused.Value(), map[string]interface{}{
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"next_retry_at":    nil,
			"speed":            0,
		})
	if err != nil {
		return err
	}
	if !transitioned {
		return fmt.Errorf("任务状态不允许暂停")
	}
	m.cancelActive(taskID)
	m.deleteSecret(taskID)
	return nil
}

func (m *DownloadManager) Resume(task *models.DownloadTask, filePassword string, taskUpdates map[string]interface{}) error {
	if task.EnableEncryption && filePassword == "" {
		return fmt.Errorf("加密任务恢复时必须输入文件密码")
	}
	if filePassword != "" {
		m.mu.Lock()
		m.secrets[task.ID] = filePassword
		m.mu.Unlock()
	}
	updates := map[string]interface{}{
		"error_msg":        "",
		"retry_count":      0,
		"next_retry_at":    nil,
		"run_token":        "",
		"worker_id":        "",
		"lease_expires_at": nil,
	}
	for key, value := range taskUpdates {
		updates[key] = value
	}
	transitioned, err := m.factory.DownloadTask().Transition(context.Background(), task.ID,
		[]int{enum.DownloadTaskStatePaused.Value()}, enum.DownloadTaskStateInit.Value(), updates)
	if err != nil || !transitioned {
		m.deleteSecret(task.ID)
		if err != nil {
			return err
		}
		return fmt.Errorf("任务状态不允许恢复")
	}
	m.Notify("", "")
	return nil
}

// Retry 将失败或已取消任务清零后重新排队。
func (m *DownloadManager) Retry(task *models.DownloadTask, filePassword string, taskUpdates map[string]interface{}) error {
	if task.EnableEncryption && filePassword == "" {
		return fmt.Errorf("加密任务重试时必须输入文件密码")
	}
	m.mu.Lock()
	if _, active := m.active[task.ID]; active {
		m.mu.Unlock()
		return fmt.Errorf("任务正在停止，请稍后重试")
	}
	latest, err := m.factory.DownloadTask().GetByID(context.Background(), task.ID)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if latest.State != enum.DownloadTaskStateFailed.Value() && latest.State != enum.DownloadTaskStateCanceled.Value() {
		m.mu.Unlock()
		return fmt.Errorf("任务状态不允许重试")
	}
	if filePassword != "" {
		m.secrets[task.ID] = filePassword
	}

	// 终态任务通常已经完成清理；这里再次幂等释放，兼容服务重启后的遗留状态。
	if err := m.releaseReservation(task.ID); err != nil {
		delete(m.secrets, task.ID)
		m.mu.Unlock()
		return fmt.Errorf("释放任务预留空间失败: %w", err)
	}
	m.cleanupTaskTemp(latest)
	updates := map[string]interface{}{
		"file_id":          "",
		"downloaded_size":  0,
		"progress":         0,
		"speed":            0,
		"path":             "",
		"error_msg":        "",
		"retry_count":      0,
		"next_retry_at":    nil,
		"run_token":        "",
		"worker_id":        "",
		"lease_expires_at": nil,
		"reserved_size":    0,
		"finish_time":      nil,
	}
	if isTorrentTask(task.Type) {
		// 重试单个种子文件时使用新批次，确保不复用旧批次的临时会话和数据。
		updates["batch_id"] = uuid.NewString()
	}
	for key, value := range taskUpdates {
		updates[key] = value
	}
	transitioned, err := m.factory.DownloadTask().Transition(context.Background(), task.ID,
		[]int{enum.DownloadTaskStateFailed.Value(), enum.DownloadTaskStateCanceled.Value()},
		enum.DownloadTaskStateInit.Value(), updates)
	if err != nil || !transitioned {
		delete(m.secrets, task.ID)
		m.mu.Unlock()
		if err != nil {
			return err
		}
		return fmt.Errorf("任务状态不允许重试")
	}
	m.mu.Unlock()
	m.Notify("", "")
	return nil
}

func (m *DownloadManager) Cancel(taskID string) error {
	task, getErr := m.factory.DownloadTask().GetByID(context.Background(), taskID)
	if getErr != nil {
		return getErr
	}
	transitioned, err := m.factory.DownloadTask().Transition(context.Background(), taskID,
		[]int{enum.DownloadTaskStateInit.Value(), enum.DownloadTaskStateDownloading.Value(), enum.DownloadTaskStatePaused.Value()},
		enum.DownloadTaskStateCanceled.Value(), map[string]interface{}{
			"error_msg":        "用户取消下载",
			"speed":            0,
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"next_retry_at":    nil,
		})
	if err != nil {
		return err
	}
	if !transitioned {
		return fmt.Errorf("任务状态不允许取消")
	}
	m.cancelActive(taskID)
	if task.State != enum.DownloadTaskStateDownloading.Value() {
		m.releaseReservation(taskID)
		m.cleanupTaskTemp(task)
	}
	m.deleteSecret(taskID)
	return nil
}

func (m *DownloadManager) cancelActive(taskID string) {
	m.mu.Lock()
	cancel := m.active[taskID]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *DownloadManager) releaseActive(task *models.DownloadTask) {
	m.mu.Lock()
	delete(m.active, task.ID)
	if task.BatchID != "" {
		delete(m.activeBatches, task.BatchID)
	}
	if m.activeUsers[task.UserID] > 1 {
		m.activeUsers[task.UserID]--
	} else {
		delete(m.activeUsers, task.UserID)
	}
	m.mu.Unlock()
}

func (m *DownloadManager) deleteSecret(taskID string) {
	m.mu.Lock()
	delete(m.secrets, taskID)
	m.mu.Unlock()
}

func (m *DownloadManager) cleanupTaskTemp(task *models.DownloadTask) {
	var path string
	if task.Type == enum.DownloadTaskTypeHttp.Value() {
		path = fmt.Sprintf("%s/http_%s", m.tempDir, task.ID)
	} else if task.Type == enum.DownloadTaskTypeHLS.Value() {
		path = fmt.Sprintf("%s/hls_%s", m.tempDir, task.ID)
	} else if task.Type == enum.DownloadTaskTypeBtp.Value() || task.Type == enum.DownloadTaskTypeMagnet.Value() {
		if task.BatchID != "" {
			var activeCount int64
			if err := m.factory.DB().Model(&models.DownloadTask{}).
				Where("batch_id = ? AND state IN ?", task.BatchID, []int{
					enum.DownloadTaskStateInit.Value(),
					enum.DownloadTaskStateDownloading.Value(),
					enum.DownloadTaskStatePaused.Value(),
				}).Count(&activeCount).Error; err != nil || activeCount > 0 {
				return
			}
		}
		if task.BatchID != "" {
			download.CloseTorrentSession(task.BatchID)
		}
		sessionID := task.BatchID
		if sessionID == "" {
			sessionID = task.ID
		}
		path = fmt.Sprintf("%s/torrent_%s", m.tempDir, sessionID)
	}
	if path != "" {
		if err := os.RemoveAll(path); err != nil {
			logger.LOG.Warn("清理下载临时目录失败", "taskID", task.ID, "path", path, "error", err)
		}
	}
}

func isTorrentTask(taskType int) bool {
	return taskType == enum.DownloadTaskTypeBtp.Value() || taskType == enum.DownloadTaskTypeMagnet.Value()
}

func (m *DownloadManager) ensureReservation(task *models.DownloadTask, requiredSize int64) (int64, error) {
	if requiredSize <= 0 || task.ReservedSize >= requiredSize {
		return task.ReservedSize, nil
	}
	user, err := m.factory.User().GetByID(context.Background(), task.UserID)
	if err != nil {
		return task.ReservedSize, fmt.Errorf("查询用户空间失败: %w", err)
	}
	if user.Space <= 0 {
		return 0, nil
	}
	additional := requiredSize - task.ReservedSize
	err = m.factory.DB().Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.UserInfo{}).
			Where("id = ? AND free_space >= ?", task.UserID, additional).
			UpdateColumn("free_space", gorm.Expr("free_space - ?", additional))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("用户可用空间不足")
		}
		result = tx.Model(&models.DownloadTask{}).
			Where("id = ? AND run_token = ? AND state = ?", task.ID, task.RunToken, enum.DownloadTaskStateDownloading.Value()).
			Update("reserved_size", requiredSize)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return context.Canceled
		}
		return nil
	})
	if err != nil {
		return task.ReservedSize, err
	}
	return requiredSize, nil
}

func (m *DownloadManager) releaseReservation(taskID string) error {
	err := m.factory.DB().Transaction(func(tx *gorm.DB) error {
		var task models.DownloadTask
		if err := tx.Where("id = ?", taskID).First(&task).Error; err != nil {
			return err
		}
		if task.ReservedSize <= 0 {
			return nil
		}
		result := tx.Model(&models.DownloadTask{}).
			Where("id = ? AND reserved_size = ?", task.ID, task.ReservedSize).
			Update("reserved_size", 0)
		if result.Error != nil || result.RowsAffected != 1 {
			return result.Error
		}
		var user models.UserInfo
		if err := tx.Where("id = ?", task.UserID).First(&user).Error; err != nil {
			return err
		}
		if user.Space > 0 {
			return tx.Model(&models.UserInfo{}).Where("id = ?", task.UserID).
				UpdateColumn("free_space", gorm.Expr("free_space + ?", task.ReservedSize)).Error
		}
		return nil
	})
	if err != nil {
		logger.LOG.Warn("释放下载任务预留空间失败", "taskID", taskID, "error", err)
	}
	return err
}

func (m *DownloadManager) recoverInterruptedTasks() error {
	db := m.factory.DB().WithContext(context.Background())
	types := []int{enum.DownloadTaskTypeHttp.Value(), enum.DownloadTaskTypeBtp.Value(), enum.DownloadTaskTypeMagnet.Value(), enum.DownloadTaskTypeHLS.Value()}
	if err := db.Model(&models.DownloadTask{}).Where("type IN ? AND state IN ? AND enable_encryption = ?", types,
		[]int{enum.DownloadTaskStateInit.Value(), enum.DownloadTaskStateDownloading.Value()}, true).
		Updates(map[string]interface{}{
			"state":            enum.DownloadTaskStatePaused.Value(),
			"error_msg":        "服务已重启，请输入文件密码后恢复",
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"speed":            0,
		}).Error; err != nil {
		return err
	}
	return db.Model(&models.DownloadTask{}).Where("type IN ? AND state = ? AND enable_encryption = ?", types, enum.DownloadTaskStateDownloading.Value(), false).
		Updates(map[string]interface{}{
			"state":            enum.DownloadTaskStateInit.Value(),
			"run_token":        "",
			"worker_id":        "",
			"lease_expires_at": nil,
			"speed":            0,
			"error_msg":        "服务重启后自动恢复",
		}).Error
}
