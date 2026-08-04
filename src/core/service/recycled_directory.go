package service

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (r *RecycledService) restoreDirectory(ctx context.Context, recycled *models.Recycled) (*models.JsonResponse, error) {
	var nodes []*models.RecycledDirectoryNode
	if err := r.factory.DB().Where("recycled_id = ?", recycled.ID).Order("depth ASC, id ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, errors.New("目录回收记录不完整")
	}
	var members []*models.RecycledDirectoryFile
	if err := r.factory.DB().Where("recycled_id = ?", recycled.ID).Find(&members).Error; err != nil {
		return nil, err
	}
	var directoryTags []*models.RecycledDirectoryTag
	if err := r.factory.DB().Where("recycled_id = ?", recycled.ID).Find(&directoryTags).Error; err != nil {
		return nil, err
	}
	targetParentID := recycled.OriginalParentID
	if targetParentID > 0 {
		parent, err := r.factory.Directory().GetByID(ctx, targetParentID)
		if err != nil || parent.UserID != recycled.UserID {
			targetParentID = 0
		}
	}
	if targetParentID == 0 {
		root, err := r.factory.Directory().GetRoot(ctx, recycled.UserID)
		if err != nil {
			return nil, fmt.Errorf("获取根目录失败: %w", err)
		}
		targetParentID = root.ID
	}

	err := r.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		idMap := make(map[int]int, len(nodes))
		for _, node := range nodes {
			parentID := targetParentID
			if node.Depth > 0 {
				mapped, ok := idMap[node.ParentOriginalID]
				if !ok {
					return errors.New("目录层级数据不完整")
				}
				parentID = mapped
			}
			name := node.Name
			if node.Depth == 0 {
				name = availableRestoreFolderName(tx, recycled.UserID, parentID, name)
			}
			created := &models.VirtualDirectory{
				UserID: recycled.UserID, Name: name, ParentID: parentID,
				CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
			}
			if err := tx.Create(created).Error; err != nil {
				return err
			}
			idMap[node.OriginalDirID] = created.ID
		}
		for _, member := range members {
			newDirID, ok := idMap[member.OriginalDirID]
			if !ok {
				return fmt.Errorf("文件目录映射不存在: %s", member.FileID)
			}
			if err := tx.Unscoped().Model(&models.UserFiles{}).
				Where("user_id = ? AND uf_id = ?", recycled.UserID, member.FileID).
				Updates(map[string]interface{}{"directory_id": newDirID, "deleted_at": nil}).Error; err != nil {
				return err
			}
			if r.tagService != nil {
				if err := r.tagService.QueueUserFile(ctx, tx, recycled.UserID, member.FileID); err != nil {
					return err
				}
			}
		}
		for _, tag := range directoryTags {
			newDirID, ok := idMap[tag.OriginalDirID]
			if !ok {
				return fmt.Errorf("标签目录映射不存在: %d", tag.OriginalDirID)
			}
			binding := &models.UserDirectoryTag{
				ID: uuid.NewString(), UserID: recycled.UserID, DirectoryID: newDirID,
				TagID: tag.TagID, CreatedBy: recycled.UserID, CreatedAt: time.Now(),
			}
			if err := tx.Create(binding).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("recycled_id = ?", recycled.ID).Delete(&models.RecycledDirectoryTag{}).Error; err != nil {
			return err
		}
		if err := tx.Where("recycled_id = ?", recycled.ID).Delete(&models.RecycledDirectoryFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("recycled_id = ?", recycled.ID).Delete(&models.RecycledDirectoryNode{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", recycled.ID).Delete(&models.Recycled{}).Error
	})
	if err != nil {
		return nil, fmt.Errorf("恢复目录失败: %w", err)
	}
	message := "目录已还原"
	if targetParentID != recycled.OriginalParentID {
		message = "目录已还原到根目录（原父目录已删除）"
	}
	return models.NewJsonResponse(200, message, nil), nil
}

func availableRestoreFolderName(tx *gorm.DB, userID string, parentID int, original string) string {
	name := original
	for suffix := 0; ; suffix++ {
		var count int64
		_ = tx.Model(&models.VirtualDirectory{}).
			Where("user_id = ? AND parent_id = ? AND name = ?", userID, parentID, name).
			Count(&count).Error
		if count == 0 {
			return name
		}
		if suffix == 0 {
			name = original + " (恢复)"
		} else {
			name = fmt.Sprintf("%s (恢复 %d)", original, suffix+1)
		}
	}
}

func (r *RecycledService) deleteDirectoryRecycled(ctx context.Context, recycled *models.Recycled) error {
	var members []*models.RecycledDirectoryFile
	if err := r.factory.DB().Where("recycled_id = ?", recycled.ID).Find(&members).Error; err != nil {
		return err
	}
	var returnedSize int64
	err := r.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, member := range members {
			var userFile models.UserFiles
			if err := tx.Unscoped().Where("user_id = ? AND uf_id = ?", recycled.UserID, member.FileID).First(&userFile).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			var fileInfo models.FileInfo
			if err := tx.Where("id = ?", userFile.FileID).First(&fileInfo).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if fileInfo.ID != "" {
				returnedSize += int64(fileInfo.Size)
			}
			if err := deleteUserFileTagRecords(tx, recycled.UserID, member.FileID); err != nil {
				return err
			}
			if err := tx.Unscoped().Where("user_id = ? AND uf_id = ?", recycled.UserID, member.FileID).Delete(&models.UserFiles{}).Error; err != nil {
				return err
			}
			var references int64
			if err := tx.Unscoped().Model(&models.UserFiles{}).Where("file_id = ?", userFile.FileID).Count(&references).Error; err != nil {
				return err
			}
			if references == 0 && fileInfo.ID != "" {
				if err := deleteFileMetadataRecords(tx, fileInfo.ID); err != nil {
					return err
				}
				if err := r.deletePhysicalFile(&fileInfo); err != nil {
					return err
				}
				if fileInfo.ThumbnailImg != "" {
					if err := r.deleteThumbnail(fileInfo.ThumbnailImg); err != nil {
						return err
					}
				}
				if fileInfo.IsChunk {
					if err := tx.Where("file_id = ?", fileInfo.ID).Delete(&models.FileChunk{}).Error; err != nil {
						return err
					}
				}
				if err := tx.Where("id = ?", fileInfo.ID).Delete(&models.FileInfo{}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Where("recycled_id = ?", recycled.ID).Delete(&models.RecycledDirectoryFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("recycled_id = ?", recycled.ID).Delete(&models.RecycledDirectoryNode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("recycled_id = ?", recycled.ID).Delete(&models.RecycledDirectoryTag{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", recycled.ID).Delete(&models.Recycled{}).Error; err != nil {
			return err
		}
		if returnedSize > 0 {
			var user models.UserInfo
			if err := tx.Where("id = ?", recycled.UserID).First(&user).Error; err != nil {
				return err
			}
			if user.Space > 0 {
				user.FreeSpace += returnedSize
				if err := tx.Save(&user).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	return err
}
