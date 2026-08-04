package service

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/core/domain/request"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MoveItems 原子移动文件和目录。
func (f *FileService) MoveItems(req *request.MoveItemsRequest, userID string) (*models.JsonResponse, error) {
	fileIDs := uniqueStrings(req.FileIDs)
	dirIDs := uniqueInts(req.DirectoryIDs)
	if len(fileIDs) == 0 && len(dirIDs) == 0 {
		return models.NewJsonResponse(400, "请选择要移动的文件或目录", nil), nil
	}
	targetID := req.TargetDirectoryID
	if targetID <= 0 {
		return models.NewJsonResponse(400, "目标目录无效", nil), nil
	}

	ctx := context.Background()
	err := f.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txFactory := f.factory.WithTx(tx)
		target, err := txFactory.Directory().GetByID(ctx, targetID)
		if err != nil || target.UserID != userID {
			return errors.New("目标目录不存在或无权访问")
		}

		for _, fileID := range fileIDs {
			userFile, err := txFactory.UserFiles().GetByUserIDAndUfID(ctx, userID, fileID)
			if err != nil {
				return fmt.Errorf("文件不存在或无权访问: %s", fileID)
			}
			if userFile.DirectoryID == targetID {
				return fmt.Errorf("文件“%s”已在目标目录", userFile.FileName)
			}
			var count int64
			if err := tx.Model(&models.UserFiles{}).
				Where("user_id = ? AND directory_id = ? AND file_name = ? AND uf_id <> ? AND deleted_at IS NULL", userID, targetID, userFile.FileName, fileID).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("目标目录已存在文件“%s”", userFile.FileName)
			}
			if err := tx.Model(&models.UserFiles{}).
				Where("user_id = ? AND uf_id = ?", userID, fileID).
				Update("directory_id", targetID).Error; err != nil {
				return err
			}
		}

		for _, dirID := range dirIDs {
			dir, err := txFactory.Directory().GetByID(ctx, dirID)
			if err != nil || dir.UserID != userID {
				return fmt.Errorf("目录不存在或无权访问: %d", dirID)
			}
			if dir.ParentID == 0 {
				return errors.New("根目录不能移动")
			}
			if dir.ParentID == targetID {
				return fmt.Errorf("目录“%s”已在目标位置", dir.Name)
			}
			if dirID == targetID || directoryContains(ctx, txFactory, userID, dirID, targetID) {
				return fmt.Errorf("不能将目录“%s”移动到自身或其子目录", dir.Name)
			}
			var count int64
			if err := tx.Model(&models.VirtualDirectory{}).
				Where("user_id = ? AND parent_id = ? AND name = ? AND id <> ?", userID, targetID, dir.Name, dirID).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("目标目录已存在目录“%s”", dir.Name)
			}
			if err := tx.Model(&models.VirtualDirectory{}).Where("id = ? AND user_id = ?", dirID, userID).
				Updates(map[string]interface{}{"parent_id": targetID, "updated_at": custom_type.Now()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return models.NewJsonResponse(400, err.Error(), nil), nil
	}
	return models.NewJsonResponse(200, "移动成功", map[string]int{"files": len(fileIDs), "folders": len(dirIDs)}), nil
}

// DeleteItems 将文件和完整目录树移动到回收站。
func (f *FileService) DeleteItems(req *request.DeleteItemsRequest, userID string) (*models.JsonResponse, error) {
	fileIDs := uniqueStrings(req.FileIDs)
	dirIDs := uniqueInts(req.DirIDs)
	if len(fileIDs) == 0 && len(dirIDs) == 0 {
		return models.NewJsonResponse(400, "请选择要删除的文件或目录", nil), nil
	}

	ctx := context.Background()
	err := f.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txFactory := f.factory.WithTx(tx)
		rootDirIDs, err := filterNestedDirectories(ctx, txFactory, userID, dirIDs)
		if err != nil {
			return err
		}
		containedFiles := make(map[string]struct{})
		for _, dirID := range rootDirIDs {
			members, err := recycleDirectoryTree(ctx, txFactory, tx, userID, dirID)
			if err != nil {
				return err
			}
			for _, fileID := range members {
				containedFiles[fileID] = struct{}{}
			}
		}
		for _, fileID := range fileIDs {
			if _, included := containedFiles[fileID]; included {
				continue
			}
			if err := recycleSingleUserFile(ctx, txFactory, tx, userID, fileID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return models.NewJsonResponse(400, err.Error(), nil), nil
	}
	return models.NewJsonResponse(200, "已移动到回收站", nil), nil
}

// DeleteDir 保持原接口兼容，目录现在作为整体进入回收站。
func (f *FileService) DeleteDir(req *request.DeleteDirRequest, userID string) (*models.JsonResponse, error) {
	return f.DeleteItems(&request.DeleteItemsRequest{DirIDs: []int{req.DirID}}, userID)
}

func recycleSingleUserFile(ctx context.Context, factory *impl.RepositoryFactory, tx *gorm.DB, userID, fileID string) error {
	userFile, err := factory.UserFiles().GetByUserIDAndUfID(ctx, userID, fileID)
	if err != nil {
		return fmt.Errorf("文件不存在或无权访问: %s", fileID)
	}
	var existing int64
	if err := tx.Model(&models.Recycled{}).Where("user_id = ? AND file_id = ?", userID, fileID).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return fmt.Errorf("文件“%s”已在回收站", userFile.FileName)
	}
	fileInfo, err := factory.FileInfo().GetByID(ctx, userFile.FileID)
	if err != nil {
		return err
	}
	recycled := &models.Recycled{
		ID: uuid.Must(uuid.NewV7()).String(), FileID: fileID, UserID: userID,
		ItemType: models.RecycledItemTypeFile, ItemName: userFile.FileName,
		TotalSize: int64(fileInfo.Size), ItemCount: 1, CreatedAt: custom_type.Now(),
	}
	if err := factory.Recycled().Create(ctx, recycled); err != nil {
		return err
	}
	return tx.Where("user_id = ? AND uf_id = ?", userID, fileID).Delete(&models.UserFiles{}).Error
}

func recycleDirectoryTree(ctx context.Context, factory *impl.RepositoryFactory, tx *gorm.DB, userID string, rootID int) ([]string, error) {
	root, err := factory.Directory().GetByID(ctx, rootID)
	if err != nil || root.UserID != userID {
		return nil, fmt.Errorf("目录不存在或无权访问: %d", rootID)
	}
	if root.ParentID == 0 {
		return nil, errors.New("根目录不能删除")
	}
	type nodeWithDepth struct {
		Node  *models.VirtualDirectory
		Depth int
	}
	nodes := []nodeWithDepth{{Node: root, Depth: 0}}
	for i := 0; i < len(nodes); i++ {
		children, err := factory.Directory().ListChildren(ctx, userID, nodes[i].Node.ID, 0, -1)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			nodes = append(nodes, nodeWithDepth{Node: child, Depth: nodes[i].Depth + 1})
		}
	}
	dirIDs := make([]int, 0, len(nodes))
	for _, node := range nodes {
		dirIDs = append(dirIDs, node.Node.ID)
	}
	var files []*models.UserFiles
	if err := tx.Where("user_id = ? AND directory_id IN ? AND deleted_at IS NULL", userID, dirIDs).Find(&files).Error; err != nil {
		return nil, err
	}

	recycledID := uuid.Must(uuid.NewV7()).String()
	parentID := root.ParentID
	var totalSize int64
	for _, file := range files {
		var info models.FileInfo
		if err := tx.Where("id = ?", file.FileID).First(&info).Error; err != nil {
			return nil, err
		}
		totalSize += int64(info.Size)
	}
	recycled := &models.Recycled{
		ID: recycledID, FileID: "", UserID: userID, ItemType: models.RecycledItemTypeFolder,
		ItemName: root.Name, OriginalParentID: parentID,
		TotalSize: totalSize, ItemCount: len(nodes) + len(files), CreatedAt: custom_type.Now(),
	}
	if err := factory.Recycled().Create(ctx, recycled); err != nil {
		return nil, err
	}
	for _, node := range nodes {
		record := &models.RecycledDirectoryNode{
			RecycledID: recycledID, OriginalDirID: node.Node.ID, ParentOriginalID: node.Node.ParentID,
			Name: node.Node.Name, Depth: node.Depth,
		}
		if err := tx.Create(record).Error; err != nil {
			return nil, err
		}
	}
	var directoryTags []models.UserDirectoryTag
	if err := tx.Where("user_id = ? AND directory_id IN ?", userID, dirIDs).Find(&directoryTags).Error; err != nil {
		return nil, err
	}
	for _, tag := range directoryTags {
		if err := tx.Create(&models.RecycledDirectoryTag{
			RecycledID: recycledID, OriginalDirID: tag.DirectoryID, TagID: tag.TagID,
		}).Error; err != nil {
			return nil, err
		}
	}
	if len(dirIDs) > 0 {
		if err := tx.Where("user_id = ? AND directory_id IN ?", userID, dirIDs).Delete(&models.UserDirectoryTag{}).Error; err != nil {
			return nil, err
		}
	}
	memberIDs := make([]string, 0, len(files))
	for _, file := range files {
		if err := tx.Create(&models.RecycledDirectoryFile{
			RecycledID: recycledID, FileID: file.UfID, OriginalDirID: file.DirectoryID,
		}).Error; err != nil {
			return nil, err
		}
		memberIDs = append(memberIDs, file.UfID)
	}
	if len(memberIDs) > 0 {
		if err := tx.Where("user_id = ? AND uf_id IN ?", userID, memberIDs).Delete(&models.UserFiles{}).Error; err != nil {
			return nil, err
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Depth > nodes[j].Depth })
	for _, node := range nodes {
		if err := tx.Unscoped().Where("id = ? AND user_id = ?", node.Node.ID, userID).Delete(&models.VirtualDirectory{}).Error; err != nil {
			return nil, err
		}
	}
	return memberIDs, nil
}

func filterNestedDirectories(ctx context.Context, factory *impl.RepositoryFactory, userID string, dirIDs []int) ([]int, error) {
	selected := make(map[int]struct{}, len(dirIDs))
	for _, id := range dirIDs {
		selected[id] = struct{}{}
	}
	result := make([]int, 0, len(dirIDs))
	for _, id := range dirIDs {
		dir, err := factory.Directory().GetByID(ctx, id)
		if err != nil || dir.UserID != userID {
			return nil, fmt.Errorf("目录不存在或无权访问: %d", id)
		}
		parent := dir.ParentID
		nested := false
		for parent > 0 {
			if _, ok := selected[parent]; ok {
				nested = true
				break
			}
			ancestor, err := factory.Directory().GetByID(ctx, parent)
			if err != nil || ancestor.UserID != userID {
				break
			}
			parent = ancestor.ParentID
		}
		if !nested {
			result = append(result, id)
		}
	}
	return result, nil
}

func directoryContains(ctx context.Context, factory *impl.RepositoryFactory, userID string, ancestorID, candidateID int) bool {
	currentID := candidateID
	for currentID > 0 {
		if currentID == ancestorID {
			return true
		}
		current, err := factory.Directory().GetByID(ctx, currentID)
		if err != nil || current.UserID != userID {
			return false
		}
		currentID = current.ParentID
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cleanFolderName(name string) string {
	return strings.TrimPrefix(name, "/")
}
