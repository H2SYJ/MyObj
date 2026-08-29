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

// CreateGlobalRebuildJob 按当前活动规则版本创建一次全量重建任务。
// 这是运维主动触发重建的入口，避免只能通过改规则间接制造任务。
func (s *TagService) CreateGlobalRebuildJob(ctx context.Context, requestedBy string) (*models.TagRebuildJob, error) {
	runtime := s.globalRuntime.Load()
	if runtime == nil || runtime.snapshot == nil {
		return nil, errors.New("全局标签规则尚未加载")
	}
	return s.CreateRebuildJob(ctx, runtime.snapshot.GlobalVersion, requestedBy)
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
		// 任务被废弃通常是因为期间发布了新规则。重试时必须把目标版本对齐到当前活动
		// 版本，否则任务一被捞起就会再次因版本不匹配被标记回 superseded，表现为
		// "重试后任务瞬间又没了"。
		updates := map[string]interface{}{
			"status": "pending", "cursor_value": "",
			"processed": 0, "succeeded": 0, "failed": 0, "last_error": "",
			"run_token": "", "lease_expires_at": nil, "started_at": nil, "finished_at": nil, "updated_at": now,
		}
		// 目标版本取自内存中的活动快照，与 worker 侧 jobVersionCurrent 的校验对象保持
		// 一致。这里不再查库，既少一次查询，也避免读到的版本与 worker 校验的版本打架。
		if runtime := s.globalRuntime.Load(); runtime != nil && runtime.snapshot != nil {
			updates["target_version"] = runtime.snapshot.GlobalVersion
		}
		result := tx.Model(&models.TagRebuildJob{}).
			Where("id = ? AND status IN ?", id, []string{"failed", "completed_with_errors", "cancelled", "superseded"}).
			Updates(updates)
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
