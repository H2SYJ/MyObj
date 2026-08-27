package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"myobj/src/pkg/models"
)

// 本文件负责标签全量重建任务的创建和人工控制。
func (s *TagService) CreateRebuildJob(ctx context.Context, targetVersion int64, requestedBy string) (*models.TagRebuildJob, error) {
	job, err := s.createRebuildJob(ctx, targetVersion, requestedBy, s.factory.DB())
	if err != nil {
		return nil, err
	}
	s.notifyRebuild()
	return job, nil
}

func (s *TagService) createRebuildJob(ctx context.Context, targetVersion int64, requestedBy string, db *gorm.DB) (*models.TagRebuildJob, error) {
	now := time.Now()
	job := &models.TagRebuildJob{
		ID: uuid.NewString(), TargetVersion: targetVersion,
		Status: "pending", RequestedBy: requestedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		unfinished := tx.Model(&models.TagRebuildJob{}).Where("status IN ?", []string{"pending", "running"})
		if err := unfinished.Updates(map[string]interface{}{
			"status": "superseded", "finished_at": now, "updated_at": now,
			"run_token": "", "lease_expires_at": nil,
		}).Error; err != nil {
			return err
		}
		query := tx.Model(&models.UserFiles{}).Where("deleted_at IS NULL")
		if err := query.Count(&job.Total).Error; err != nil {
			return err
		}
		return tx.Create(job).Error
	}); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *TagService) RebuildJobs(ctx context.Context, limit int) ([]models.TagRebuildJob, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var jobs []models.TagRebuildJob
	err := s.factory.DB().WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

func (s *TagService) CancelRebuildJob(ctx context.Context, id string) error {
	now := time.Now()
	result := s.factory.DB().WithContext(ctx).Model(&models.TagRebuildJob{}).
		Where("id = ? AND status IN ?", id, []string{"pending", "running"}).
		Updates(map[string]interface{}{"status": "cancelled", "finished_at": now, "run_token": "", "lease_expires_at": nil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("重建任务不存在或当前状态不可取消")
	}
	return nil
}

func (s *TagService) RetryRebuildJob(ctx context.Context, id string) error {
	now := time.Now()
	err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.TagRebuildJob{}).
			Where("id = ? AND status IN ?", id, []string{"failed", "completed_with_errors", "cancelled"}).
			Updates(map[string]interface{}{
				"status": "pending", "cursor_value": "", "processed": 0, "succeeded": 0, "failed": 0,
				"last_error": "", "run_token": "", "lease_expires_at": nil, "started_at": nil, "finished_at": nil, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("重建任务不存在或当前状态不可重试")
		}
		return tx.Where("job_id = ?", id).Delete(&models.TagRebuildFailure{}).Error
	})
	if err == nil {
		s.notifyRebuild()
	}
	return err
}
