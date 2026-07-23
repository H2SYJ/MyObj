package impl

import (
	"context"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"

	"gorm.io/gorm"
)

type directoryRepository struct{ db *gorm.DB }

func NewDirectoryRepository(db *gorm.DB) repository.DirectoryRepository {
	return &directoryRepository{db: db}
}

func (r *directoryRepository) Create(ctx context.Context, directory *models.VirtualDirectory) error {
	return r.db.WithContext(ctx).Create(directory).Error
}

func (r *directoryRepository) GetByID(ctx context.Context, id int) (*models.VirtualDirectory, error) {
	var directory models.VirtualDirectory
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&directory).Error; err != nil {
		return nil, err
	}
	return &directory, nil
}

func (r *directoryRepository) GetChild(ctx context.Context, userID string, parentID int, name string) (*models.VirtualDirectory, error) {
	var directory models.VirtualDirectory
	if err := r.db.WithContext(ctx).Where("user_id = ? AND parent_id = ? AND name = ?", userID, parentID, name).First(&directory).Error; err != nil {
		return nil, err
	}
	return &directory, nil
}

func (r *directoryRepository) Update(ctx context.Context, directory *models.VirtualDirectory) error {
	return r.db.WithContext(ctx).Save(directory).Error
}

func (r *directoryRepository) Delete(ctx context.Context, id int) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.VirtualDirectory{}).Error
}

func (r *directoryRepository) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.VirtualDirectory, error) {
	var directories []*models.VirtualDirectory
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Offset(offset).Limit(limit).Find(&directories).Error
	return directories, err
}

func (r *directoryRepository) Count(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.VirtualDirectory{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

func (r *directoryRepository) ListChildren(ctx context.Context, userID string, parentID int, offset, limit int) ([]*models.VirtualDirectory, error) {
	return r.ListChildrenSorted(ctx, userID, parentID, "name", "asc", offset, limit)
}

func (r *directoryRepository) ListChildrenSorted(ctx context.Context, userID string, parentID int, sortBy, sortOrder string, offset, limit int) ([]*models.VirtualDirectory, error) {
	var directories []*models.VirtualDirectory
	direction := "ASC"
	if sortOrder == "desc" {
		direction = "DESC"
	}
	column := "name"
	if sortBy == "time" {
		column = "created_at"
	}
	err := r.db.WithContext(ctx).Where("user_id = ? AND parent_id = ?", userID, parentID).
		Order(column + " " + direction + ", id ASC").Offset(offset).Limit(limit).Find(&directories).Error
	return directories, err
}

func (r *directoryRepository) CountSubFoldersByParentID(ctx context.Context, userID string, parentID int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.VirtualDirectory{}).Where("user_id = ? AND parent_id = ?", userID, parentID).Count(&count).Error
	return count, err
}

func (r *directoryRepository) GetRoot(ctx context.Context, userID string) (*models.VirtualDirectory, error) {
	var directory models.VirtualDirectory
	if err := r.db.WithContext(ctx).Where("user_id = ? AND parent_id = 0 AND name = ''", userID).First(&directory).Error; err != nil {
		return nil, err
	}
	return &directory, nil
}
