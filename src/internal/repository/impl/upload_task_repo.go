package impl

import (
	"context"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"time"

	"gorm.io/gorm"
)

type uploadTaskRepository struct {
	db *gorm.DB
}

// NewUploadTaskRepository 创建上传任务仓储实例
func NewUploadTaskRepository(db *gorm.DB) repository.UploadTaskRepository {
	return &uploadTaskRepository{db: db}
}

// Create 创建上传任务记录
func (r *uploadTaskRepository) Create(ctx context.Context, task *models.UploadTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// GetByID 根据ID获取上传任务
func (r *uploadTaskRepository) GetByID(ctx context.Context, id string) (*models.UploadTask, error) {
	var task models.UploadTask
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// GetByUserID 根据用户ID获取所有上传任务
func (r *uploadTaskRepository) GetByUserID(ctx context.Context, userID string) ([]*models.UploadTask, error) {
	var tasks []*models.UploadTask
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("create_time DESC").Find(&tasks).Error
	return tasks, err
}

// GetUncompletedByUserID 根据用户ID获取未完成的上传任务
func (r *uploadTaskRepository) GetUncompletedByUserID(ctx context.Context, userID string) ([]*models.UploadTask, error) {
	var tasks []*models.UploadTask
	now := time.Now()
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status IN (?) AND expire_time > ?", userID, []string{"pending", "uploading", "processing"}, now).
		Order("create_time DESC").Find(&tasks).Error
	return tasks, err
}

// Update 更新上传任务
func (r *uploadTaskRepository) Update(ctx context.Context, task *models.UploadTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

// Delete 删除上传任务
func (r *uploadTaskRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.UploadTask{}).Error
}

// DeleteExpired 删除过期的上传任务（所有用户）
func (r *uploadTaskRepository) DeleteExpired(ctx context.Context) (int64, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Where("expire_time < ? AND status IN (?)", now, []string{"pending", "uploading", "failed", "aborted"}).
		Delete(&models.UploadTask{})
	return result.RowsAffected, result.Error
}

// DeleteExpiredByUserID 删除指定用户的过期上传任务
func (r *uploadTaskRepository) DeleteExpiredByUserID(ctx context.Context, userID string) (int64, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND expire_time < ? AND status IN (?)", userID, now, []string{"pending", "uploading", "failed", "aborted"}).
		Delete(&models.UploadTask{})
	return result.RowsAffected, result.Error
}

// GetExpiredByUserID 根据用户ID获取过期的上传任务
func (r *uploadTaskRepository) GetExpiredByUserID(ctx context.Context, userID string) ([]*models.UploadTask, error) {
	var tasks []*models.UploadTask
	now := time.Now()
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND expire_time < ? AND status IN (?)", userID, now, []string{"pending", "uploading", "failed", "aborted"}).
		Order("expire_time ASC").Find(&tasks).Error
	return tasks, err
}

func (r *uploadTaskRepository) ListExpired(ctx context.Context) ([]*models.UploadTask, error) {
	var tasks []*models.UploadTask
	now := time.Now()
	err := r.db.WithContext(ctx).
		Where("expire_time < ? AND status IN (?)", now, []string{"pending", "uploading", "failed", "aborted"}).
		Order("expire_time ASC").Find(&tasks).Error
	return tasks, err
}

// ListByUserID 根据用户ID获取上传任务列表
func (r *uploadTaskRepository) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.UploadTask, error) {
	var tasks []*models.UploadTask
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// CountByUserID 统计用户上传任务总数
func (r *uploadTaskRepository) CountByUserID(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.UploadTask{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

// ListByStatus 查询指定状态的上传任务，供后台处理器恢复任务。
func (r *uploadTaskRepository) ListByStatus(ctx context.Context, status string) ([]*models.UploadTask, error) {
	var tasks []*models.UploadTask
	err := r.db.WithContext(ctx).Where("status = ?", status).Order("update_time ASC").Find(&tasks).Error
	return tasks, err
}

// ClaimProcessing 原子地把任务切换为后台处理状态，避免最后几个并发分片重复入队。
func (r *uploadTaskRepository) ClaimProcessing(ctx context.Context, id string, allowedStatuses []string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&models.UploadTask{}).
		Where("id = ? AND status IN ?", id, allowedStatuses).
		Updates(map[string]interface{}{
			"status":           "processing",
			"processing_stage": "queued",
			"error_message":    "",
			"update_time":      custom_type.Now(),
		})
	return result.RowsAffected == 1, result.Error
}
