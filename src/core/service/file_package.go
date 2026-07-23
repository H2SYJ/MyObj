package service

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/util"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 打包任务状态管理
var packageTasks sync.Map // key: packageID, value: *PackageTask

// PackageTask 打包任务
type PackageTask struct {
	PackageID    string
	PackageName  string
	UserID       string
	FileIDs      []string
	Entries      []PackageEntry
	EmptyDirs    []string
	FilePassword string
	Status       string // creating, ready, failed
	Progress     int    // 0-100
	TotalSize    int64
	CreatedSize  int64
	FilePath     string
	ErrorMsg     string
	CreatedAt    time.Time // 创建时间，用于清理过期任务
	mu           sync.Mutex
}

type PackageEntry struct {
	FileID      string
	ArchivePath string
}

func (f *FileService) publishPackageTask(task *PackageTask, action string, coalesce bool) {
	if f.taskEvents == nil || task == nil {
		return
	}
	now := time.Now().UTC()
	task.mu.Lock()
	event := TaskEvent{
		Version:    1,
		Kind:       TaskEventPackage,
		Action:     action,
		ResourceID: task.PackageID,
		Terminal:   task.Status == "ready" || task.Status == "failed",
		UserID:     task.UserID,
		OccurredAt: now,
		Payload: map[string]any{
			"package_id":   task.PackageID,
			"package_name": task.PackageName,
			"status":       task.Status,
			"progress":     task.Progress,
			"created_size": task.CreatedSize,
			"total_size":   task.TotalSize,
			"error_msg":    task.ErrorMsg,
			"update_time":  now,
		},
	}
	task.mu.Unlock()
	f.taskEvents.Publish(event, coalesce)
}

// CreatePackage 创建打包下载任务
func (f *FileService) CreatePackage(req *request.PackageCreateRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	entries, emptyDirs, totalSize, hasEncrypted, err := f.resolvePackageEntries(ctx, userID, req.FileIDs, req.DirIDs)
	if err != nil {
		return models.NewJsonResponse(400, err.Error(), nil), nil
	}
	if len(entries) == 0 && len(emptyDirs) == 0 {
		return models.NewJsonResponse(400, "请选择要下载的文件或目录", nil), nil
	}
	if hasEncrypted {
		user, err := f.factory.User().GetByID(ctx, userID)
		if err != nil {
			return nil, err
		}
		if req.FilePassword == "" || !util.CheckPassword(user.FilePassword, req.FilePassword) {
			return models.NewJsonResponse(400, "文件密码错误", nil), nil
		}
	}

	// 生成打包ID
	packageID := uuid.New().String()

	// 设置打包名称
	packageName := req.PackageName
	if packageName == "" {
		if len(req.DirIDs) == 1 && len(req.FileIDs) == 0 {
			if dir, dirErr := f.factory.Directory().GetByID(ctx, req.DirIDs[0]); dirErr == nil {
				packageName = safeArchiveSegment(dir.Name) + ".zip"
			}
		}
		if packageName == "" {
			packageName = fmt.Sprintf("files_%d.zip", time.Now().Unix())
		}
	}
	packageName = safeArchiveSegment(path.Base(strings.ReplaceAll(packageName, "\\", "/")))
	if !strings.HasSuffix(packageName, ".zip") {
		packageName += ".zip"
	}

	// 创建打包任务
	task := &PackageTask{
		PackageID:    packageID,
		PackageName:  packageName,
		UserID:       userID,
		FileIDs:      packageFileIDs(entries),
		Entries:      entries,
		EmptyDirs:    emptyDirs,
		FilePassword: req.FilePassword,
		Status:       "creating",
		Progress:     0,
		TotalSize:    totalSize,
		CreatedSize:  0,
		CreatedAt:    time.Now(),
	}
	packageTasks.Store(packageID, task)
	f.publishPackageTask(task, "created", false)

	// 异步创建压缩包
	go f.createZipPackage(ctx, task)

	return models.NewJsonResponse(200, "创建成功", response.PackageCreateResponse{
		PackageID:   packageID,
		PackageName: packageName,
		Status:      "creating",
		Progress:    0,
		TotalSize:   totalSize,
	}), nil
}

func (f *FileService) resolvePackageEntries(
	ctx context.Context,
	userID string,
	fileIDs []string,
	dirIDs []int,
) ([]PackageEntry, []string, int64, bool, error) {
	fileIDs = uniqueStrings(fileIDs)
	dirIDs = uniqueInts(dirIDs)
	if len(fileIDs) == 0 && len(dirIDs) == 0 {
		return nil, nil, 0, false, errors.New("请选择要下载的文件或目录")
	}
	rootDirIDs, err := filterNestedDirectories(ctx, f.factory, userID, dirIDs)
	if err != nil {
		return nil, nil, 0, false, err
	}

	entries := make([]PackageEntry, 0)
	directories := make([]string, 0)
	seenFiles := make(map[string]struct{})
	seenPaths := make(map[string]struct{})
	var totalSize int64
	hasEncrypted := false

	addFile := func(file *models.UserFiles, archivePath string) error {
		if _, exists := seenFiles[file.UfID]; exists {
			return nil
		}
		archivePath = path.Clean(strings.ReplaceAll(archivePath, "\\", "/"))
		if !validArchivePath(archivePath) {
			return fmt.Errorf("压缩包路径不安全: %s", archivePath)
		}
		if _, exists := seenPaths[archivePath]; exists {
			return fmt.Errorf("压缩包内存在重名条目: %s", archivePath)
		}
		info, err := f.factory.FileInfo().GetByID(ctx, file.FileID)
		if err != nil {
			return err
		}
		seenFiles[file.UfID] = struct{}{}
		seenPaths[archivePath] = struct{}{}
		entries = append(entries, PackageEntry{FileID: file.UfID, ArchivePath: archivePath})
		totalSize += int64(info.Size)
		hasEncrypted = hasEncrypted || info.IsEnc
		return nil
	}

	for _, rootID := range rootDirIDs {
		root, err := f.factory.Directory().GetByID(ctx, rootID)
		if err != nil || root.UserID != userID {
			return nil, nil, 0, false, fmt.Errorf("目录不存在或无权限: %d", rootID)
		}
		rootName := root.Name
		if err := validateArchiveSegment(rootName); err != nil {
			return nil, nil, 0, false, err
		}
		type dirEntry struct {
			Dir         *models.VirtualDirectory
			ArchivePath string
		}
		queue := []dirEntry{{Dir: root, ArchivePath: rootName}}
		for i := 0; i < len(queue); i++ {
			current := queue[i]
			if _, exists := seenPaths[current.ArchivePath+"/"]; !exists {
				seenPaths[current.ArchivePath+"/"] = struct{}{}
				directories = append(directories, current.ArchivePath+"/")
			}
			files, err := f.factory.UserFiles().ListByDirectoryID(ctx, userID, current.Dir.ID, 0, -1)
			if err != nil {
				return nil, nil, 0, false, err
			}
			for _, file := range files {
				if err := validateArchiveSegment(file.FileName); err != nil {
					return nil, nil, 0, false, err
				}
				if err := addFile(file, path.Join(current.ArchivePath, file.FileName)); err != nil {
					return nil, nil, 0, false, err
				}
			}
			children, err := f.factory.Directory().ListChildren(ctx, userID, current.Dir.ID, 0, -1)
			if err != nil {
				return nil, nil, 0, false, err
			}
			for _, child := range children {
				name := child.Name
				if err := validateArchiveSegment(name); err != nil {
					return nil, nil, 0, false, err
				}
				queue = append(queue, dirEntry{Dir: child, ArchivePath: path.Join(current.ArchivePath, name)})
			}
		}
	}
	// 目录优先展开，若调用方同时选择目录及其成员，保留成员在目录中的相对路径。
	for _, fileID := range fileIDs {
		file, err := f.factory.UserFiles().GetByUserIDAndUfID(ctx, userID, fileID)
		if err != nil {
			return nil, nil, 0, false, fmt.Errorf("文件不存在或无权限: %s", fileID)
		}
		if err := validateArchiveSegment(file.FileName); err != nil {
			return nil, nil, 0, false, err
		}
		if err := addFile(file, file.FileName); err != nil {
			return nil, nil, 0, false, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ArchivePath < entries[j].ArchivePath })
	sort.Strings(directories)
	return entries, directories, totalSize, hasEncrypted, nil
}

func validateArchiveSegment(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "." || trimmed == ".." || strings.ContainsAny(trimmed, "/\\\x00") {
		return fmt.Errorf("文件或目录名称不适合打包: %s", name)
	}
	return nil
}

func validArchivePath(value string) bool {
	if value == "" || value == "." || value == ".." || path.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func safeArchiveSegment(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "files"
	}
	return name
}

func packageFileIDs(entries []PackageEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.FileID)
	}
	return result
}

// createZipPackage 异步创建ZIP压缩包
func (f *FileService) createZipPackage(ctx context.Context, task *PackageTask) {
	tempDir := filepath.Join(os.TempDir(), "package_"+task.PackageID)
	defer func() {
		task.mu.Lock()
		task.FilePassword = ""
		task.mu.Unlock()
		if r := recover(); r != nil {
			_ = os.RemoveAll(tempDir)
			task.mu.Lock()
			task.Status = "failed"
			task.ErrorMsg = fmt.Sprintf("打包失败: %v", r)
			task.mu.Unlock()
			f.publishPackageTask(task, "updated", false)
			logger.LOG.Error("打包任务异常", "packageID", task.PackageID, "error", r)
		}
	}()

	// 创建临时目录
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		_ = os.RemoveAll(tempDir)
		task.mu.Lock()
		task.Status = "failed"
		task.ErrorMsg = fmt.Sprintf("创建临时目录失败: %v", err)
		task.mu.Unlock()
		f.publishPackageTask(task, "updated", false)
		return
	}
	// 注意：不要在这里立即删除临时目录，因为文件需要保留供下载使用
	// 文件会在下载完成后或任务过期后清理

	// 创建ZIP文件
	zipPath := filepath.Join(tempDir, task.PackageName)
	zipFile, err := os.Create(zipPath)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		task.mu.Lock()
		task.Status = "failed"
		task.ErrorMsg = fmt.Sprintf("创建ZIP文件失败: %v", err)
		task.mu.Unlock()
		f.publishPackageTask(task, "updated", false)
		return
	}
	zipWriter := zip.NewWriter(zipFile)
	fail := func(err error) {
		_ = zipWriter.Close()
		_ = zipFile.Close()
		_ = os.RemoveAll(tempDir)
		task.mu.Lock()
		task.Status = "failed"
		task.ErrorMsg = err.Error()
		task.mu.Unlock()
		f.publishPackageTask(task, "updated", false)
	}
	for _, directory := range task.EmptyDirs {
		if _, err := zipWriter.Create(directory); err != nil {
			fail(fmt.Errorf("创建目录条目失败: %w", err))
			return
		}
	}

	totalFiles := len(task.Entries)
	for i, entry := range task.Entries {
		userFile, err := f.factory.UserFiles().GetByUserIDAndUfID(ctx, task.UserID, entry.FileID)
		if err != nil {
			fail(fmt.Errorf("获取打包文件失败: %w", err))
			return
		}
		fileInfo, err := f.factory.FileInfo().GetByID(ctx, userFile.FileID)
		if err != nil {
			fail(fmt.Errorf("获取文件信息失败: %w", err))
			return
		}
		downloadResult, err := download.PrepareLocalFileDownload(
			ctx, userFile.FileID, task.UserID, tempDir, f.factory,
			&download.LocalFileDownloadOptions{FilePassword: task.FilePassword},
		)
		if err != nil {
			fail(fmt.Errorf("准备文件“%s”失败: %w", userFile.FileName, err))
			return
		}
		sourceFile, err := os.Open(downloadResult.TempFilePath)
		if err != nil {
			fail(fmt.Errorf("打开文件“%s”失败: %w", userFile.FileName, err))
			return
		}
		zipEntry, err := zipWriter.Create(entry.ArchivePath)
		if err != nil {
			_ = sourceFile.Close()
			fail(fmt.Errorf("创建压缩条目失败: %w", err))
			return
		}
		written, copyErr := io.Copy(zipEntry, sourceFile)
		closeErr := sourceFile.Close()
		if copyErr != nil || closeErr != nil {
			if copyErr == nil {
				copyErr = closeErr
			}
			fail(fmt.Errorf("写入文件“%s”失败: %w", userFile.FileName, copyErr))
			return
		}
		task.mu.Lock()
		task.CreatedSize += written
		if totalFiles > 0 {
			task.Progress = (i + 1) * 100 / totalFiles
		}
		task.mu.Unlock()
		f.publishPackageTask(task, "updated", true)
		if downloadResult.TempFilePath != fileInfo.Path && strings.Contains(downloadResult.TempFilePath, "temp") {
			preparedDir := filepath.Dir(downloadResult.TempFilePath)
			if preparedDir != tempDir && strings.Contains(preparedDir, "temp") {
				_ = os.RemoveAll(preparedDir)
			}
		}
	}
	if err := zipWriter.Close(); err != nil {
		fail(fmt.Errorf("完成压缩包失败: %w", err))
		return
	}
	if err := zipFile.Close(); err != nil {
		fail(fmt.Errorf("关闭压缩包失败: %w", err))
		return
	}

	// 完成打包
	task.mu.Lock()
	task.Status = "ready"
	task.Progress = 100
	task.FilePath = zipPath
	task.mu.Unlock()
	f.publishPackageTask(task, "updated", false)

	logger.LOG.Info("打包完成", "packageID", task.PackageID, "filePath", zipPath)

	// 为每个文件创建下载任务记录
	f.createDownloadTasksForPackage(ctx, task)
}

// GetPackageProgress 获取打包进度
func (f *FileService) GetPackageProgress(packageID, userID string) (*models.JsonResponse, error) {
	value, ok := packageTasks.Load(packageID)
	if !ok {
		return nil, fmt.Errorf("打包任务不存在")
	}

	task := value.(*PackageTask)
	if task.UserID != userID {
		return nil, fmt.Errorf("无权限访问该打包任务")
	}

	task.mu.Lock()
	defer task.mu.Unlock()

	return models.NewJsonResponse(200, "查询成功", response.PackageProgressResponse{
		PackageID:   task.PackageID,
		Status:      task.Status,
		Progress:    task.Progress,
		TotalSize:   task.TotalSize,
		CreatedSize: task.CreatedSize,
		ErrorMsg:    task.ErrorMsg,
	}), nil
}

// DownloadPackage 下载打包文件
func (f *FileService) DownloadPackage(packageID, userID string) (string, string, error) {
	value, ok := packageTasks.Load(packageID)
	if !ok {
		return "", "", fmt.Errorf("打包任务不存在")
	}

	task := value.(*PackageTask)
	if task.UserID != userID {
		return "", "", fmt.Errorf("无权限访问该打包任务")
	}

	task.mu.Lock()
	defer task.mu.Unlock()

	if task.Status != "ready" {
		return "", "", fmt.Errorf("打包任务未完成，状态: %s", task.Status)
	}

	if task.FilePath == "" {
		return "", "", fmt.Errorf("打包文件路径不存在")
	}

	// 检查文件是否存在
	if _, err := os.Stat(task.FilePath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("打包文件不存在")
	}

	// 下载完成后，异步清理文件（延迟5分钟，给用户足够时间下载）
	go func() {
		time.Sleep(5 * time.Minute)
		// 再次检查任务状态，如果还是 ready，则清理文件
		task.mu.Lock()
		if task.Status == "ready" && task.FilePath != "" {
			// 删除文件
			if err := os.Remove(task.FilePath); err != nil {
				logger.LOG.Warn("删除打包文件失败", "packageID", packageID, "filePath", task.FilePath, "error", err)
			} else {
				logger.LOG.Info("打包文件已清理", "packageID", packageID, "filePath", task.FilePath)
			}
			// 删除临时目录
			tempDir := filepath.Dir(task.FilePath)
			if err := os.RemoveAll(tempDir); err != nil {
				logger.LOG.Warn("删除临时目录失败", "packageID", packageID, "tempDir", tempDir, "error", err)
			}
			// 从任务列表中移除
			packageTasks.Delete(packageID)
		}
		task.mu.Unlock()
	}()

	return task.FilePath, task.PackageName, nil
}

// createDownloadTasksForPackage 为打包中的每个文件创建下载任务记录
func (f *FileService) createDownloadTasksForPackage(ctx context.Context, task *PackageTask) {
	for _, fileID := range task.FileIDs {
		// 获取用户文件（前端传递的是 uf_id）
		userFile, err := f.factory.UserFiles().GetByUserIDAndUfID(ctx, task.UserID, fileID)
		if err != nil {
			logger.LOG.Warn("获取用户文件失败", "fileID", fileID, "error", err)
			continue
		}

		// 获取文件信息
		fileInfo, err := f.factory.FileInfo().GetByID(ctx, userFile.FileID)
		if err != nil {
			logger.LOG.Warn("获取文件信息失败", "fileID", fileID, "error", err)
			continue
		}

		// 创建下载任务记录
		taskID := uuid.Must(uuid.NewV7()).String()
		downloadTask := &models.DownloadTask{
			ID:               taskID,
			UserID:           task.UserID,
			Type:             enum.DownloadTaskTypePackage.Value(),
			URL:              task.PackageID, // 在URL字段存储打包ID
			FileName:         userFile.FileName,
			FileSize:         int64(fileInfo.Size),
			FileID:           userFile.FileID,
			SavePath:         "", // 打包下载不需要保存目录
			EnableEncryption: false,
			State:            enum.DownloadTaskStateFinished.Value(), // 直接设置为已完成
			Progress:         100,
			DownloadedSize:   int64(fileInfo.Size),
			CreateTime:       custom_type.Now(),
			UpdateTime:       custom_type.Now(),
			FinishTime:       custom_type.Now(),
		}

		if err := f.factory.DownloadTask().Create(ctx, downloadTask); err != nil {
			logger.LOG.Error("创建打包下载任务记录失败", "fileID", fileID, "error", err)
			continue
		}
		if f.taskEvents != nil {
			f.taskEvents.Publish(downloadTaskEvent(downloadTask, "created"), false)
		}

		logger.LOG.Info("创建打包下载任务记录成功", "taskID", taskID, "fileName", userFile.FileName, "packageID", task.PackageID)
	}
}
