package impl

import (
	"context"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"time"

	"gorm.io/gorm"
)

type downloadTaskRepository struct {
	db *gorm.DB
}

// NewDownloadTaskRepository 创建下载任务仓储实例
func NewDownloadTaskRepository(db *gorm.DB) repository.DownloadTaskRepository {
	return &downloadTaskRepository{db: db}
}

func (r *downloadTaskRepository) Create(ctx context.Context, task *models.DownloadTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *downloadTaskRepository) GetByID(ctx context.Context, id string) (*models.DownloadTask, error) {
	var task models.DownloadTask
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *downloadTaskRepository) Update(ctx context.Context, task *models.DownloadTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *downloadTaskRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.DownloadTask{}).Error
}

func (r *downloadTaskRepository) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.DownloadTask, error) {
	var tasks []*models.DownloadTask
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *downloadTaskRepository) Count(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.DownloadTask{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count, err
}

func (r *downloadTaskRepository) ListByState(ctx context.Context, userID string, state int, offset, limit int) ([]*models.DownloadTask, error) {
	var tasks []*models.DownloadTask
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND state = ?", userID, state).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *downloadTaskRepository) CountByState(ctx context.Context, userID string, state int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.DownloadTask{}).
		Where("user_id = ? AND state = ?", userID, state).
		Count(&count).Error
	return count, err
}

func (r *downloadTaskRepository) ListByType(ctx context.Context, userID string, taskType int, offset, limit int) ([]*models.DownloadTask, error) {
	var tasks []*models.DownloadTask
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND type = ?", userID, taskType).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *downloadTaskRepository) CountByType(ctx context.Context, userID string, taskType int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.DownloadTask{}).
		Where("user_id = ? AND type = ?", userID, taskType).
		Count(&count).Error
	return count, err
}

func (r *downloadTaskRepository) ListByStateAndType(ctx context.Context, userID string, state int, taskType int, offset, limit int) ([]*models.DownloadTask, error) {
	var tasks []*models.DownloadTask
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND state = ? AND type = ?", userID, state, taskType).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *downloadTaskRepository) CountByStateAndType(ctx context.Context, userID string, state int, taskType int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.DownloadTask{}).
		Where("user_id = ? AND state = ? AND type = ?", userID, state, taskType).
		Count(&count).Error
	return count, err
}

func (r *downloadTaskRepository) ListByFilters(ctx context.Context, userID string, state *int, taskTypes []int, offset, limit int) ([]*models.DownloadTask, error) {
	var tasks []*models.DownloadTask
	query := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if state != nil {
		query = query.Where("state = ?", *state)
	}
	if len(taskTypes) > 0 {
		query = query.Where("type IN ?", taskTypes)
	}
	err := query.Order("create_time DESC").Offset(offset).Limit(limit).Find(&tasks).Error
	return tasks, err
}

func (r *downloadTaskRepository) CountByFilters(ctx context.Context, userID string, state *int, taskTypes []int) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&models.DownloadTask{}).Where("user_id = ?", userID)
	if state != nil {
		query = query.Where("state = ?", *state)
	}
	if len(taskTypes) > 0 {
		query = query.Where("type IN ?", taskTypes)
	}
	err := query.Count(&count).Error
	return count, err
}

func (r *downloadTaskRepository) ListRunnable(ctx context.Context, now time.Time, limit int) ([]*models.DownloadTask, error) {
	var tasks []*models.DownloadTask
	err := r.db.WithContext(ctx).
		Where("state = ?", 0).
		Where("type IN ?", []int{0, 4, 5, 9}).
		Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
		Order("create_time ASC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (r *downloadTaskRepository) Claim(ctx context.Context, id, workerID, runToken string, leaseExpiresAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&models.DownloadTask{}).
		Where("id = ? AND state = ?", id, 0).
		Updates(map[string]interface{}{
			"state":            1,
			"worker_id":        workerID,
			"run_token":        runToken,
			"lease_expires_at": leaseExpiresAt,
			"next_retry_at":    nil,
			"error_msg":        "",
			"update_time":      time.Now(),
		})
	return result.RowsAffected == 1, result.Error
}

func (r *downloadTaskRepository) Transition(ctx context.Context, id string, allowedStates []int, newState int, updates map[string]interface{}) (bool, error) {
	values := make(map[string]interface{}, len(updates)+2)
	for key, value := range updates {
		values[key] = value
	}
	values["state"] = newState
	values["update_time"] = time.Now()
	result := r.db.WithContext(ctx).Model(&models.DownloadTask{}).
		Where("id = ? AND state IN ?", id, allowedStates).
		Updates(values)
	return result.RowsAffected == 1, result.Error
}

func (r *downloadTaskRepository) UpdateIfRunToken(ctx context.Context, id, runToken string, updates map[string]interface{}) (bool, error) {
	values := make(map[string]interface{}, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["update_time"] = time.Now()
	result := r.db.WithContext(ctx).Model(&models.DownloadTask{}).
		Where("id = ? AND run_token = ? AND state = ?", id, runToken, 1).
		Updates(values)
	return result.RowsAffected == 1, result.Error
}

func (r *downloadTaskRepository) Heartbeat(ctx context.Context, id, runToken string, leaseExpiresAt time.Time) (bool, error) {
	return r.UpdateIfRunToken(ctx, id, runToken, map[string]interface{}{
		"lease_expires_at": leaseExpiresAt,
	})
}
