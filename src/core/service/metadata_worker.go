package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/pkg/logger"
	metadatapkg "myobj/src/pkg/metadata"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

const (
	metadataWorkerLease        = 45 * time.Second
	metadataWorkerBatch        = 10
	metadataBackfillBatch      = 100
	metadataBackfillBatchDelay = time.Second
	metadataReconcileInterval  = 10 * time.Minute
)

func (s *TagService) runMetadataWorker() {
	defer s.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	nextReconcile := time.Now()
	runDispatch := func() {
		now := time.Now()
		if !now.Before(nextReconcile) {
			seeded, err := s.seedMissingMetadataStates()
			if err != nil {
				logger.LOG.Error("回填文件元数据任务失败", "worker", "metadata", "error", err, "retry_after", schedulerErrorRetry)
				resetSchedulerTimer(timer, schedulerErrorRetry)
				return
			}
			if seeded >= metadataBackfillBatch {
				nextReconcile = now.Add(metadataBackfillBatchDelay)
			} else {
				nextReconcile = now.Add(metadataReconcileInterval)
			}
			if seeded > 0 {
				logger.LOG.Info("文件元数据任务回填完成", "worker", "metadata", "count", seeded, "next_reconcile_at", nextReconcile)
			}
		}

		nextTask, err := s.dispatchMetadata()
		if err != nil {
			logger.LOG.Error("调度文件元数据任务失败", "worker", "metadata", "error", err, "retry_after", schedulerErrorRetry)
			resetSchedulerTimer(timer, schedulerErrorRetry)
			return
		}
		next := earlierTime(nextTask, &nextReconcile)
		if nextTask != nil {
			logger.LOG.Debug("文件元数据任务已安排下次唤醒", "worker", "metadata", "next_wake_at", *nextTask)
		}
		resetSchedulerTimer(timer, schedulerWakeDelay(time.Now(), next))
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.metadataWake:
			runDispatch()
		case <-timer.C:
			runDispatch()
		}
	}
}

func (s *TagService) dispatchMetadata() (*time.Time, error) {
	for index := 0; index < metadataWorkerBatch; index++ {
		state, ok, err := s.claimMetadataState()
		if err != nil {
			return nil, err
		}
		if !ok {
			return s.nextMetadataWakeAt(time.Now())
		}
		if err := s.extractMetadata(state); err != nil {
			s.failMetadataState(state, err)
		}
	}
	immediate := time.Now()
	return &immediate, nil
}

func (s *TagService) seedMissingMetadataStates() (int, error) {
	var files []models.FileInfo
	err := s.factory.DB().WithContext(s.ctx).
		Select("id").
		Where("NOT EXISTS (SELECT 1 FROM file_metadata_state fms WHERE fms.file_id = file_info.id)").
		Order("created_at ASC").Limit(metadataBackfillBatch).Find(&files).Error
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	now := time.Now()
	states := make([]models.FileMetadataState, 0, len(files))
	for _, file := range files {
		states = append(states, models.FileMetadataState{FileID: file.ID, Status: models.TagStatePending, UpdatedAt: now})
	}
	result := s.factory.DB().WithContext(s.ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&states)
	return int(result.RowsAffected), result.Error
}

func (s *TagService) claimMetadataState() (*models.FileMetadataState, bool, error) {
	now := time.Now()
	var state models.FileMetadataState
	query := s.factory.DB().WithContext(s.ctx).
		Where("status = ? OR (status = ? AND next_retry_at <= ?) OR (status = ? AND lease_expires_at < ?)",
			models.TagStatePending, models.TagStateFailed, now, models.TagStateRunning, now).
		Order("updated_at ASC").Limit(1).Find(&state)
	if query.Error != nil {
		return nil, false, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, false, nil
	}
	token := uuid.NewString()
	result := s.factory.DB().WithContext(s.ctx).Model(&models.FileMetadataState{}).
		Where("file_id = ? AND status = ? AND run_token = ?", state.FileID, state.Status, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStateRunning, "run_token": token,
			"lease_expires_at": now.Add(metadataWorkerLease), "updated_at": now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, false, nil
	}
	state.Status = models.TagStateRunning
	state.RunToken = token
	return &state, true, nil
}

func (s *TagService) nextMetadataWakeAt(now time.Time) (*time.Time, error) {
	var retry models.FileMetadataState
	retryQuery := s.factory.DB().WithContext(s.ctx).Select("next_retry_at").
		Where("status = ? AND next_retry_at IS NOT NULL AND next_retry_at > ?", models.TagStateFailed, now).
		Order("next_retry_at ASC").Limit(1).Find(&retry)
	if retryQuery.Error != nil {
		return nil, retryQuery.Error
	}
	var next *time.Time
	if retryQuery.RowsAffected == 1 {
		next = retry.NextRetryAt
	}

	var running models.FileMetadataState
	leaseQuery := s.factory.DB().WithContext(s.ctx).Select("lease_expires_at").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at > ?", models.TagStateRunning, now).
		Order("lease_expires_at ASC").Limit(1).Find(&running)
	if leaseQuery.Error != nil {
		return nil, leaseQuery.Error
	}
	if leaseQuery.RowsAffected == 1 {
		next = earlierTime(next, running.LeaseExpires)
	}
	return next, nil
}

func (s *TagService) extractMetadata(state *models.FileMetadataState) error {
	var file models.FileInfo
	if err := s.factory.DB().WithContext(s.ctx).Where("id = ?", state.FileID).First(&file).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.factory.DB().Where("file_id = ? AND run_token = ?", state.FileID, state.RunToken).
				Delete(&models.FileMetadataState{}).Error
		}
		return err
	}
	input := metadatapkg.Input{
		Path: file.Path, FileName: file.Name, MIME: file.Mime, Size: int64(file.Size), Encrypted: file.IsEnc,
	}
	var extracted metadatapkg.Result
	if file.IsEnc || file.IsChunk {
		extracted = metadatapkg.Extract(s.ctx, input, metadatapkg.BasicProvider{})
		extracted.Partial = true
		extracted.Errors = append(extracted.Errors, errors.New("历史加密或分块文件没有可直接读取的原始文件"))
	} else if info, err := os.Stat(file.Path); err != nil || !info.Mode().IsRegular() {
		extracted = metadatapkg.Extract(s.ctx, input, metadatapkg.BasicProvider{})
		extracted.Partial = true
		extracted.Errors = append(extracted.Errors, fmt.Errorf("物理文件不可读取: %v", err))
	} else {
		extracted = metadatapkg.Extract(s.ctx, input)
	}
	if extracted.Partial {
		logger.LOG.Warn("文件元数据提取部分失败", "file_id", state.FileID, "error", extracted.ErrorText())
	}
	err := s.factory.DB().WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.FileMetadataState{}).
			Where("file_id = ? AND run_token = ?", state.FileID, state.RunToken).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errStaleTagGeneration
		}
		if _, err := metadatapkg.Persist(context.Background(), tx, state.FileID, extracted); err != nil {
			return err
		}
		var references []models.UserFiles
		if err := tx.Where("file_id = ? AND deleted_at IS NULL", state.FileID).Find(&references).Error; err != nil {
			return err
		}
		for _, reference := range references {
			if err := tagging.QueueUserFile(s.ctx, tx, reference.UserID, reference.UfID); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.notifyPending()
	}
	return err
}

func (s *TagService) failMetadataState(state *models.FileMetadataState, extractErr error) {
	retryCount := state.RetryCount + 1
	backoff := time.Minute * time.Duration(1<<min(retryCount-1, 5))
	now := time.Now()
	_ = s.factory.DB().Model(&models.FileMetadataState{}).
		Where("file_id = ? AND run_token = ?", state.FileID, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStateFailed, "last_error": extractErr.Error(), "retry_count": retryCount,
			"next_retry_at": now.Add(backoff), "run_token": "", "lease_expires_at": nil, "updated_at": now,
		}).Error
}
