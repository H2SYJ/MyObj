package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

const (
	tagWorkerLease  = 45 * time.Second
	tagPendingBatch = 20
)

// 本文件负责增量文件标签任务。
// QueueUserFile 只在当前数据库事务内持久化任务，调用方必须在事务提交成功后发送待处理通知。
func (s *TagService) QueueUserFile(ctx context.Context, db *gorm.DB, userID, ufID string) error {
	return tagging.QueueUserFile(ctx, db, userID, ufID)
}

// RetryUserFile 将单个用户文件重新放入自动标签队列，不改变手工标签和屏蔽记录。
func (s *TagService) RetryUserFile(ctx context.Context, userID, ufID string) error {
	if err := s.ensureOwnership(ctx, userID, []string{ufID}); err != nil {
		return err
	}
	if err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.QueueUserFile(ctx, tx, userID, ufID)
	}); err != nil {
		return err
	}
	s.notifyPending()
	return nil
}

func (s *TagService) runPendingWorker() {
	defer s.wg.Done()
	if !s.waitForRuntime() {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	runDispatch := func() {
		next, err := s.dispatchPending()
		delay := schedulerWakeDelay(time.Now(), next)
		if err != nil {
			logger.LOG.Error("调度自动标签任务失败", "worker", "pending", "error", err, "retry_after", schedulerErrorRetry)
			delay = schedulerErrorRetry
		} else if next != nil {
			logger.LOG.Debug("自动标签任务已安排下次唤醒", "worker", "pending", "next_wake_at", *next)
		}
		resetSchedulerTimer(timer, delay)
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.pendingWake:
			runDispatch()
		case <-timer.C:
			runDispatch()
		}
	}
}

func (s *TagService) dispatchPending() (*time.Time, error) {
	if !s.autoEnabled.Load() {
		return nil, nil
	}
	for index := 0; index < tagPendingBatch; index++ {
		if !s.autoEnabled.Load() {
			return nil, nil
		}
		state, ok, err := s.claimPendingState()
		if err != nil {
			return nil, err
		}
		if !ok {
			return s.nextPendingWakeAt(time.Now())
		}
		err = s.GenerateUserFile(s.ctx, state.UserID, state.UFID, state.RunToken, 0)
		if errors.Is(err, errAutoTagDisabled) {
			s.releasePendingState(state)
			return nil, nil
		}
		if err == nil {
			s.resolveQueuedRebuildFailures(state.UFID)
			continue
		}
		if errors.Is(err, errStaleTagGeneration) {
			continue
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if deleteErr := s.factory.DB().Where("uf_id = ? AND run_token = ?", state.UFID, state.RunToken).Delete(&models.UserFileTagState{}).Error; deleteErr != nil {
				return nil, deleteErr
			}
			continue
		}
		s.failPendingState(state, err)
	}
	immediate := time.Now()
	return &immediate, nil
}

func (s *TagService) releasePendingState(state *models.UserFileTagState) {
	_ = s.factory.DB().Model(&models.UserFileTagState{}).
		Where("uf_id = ? AND run_token = ?", state.UFID, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStatePending, "run_token": "", "lease_expires_at": nil, "updated_at": time.Now(),
		}).Error
}

func (s *TagService) claimPendingState() (*models.UserFileTagState, bool, error) {
	now := time.Now()
	var state models.UserFileTagState
	query := s.factory.DB().WithContext(s.ctx).
		Where("(status = ?) OR (status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND lease_expires_at < ?)",
			models.TagStatePending, models.TagStateFailed, now, models.TagStateRunning, now).
		Order("updated_at ASC").Limit(1).Find(&state)
	if query.Error != nil {
		return nil, false, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, false, nil
	}
	token := uuid.NewString()
	result := s.factory.DB().WithContext(s.ctx).Model(&models.UserFileTagState{}).
		Where("uf_id = ? AND status = ? AND run_token = ?", state.UFID, state.Status, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStateRunning, "run_token": token,
			"lease_expires_at": now.Add(tagWorkerLease), "updated_at": now,
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

func (s *TagService) nextPendingWakeAt(now time.Time) (*time.Time, error) {
	var retry models.UserFileTagState
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

	var running models.UserFileTagState
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

func (s *TagService) failPendingState(state *models.UserFileTagState, generationErr error) {
	retryCount := state.RetryCount + 1
	backoff := time.Minute
	if retryCount == 2 {
		backoff = 5 * time.Minute
	} else if retryCount >= 3 {
		backoff = 30 * time.Minute
	}
	now := time.Now()
	_ = s.factory.DB().Model(&models.UserFileTagState{}).
		Where("uf_id = ? AND run_token = ?", state.UFID, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStateFailed, "last_error": generationErr.Error(),
			"retry_count": retryCount, "next_retry_at": now.Add(backoff),
			"run_token": "", "lease_expires_at": nil, "updated_at": now,
		}).Error
	_ = s.factory.DB().Model(&models.TagRebuildFailure{}).
		Where("uf_id = ? AND status = ?", state.UFID, models.TagRebuildFailureRetrying).
		Updates(map[string]interface{}{
			"status": models.TagRebuildFailureFailed, "error_message": generationErr.Error(), "updated_at": now,
		}).Error
	logger.LOG.Warn("自动标签生成失败", "uf_id", state.UFID, "retry_count", retryCount, "error", generationErr)
}
