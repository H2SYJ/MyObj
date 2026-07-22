package task

import (
	"context"
	"fmt"
	"myobj/src/core/service"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/upload"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RecycledTask 回收站 定时任务
type RecycledTask struct {
	factory *impl.RepositoryFactory
}

// NewRecycledTask 创建回收站定时任务
func NewRecycledTask(factory *impl.RepositoryFactory) *RecycledTask {
	rt := &RecycledTask{
		factory: factory,
	}
	return rt
}

// CleanupExpiredFiles 清理过期的回收站文件
// days: 保留天数，超过该天数的文件将被清理
func (t *RecycledTask) CleanupExpiredFiles(days int) error {
	ctx := context.Background()
	logger.LOG.Info("开始执行回收站清理任务", "days", days)

	// 1. 获取超过指定天数的回收站记录
	expiredRecords, err := t.factory.Recycled().GetExpiredRecords(ctx, days)
	if err != nil {
		logger.LOG.Error("获取过期回收站记录失败", "error", err)
		return fmt.Errorf("获取过期回收站记录失败: %w", err)
	}

	if len(expiredRecords) == 0 {
		logger.LOG.Info("没有需要清理的过期文件")
		return nil
	}

	logger.LOG.Info("找到过期回收站记录", "count", len(expiredRecords))

	// 2. 逐个处理过期记录
	successCount := 0
	failCount := 0

	for _, record := range expiredRecords {
		if err := t.processExpiredRecord(ctx, record); err != nil {
			logger.LOG.Error("处理过期记录失败",
				"record_id", record.ID,
				"file_id", record.FileID,
				"user_id", record.UserID,
				"error", err)
			failCount++
		} else {
			successCount++
		}
	}

	logger.LOG.Info("回收站清理任务完成",
		"total", len(expiredRecords),
		"success", successCount,
		"failed", failCount)

	return nil
}

// processExpiredRecord 处理单个过期记录
func (t *RecycledTask) processExpiredRecord(ctx context.Context, record *models.Recycled) error {
	return service.NewRecycledService(t.factory, nil).PurgeRecord(ctx, record)
}

// deletePhysicalFile 删除物理文件
func (t *RecycledTask) deletePhysicalFile(fileInfo *models.FileInfo) error {
	// 如果有加密文件，优先删除加密文件
	if fileInfo.IsEnc && fileInfo.EncPath != "" {
		if err := t.deleteFile(fileInfo.EncPath); err != nil {
			logger.LOG.Warn("删除加密文件失败", "path", fileInfo.EncPath, "error", err)
		}
		// 删除.info文件
		infoPath := fileInfo.EncPath + ".info"
		if err := t.deleteFile(infoPath); err != nil {
			logger.LOG.Warn("删除.info文件失败", "path", infoPath, "error", err)
		}
	}

	// 删除普通文件
	if fileInfo.Path != "" {
		if err := t.deleteFile(fileInfo.Path); err != nil {
			logger.LOG.Warn("删除普通文件失败", "path", fileInfo.Path, "error", err)
		}
		// 删除.info文件（对于非加密文件）
		if !fileInfo.IsEnc {
			infoPath := fileInfo.Path + ".info"
			if err := t.deleteFile(infoPath); err != nil {
				logger.LOG.Warn("删除.info文件失败", "path", infoPath, "error", err)
			}
		}
	}

	// 如果是分片文件，删除分片目录
	if fileInfo.IsChunk && fileInfo.Path != "" {
		// 文件路径格式: {DataPath}/data/{原文件名不带后缀}/{虚拟文件名}.data
		// 分片目录为: {DataPath}/data/{原文件名不带后缀}/{虚拟文件名}
		chunkDir := strings.TrimSuffix(fileInfo.Path, ".data")
		if err := t.deleteDirectory(chunkDir); err != nil {
			logger.LOG.Warn("删除分片目录失败", "path", chunkDir, "error", err)
		}
		// 删除父目录（如果为空）
		// 路径格式: {DataPath}/data/{原文件名不带后缀}
		parentDir := filepath.Dir(fileInfo.Path)
		if err := t.deleteDirectoryIfEmpty(parentDir); err != nil {
			logger.LOG.Warn("删除父目录失败", "path", parentDir, "error", err)
		}
	} else if fileInfo.Path != "" {
		// 对于非分片文件，删除 .data 文件所在的文件夹（如果为空）
		// 路径格式: {DataPath}/data/{原文件名不带后缀}/{虚拟文件名}.data
		parentDir := filepath.Dir(fileInfo.Path)
		if err := t.deleteDirectoryIfEmpty(parentDir); err != nil {
			logger.LOG.Warn("删除文件夹失败", "path", parentDir, "error", err)
		}
	}

	return nil
}

// deleteThumbnail 删除缩略图
func (t *RecycledTask) deleteThumbnail(thumbnailPath string) error {
	return t.deleteFile(thumbnailPath)
}

// deleteFile 删除文件
func (t *RecycledTask) deleteFile(filePath string) error {
	if filePath == "" {
		return nil
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logger.LOG.Debug("文件不存在，跳过删除", "path", filePath)
		return nil
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("删除文件失败 %s: %w", filePath, err)
	}

	logger.LOG.Debug("成功删除文件", "path", filePath)
	return nil
}

// deleteDirectory 删除目录
func (t *RecycledTask) deleteDirectory(dirPath string) error {
	if dirPath == "" {
		return nil
	}

	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		logger.LOG.Debug("目录不存在，跳过删除", "path", dirPath)
		return nil
	}

	if err := os.RemoveAll(dirPath); err != nil {
		return fmt.Errorf("删除目录失败 %s: %w", dirPath, err)
	}

	logger.LOG.Debug("成功删除目录", "path", dirPath)
	return nil
}

// deleteDirectoryIfEmpty 删除空目录（如果目录为空）
func (t *RecycledTask) deleteDirectoryIfEmpty(dirPath string) error {
	if dirPath == "" {
		return nil
	}

	// 检查目录是否存在
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		logger.LOG.Debug("目录不存在，跳过删除", "path", dirPath)
		return nil
	}

	// 读取目录内容
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("读取目录失败 %s: %w", dirPath, err)
	}

	// 如果目录不为空，不删除
	if len(entries) > 0 {
		logger.LOG.Debug("目录不为空，跳过删除", "path", dirPath, "file_count", len(entries))
		return nil
	}

	// 删除空目录
	if err := os.Remove(dirPath); err != nil {
		return fmt.Errorf("删除空目录失败 %s: %w", dirPath, err)
	}

	logger.LOG.Debug("成功删除空目录", "path", dirPath)
	return nil
}

// StartScheduledCleanup 启动定时清理任务
// days: 保留天数
// interval: 执行间隔（例如每天1次）
func (t *RecycledTask) StartScheduledCleanup(days int, interval time.Duration) {
	logger.LOG.Info("启动回收站定时清理任务",
		"retention_days", days,
		"interval", interval)

	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := t.CleanupExpiredFiles(days); err != nil {
				logger.LOG.Error("定时清理任务执行失败", "error", err)
			}
		}
	}()
}

// UploadTask 上传任务定时任务
type UploadTask struct {
	factory *impl.RepositoryFactory
}

// NewUploadTask 创建上传任务定时任务
func NewUploadTask(factory *impl.RepositoryFactory) *UploadTask {
	return &UploadTask{
		factory: factory,
	}
}

// CleanupExpiredTasks 清理过期的上传任务
func (t *UploadTask) CleanupExpiredTasks() error {
	ctx := context.Background()
	logger.LOG.Info("开始执行上传任务清理任务")
	tasks, err := t.factory.UploadTask().ListExpired(ctx)
	if err != nil {
		return fmt.Errorf("查询过期上传任务失败: %w", err)
	}
	for _, uploadTask := range tasks {
		if err := upload.CleanupTaskTempDir(uploadTask.TempDir); err != nil {
			return fmt.Errorf("清理过期上传临时目录失败: %w", err)
		}
	}

	count, err := t.factory.UploadTask().DeleteExpired(ctx)
	if err != nil {
		logger.LOG.Error("清理过期上传任务失败", "error", err)
		return fmt.Errorf("清理过期上传任务失败: %w", err)
	}

	if count > 0 {
		logger.LOG.Info("上传任务清理完成", "cleaned_count", count)
	} else {
		logger.LOG.Debug("没有需要清理的过期上传任务")
	}

	return nil
}

// StartScheduledCleanup 启动定时清理任务
// interval: 执行间隔（例如每天1次）
func (t *UploadTask) StartScheduledCleanup(interval time.Duration) {
	logger.LOG.Info("启动上传任务定时清理任务", "interval", interval)

	// 在启动定时任务前，先确保表存在
	db := t.factory.DB()
	if db != nil {
		logger.LOG.Info("检查 upload_task 表是否存在...")
		if err := db.AutoMigrate(&models.UploadTask{}); err != nil {
			logger.LOG.Warn("创建 upload_task 表失败（可能已存在）", "error", err)
		} else {
			logger.LOG.Info("✓ upload_task 表已创建或已存在")
		}
	}

	ticker := time.NewTicker(interval)
	go func() {
		// 启动时立即执行一次
		if err := t.CleanupExpiredTasks(); err != nil {
			logger.LOG.Error("定时清理任务执行失败", "error", err)
		}

		// 然后按间隔执行
		for range ticker.C {
			if err := t.CleanupExpiredTasks(); err != nil {
				logger.LOG.Error("定时清理任务执行失败", "error", err)
			}
		}
	}()
}
