package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/upload"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var errRangeUnsupported = errors.New("服务器不支持可靠Range下载")

// 下载任务管理器
var (
	downloadTasks   = make(map[string]context.CancelFunc)
	downloadTasksMu sync.RWMutex
)

// HTTPDownloadOptions HTTP下载配置
type HTTPDownloadOptions struct {
	EnableEncryption bool                       // 是否加密存储
	VirtualPath      string                     // 虚拟保存路径
	MaxRetries       int                        // 最大重试次数
	ChunkSize        int64                      // 分片大小（字节），默认10MB
	MaxConcurrent    int                        // 最大并发数，默认4
	Timeout          int                        // 超时时间（秒），默认300
	FilePassword     string                     // 加密文件密码（加密存储必备）
	RunToken         string                     // 当前执行令牌
	Client           *http.Client               // 测试或受控场景注入的HTTP客户端
	ReservedSize     int64                      // 已预留的用户空间
	ReserveSpace     func(int64) (int64, error) // 根据远端大小预留用户空间
}

// HTTPDownloadResult HTTP下载结果
type HTTPDownloadResult struct {
	FileID   string // 上传成功的文件ID
	FileName string // 文件名
	FileSize int64  // 文件大小
	Error    string // 错误信息（如果有）
}

// chunkInfo 分片下载信息
type chunkInfo struct {
	Index      int   // 分片索引
	Start      int64 // 起始位置
	End        int64 // 结束位置
	RetryCount int   // 重试次数
}

// downloadProgress 下载进度管理器
type downloadProgress struct {
	TaskID             string
	TotalSize          int64
	DownloadedSize     int64
	LastDownloadedSize int64 // 上次下载量，用于计算实时速度
	Speed              int64
	LastUpdate         time.Time
	SpeedHistory       []int64 // 速度历史记录（最多10条），用于平滑显示
	RepoFactory        *impl.RepositoryFactory
	RunToken           string
	ReservedSize       int64
	ReserveSpace       func(int64) (int64, error)
	mu                 sync.RWMutex
}

// newDownloadProgress 创建进度管理器
func newDownloadProgress(taskID string, totalSize int64, repoFactory *impl.RepositoryFactory, runToken string) *downloadProgress {
	return &downloadProgress{
		TaskID:             taskID,
		TotalSize:          totalSize,
		LastDownloadedSize: 0,
		LastUpdate:         time.Now(),
		SpeedHistory:       make([]int64, 0, 10),
		RepoFactory:        repoFactory,
		RunToken:           runToken,
	}
}

// updateProgress 更新下载进度（计算实时速度）
func (dp *downloadProgress) updateProgress(downloaded int64) {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(dp.LastUpdate).Seconds()

	// 每秒更新一次速度
	if elapsed >= 1.0 {
		// 计算实时速度（增量/时间差）
		sizeDiff := downloaded - dp.LastDownloadedSize
		if sizeDiff > 0 && elapsed > 0 {
			currentSpeed := int64(float64(sizeDiff) / elapsed)

			// 添加到速度历史记录（最多保留10条）
			dp.SpeedHistory = append(dp.SpeedHistory, currentSpeed)
			if len(dp.SpeedHistory) > 10 {
				dp.SpeedHistory = dp.SpeedHistory[1:]
			}

			// 计算平均速度（平滑显示）
			var totalSpeed int64 = 0
			validCount := 0
			for _, speed := range dp.SpeedHistory {
				if speed >= 0 {
					totalSpeed += speed
					validCount++
				}
			}
			if validCount > 0 {
				dp.Speed = totalSpeed / int64(validCount)
			} else {
				dp.Speed = currentSpeed
			}
		} else if sizeDiff == 0 {
			// 如果没有下载进度，速度设为0
			dp.Speed = 0
		}

		dp.LastUpdate = now
		dp.LastDownloadedSize = downloaded
		dp.DownloadedSize = downloaded

		progressValue := 0
		if dp.TotalSize > 0 {
			progressValue = int(float64(dp.DownloadedSize) / float64(dp.TotalSize) * 100)
			if progressValue > 100 {
				progressValue = 100
			}
		}
		_, err := dp.RepoFactory.DownloadTask().UpdateIfRunToken(context.Background(), dp.TaskID, dp.RunToken, map[string]interface{}{
			"downloaded_size": dp.DownloadedSize,
			"speed":           dp.Speed,
			"progress":        progressValue,
		})
		if err != nil {
			logger.LOG.Error("更新下载任务进度失败", "taskID", dp.TaskID, "error", err)
		}
	}
}

func (dp *downloadProgress) ensureUnknownSizeReservation(requiredSize int64) error {
	if dp.TotalSize > 0 || dp.ReserveSpace == nil || requiredSize <= dp.ReservedSize {
		return nil
	}
	const reservationBlock = int64(64 * 1024 * 1024)
	target := ((requiredSize + reservationBlock - 1) / reservationBlock) * reservationBlock
	reserved, err := dp.ReserveSpace(target)
	if err != nil {
		return err
	}
	dp.ReservedSize = reserved
	return nil
}

// DownloadHTTP 下载HTTP/HTTPS文件
// 参数:
//   - taskID: 下载任务ID
//   - url: 下载地址
//   - userID: 用户ID
//   - tempDir: 临时目录
//   - repoFactory: 数据库仓储工厂
//   - opts: 下载配置选项
//
// 返回:
//   - result: 下载结果
//   - err: 错误信息
func DownloadHTTP(
	taskID string,
	url string,
	userID string,
	tempDir string,
	repoFactory *impl.RepositoryFactory,
	opts *HTTPDownloadOptions,
) (*HTTPDownloadResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	downloadTasksMu.Lock()
	downloadTasks[taskID] = cancel
	downloadTasksMu.Unlock()
	defer func() {
		downloadTasksMu.Lock()
		delete(downloadTasks, taskID)
		downloadTasksMu.Unlock()
	}()
	return DownloadHTTPWithContext(ctx, taskID, url, userID, tempDir, repoFactory, opts)
}

// DownloadHTTPWithContext 使用调用方上下文执行HTTP/HTTPS下载。
// 最终任务状态由DownloadManager统一提交，本函数只更新元数据和进度。
func DownloadHTTPWithContext(
	ctx context.Context,
	taskID string,
	url string,
	userID string,
	tempDir string,
	repoFactory *impl.RepositoryFactory,
	opts *HTTPDownloadOptions,
) (*HTTPDownloadResult, error) {

	if opts == nil {
		opts = &HTTPDownloadOptions{
			EnableEncryption: false,
			VirtualPath:      "/离线下载/",
			MaxRetries:       3,
			ChunkSize:        10 * 1024 * 1024, // 10MB
			MaxConcurrent:    4,
			Timeout:          300,
		}
	}
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 10 * 1024 * 1024
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 300
	}
	client := opts.Client
	if client == nil {
		if err := ValidatePublicHTTPURL(url); err != nil {
			return nil, err
		}
		client = newPublicHTTPClient(opts.Timeout)
	}

	// 1. 获取文件信息
	logger.LOG.Info("开始获取文件信息", "url", url)
	fileInfo, supportRange, err := GetFileInfoWithClient(ctx, url, client)
	if err != nil {
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	logger.LOG.Info("文件信息获取成功",
		"fileName", fileInfo.FileName,
		"fileSize", fileInfo.FileSize,
		"supportRange", supportRange,
	)

	// 2. 更新任务信息
	updated, err := repoFactory.DownloadTask().UpdateIfRunToken(ctx, taskID, opts.RunToken, map[string]interface{}{
		"file_name":     fileInfo.FileName,
		"file_size":     fileInfo.FileSize,
		"support_range": supportRange,
	})
	if err != nil {
		return nil, fmt.Errorf("更新任务信息失败: %w", err)
	}
	if !updated {
		return nil, context.Canceled
	}
	if opts.ReserveSpace != nil && fileInfo.FileSize > 0 {
		reservedSize, reserveErr := opts.ReserveSpace(fileInfo.FileSize)
		if reserveErr != nil {
			return nil, reserveErr
		}
		opts.ReservedSize = reservedSize
	}

	// 3. 创建临时目录
	sessionDir := filepath.Join(tempDir, fmt.Sprintf("http_%s", taskID))
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	// 注意：不在defer中删除，以支持断点续传

	// 4. 下载文件
	filePath := filepath.Join(sessionDir, fileInfo.FileName)
	progress := newDownloadProgress(taskID, fileInfo.FileSize, repoFactory, opts.RunToken)
	progress.ReservedSize = opts.ReservedSize
	progress.ReserveSpace = opts.ReserveSpace

	if supportRange && fileInfo.FileSize > 0 {
		logger.LOG.Info("使用多线程下载", "chunkSize", opts.ChunkSize, "concurrent", opts.MaxConcurrent)
		err = downloadWithRange(ctx, url, filePath, fileInfo, opts, progress, client)
		if errors.Is(err, errRangeUnsupported) {
			logger.LOG.Warn("服务器未正确实现Range，回退到单线程下载", "url", url)
			_ = os.Remove(filePath)
			_ = os.Remove(filePath + ".manifest.json")
			err = downloadDirect(ctx, url, filePath, client, progress)
		}
	} else {
		logger.LOG.Info("使用单线程下载")
		err = downloadDirect(ctx, url, filePath, client, progress)
	}

	if err != nil {
		logger.LOG.Error("文件下载失败", "taskID", taskID, "error", err)
		return nil, fmt.Errorf("文件下载失败: %w", err)
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("检查下载文件失败: %w", err)
	}
	actualSize := stat.Size()
	if fileInfo.FileSize >= 0 && actualSize != fileInfo.FileSize {
		return nil, fmt.Errorf("下载文件大小不一致: 期望%d字节，实际%d字节", fileInfo.FileSize, actualSize)
	}
	fileInfo.FileSize = actualSize
	opts.ReservedSize = progress.ReservedSize

	logger.LOG.Info("文件下载完成", "fileName", fileInfo.FileName, "size", fileInfo.FileSize)

	// 5. 确保虚拟路径存在
	if err := ensureVirtualPath(ctx, userID, opts.VirtualPath, repoFactory); err != nil {
		// 清理临时文件
		os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("创建虚拟路径失败: %w", err)
	}

	// 6. 上传文件到系统
	uploadData := &upload.FileUploadData{
		TempFilePath: filePath,
		FileName:     fileInfo.FileName,
		FileSize:     fileInfo.FileSize,
		VirtualPath:  opts.VirtualPath,
		UserID:       userID,
		IsEnc:        opts.EnableEncryption,
		IsChunk:      false,
		FilePassword: opts.FilePassword,
		ReservedSize: opts.ReservedSize,
	}

	fileID, err := upload.ProcessUploadedFile(uploadData, repoFactory)
	if err != nil {
		logger.LOG.Error("上传文件失败", "taskID", taskID, "error", err)
		// 清理临时文件
		os.RemoveAll(sessionDir)
		return nil, fmt.Errorf("上传文件失败: %w", err)
	}

	// 清理临时文件（上传成功后）
	if removeErr := os.RemoveAll(sessionDir); removeErr != nil {
		logger.LOG.Warn("清理临时目录失败", "path", sessionDir, "error", removeErr)
	}

	logger.LOG.Info("离线下载任务完成", "taskID", taskID, "fileID", fileID)

	return &HTTPDownloadResult{
		FileID:   fileID,
		FileName: fileInfo.FileName,
		FileSize: fileInfo.FileSize,
	}, nil
}

// FileInfoResult 文件信息结果
type FileInfoResult struct {
	FileName     string
	FileSize     int64
	Size         int64 // 别名，与FileSize一致
	ETag         string
	LastModified string
}

// GetFileInfo 获取文件信息（文件名和大小）
func GetFileInfo(url string, timeout int) (*FileInfoResult, bool, error) {
	if err := ValidatePublicHTTPURL(url); err != nil {
		return nil, false, err
	}
	return GetFileInfoWithClient(context.Background(), url, newPublicHTTPClient(timeout))
}

// GetFileInfoWithClient 获取远端文件元数据；HEAD不可用时回退到Range GET探测。
func GetFileInfoWithClient(ctx context.Context, rawURL string, client *http.Client) (*FileInfoResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("创建请求失败: %w", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fileSize := resp.ContentLength
			return &FileInfoResult{
				FileName:     extractFileName(rawURL, resp.Header.Get("Content-Disposition")),
				FileSize:     fileSize,
				Size:         fileSize,
				ETag:         resp.Header.Get("ETag"),
				LastModified: resp.Header.Get("Last-Modified"),
			}, resp.Header.Get("Accept-Ranges") == "bytes", nil
		}
	}

	getReq, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if requestErr != nil {
		return nil, false, fmt.Errorf("创建探测请求失败: %w", requestErr)
	}
	getReq.Header.Set("Range", "bytes=0-0")
	getResp, requestErr := client.Do(getReq)
	if requestErr != nil {
		if err != nil {
			return nil, false, fmt.Errorf("HEAD和GET探测均失败: %v; %w", err, requestErr)
		}
		return nil, false, fmt.Errorf("GET探测失败: %w", requestErr)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusPartialContent && getResp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("服务器返回错误: %d", getResp.StatusCode)
	}
	fileSize := getResp.ContentLength
	supportRange := getResp.StatusCode == http.StatusPartialContent
	if supportRange {
		_, _, total, parseErr := parseContentRange(getResp.Header.Get("Content-Range"))
		if parseErr != nil {
			return nil, false, parseErr
		}
		fileSize = total
	}
	return &FileInfoResult{
		FileName:     extractFileName(rawURL, getResp.Header.Get("Content-Disposition")),
		FileSize:     fileSize,
		Size:         fileSize,
		ETag:         getResp.Header.Get("ETag"),
		LastModified: getResp.Header.Get("Last-Modified"),
	}, supportRange, nil
}

func parseContentRange(value string) (int64, int64, int64, error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, fmt.Errorf("无效的Content-Range: %s", value)
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 {
		return 0, 0, 0, fmt.Errorf("无效的Content-Range: %s", value)
	}
	rangeParts := strings.Split(parts[0], "-")
	if len(rangeParts) != 2 {
		return 0, 0, 0, fmt.Errorf("无效的Content-Range: %s", value)
	}
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("无效的Content-Range起点: %w", err)
	}
	end, err := strconv.ParseInt(rangeParts[1], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("无效的Content-Range终点: %w", err)
	}
	total, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("无效的Content-Range总大小: %w", err)
	}
	if start < 0 || end < start || total <= end {
		return 0, 0, 0, fmt.Errorf("Content-Range范围不合法: %s", value)
	}
	return start, end, total, nil
}

// extractFileName 从URL或Content-Disposition中提取文件名
func extractFileName(url, contentDisposition string) string {
	// 优先从Content-Disposition获取
	if contentDisposition != "" {
		// 1. 优先处理 filename*=UTF-8''xxx (RFC 5987 格式)
		if idx := strings.Index(contentDisposition, "filename*=UTF-8''"); idx >= 0 {
			value := contentDisposition[idx+len("filename*=UTF-8''"):]
			// 移除分号后的内容（如果有）
			if semicolonIdx := strings.Index(value, ";"); semicolonIdx > 0 {
				value = value[:semicolonIdx]
			}
			// URL解码
			if decoded, err := neturl.QueryUnescape(value); err == nil {
				fileName := sanitizeFileName(decoded)
				if fileName != "" {
					return fileName
				}
			}
		}

		// 2. 处理 filename="xxx" 或 filename=xxx
		if idx := strings.Index(contentDisposition, "filename="); idx >= 0 {
			value := contentDisposition[idx+len("filename="):]
			// 移除分号后的内容（如果有）
			if semicolonIdx := strings.Index(value, ";"); semicolonIdx > 0 {
				value = value[:semicolonIdx]
			}
			// 移除引号和空格
			value = strings.Trim(value, " \"")
			// 如果值不为空且不是 filename*= 的开头（避免重复处理）
			if value != "" && !strings.HasPrefix(value, "UTF-8''") {
				fileName := sanitizeFileName(value)
				if fileName != "" {
					return fileName
				}
			}
		}
	}

	// 从URL中提取
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// 移除查询参数
		if idx := strings.Index(lastPart, "?"); idx > 0 {
			lastPart = lastPart[:idx]
		}
		if lastPart != "" {
			fileName := sanitizeFileName(lastPart)
			if fileName != "" {
				return fileName
			}
		}
	}

	// 使用默认文件名
	return fmt.Sprintf("未命名文件_%s", time.Now().Format("20060102150405"))
}

// sanitizeFileName 清理文件名，移除Windows不允许的字符
func sanitizeFileName(fileName string) string {
	// Windows不允许的字符: < > : " / \ | ? *
	invalidChars := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	result := fileName

	// 移除所有非法字符
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "_")
	}

	// 移除控制字符（ASCII 0-31）
	var builder strings.Builder
	for _, r := range result {
		if r > 31 && r != 127 { // 保留可打印字符，排除DEL (127)
			builder.WriteRune(r)
		}
	}
	result = builder.String()

	// 移除首尾空格和点号（Windows不允许）
	result = strings.Trim(result, " .")

	// 如果结果为空，返回默认名称
	if result == "" {
		return fmt.Sprintf("未命名文件_%s", time.Now().Format("20060102150405"))
	}

	// Windows保留名称检查
	reservedNames := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	upperName := strings.ToUpper(result)
	for _, reserved := range reservedNames {
		if upperName == reserved || strings.HasPrefix(upperName, reserved+".") {
			return fmt.Sprintf("未命名文件_%s", time.Now().Format("20060102150405"))
		}
	}

	return result
}

// downloadDirect 直接下载（不支持断点续传）
func downloadDirect(
	ctx context.Context,
	url string,
	filePath string,
	client *http.Client,
	progress *downloadProgress,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("创建下载请求失败: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，状态码: %d", resp.StatusCode)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	// 带进度的复制
	buffer := make([]byte, 32*1024) // 32KB缓冲区
	var downloaded int64

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if reserveErr := progress.ensureUnknownSizeReservation(downloaded + int64(n)); reserveErr != nil {
				return fmt.Errorf("预留用户空间失败: %w", reserveErr)
			}
			if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}
			downloaded += int64(n)
			progress.updateProgress(downloaded)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("读取数据失败: %w", err)
		}
	}

	return nil
}

// downloadWithRange 使用断点续传多线程下载
func downloadWithRange(
	ctx context.Context,
	url string,
	filePath string,
	fileInfo *FileInfoResult,
	opts *HTTPDownloadOptions,
	progress *downloadProgress,
	client *http.Client,
) error {
	if err := probeRangeSupport(ctx, url, fileInfo.FileSize, client); err != nil {
		return err
	}
	totalSize := fileInfo.FileSize
	manifestPath := filePath + ".manifest.json"
	manifest, loadErr := loadDownloadManifest(manifestPath)
	resume := loadErr == nil && manifestMatches(manifest, url, fileInfo)
	if !resume {
		_ = os.Remove(filePath)
		_ = os.Remove(manifestPath)
		chunks := calculateChunks(totalSize, opts.ChunkSize)
		manifest = &downloadManifest{
			Version:      1,
			URL:          url,
			FileSize:     totalSize,
			ETag:         fileInfo.ETag,
			LastModified: fileInfo.LastModified,
			Chunks:       make([]manifestChunk, 0, len(chunks)),
		}
		for _, chunk := range chunks {
			manifest.Chunks = append(manifest.Chunks, manifestChunk{Index: chunk.Index, Start: chunk.Start, End: chunk.End})
		}
		if err := saveDownloadManifest(manifestPath, manifest); err != nil {
			return err
		}
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("打开下载文件失败: %w", err)
	}
	defer file.Close()
	if stat, statErr := file.Stat(); statErr != nil || stat.Size() != totalSize {
		if err := file.Truncate(totalSize); err != nil {
			return fmt.Errorf("预分配文件空间失败: %w", err)
		}
	}

	pendingChunks := make([]chunkInfo, 0, len(manifest.Chunks))
	var downloadedBytes int64
	for _, chunk := range manifest.Chunks {
		if chunk.Done {
			downloadedBytes += chunk.End - chunk.Start + 1
			continue
		}
		pendingChunks = append(pendingChunks, chunkInfo{Index: chunk.Index, Start: chunk.Start, End: chunk.End})
	}
	if len(pendingChunks) == 0 {
		return nil
	}
	progress.DownloadedSize = downloadedBytes
	progress.LastDownloadedSize = downloadedBytes
	initialProgress := int(float64(downloadedBytes) / float64(totalSize) * 100)
	_, _ = progress.RepoFactory.DownloadTask().UpdateIfRunToken(context.Background(), progress.TaskID, progress.RunToken, map[string]interface{}{
		"downloaded_size": downloadedBytes,
		"progress":        initialProgress,
		"speed":           0,
	})
	logger.LOG.Info("待下载分片", "总数", len(manifest.Chunks), "待下载", len(pendingChunks), "已下载", len(manifest.Chunks)-len(pendingChunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.MaxConcurrent)
	errChan := make(chan error, len(pendingChunks))
	var manifestMu sync.Mutex

launchChunks:
	for i := range pendingChunks {
		select {
		case <-ctx.Done():
			break launchChunks
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(chunk *chunkInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			for retry := 0; retry <= opts.MaxRetries; retry++ {
				if ctx.Err() != nil {
					return
				}
				if retry > 0 {
					logger.LOG.Warn("重试下载分片", "chunk", chunk.Index, "retry", retry, "maxRetries", opts.MaxRetries)
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Duration(retry*2) * time.Second):
					}
				}
				err := downloadChunk(ctx, url, file, chunk, &downloadedBytes, progress, client, fileInfo)
				if err == nil {
					manifestMu.Lock()
					syncErr := file.Sync()
					if syncErr == nil {
						manifest.Chunks[chunk.Index].Done = true
						syncErr = saveDownloadManifest(manifestPath, manifest)
					}
					manifestMu.Unlock()
					if syncErr != nil {
						errChan <- syncErr
					}
					return
				}
				if ctx.Err() != nil {
					return
				}
				if retry == opts.MaxRetries {
					errChan <- fmt.Errorf("分片 %d 下载失败: %w", chunk.Index, err)
					return
				}
			}
		}(&pendingChunks[i])
	}

	wg.Wait()
	close(errChan)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	logger.LOG.Info("所有分片下载完成")
	return nil
}

func probeRangeSupport(ctx context.Context, rawURL string, totalSize int64, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("创建Range探测请求失败: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Range探测失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("%w，状态码: %d", errRangeUnsupported, resp.StatusCode)
	}
	start, end, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return err
	}
	if start != 0 || end != 0 || total != totalSize {
		return fmt.Errorf("Range探测结果不匹配")
	}
	return nil
}

// calculateChunks 计算分片信息
func calculateChunks(totalSize, chunkSize int64) []chunkInfo {
	var chunks []chunkInfo
	var start int64

	for start < totalSize {
		end := start + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}

		chunks = append(chunks, chunkInfo{
			Index: len(chunks),
			Start: start,
			End:   end,
		})

		start = end + 1
	}

	return chunks
}

// downloadChunk 下载单个分片
func downloadChunk(
	ctx context.Context,
	url string,
	file *os.File,
	chunk *chunkInfo,
	downloadedBytes *int64,
	progress *downloadProgress,
	client *http.Client,
	fileInfo *FileInfoResult,
) (resultErr error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置Range头
	rangeHeader := fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End)
	req.Header.Set("Range", rangeHeader)
	if fileInfo.ETag != "" {
		req.Header.Set("If-Range", fileInfo.ETag)
	} else if fileInfo.LastModified != "" {
		req.Header.Set("If-Range", fileInfo.LastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("Range请求必须返回206，实际状态码: %d", resp.StatusCode)
	}
	start, end, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return err
	}
	if start != chunk.Start || end != chunk.End || total != fileInfo.FileSize {
		return fmt.Errorf("Content-Range与请求范围不一致")
	}
	expected := chunk.End - chunk.Start + 1
	if resp.ContentLength >= 0 && resp.ContentLength != expected {
		return fmt.Errorf("Range响应大小不一致: 期望%d字节，实际%d字节", expected, resp.ContentLength)
	}

	// 读取数据并写入文件
	buffer := make([]byte, 32*1024) // 32KB缓冲区
	offset := chunk.Start
	remaining := expected
	var written int64
	defer func() {
		if resultErr != nil && written > 0 {
			current := atomic.AddInt64(downloadedBytes, -written)
			progress.updateProgress(current)
		}
	}()

	for remaining > 0 {
		readSize := int64(len(buffer))
		if remaining < readSize {
			readSize = remaining
		}
		n, err := resp.Body.Read(buffer[:readSize])
		if n > 0 {
			// 写入指定位置
			if _, writeErr := file.WriteAt(buffer[:n], offset); writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}
			offset += int64(n)
			remaining -= int64(n)
			written += int64(n)
			atomic.AddInt64(downloadedBytes, int64(n))
			progress.updateProgress(atomic.LoadInt64(downloadedBytes))
		}
		if err == io.EOF && remaining > 0 {
			return fmt.Errorf("Range响应提前结束，还缺少%d字节", remaining)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("读取数据失败: %w", err)
		}
	}
	var extra [1]byte
	if n, extraErr := resp.Body.Read(extra[:]); n > 0 {
		return fmt.Errorf("Range响应超过声明的字节范围")
	} else if extraErr != nil && extraErr != io.EOF {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("校验Range响应结束失败: %w", extraErr)
	}

	return nil
}

// PauseDownload 暂停下载任务
func PauseDownload(taskID string, repoFactory *impl.RepositoryFactory) error {
	ctx := context.Background()

	task, err := repoFactory.DownloadTask().GetByID(ctx, taskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "taskID", taskID, "error", err)
		return fmt.Errorf("获取任务失败: %w", err)
	}

	if task.State != enum.DownloadTaskStateDownloading.Value() {
		return fmt.Errorf("任务状态不允许暂停")
	}

	// 取消下载任务的context
	downloadTasksMu.RLock()
	cancel, exists := downloadTasks[taskID]
	downloadTasksMu.RUnlock()

	if exists && cancel != nil {
		cancel() // 取消context，停止所有goroutine
		logger.LOG.Info("已取消下载任务的goroutine", "taskID", taskID)
	}

	task.State = enum.DownloadTaskStatePaused.Value()
	task.UpdateTime = custom_type.Now()

	if err := repoFactory.DownloadTask().Update(ctx, task); err != nil {
		logger.LOG.Error("更新任务状态失败", "taskID", taskID, "error", err)
		return fmt.Errorf("暂停任务失败: %w", err)
	}

	logger.LOG.Info("任务已暂停", "taskID", taskID)
	return nil
}

// ResumeDownload 恢复下载任务
func ResumeDownload(taskID string, userID string, tempDir string, repoFactory *impl.RepositoryFactory) error {
	ctx := context.Background()

	task, err := repoFactory.DownloadTask().GetByID(ctx, taskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "taskID", taskID, "error", err)
		return fmt.Errorf("获取任务失败: %w", err)
	}

	if task.State != enum.DownloadTaskStatePaused.Value() {
		return fmt.Errorf("任务状态不允许恢复")
	}

	task.State = enum.DownloadTaskStateDownloading.Value()
	task.UpdateTime = custom_type.Now()

	if err := repoFactory.DownloadTask().Update(ctx, task); err != nil {
		logger.LOG.Error("更新任务状态失败", "taskID", taskID, "error", err)
		return fmt.Errorf("恢复任务失败: %w", err)
	}

	// 重新启动下载（异步）
	go func() {
		opts := &HTTPDownloadOptions{
			EnableEncryption: false, // HTTP离线下载不加密
			VirtualPath:      task.VirtualPath,
			MaxRetries:       3,
			ChunkSize:        10 * 1024 * 1024,
			MaxConcurrent:    4,
			Timeout:          300,
		}
		_, err := DownloadHTTP(taskID, task.URL, userID, tempDir, repoFactory, opts)
		if err != nil {
			logger.LOG.Error("恢复下载失败", "taskID", taskID, "error", err)
		}
	}()

	logger.LOG.Info("任务已恢复", "taskID", taskID)
	return nil
}

// CancelDownload 取消下载任务
func CancelDownload(taskID string, repoFactory *impl.RepositoryFactory) error {
	ctx := context.Background()

	task, err := repoFactory.DownloadTask().GetByID(ctx, taskID)
	if err != nil {
		logger.LOG.Error("获取下载任务失败", "taskID", taskID, "error", err)
		return fmt.Errorf("获取任务失败: %w", err)
	}

	if task.State == enum.DownloadTaskStateFinished.Value() {
		return fmt.Errorf("任务已完成，无法取消")
	}

	// 取消下载任务的context
	downloadTasksMu.RLock()
	cancel, exists := downloadTasks[taskID]
	downloadTasksMu.RUnlock()

	if exists && cancel != nil {
		cancel() // 取消context，停止所有goroutine
		logger.LOG.Info("已取消下载任务的goroutine", "taskID", taskID)
	}

	task.State = enum.DownloadTaskStateFailed.Value()
	task.ErrorMsg = "用户取消下载"
	task.UpdateTime = custom_type.Now()

	if err := repoFactory.DownloadTask().Update(ctx, task); err != nil {
		logger.LOG.Error("更新任务状态失败", "taskID", taskID, "error", err)
		return fmt.Errorf("取消任务失败: %w", err)
	}

	logger.LOG.Info("任务已取消", "taskID", taskID)
	return nil
}
