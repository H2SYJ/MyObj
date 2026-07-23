package service

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/config"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/virtualpath"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DownloadService 下载服务
type DownloadService struct {
	factory       *impl.RepositoryFactory
	tempDir       string // 临时目录
	manager       *DownloadManager
	networkPolicy *download.NetworkPolicy
	taskEvents    *TaskEventHub
}

func (d *DownloadService) SetTaskEventHub(events *TaskEventHub) {
	d.taskEvents = events
	d.manager.SetTaskEventHub(events)
}

func (d *DownloadService) publishTask(task *models.DownloadTask, action string, coalesce bool) {
	if d.taskEvents != nil && task != nil {
		d.taskEvents.Publish(downloadTaskEvent(task, action), coalesce)
	}
}

func (d *DownloadService) publishTaskByID(taskID, action string, coalesce bool) {
	if d.taskEvents == nil || taskID == "" {
		return
	}
	if task, err := d.factory.DownloadTask().GetByID(context.Background(), taskID); err == nil {
		d.publishTask(task, action, coalesce)
	}
}

func NewDownloadService(factory *impl.RepositoryFactory, policies ...*download.NetworkPolicy) *DownloadService {
	networkPolicy := download.NewNetworkPolicy()
	if len(policies) > 0 && policies[0] != nil {
		networkPolicy = policies[0]
	} else {
		networkPolicy = initializeDownloadNetworkPolicy(factory)
	}
	// 选择最大磁盘创建临时目录
	tempDir := "./obj_temp/downloads" // 默认值

	ctx := context.Background()
	disk, err := factory.Disk().GetBigDisk(ctx)
	if err == nil && disk != nil {
		// 在最大磁盘的data_path下创建 temp 目录
		tempDir = filepath.Join(disk.DataPath, "temp", "downloads")
		logger.LOG.Info("使用最大磁盘创建临时目录", "disk", disk.DiskPath, "tempDir", tempDir)
	} else {
		logger.LOG.Warn("获取最大磁盘失败，使用默认临时目录", "error", err)
	}

	// 确保临时目录存在
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		logger.LOG.Error("创建临时目录失败", "tempDir", tempDir, "error", err)
	} else {
		logger.LOG.Info("临时目录初始化成功", "tempDir", tempDir)
	}

	service := &DownloadService{
		factory:       factory,
		tempDir:       tempDir,
		networkPolicy: networkPolicy,
	}
	service.manager = NewDownloadManager(factory, tempDir, networkPolicy)
	service.manager.Start()
	return service
}

func (d *DownloadService) GetRepository() *impl.RepositoryFactory {
	return d.factory
}

type SubscriptionDownloadInput struct {
	ItemID                  string
	UserID                  string
	URL                     string
	DownloadType            string
	FileName                string
	SavePath                string
	RequestHeadersEncrypted string
	HeaderHostsJSON         string
}

// EnqueueSubscriptionDownload 将订阅条目原子关联到普通离线下载任务。
func (d *DownloadService) EnqueueSubscriptionDownload(ctx context.Context, input SubscriptionDownloadInput) (string, error) {
	task, err := d.buildSubscriptionDownload(input)
	if err != nil {
		return "", err
	}
	err = d.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		result := tx.Model(&models.SubscriptionItem{}).Where("id = ? AND download_task_id = '' AND status IN ?", input.ItemID, []string{"deferred", "submit_failed"}).
			Updates(map[string]interface{}{"download_task_id": task.ID, "status": "submitted", "error_msg": "", "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("订阅条目已提交或状态不允许提交")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	d.publishTask(task, "created", false)
	d.manager.Notify(task.ID, "")
	return task.ID, nil
}

// CreateSubscriptionItemAndDownload 在同一事务内创建订阅条目和普通离线下载任务。
func (d *DownloadService) CreateSubscriptionItemAndDownload(ctx context.Context, item *models.SubscriptionItem, input SubscriptionDownloadInput) (string, error) {
	task, err := d.buildSubscriptionDownload(input)
	if err != nil {
		return "", err
	}
	item.DownloadTaskID = task.ID
	item.Status = "submitted"
	err = d.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		return tx.Create(task).Error
	})
	if err != nil {
		return "", err
	}
	d.publishTask(task, "created", false)
	d.manager.Notify(task.ID, "")
	return task.ID, nil
}

func (d *DownloadService) buildSubscriptionDownload(input SubscriptionDownloadInput) (*models.DownloadTask, error) {
	if err := download.ValidatePublicHTTPURL(input.URL); err != nil {
		return nil, err
	}
	taskType := enum.DownloadTaskTypeHttp.Value()
	fileName := input.FileName
	if input.DownloadType == "hls" {
		taskType = enum.DownloadTaskTypeHLS.Value()
		var err error
		fileName, err = download.NormalizeHLSOutputFileName(fileName, input.URL, input.ItemID)
		if err != nil {
			return nil, err
		}
	} else if input.DownloadType != "http" {
		return nil, fmt.Errorf("插件下载类型仅支持http或hls")
	} else if fileName != "" {
		var err error
		fileName, err = download.NormalizeHTTPOutputFileName(fileName)
		if err != nil {
			return nil, err
		}
	}
	headers, err := download.DecryptRequestHeaders(config.CONFIG.Auth.Secret, input.ItemID, input.UserID, input.RequestHeadersEncrypted)
	if err != nil {
		return nil, err
	}
	taskID := uuid.Must(uuid.NewV7()).String()
	taskHeaders, err := download.EncryptRequestHeaders(config.CONFIG.Auth.Secret, taskID, input.UserID, headers)
	if err != nil {
		return nil, err
	}
	now := custom_type.Now()
	savePath, err := virtualpath.NormalizeAbsolutePath(input.SavePath)
	if err != nil {
		return nil, err
	}
	return &models.DownloadTask{ID: taskID, UserID: input.UserID, Type: taskType, FileName: fileName, URL: input.URL,
		SavePath: savePath, State: enum.DownloadTaskStateInit.Value(), TargetDir: d.tempDir, SupportRange: taskType == enum.DownloadTaskTypeHLS.Value(),
		RequestHeadersEncrypted: taskHeaders, HeaderHostsJSON: input.HeaderHostsJSON, CreateTime: now, UpdateTime: now}, nil
}

func (d *DownloadService) RefreshTaskHeaders(ctx context.Context, taskID, itemID, userID, encrypted, hostsJSON string) error {
	task, err := d.factory.DownloadTask().GetByID(ctx, taskID)
	if err != nil || task.UserID != userID {
		return fmt.Errorf("下载任务不存在")
	}
	if task.State == enum.DownloadTaskStateFinished.Value() || task.State == enum.DownloadTaskStateCanceled.Value() {
		return nil
	}
	headers, err := download.DecryptRequestHeaders(config.CONFIG.Auth.Secret, itemID, userID, encrypted)
	if err != nil {
		return err
	}
	taskEncrypted, err := download.EncryptRequestHeaders(config.CONFIG.Auth.Secret, task.ID, userID, headers)
	if err != nil {
		return err
	}
	updates := map[string]interface{}{"request_headers_encrypted": taskEncrypted, "header_hosts_json": hostsJSON, "requires_headers": false}
	if task.State == enum.DownloadTaskStateDownloading.Value() {
		if err := d.manager.Pause(task.ID); err != nil {
			return err
		}
		task.State = enum.DownloadTaskStatePaused.Value()
		// 活动任务必须撤销旧run_token并用新凭据重新排队，断点清单继续复用。
		return d.manager.Resume(task, "", updates)
	}
	if task.State == enum.DownloadTaskStatePaused.Value() {
		if task.RequiresHeaders {
			return d.manager.Resume(task, "", updates)
		}
		// 用户主动暂停的任务只更新凭据，不能被插件刷新意外恢复。
		return d.factory.DB().WithContext(ctx).Model(&models.DownloadTask{}).
			Where("id = ? AND user_id = ? AND state = ?", task.ID, userID, enum.DownloadTaskStatePaused.Value()).
			Updates(updates).Error
	}
	// 排队任务直接使用新凭据；普通网络失败任务只更新凭据但保持失败，避免重复下载。
	if task.State == enum.DownloadTaskStateInit.Value() || task.State == enum.DownloadTaskStateFailed.Value() {
		return d.factory.DB().WithContext(ctx).Model(&models.DownloadTask{}).
			Where("id = ? AND user_id = ? AND state = ?", task.ID, userID, task.State).
			Updates(updates).Error
	}
	return nil
}

// CreateOfflineDownload 创建离线下载任务
func (d *DownloadService) CreateOfflineDownload(req *request.CreateOfflineDownloadRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	if err := download.ValidatePublicHTTPURL(req.URL); err != nil {
		return nil, err
	}
	downloadType := strings.ToLower(strings.TrimSpace(req.DownloadType))
	if downloadType == "" {
		downloadType = "auto"
	}
	if downloadType != "auto" && downloadType != "http" && downloadType != "hls" {
		return nil, fmt.Errorf("download_type仅支持auto、http或hls")
	}

	// 1. 验证用户是否存在并获取用户信息
	user, err := d.factory.User().GetByID(ctx, userID)
	if err != nil {
		logger.LOG.Error("获取用户信息失败", "error", err, "userID", userID)
		return nil, fmt.Errorf("用户不存在")
	}

	// 2. 设置默认虚拟路径
	savePath := req.SavePath
	if savePath == "" {
		savePath = "/离线下载"
	}
	savePath, err = virtualpath.NormalizeAbsolutePath(savePath)
	if err != nil {
		return nil, err
	}

	if req.EnableEncryption {
		if req.FilePassword == "" {
			return nil, fmt.Errorf("加密存储密码不能为空")
		}
	}

	taskID := uuid.Must(uuid.NewV7()).String()
	rawHeaders := map[string]string{}
	if req.RequestHeaders != nil {
		rawHeaders = *req.RequestHeaders
	}
	extraHosts := []string{}
	if req.HeaderHosts != nil {
		extraHosts = *req.HeaderHosts
	}
	if len(extraHosts) > 0 && len(rawHeaders) == 0 {
		return nil, fmt.Errorf("未配置请求头时不能单独配置请求头主机")
	}
	headers, headerHosts, err := download.NormalizeRequestConfig(req.URL, rawHeaders, extraHosts)
	if err != nil {
		return nil, err
	}

	isHLS := downloadType == "hls"
	if downloadType == "auto" {
		isHLS = download.LooksLikeHLSURL(req.URL)
		if !isHLS {
			detected, detectErr := download.DetectHLSContentType(ctx, req.URL, d.networkPolicy.ProxyURL(),
				d.networkPolicy.DownloadLimiter(), headers, headerHosts)
			if detectErr != nil {
				logger.LOG.Warn("自动探测HLS类型失败，按普通HTTP任务处理", "url", download.RedactURLForLog(req.URL), "error", detectErr)
			} else {
				isHLS = detected
			}
		}
	}
	if !isHLS && req.FileName != "" {
		return nil, fmt.Errorf("file_name仅支持HLS任务，请将download_type设为hls")
	}

	var fileInfo *download.FileInfoResult
	supportRange := false
	if !isHLS {
		fileInfo, supportRange, err = download.GetFileInfoWithRequestConfig(req.URL,
			d.networkPolicy.ProxyURL(), d.networkPolicy.DownloadLimiter(), headers, headerHosts)
		if err != nil {
			// 无法获取文件大小时，仍然允许创建任务（可能是动态内容）
			logger.LOG.Warn("无法获取文件信息，跳过空间检查", "url", download.RedactURLForLog(req.URL), "error", err)
		}
	} else {
		supportRange = true
		if probeErr := download.ProbeHLSPlaylist(ctx, req.URL, d.networkPolicy.ProxyURL(),
			d.networkPolicy.DownloadLimiter(), headers, headerHosts); probeErr != nil {
			// 网络临时错误交由持久任务重试；确定性格式错误也会在执行器中给出完整错误。
			logger.LOG.Warn("创建HLS任务时预检播放列表失败", "url", download.RedactURLForLog(req.URL), "error", probeErr)
		}
	}
	if fileInfo != nil && fileInfo.Size > 0 {
		// 检查用户可用空间（只对非无限空间用户）
		if user.Space > 0 && user.FreeSpace < fileInfo.Size {
			return models.NewJsonResponse(400, "用户可用空间不足", map[string]interface{}{
				"required_size": fileInfo.Size,
				"free_space":    user.FreeSpace,
			}), nil
		}
		logger.LOG.Info("空间检查通过",
			"file_size", fileInfo.Size,
			"free_space", user.FreeSpace,
			"user_id", userID)
	}

	requestHeadersEncrypted := ""
	headerHostsJSON := ""
	if len(headers) > 0 {
		secret := ""
		if config.CONFIG != nil {
			secret = config.CONFIG.Auth.Secret
		}
		requestHeadersEncrypted, err = download.EncryptRequestHeaders(secret, taskID, userID, headers)
		if err != nil {
			return nil, err
		}
		headerHostsJSON, err = download.EncodeHeaderHosts(headerHosts)
		if err != nil {
			return nil, err
		}
	}

	// 4. 创建下载任务记录
	taskType := enum.DownloadTaskTypeHttp.Value()
	fileName := ""
	if isHLS {
		taskType = enum.DownloadTaskTypeHLS.Value()
		fileName, err = download.NormalizeHLSOutputFileName(req.FileName, req.URL, taskID)
		if err != nil {
			return nil, err
		}
	}
	task := &models.DownloadTask{
		ID:                      taskID,
		UserID:                  userID,
		Type:                    taskType,
		FileName:                fileName,
		URL:                     req.URL,
		SavePath:                savePath,
		EnableEncryption:        req.EnableEncryption,
		State:                   enum.DownloadTaskStateInit.Value(),
		TargetDir:               d.tempDir,
		SupportRange:            supportRange,
		RequestHeadersEncrypted: requestHeadersEncrypted,
		HeaderHostsJSON:         headerHostsJSON,
		CreateTime:              custom_type.Now(),
		UpdateTime:              custom_type.Now(),
	}
	if fileInfo != nil {
		task.FileName = fileInfo.FileName
		task.FileSize = fileInfo.FileSize
		task.SupportRange = supportRange
	}

	if err := d.factory.DownloadTask().Create(ctx, task); err != nil {
		logger.LOG.Error("创建下载任务失败", "error", err, "userID", userID, "url", download.RedactURLForLog(req.URL))
		return nil, fmt.Errorf("创建任务失败: %w", err)
	}
	d.publishTask(task, "created", false)

	// 5. 通知下载管理器排队执行，密码仅保存在内存中。
	d.manager.Notify(taskID, req.FilePassword)

	logger.LOG.Info("离线下载任务已创建", "taskID", taskID, "userID", userID, "url", download.RedactURLForLog(req.URL))

	// 返回任务信息
	taskResp := d.convertTaskToResponse(task)
	return models.NewJsonResponse(200, "任务创建成功", taskResp), nil
}

// GetTaskList 获取下载任务列表
func (d *DownloadService) GetTaskList(req *request.DownloadTaskListRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	offset := (req.Page - 1) * req.PageSize

	var tasks []*models.DownloadTask
	var total int64
	var err error

	if req.Types != "" {
		if req.Type >= 0 {
			return nil, fmt.Errorf("type和types不能同时传递")
		}
		parts := strings.Split(req.Types, ",")
		types := make([]int, 0, len(parts))
		for _, part := range parts {
			value, parseErr := strconv.Atoi(strings.TrimSpace(part))
			if parseErr != nil || value < 0 || value > enum.DownloadTaskTypeHLS.Value() {
				return nil, fmt.Errorf("无效的任务类型: %s", part)
			}
			types = append(types, value)
		}
		var state *int
		if req.State >= 0 {
			state = &req.State
		}
		tasks, err = d.factory.DownloadTask().ListByFilters(ctx, userID, state, types, offset, req.PageSize)
		if err == nil {
			total, err = d.factory.DownloadTask().CountByFilters(ctx, userID, state, types)
		}
		if err != nil {
			return nil, fmt.Errorf("查询任务失败: %w", err)
		}
	}

	if req.Types == "" {
		// 判断是否指定了类型过滤
		hasTypeFilter := req.Type >= 0
		hasStateFilter := req.State >= 0

		if hasStateFilter && hasTypeFilter {
			// 按状态和类型查询
			tasks, err = d.factory.DownloadTask().ListByStateAndType(ctx, userID, req.State, req.Type, offset, req.PageSize)
			if err != nil {
				logger.LOG.Error("查询下载任务失败", "error", err, "userID", userID, "state", req.State, "type", req.Type)
				return nil, fmt.Errorf("查询任务失败: %w", err)
			}
			total, err = d.factory.DownloadTask().CountByStateAndType(ctx, userID, req.State, req.Type)
		} else if hasStateFilter {
			// 只按状态查询
			tasks, err = d.factory.DownloadTask().ListByState(ctx, userID, req.State, offset, req.PageSize)
			if err != nil {
				logger.LOG.Error("查询下载任务失败", "error", err, "userID", userID, "state", req.State)
				return nil, fmt.Errorf("查询任务失败: %w", err)
			}
			total, err = d.factory.DownloadTask().CountByState(ctx, userID, req.State)
		} else if hasTypeFilter {
			// 只按类型查询
			tasks, err = d.factory.DownloadTask().ListByType(ctx, userID, req.Type, offset, req.PageSize)
			if err != nil {
				logger.LOG.Error("查询下载任务失败", "error", err, "userID", userID, "type", req.Type)
				return nil, fmt.Errorf("查询任务失败: %w", err)
			}
			total, err = d.factory.DownloadTask().CountByType(ctx, userID, req.Type)
		} else {
			// 查询所有任务
			tasks, err = d.factory.DownloadTask().ListByUserID(ctx, userID, offset, req.PageSize)
			if err != nil {
				logger.LOG.Error("查询下载任务失败", "error", err, "userID", userID)
				return nil, fmt.Errorf("查询任务失败: %w", err)
			}
			total, err = d.factory.DownloadTask().Count(ctx, userID)
		}

		if err != nil {
			logger.LOG.Error("统计下载任务失败", "error", err, "userID", userID)
			return nil, fmt.Errorf("统计任务失败: %w", err)
		}
	}

	// 转换为响应格式
	taskResponses := make([]*response.DownloadTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		t := d.convertTaskToResponse(task)

		// 只有已完成的任务才有 FileID，需要查询 user_files 获取 uf_id
		if task.State == enum.DownloadTaskStateFinished.Value() && task.FileID != "" {
			userFile, err := d.factory.UserFiles().GetByUserIDAndFileID(ctx, userID, task.FileID)
			if err != nil {
				logger.LOG.Warn("获取用户文件信息失败", "error", err, "fileID", task.FileID, "userID", userID)
				// 不阻断整个列表，继续处理下一个任务
				t.FileID = task.FileID // 使用原始 FileID
			} else {
				t.FileID = userFile.UfID // 返回 uf_id
			}
		} else {
			// 未完成的任务，返回空字符串
			t.FileID = ""
		}

		taskResponses = append(taskResponses, t)
	}

	result := &response.DownloadTaskListResponse{
		Tasks:    taskResponses,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	return models.NewJsonResponse(200, "查询成功", result), nil
}

// GetLocalDownloadTask 查询当前用户的单个网盘文件下载任务。
func (d *DownloadService) GetLocalDownloadTask(taskID, userID string) (*models.JsonResponse, error) {
	task, err := d.factory.DownloadTask().GetByID(context.Background(), taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "下载任务不存在", nil), nil
		}
		return nil, err
	}
	if task.UserID != userID || task.Type != enum.DownloadTaskTypeLocalFile.Value() {
		return models.NewJsonResponse(404, "下载任务不存在", nil), nil
	}
	return models.NewJsonResponse(200, "查询成功", d.convertTaskToResponse(task)), nil
}

// PauseTask 暂停下载任务
func (d *DownloadService) PauseTask(req *request.TaskOperationRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 验证任务是否属于该用户
	task, err := d.factory.DownloadTask().GetByID(ctx, req.TaskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "error", err, "taskID", req.TaskID)
		return nil, fmt.Errorf("任务不存在")
	}

	if task.UserID != userID {
		logger.LOG.Warn("用户尝试操作他人任务", "userID", userID, "taskID", req.TaskID, "taskOwner", task.UserID)
		return nil, fmt.Errorf("无权操作此任务")
	}

	if !isManagedOfflineType(task.Type) {
		return nil, fmt.Errorf("该任务类型不支持暂停")
	}
	if err := d.manager.Pause(req.TaskID); err != nil {
		logger.LOG.Error("暂停下载任务失败", "error", err, "taskID", req.TaskID)
		return nil, fmt.Errorf("暂停任务失败: %w", err)
	}
	d.publishTaskByID(req.TaskID, "updated", false)

	logger.LOG.Info("下载任务已暂停", "taskID", req.TaskID, "userID", userID)
	return models.NewJsonResponse(200, "任务已暂停", nil), nil
}

// ResumeTask 恢复下载任务
func (d *DownloadService) ResumeTask(req *request.TaskOperationRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 验证任务是否属于该用户
	task, err := d.factory.DownloadTask().GetByID(ctx, req.TaskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "error", err, "taskID", req.TaskID)
		return nil, fmt.Errorf("任务不存在")
	}

	if task.UserID != userID {
		logger.LOG.Warn("用户尝试操作他人任务", "userID", userID, "taskID", req.TaskID, "taskOwner", task.UserID)
		return nil, fmt.Errorf("无权操作此任务")
	}

	if !isManagedOfflineType(task.Type) {
		return nil, fmt.Errorf("该任务类型不支持恢复")
	}
	resumeUpdates, err := d.buildTaskRequestUpdates(task, req)
	if err != nil {
		return nil, err
	}
	if err := d.manager.Resume(task, req.FilePassword, resumeUpdates); err != nil {
		logger.LOG.Error("恢复下载任务失败", "error", err, "taskID", req.TaskID)
		return nil, fmt.Errorf("恢复任务失败: %w", err)
	}
	d.publishTaskByID(req.TaskID, "updated", false)

	logger.LOG.Info("下载任务已恢复", "taskID", req.TaskID, "userID", userID)
	return models.NewJsonResponse(200, "任务已恢复", nil), nil
}

// RetryTask 重试失败或已取消的下载任务。
func (d *DownloadService) RetryTask(req *request.TaskOperationRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	task, err := d.factory.DownloadTask().GetByID(ctx, req.TaskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "error", err, "taskID", req.TaskID)
		return nil, fmt.Errorf("任务不存在")
	}
	if task.UserID != userID {
		logger.LOG.Warn("用户尝试操作他人任务", "userID", userID, "taskID", req.TaskID, "taskOwner", task.UserID)
		return nil, fmt.Errorf("无权操作此任务")
	}
	if !isManagedOfflineType(task.Type) {
		return nil, fmt.Errorf("该任务类型不支持重试")
	}
	if task.State != enum.DownloadTaskStateFailed.Value() && task.State != enum.DownloadTaskStateCanceled.Value() {
		return nil, fmt.Errorf("只有失败或已取消任务可以重试")
	}
	retryUpdates, err := d.buildTaskRequestUpdates(task, req)
	if err != nil {
		return nil, err
	}
	if err := d.manager.Retry(task, req.FilePassword, retryUpdates); err != nil {
		logger.LOG.Error("重试下载任务失败", "error", err, "taskID", req.TaskID)
		return nil, fmt.Errorf("重试任务失败: %w", err)
	}
	d.publishTaskByID(req.TaskID, "updated", false)

	logger.LOG.Info("下载任务已重新排队", "taskID", req.TaskID, "userID", userID)
	return models.NewJsonResponse(200, "任务已重新排队", nil), nil
}

func (d *DownloadService) buildTaskRequestUpdates(task *models.DownloadTask, req *request.TaskOperationRequest) (map[string]interface{}, error) {
	updates := map[string]interface{}{}
	if task.Type == enum.DownloadTaskTypeHLS.Value() || task.Type == enum.DownloadTaskTypeHttp.Value() {
		if task.RequiresHeaders && req.RequestHeaders == nil {
			return nil, fmt.Errorf("该任务需要更新请求头后才能继续")
		}
		if req.RequestHeaders != nil || req.HeaderHosts != nil {
			secret := ""
			var err error
			if config.CONFIG != nil {
				secret = config.CONFIG.Auth.Secret
			}
			headers := map[string]string{}
			if req.RequestHeaders != nil {
				headers = *req.RequestHeaders
			} else {
				headers, err = download.DecryptRequestHeaders(secret, task.ID, task.UserID, task.RequestHeadersEncrypted)
				if err != nil {
					return nil, fmt.Errorf("读取原请求头失败，请重新提交请求头: %w", err)
				}
			}
			hosts := []string{}
			if req.HeaderHosts != nil {
				hosts = *req.HeaderHosts
			} else {
				hosts, err = download.DecodeHeaderHosts(task.HeaderHostsJSON)
				if err != nil {
					return nil, err
				}
			}
			headers, hosts, err = download.NormalizeRequestConfig(task.URL, headers, hosts)
			if err != nil {
				return nil, err
			}
			encrypted, encryptErr := download.EncryptRequestHeaders(secret, task.ID, task.UserID, headers)
			if encryptErr != nil {
				return nil, encryptErr
			}
			hostsJSON, encodeErr := download.EncodeHeaderHosts(hosts)
			if encodeErr != nil {
				return nil, encodeErr
			}
			updates["request_headers_encrypted"] = encrypted
			updates["header_hosts_json"] = hostsJSON
			updates["requires_headers"] = false
		}
	}
	return updates, nil
}

// CancelTask 取消下载任务
func (d *DownloadService) CancelTask(req *request.TaskOperationRequest, userID string) (*models.JsonResponse, error) {
	if err := d.cancelTask(req.TaskID, userID); err != nil {
		return nil, err
	}

	logger.LOG.Info("下载任务已取消", "taskID", req.TaskID, "userID", userID)
	return models.NewJsonResponse(200, "任务已取消", nil), nil
}

func (d *DownloadService) cancelTask(taskID string, userID string) error {
	ctx := context.Background()

	// 验证任务是否属于该用户
	task, err := d.factory.DownloadTask().GetByID(ctx, taskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "error", err, "taskID", taskID)
		return fmt.Errorf("任务不存在")
	}

	if task.UserID != userID {
		logger.LOG.Warn("用户尝试操作他人任务", "userID", userID, "taskID", taskID, "taskOwner", task.UserID)
		return fmt.Errorf("无权操作此任务")
	}

	if !isManagedOfflineType(task.Type) {
		return fmt.Errorf("该任务类型不支持取消")
	}
	if err := d.manager.Cancel(taskID); err != nil {
		logger.LOG.Error("取消下载任务失败", "error", err, "taskID", taskID)
		return fmt.Errorf("取消任务失败: %w", err)
	}
	d.publishTaskByID(taskID, "updated", false)
	return nil
}

// BatchCancelTasks 批量取消下载任务，单个任务失败不影响其他任务。
func (d *DownloadService) BatchCancelTasks(req *request.BatchTaskOperationRequest, userID string) *models.JsonResponse {
	return d.batchTaskOperation(req.TaskIDs, "取消", func(taskID string) error {
		return d.cancelTask(taskID, userID)
	})
}

// DeleteTask 删除下载任务
func (d *DownloadService) DeleteTask(req *request.DeleteTaskRequest, userID string) (*models.JsonResponse, error) {
	if err := d.deleteTask(req.TaskID, userID); err != nil {
		return nil, err
	}

	logger.LOG.Info("下载任务已删除", "taskID", req.TaskID, "userID", userID)
	return models.NewJsonResponse(200, "任务已删除", nil), nil
}

func (d *DownloadService) deleteTask(taskID string, userID string) error {
	ctx := context.Background()

	// 验证任务是否属于该用户
	task, err := d.factory.DownloadTask().GetByID(ctx, taskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "error", err, "taskID", taskID)
		return fmt.Errorf("任务不存在")
	}

	if task.UserID != userID {
		logger.LOG.Warn("用户尝试删除他人任务", "userID", userID, "taskID", taskID, "taskOwner", task.UserID)
		return fmt.Errorf("无权删除此任务")
	}

	// 只能删除终态任务
	if task.State != enum.DownloadTaskStateFinished.Value() && task.State != enum.DownloadTaskStateFailed.Value() && task.State != enum.DownloadTaskStateCanceled.Value() {
		return fmt.Errorf("只能删除已完成、失败或取消的任务")
	}

	// 删除任务前，先清理临时文件（如果存在）
	if task.Path != "" && download.IsTempPath(task.Path) {
		logger.LOG.Info("删除任务时清理临时文件", "taskID", taskID, "path", task.Path)
		if err := os.RemoveAll(task.Path); err != nil {
			logger.LOG.Warn("清理临时文件失败", "error", err, "path", task.Path)
			// 清理失败不影响删除任务
		}
	}

	// 如果是种子下载任务，清理对应的临时目录
	if task.Type == enum.DownloadTaskTypeBtp.Value() || task.Type == enum.DownloadTaskTypeMagnet.Value() {
		torrentTempDir := filepath.Join(d.tempDir, fmt.Sprintf("torrent_%s", taskID))
		if _, err := os.Stat(torrentTempDir); err == nil {
			logger.LOG.Info("删除种子下载临时目录", "taskID", taskID, "path", torrentTempDir)
			if err := os.RemoveAll(torrentTempDir); err != nil {
				logger.LOG.Warn("清理种子临时目录失败", "error", err, "path", torrentTempDir)
			}
		}
	}

	if err := d.factory.DownloadTask().Delete(ctx, taskID); err != nil {
		logger.LOG.Error("删除下载任务失败", "error", err, "taskID", taskID)
		return fmt.Errorf("删除任务失败: %w", err)
	}
	d.publishTask(task, "deleted", false)
	return nil
}

// BatchDeleteTasks 批量删除下载任务，单个任务失败不影响其他任务。
func (d *DownloadService) BatchDeleteTasks(req *request.BatchTaskOperationRequest, userID string) *models.JsonResponse {
	return d.batchTaskOperation(req.TaskIDs, "删除", func(taskID string) error {
		return d.deleteTask(taskID, userID)
	})
}

func (d *DownloadService) batchTaskOperation(taskIDs []string, operation string, operate func(string) error) *models.JsonResponse {
	result := &response.BatchTaskOperationResponse{
		TotalCount:  len(taskIDs),
		FailedItems: make([]response.BatchTaskOperationFailedItem, 0),
	}
	for _, taskID := range taskIDs {
		if err := operate(taskID); err != nil {
			result.FailedItems = append(result.FailedItems, response.BatchTaskOperationFailedItem{
				TaskID: taskID,
				Reason: err.Error(),
			})
			continue
		}
		result.SuccessCount++
	}
	result.FailedCount = len(result.FailedItems)

	message := fmt.Sprintf("成功%s%d个任务", operation, result.SuccessCount)
	if result.FailedCount > 0 {
		message += fmt.Sprintf("，%d个任务失败", result.FailedCount)
	}
	logger.LOG.Info("批量下载任务操作完成", "operation", operation, "total", result.TotalCount,
		"success", result.SuccessCount, "failed", result.FailedCount)
	return models.NewJsonResponse(200, message, result)
}

// convertTaskToResponse 转换任务模型为响应格式
func (d *DownloadService) convertTaskToResponse(task *models.DownloadTask) *response.DownloadTaskResponse {
	stateText := d.getStateText(task.State)
	typeText := d.getTypeText(task.Type)

	return &response.DownloadTaskResponse{
		ID:               task.ID,
		URL:              task.URL,
		FileName:         task.FileName,
		FileSize:         task.FileSize,
		DownloadedSize:   task.DownloadedSize,
		Progress:         task.Progress,
		Speed:            task.Speed,
		Type:             task.Type,
		TypeText:         typeText,
		State:            task.State,
		StateText:        stateText,
		SavePath:         task.SavePath,
		SupportRange:     task.SupportRange,
		EnableEncryption: task.EnableEncryption,
		RequiresPassword: task.EnableEncryption && (task.State == enum.DownloadTaskStatePaused.Value() ||
			task.State == enum.DownloadTaskStateFailed.Value() || task.State == enum.DownloadTaskStateCanceled.Value()),
		HasRequestHeaders: task.RequestHeadersEncrypted != "",
		RequiresHeaders:   task.RequiresHeaders,
		ErrorMsg:          task.ErrorMsg,
		FileID:            task.FileID,
		CreateTime:        task.CreateTime,
		UpdateTime:        task.UpdateTime,
		FinishTime:        task.FinishTime,
	}
}

// getStateText 获取状态文本
func (d *DownloadService) getStateText(state int) string {
	switch state {
	case enum.DownloadTaskStateInit.Value():
		return "排队中"
	case enum.DownloadTaskStateDownloading.Value():
		return "下载中"
	case enum.DownloadTaskStatePaused.Value():
		return "已暂停"
	case enum.DownloadTaskStateFinished.Value():
		return "已完成"
	case enum.DownloadTaskStateFailed.Value():
		return "失败"
	case enum.DownloadTaskStateCanceled.Value():
		return "已取消"
	default:
		return "未知"
	}
}

func isManagedOfflineType(taskType int) bool {
	return taskType == enum.DownloadTaskTypeHttp.Value() || taskType == enum.DownloadTaskTypeBtp.Value() ||
		taskType == enum.DownloadTaskTypeMagnet.Value() || taskType == enum.DownloadTaskTypeHLS.Value()
}

// getTypeText 获取类型文本
func (d *DownloadService) getTypeText(taskType int) string {
	switch taskType {
	case enum.DownloadTaskTypeHttp.Value():
		return "HTTP"
	case enum.DownloadTaskTypeFTP.Value():
		return "FTP"
	case enum.DownloadTaskTypeSFTP.Value():
		return "SFTP"
	case enum.DownloadTaskTypeS3.Value():
		return "S3"
	case enum.DownloadTaskTypeBtp.Value():
		return "种子"
	case enum.DownloadTaskTypeMagnet.Value():
		return "磁力链接"
	case enum.DownloadTaskTypeLocal.Value():
		return "本地文件"
	case enum.DownloadTaskTypeLocalFile.Value():
		return "网盘下载"
	case enum.DownloadTaskTypePackage.Value():
		return "打包下载"
	case enum.DownloadTaskTypeHLS.Value():
		return "HLS"
	default:
		return "未知"
	}
}

// CreateLocalFileDownload 创建网盘文件下载任务
func (d *DownloadService) CreateLocalFileDownload(req *request.CreateLocalFileDownloadRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 验证用户是否存在
	_, err := d.factory.User().GetByID(ctx, userID)
	if err != nil {
		logger.LOG.Error("获取用户信息失败", "error", err, "userID", userID)
		return nil, fmt.Errorf("用户不存在")
	}

	// 2. 验证文件是否存在
	userFile, err := d.factory.UserFiles().GetByUfID(ctx, req.FileID)
	if err != nil {
		logger.LOG.Error("获取用户文件信息失败", "error", err, "fileID", req.FileID)
		return nil, err
	}
	if !userFile.IsPublic && userFile.UserID != userID {
		logger.LOG.Warn("用户尝试下载非公开文件", "userID", userID, "fileID", req.FileID)
		return nil, fmt.Errorf("无权下载此文件")
	}
	fileInfo, err := d.factory.FileInfo().GetByID(ctx, userFile.FileID)
	if err != nil {
		logger.LOG.Error("文件不存在", "error", err, "fileID", req.FileID)
		return nil, fmt.Errorf("文件不存在")
	}

	// 3. 创建下载任务记录
	taskID := uuid.Must(uuid.NewV7()).String()
	task := &models.DownloadTask{
		ID:               taskID,
		UserID:           userID,
		Type:             enum.DownloadTaskTypeLocalFile.Value(),
		URL:              req.FileID, // 存储 uf_id 在URL字段
		FileName:         fileInfo.Name,
		FileSize:         int64(fileInfo.Size),
		SavePath:         "",    // 网盘下载不需要保存目录
		EnableEncryption: false, // 网盘文件下载不加密存储（文件本身可能已加密）
		State:            enum.DownloadTaskStateInit.Value(),
		TargetDir:        d.tempDir,
		CreateTime:       custom_type.Now(),
		UpdateTime:       custom_type.Now(),
	}

	if err := d.factory.DownloadTask().Create(ctx, task); err != nil {
		logger.LOG.Error("创建下载任务失败", "error", err, "userID", userID, "fileID", req.FileID)
		return nil, fmt.Errorf("创建任务失败: %w", err)
	}
	d.publishTask(task, "created", false)

	// 保存真实的 file_id，用于异步任务
	realFileID := userFile.FileID

	// 4. 异步准备下载文件（解密+合并）
	go func() {
		// 更新任务状态为准备中
		task.State = enum.DownloadTaskStateDownloading.Value()
		task.UpdateTime = custom_type.Now()
		if err := d.factory.DownloadTask().Update(context.Background(), task); err == nil {
			d.publishTask(task, "updated", false)
		}

		opts := &download.LocalFileDownloadOptions{
			FilePassword: req.FilePassword,
		}

		result, err := download.PrepareLocalFileDownload(
			context.Background(),
			realFileID, // 使用真实的 file_id
			userID,
			d.tempDir,
			d.factory,
			opts,
		)

		if err != nil {
			// 准备失败
			task.State = enum.DownloadTaskStateFailed.Value()
			task.ErrorMsg = err.Error()
			task.UpdateTime = custom_type.Now()
			if updateErr := d.factory.DownloadTask().Update(context.Background(), task); updateErr == nil {
				d.publishTask(task, "updated", false)
			}
			logger.LOG.Error("准备下载文件失败", "taskID", taskID, "error", err)
			return
		}

		// 准备完成，更新任务状态为已完成（网盘文件下载准备完成即可下载）
		task.State = enum.DownloadTaskStateFinished.Value() // state=3 表示准备完成，可下载
		task.Progress = 100
		task.DownloadedSize = result.FileSize
		task.Path = result.TempFilePath // 存储临时文件路径
		task.UpdateTime = custom_type.Now()
		task.FinishTime = custom_type.Now()
		if err := d.factory.DownloadTask().Update(context.Background(), task); err == nil {
			d.publishTask(task, "updated", false)
		}

		logger.LOG.Info("网盘文件下载准备完成", "taskID", taskID, "realFileID", realFileID, "ufID", req.FileID, "tempPath", result.TempFilePath)
	}()

	logger.LOG.Info("网盘文件下载任务已创建", "taskID", taskID, "userID", userID, "ufID", req.FileID, "realFileID", realFileID)

	// 返回任务信息
	return models.NewJsonResponse(200, "任务创建成功", map[string]interface{}{
		"task_id":   taskID,
		"file_name": fileInfo.Name,
		"file_size": fileInfo.Size,
	}), nil
}

// ParseTorrent 解析种子/磁力链
func (d *DownloadService) ParseTorrent(req *request.ParseTorrentRequest) (*models.JsonResponse, error) {
	// 调用解析功能（超时120秒）
	result, err := download.ParseTorrentWithLimiters(req.Content, 120,
		d.networkPolicy.DownloadLimiter(), d.networkPolicy.BTUploadLimiter())
	if err != nil {
		logger.LOG.Error("解析种子失败", "error", err)
		return nil, fmt.Errorf("解析失败: %w", err)
	}

	// 转换为响应格式
	files := make([]response.TorrentFileInfo, 0, len(result.Files))
	for _, f := range result.Files {
		files = append(files, response.TorrentFileInfo{
			Index: f.Index,
			Name:  f.Name,
			Size:  f.Size,
			Path:  f.Path,
		})
	}

	resp := &response.ParseTorrentResponse{
		Name:      result.Name,
		InfoHash:  result.InfoHash,
		Files:     files,
		TotalSize: result.TotalSize,
	}

	logger.LOG.Info("种子解析成功",
		"name", result.Name,
		"infoHash", result.InfoHash,
		"fileCount", len(files),
		"totalSize", result.TotalSize,
	)

	return models.NewJsonResponse(200, "解析成功", resp), nil
}

// StartTorrentDownload 开始种子/磁力链下载
func (d *DownloadService) StartTorrentDownload(req *request.StartTorrentDownloadRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 验证用户是否存在并获取用户信息
	user, err := d.factory.User().GetByID(ctx, userID)
	if err != nil {
		logger.LOG.Error("获取用户信息失败", "error", err, "userID", userID)
		return nil, fmt.Errorf("用户不存在")
	}

	// 2. 解析种子获取元数据
	parseResult, err := download.ParseTorrentWithLimiters(req.Content, 120,
		d.networkPolicy.DownloadLimiter(), d.networkPolicy.BTUploadLimiter())
	if err != nil {
		logger.LOG.Error("解析种子失败", "error", err)
		return nil, fmt.Errorf("解析种子失败: %w", err)
	}

	// 3. 验证文件索引并计算总大小
	var totalSize int64 = 0
	seenIndexes := make(map[int]struct{}, len(req.FileIndexes))
	for _, idx := range req.FileIndexes {
		if idx < 0 || idx >= len(parseResult.Files) {
			return nil, fmt.Errorf("文件索引无效: %d", idx)
		}
		if _, exists := seenIndexes[idx]; exists {
			return nil, fmt.Errorf("文件索引重复: %d", idx)
		}
		seenIndexes[idx] = struct{}{}
		totalSize += parseResult.Files[idx].Size
	}

	// 4. 检查用户可用空间（只对非无限空间用户）
	if user.Space > 0 && user.FreeSpace < totalSize {
		return models.NewJsonResponse(400, "用户可用空间不足", map[string]interface{}{
			"required_size": totalSize,
			"free_space":    user.FreeSpace,
			"file_count":    len(req.FileIndexes),
		}), nil
	}
	logger.LOG.Info("种子下载空间检查通过",
		"total_size", totalSize,
		"free_space", user.FreeSpace,
		"file_count", len(req.FileIndexes),
		"user_id", userID)

	// 5. 设置默认虚拟路径
	savePath := req.SavePath
	if savePath == "" {
		savePath = "/离线下载"
	}
	savePath, err = virtualpath.NormalizeAbsolutePath(savePath)
	if err != nil {
		return nil, err
	}

	// 验证加密存储密码
	if req.EnableEncryption {
		if req.FilePassword == "" {
			return nil, fmt.Errorf("加密存储密码不能为空")
		}
	}

	// 6. 为每个文件创建下载任务
	taskIDs := make([]string, 0, len(req.FileIndexes))
	batchID := uuid.Must(uuid.NewV7()).String()
	for _, fileIndex := range req.FileIndexes {
		fileInfo := parseResult.Files[fileIndex]
		taskID := uuid.Must(uuid.NewV7()).String()

		// 判断任务类型（磁力链或种子）
		taskType := enum.DownloadTaskTypeBtp.Value()
		if strings.HasPrefix(req.Content, "magnet:") {
			taskType = enum.DownloadTaskTypeMagnet.Value()
		}

		task := &models.DownloadTask{
			ID:               taskID,
			UserID:           userID,
			Type:             taskType,
			URL:              req.Content, // 存储种子内容或磁力链
			FileName:         fileInfo.Name,
			FileSize:         fileInfo.Size,
			SavePath:         savePath,
			EnableEncryption: req.EnableEncryption,
			InfoHash:         parseResult.InfoHash,
			FileIndex:        fileIndex,
			TorrentName:      parseResult.Name,
			BatchID:          batchID,
			State:            enum.DownloadTaskStateInit.Value(),
			TargetDir:        d.tempDir,
			CreateTime:       custom_type.Now(),
			UpdateTime:       custom_type.Now(),
		}

		if err := d.factory.DownloadTask().Create(ctx, task); err != nil {
			logger.LOG.Error("创建下载任务失败", "error", err, "userID", userID, "fileIndex", fileIndex)
			return nil, fmt.Errorf("创建任务失败: %w", err)
		}

		d.publishTask(task, "created", false)
		taskIDs = append(taskIDs, taskID)
		d.manager.Notify(taskID, req.FilePassword)
	}

	logger.LOG.Info("种子下载任务已创建",
		"torrentName", parseResult.Name,
		"infoHash", parseResult.InfoHash,
		"taskCount", len(taskIDs),
		"userID", userID,
	)

	// 返回任务信息
	resp := &response.StartTorrentDownloadResponse{
		TaskIDs:     taskIDs,
		TorrentName: parseResult.Name,
		TaskCount:   len(taskIDs),
	}

	return models.NewJsonResponse(200, "任务创建成功", resp), nil
}
