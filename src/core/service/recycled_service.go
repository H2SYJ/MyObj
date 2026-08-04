package service

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RecycledService 回收站服务
type RecycledService struct {
	factory    *impl.RepositoryFactory
	cacheLocal cache.Cache
	tagService *TagService
}

func (r *RecycledService) SetTagService(service *TagService) { r.tagService = service }

func NewRecycledService(factory *impl.RepositoryFactory, cacheLocal cache.Cache) *RecycledService {
	return &RecycledService{
		factory:    factory,
		cacheLocal: cacheLocal,
	}
}

func (r *RecycledService) GetRepository() *impl.RepositoryFactory {
	return r.factory
}

// GetRecycledList 获取回收站列表
func (r *RecycledService) GetRecycledList(req *request.RecycledListRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	offset := (req.Page - 1) * req.PageSize

	// 查询回收站记录
	recycleds, err := r.factory.Recycled().ListByUserID(ctx, userID, offset, req.PageSize)
	if err != nil {
		logger.LOG.Error("查询回收站列表失败", "error", err, "userID", userID)
		return nil, fmt.Errorf("查询回收站列表失败: %w", err)
	}

	// 统计总数
	total, err := r.factory.Recycled().Count(ctx, userID)
	if err != nil {
		logger.LOG.Error("统计回收站数量失败", "error", err, "userID", userID)
		return nil, fmt.Errorf("统计回收站数量失败: %w", err)
	}

	// 构造响应数据
	items := make([]*response.RecycledItem, 0, len(recycleds))
	for _, recycled := range recycleds {
		itemType := recycled.ItemType
		if itemType == "" {
			itemType = models.RecycledItemTypeFile
		}
		if itemType == models.RecycledItemTypeFolder {
			items = append(items, &response.RecycledItem{
				RecycledID: recycled.ID, ItemType: itemType, ItemName: recycled.ItemName,
				ItemCount: recycled.ItemCount, FileName: recycled.ItemName,
				FileSize: recycled.TotalSize, DeletedAt: recycled.CreatedAt,
			})
			continue
		}
		// 获取用户文件关联，以获取文件名（使用 Unscoped 查询软删除的记录）
		var userFile models.UserFiles
		err = r.factory.DB().Unscoped().Where("user_id = ? AND uf_id = ?", userID, recycled.FileID).First(&userFile).Error
		if err != nil {
			logger.LOG.Warn("获取用户文件关联失败", "error", err, "userID", userID, "fileID", recycled.FileID)
			continue
		}
		fileInfo, err := r.factory.FileInfo().GetByID(ctx, userFile.FileID)
		if err != nil {
			logger.LOG.Warn("获取文件信息失败", "error", err, "userID", userID, "fileID", recycled.FileID)
			continue
		}
		items = append(items, &response.RecycledItem{
			RecycledID:   recycled.ID,
			ItemType:     itemType,
			ItemName:     userFile.FileName,
			ItemCount:    1,
			FileID:       recycled.FileID,
			FileName:     userFile.FileName,
			FileSize:     int64(fileInfo.Size),
			MimeType:     fileInfo.Mime,
			IsEnc:        fileInfo.IsEnc,
			HasThumbnail: fileInfo.ThumbnailImg != "",
			DeletedAt:    recycled.CreatedAt,
		})
	}

	result := &response.RecycledListResponse{
		Items:    items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	return models.NewJsonResponse(200, "获取成功", result), nil
}

// RestoreFile 还原文件
func (r *RecycledService) RestoreFile(req *request.RestoreFileRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 验证回收站记录是否存在且属于该用户
	recycled, err := r.factory.Recycled().GetByID(ctx, req.RecycledID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("回收站记录不存在")
		}
		logger.LOG.Error("获取回收站记录失败", "error", err, "recycledID", req.RecycledID)
		return nil, fmt.Errorf("获取回收站记录失败: %w", err)
	}

	if recycled.UserID != userID {
		logger.LOG.Warn("用户尝试还原他人文件", "userID", userID, "recycledID", req.RecycledID)
		return nil, fmt.Errorf("无权操作此文件")
	}
	if recycled.ItemType == models.RecycledItemTypeFolder {
		return r.restoreDirectory(ctx, recycled)
	}

	// 获取要还原的文件记录（使用 Unscoped 查询软删除的记录）
	var userFile models.UserFiles
	err = r.factory.DB().Unscoped().Where("user_id = ? AND uf_id = ?", userID, recycled.FileID).First(&userFile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("文件记录不存在")
		}
		logger.LOG.Error("获取文件记录失败", "error", err, "userID", userID, "fileID", recycled.FileID)
		return nil, fmt.Errorf("获取文件记录失败: %w", err)
	}

	// 检查父目录是否存在
	targetDirectoryID := userFile.DirectoryID
	parentDirExists := false

	// 如果原目录不存在，则还原到根目录。
	if directory, directoryErr := r.factory.Directory().GetByID(ctx, userFile.DirectoryID); directoryErr == nil && directory.UserID == userID {
		parentDirExists = true
	} else {
		logger.LOG.Warn("文件原父目录已删除，将还原到根目录", "userID", userID, "fileID", recycled.FileID, "original_directory_id", userFile.DirectoryID)
	}

	// 如果父目录不存在，获取根目录ID
	if !parentDirExists {
		rootDirectory, err := r.factory.Directory().GetRoot(ctx, userID)
		if err != nil {
			logger.LOG.Error("获取根目录失败", "error", err, "userID", userID)
			return nil, fmt.Errorf("获取根目录失败: %w", err)
		}
		targetDirectoryID = rootDirectory.ID
		logger.LOG.Info("文件将还原到根目录",
			"userID", userID,
			"fileID", recycled.FileID,
			"original_directory_id", userFile.DirectoryID,
			"new_directory_id", targetDirectoryID)
	}

	// 在事务中恢复文件、必要时更新目录ID并删除回收站记录。
	err = r.factory.DB().Transaction(func(tx *gorm.DB) error {
		txFactory := r.factory.WithTx(tx)

		// 恢复 user_files 软删除（清除 deleted_at）
		// 如果父目录不存在，同时更新目录ID。
		updateMap := map[string]interface{}{
			"deleted_at": nil,
		}
		if !parentDirExists {
			updateMap["directory_id"] = targetDirectoryID
		}

		if err := tx.Model(&models.UserFiles{}).Unscoped().
			Where("user_id = ? AND uf_id = ?", userID, recycled.FileID).
			Updates(updateMap).Error; err != nil {
			return fmt.Errorf("恢复用户文件失败: %w", err)
		}
		if r.tagService != nil {
			if err := r.tagService.QueueUserFile(ctx, tx, userID, recycled.FileID); err != nil {
				return fmt.Errorf("恢复文件标签任务失败: %w", err)
			}
		}

		// 删除回收站记录
		if err := txFactory.Recycled().Delete(ctx, req.RecycledID); err != nil {
			return fmt.Errorf("删除回收站记录失败: %w", err)
		}

		return nil
	})

	if err != nil {
		logger.LOG.Error("还原文件失败", "error", err, "recycledID", req.RecycledID)
		return nil, fmt.Errorf("还原文件失败: %w", err)
	}

	message := "文件已还原"
	if !parentDirExists {
		message = "文件已还原到根目录（原父目录已删除）"
	}

	logger.LOG.Info("文件已还原",
		"recycledID", req.RecycledID,
		"userID", userID,
		"fileID", recycled.FileID,
		"original_directory_id", userFile.DirectoryID,
		"new_directory_id", targetDirectoryID)
	return models.NewJsonResponse(200, message, nil), nil
}

// DeletePermanently 永久删除文件
func (r *RecycledService) DeletePermanently(req *request.DeleteRecycledRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 验证回收站记录
	recycled, err := r.factory.Recycled().GetByID(ctx, req.RecycledID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("回收站记录不存在")
		}
		logger.LOG.Error("获取回收站记录失败", "error", err, "recycledID", req.RecycledID)
		return nil, fmt.Errorf("获取回收站记录失败: %w", err)
	}

	if recycled.UserID != userID {
		logger.LOG.Warn("用户尝试删除他人文件", "userID", userID, "recycledID", req.RecycledID)
		return nil, fmt.Errorf("无权操作此文件")
	}
	if recycled.ItemType == models.RecycledItemTypeFolder {
		if err := r.deleteDirectoryRecycled(ctx, recycled); err != nil {
			return nil, err
		}
		return models.NewJsonResponse(200, "目录已永久删除", nil), nil
	}

	// 执行永久删除
	if err := r.deleteSingleFile(ctx, recycled); err != nil {
		logger.LOG.Error("永久删除文件失败", "error", err, "recycledID", req.RecycledID)
		return nil, fmt.Errorf("永久删除文件失败: %w", err)
	}

	logger.LOG.Info("文件已永久删除", "recycledID", req.RecycledID, "userID", userID, "fileID", recycled.FileID)
	return models.NewJsonResponse(200, "文件已永久删除", nil), nil
}

// BatchRestore 批量还原回收站记录，单项失败不影响其他项目。
func (r *RecycledService) BatchRestore(req *request.BatchRecycledRequest, userID string) *models.JsonResponse {
	return r.batchOperate(req.RecycledIDs, func(recycledID string) error {
		_, err := r.RestoreFile(&request.RestoreFileRequest{RecycledID: recycledID}, userID)
		return err
	}, "批量还原完成")
}

// BatchDeletePermanently 批量彻底删除回收站记录，单项失败不影响其他项目。
func (r *RecycledService) BatchDeletePermanently(req *request.BatchRecycledRequest, userID string) *models.JsonResponse {
	return r.batchOperate(req.RecycledIDs, func(recycledID string) error {
		_, err := r.DeletePermanently(&request.DeleteRecycledRequest{RecycledID: recycledID}, userID)
		return err
	}, "批量彻底删除完成")
}

func (r *RecycledService) batchOperate(
	recycledIDs []string,
	operation func(string) error,
	message string,
) *models.JsonResponse {
	result := &response.BatchOperationResponse{FailedItems: make([]response.BatchOperationFailedItem, 0)}
	seen := make(map[string]struct{}, len(recycledIDs))
	for _, recycledID := range recycledIDs {
		if _, exists := seen[recycledID]; exists {
			continue
		}
		seen[recycledID] = struct{}{}
		result.TotalCount++
		if err := operation(recycledID); err != nil {
			result.FailedCount++
			result.FailedItems = append(result.FailedItems, response.BatchOperationFailedItem{
				ItemID: recycledID,
				Reason: err.Error(),
			})
			continue
		}
		result.SuccessCount++
	}
	return models.NewJsonResponse(200, message, result)
}

// EmptyRecycled 清空回收站
func (r *RecycledService) EmptyRecycled(userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 获取该用户的所有回收站记录
	recycleds, err := r.factory.Recycled().ListByUserID(ctx, userID, 0, -1)
	if err != nil {
		logger.LOG.Error("查询回收站列表失败", "error", err, "userID", userID)
		return nil, fmt.Errorf("查询回收站列表失败: %w", err)
	}

	deletedCount := 0
	failedCount := 0

	// 逐个删除
	for _, recycled := range recycleds {
		if err := r.PurgeRecord(ctx, recycled); err != nil {
			logger.LOG.Error("删除文件失败", "error", err, "recycledID", recycled.ID)
			failedCount++
		} else {
			deletedCount++
		}
	}

	logger.LOG.Info("清空回收站完成",
		"userID", userID,
		"deleted", deletedCount,
		"failed", failedCount)

	message := fmt.Sprintf("已清空回收站，成功删除 %d 个文件", deletedCount)
	if failedCount > 0 {
		message = fmt.Sprintf("%s，失败 %d 个", message, failedCount)
	}

	return models.NewJsonResponse(200, message, map[string]int{
		"deleted": deletedCount,
		"failed":  failedCount,
	}), nil
}

// PurgeRecord 永久清理一个回收站条目，供接口和定时任务共用。
func (r *RecycledService) PurgeRecord(ctx context.Context, recycled *models.Recycled) error {
	if recycled.ItemType == models.RecycledItemTypeFolder {
		return r.deleteDirectoryRecycled(ctx, recycled)
	}
	return r.deleteSingleFile(ctx, recycled)
}

// MoveToRecycled 将文件移动到回收站
func (r *RecycledService) MoveToRecycled(fileID, userID string) error {
	ctx := context.Background()

	// 创建回收站记录
	recycled := &models.Recycled{
		ID:        uuid.Must(uuid.NewV7()).String(),
		FileID:    fileID,
		UserID:    userID,
		CreatedAt: custom_type.Now(),
	}

	if err := r.factory.Recycled().Create(ctx, recycled); err != nil {
		logger.LOG.Error("创建回收站记录失败", "error", err, "fileID", fileID, "userID", userID)
		return fmt.Errorf("移动到回收站失败: %w", err)
	}

	logger.LOG.Info("文件已移动到回收站", "fileID", fileID, "userID", userID)
	return nil
}

// deleteSingleFile 删除单个文件（参考定时任务的逻辑）
func (r *RecycledService) deleteSingleFile(ctx context.Context, recycled *models.Recycled) error {
	// 1. 检查文件引用数
	var userFile *models.UserFiles
	err := r.factory.DB().Unscoped().Where("user_id = ? AND uf_id = ?", recycled.UserID, recycled.FileID).First(&userFile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.LOG.Warn("用户文件记录不存在，直接删除回收站记录", "file_id", recycled.FileID)
			return r.factory.Recycled().Delete(ctx, recycled.ID)
		}
		logger.LOG.Error("获取用户文件记录失败", "error", err, "file_id", recycled.FileID)
		return err
	}
	refCount, err := r.factory.Recycled().CountFileReferences(ctx, userFile.FileID)
	if err != nil {
		return fmt.Errorf("统计文件引用失败: %w", err)
	}

	// 2. 获取文件信息
	fileInfo, err := r.factory.FileInfo().GetByID(ctx, userFile.FileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return r.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := deleteUserFileTagRecords(tx, recycled.UserID, recycled.FileID); err != nil {
					return err
				}
				if err := tx.Unscoped().Where("user_id = ? AND uf_id = ?", recycled.UserID, recycled.FileID).
					Delete(&models.UserFiles{}).Error; err != nil {
					return err
				}
				return tx.Where("id = ?", recycled.ID).Delete(&models.Recycled{}).Error
			})
		}
		return fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 3. 获取用户信息（用于空间归还）
	user, err := r.factory.User().GetByID(ctx, recycled.UserID)
	if err != nil {
		return fmt.Errorf("获取用户信息失败: %w", err)
	}

	// 4. 在事务中移除当前用户关联；只有最后一个引用才删除物理对象和文件信息。
	return r.factory.DB().Transaction(func(tx *gorm.DB) error {
		txFactory := r.factory.WithTx(tx)

		if refCount <= 1 {
			if err := r.deletePhysicalFile(fileInfo); err != nil {
				logger.LOG.Warn("删除物理文件失败", "error", err)
			}
			if fileInfo.ThumbnailImg != "" {
				if err := r.deleteThumbnail(fileInfo.ThumbnailImg); err != nil {
					logger.LOG.Warn("删除缩略图失败", "error", err)
				}
			}
		}
		if err := deleteUserFileTagRecords(tx, recycled.UserID, recycled.FileID); err != nil {
			return fmt.Errorf("删除文件标签失败: %w", err)
		}

		if err := tx.Unscoped().Where("user_id = ? AND uf_id = ?", recycled.UserID, recycled.FileID).
			Delete(&models.UserFiles{}).Error; err != nil {
			return fmt.Errorf("删除用户文件关联失败: %w", err)
		}

		if refCount <= 1 {
			if err := deleteFileMetadataRecords(tx, userFile.FileID); err != nil {
				return fmt.Errorf("删除文件元数据失败: %w", err)
			}
			if fileInfo.IsChunk {
				if err := txFactory.FileChunk().DeleteByFileID(ctx, userFile.FileID); err != nil {
					return fmt.Errorf("删除文件分片记录失败: %w", err)
				}
			}
			if err := txFactory.FileInfo().Delete(ctx, userFile.FileID); err != nil {
				return fmt.Errorf("删除文件信息记录失败: %w", err)
			}
		}

		if err := txFactory.Recycled().Delete(ctx, recycled.ID); err != nil {
			return fmt.Errorf("删除回收站记录失败: %w", err)
		}

		if user.Space > 0 {
			user.FreeSpace += int64(fileInfo.Size)
			if err := txFactory.User().Update(ctx, user); err != nil {
				return fmt.Errorf("更新用户空间失败: %w", err)
			}
			logger.LOG.Debug("归还用户空间",
				"user_id", user.ID,
				"returned_size", fileInfo.Size,
				"new_free_space", user.FreeSpace)
		}
		return nil
	})
}

// deletePhysicalFile 删除物理文件
func (r *RecycledService) deletePhysicalFile(fileInfo *models.FileInfo) error {
	// 如果有加密文件，优先删除加密文件
	if fileInfo.IsEnc && fileInfo.EncPath != "" {
		if err := r.deleteFile(fileInfo.EncPath); err != nil {
			logger.LOG.Warn("删除加密文件失败", "path", fileInfo.EncPath, "error", err)
		}
		// 删除.info文件
		infoPath := fileInfo.EncPath + ".info"
		if err := r.deleteFile(infoPath); err != nil {
			logger.LOG.Warn("删除.info文件失败", "path", infoPath, "error", err)
		}
	}

	// 删除普通文件
	if fileInfo.Path != "" {
		if err := r.deleteFile(fileInfo.Path); err != nil {
			logger.LOG.Warn("删除普通文件失败", "path", fileInfo.Path, "error", err)
		}
		ext := filepath.Ext(fileInfo.Path)
		basePath := strings.TrimSuffix(fileInfo.Path, ext)
		infoPath := basePath + ".info"
		if err := r.deleteFile(infoPath); err != nil {
			logger.LOG.Warn("删除.info文件失败", "path", infoPath, "error", err)
		}

	}

	// 如果是分片文件，删除分片目录
	if fileInfo.IsChunk && fileInfo.Path != "" {
		// 文件路径格式: {DataPath}/data/{原文件名不带后缀}/{虚拟文件名}.data
		// 分片目录为: {DataPath}/data/{原文件名不带后缀}/{虚拟文件名}
		chunkDir := strings.TrimSuffix(fileInfo.Path, ".data")
		if err := r.deleteDirectory(chunkDir); err != nil {
			logger.LOG.Warn("删除分片目录失败", "path", chunkDir, "error", err)
		}
		// 删除父目录（如果为空）
		// 路径格式: {DataPath}/data/{原文件名不带后缀}
		parentDir := filepath.Dir(fileInfo.Path)
		if err := r.deleteDirectoryIfEmpty(parentDir); err != nil {
			logger.LOG.Warn("删除父目录失败", "path", parentDir, "error", err)
		}
	} else if fileInfo.Path != "" {
		// 对于非分片文件，删除 .data 文件所在的文件夹（如果为空）
		// 路径格式: {DataPath}/data/{原文件名不带后缀}/{虚拟文件名}.data
		parentDir := filepath.Dir(fileInfo.Path)
		if err := r.deleteDirectoryIfEmpty(parentDir); err != nil {
			logger.LOG.Warn("删除文件夹失败", "path", parentDir, "error", err)
		}
	}

	return nil
}

// deleteThumbnail 删除缩略图
func (r *RecycledService) deleteThumbnail(thumbnailPath string) error {
	return r.deleteFile(thumbnailPath)
}

// deleteFile 删除文件
func (r *RecycledService) deleteFile(filePath string) error {
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
func (r *RecycledService) deleteDirectory(dirPath string) error {
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
func (r *RecycledService) deleteDirectoryIfEmpty(dirPath string) error {
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
