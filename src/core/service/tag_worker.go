package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
)

const (
	tagWorkerLease  = 45 * time.Second
	tagRebuildBatch = 100
)

func (s *TagService) runPendingWorker() {
	defer s.wg.Done()
	if !s.waitForRuntime() {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if !s.autoEnabled.Load() {
			continue
		}
		for index := 0; index < 20; index++ {
			if !s.autoEnabled.Load() {
				break
			}
			state, ok := s.claimPendingState()
			if !ok {
				break
			}
			err := s.GenerateUserFile(s.ctx, state.UserID, state.UFID, state.RunToken, 0)
			if errors.Is(err, errAutoTagDisabled) {
				s.releasePendingState(state)
				break
			}
			if err == nil {
				s.resolveQueuedRebuildFailures(state.UFID)
				continue
			}
			if errors.Is(err, errStaleTagGeneration) {
				continue
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				_ = s.factory.DB().Where("uf_id = ? AND run_token = ?", state.UFID, state.RunToken).Delete(&models.UserFileTagState{}).Error
				continue
			}
			s.failPendingState(state, err)
		}
	}
}

func (s *TagService) releasePendingState(state *models.UserFileTagState) {
	_ = s.factory.DB().Model(&models.UserFileTagState{}).
		Where("uf_id = ? AND run_token = ?", state.UFID, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStatePending, "run_token": "", "lease_expires_at": nil, "updated_at": time.Now(),
		}).Error
}

func (s *TagService) claimPendingState() (*models.UserFileTagState, bool) {
	now := time.Now()
	var state models.UserFileTagState
	query := s.factory.DB().WithContext(s.ctx).
		Where("(status = ?) OR (status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND lease_expires_at < ?)",
			models.TagStatePending, models.TagStateFailed, now, models.TagStateRunning, now).
		Order("updated_at ASC").Limit(1).Find(&state)
	if query.Error != nil || query.RowsAffected == 0 {
		return nil, false
	}
	token := uuid.NewString()
	result := s.factory.DB().WithContext(s.ctx).Model(&models.UserFileTagState{}).
		Where("uf_id = ? AND status = ? AND run_token = ?", state.UFID, state.Status, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStateRunning, "run_token": token,
			"lease_expires_at": now.Add(tagWorkerLease), "updated_at": now,
		})
	if result.Error != nil || result.RowsAffected != 1 {
		return nil, false
	}
	state.Status = models.TagStateRunning
	state.RunToken = token
	return &state, true
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

func (s *TagService) runRebuildWorker() {
	defer s.wg.Done()
	if !s.waitForRuntime() {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if !s.autoEnabled.Load() {
			continue
		}
		job, ok := s.claimRebuildJob()
		if !ok {
			continue
		}
		s.processRebuildJob(job)
	}
}

func (s *TagService) claimRebuildJob() (*models.TagRebuildJob, bool) {
	now := time.Now()
	var job models.TagRebuildJob
	query := s.factory.DB().WithContext(s.ctx).
		Where("status = ? OR (status = ? AND lease_expires_at < ?)", "pending", "running", now).
		Order("created_at ASC").Limit(1).Find(&job)
	if query.Error != nil || query.RowsAffected == 0 {
		return nil, false
	}
	token := uuid.NewString()
	updates := map[string]interface{}{
		"status": "running", "run_token": token,
		"lease_expires_at": now.Add(tagWorkerLease), "updated_at": now,
	}
	startedNow := job.StartedAt == nil
	if job.StartedAt == nil {
		updates["started_at"] = now
	}
	result := s.factory.DB().WithContext(s.ctx).Model(&models.TagRebuildJob{}).
		Where("id = ? AND status = ? AND run_token = ?", job.ID, job.Status, job.RunToken).Updates(updates)
	if result.Error != nil || result.RowsAffected != 1 {
		return nil, false
	}
	job.Status = "running"
	job.RunToken = token
	job.LeaseExpires = timePointer(now.Add(tagWorkerLease))
	if startedNow {
		job.StartedAt = timePointer(now)
	}
	return &job, true
}

func (s *TagService) processRebuildJob(job *models.TagRebuildJob) {
	if !s.jobVersionCurrent(job) {
		s.finishRebuildJob(job, "superseded", "活动词典版本已变化")
		return
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		if !s.jobStillOwned(job) {
			return
		}
		if !s.autoEnabled.Load() {
			s.pauseRebuildJob(job)
			return
		}
		var files []models.UserFiles
		query := s.factory.DB().WithContext(s.ctx).Where("deleted_at IS NULL AND uf_id > ?", job.Cursor)
		if job.ScopeType == models.TagRuleScopeUser {
			query = query.Where("user_id = ?", job.ScopeID)
		}
		if err := query.Order("uf_id ASC").Limit(tagRebuildBatch).Find(&files).Error; err != nil {
			s.finishRebuildJob(job, "failed", err.Error())
			return
		}
		if len(files) == 0 {
			status := "completed"
			if job.Failed > 0 {
				status = "completed_with_errors"
			}
			s.finishRebuildJob(job, status, job.LastError)
			return
		}

		batchSucceeded, batchFailed := int64(0), int64(0)
		lastCursor := job.Cursor
		for _, file := range files {
			lastCursor = file.UfID
			if !s.autoEnabled.Load() {
				s.pauseRebuildJob(job)
				return
			}
			if !s.renewRebuildLease(job) {
				return
			}
			if !s.jobVersionCurrent(job) {
				s.finishRebuildJob(job, "superseded", "活动词典版本已变化")
				return
			}
			state, err := s.claimRebuildState(file.UserID, file.UfID)
			if err != nil {
				batchFailed++
				job.LastError = err.Error()
				s.recordRebuildFailure(job.ID, file.UserID, file.UfID, err)
				continue
			}
			targetGlobalVersion := int64(0)
			if job.ScopeType == models.TagRuleScopeGlobal {
				targetGlobalVersion = job.TargetVersion
			}
			err = s.generateUserFile(s.ctx, file.UserID, file.UfID, state.RunToken, targetGlobalVersion, &tagRebuildGuard{
				jobID: job.ID, runToken: job.RunToken,
			})
			if err != nil {
				if errors.Is(err, errAutoTagDisabled) {
					s.releasePendingState(state)
					s.pauseRebuildJob(job)
					return
				}
				if errors.Is(err, errStaleTagGeneration) {
					s.releasePendingState(state)
					if !s.jobStillOwned(job) {
						return
					}
					if !s.jobVersionCurrent(job) {
						s.finishRebuildJob(job, "superseded", "活动词典版本已变化")
						return
					}
					// 文件在生成期间被重命名等新事件重新入队，交由最新文件级任务处理。
					batchSucceeded++
					continue
				}
				s.failPendingState(state, err)
				batchFailed++
				job.LastError = err.Error()
				s.recordRebuildFailure(job.ID, file.UserID, file.UfID, err)
			} else {
				batchSucceeded++
				s.resolveRebuildFailure(job.ID, file.UfID)
			}
		}
		job.Cursor = lastCursor
		job.Processed += int64(len(files))
		job.Succeeded += batchSucceeded
		job.Failed += batchFailed
		now := time.Now()
		result := s.factory.DB().Model(&models.TagRebuildJob{}).
			Where("id = ? AND run_token = ? AND status = ?", job.ID, job.RunToken, "running").
			Updates(map[string]interface{}{
				"cursor_value": job.Cursor, "processed": job.Processed,
				"succeeded": job.Succeeded, "failed": job.Failed, "last_error": job.LastError,
				"lease_expires_at": now.Add(tagWorkerLease), "updated_at": now,
			})
		if result.Error != nil || result.RowsAffected != 1 {
			return
		}
	}
}

func (s *TagService) claimRebuildState(userID, ufID string) (*models.UserFileTagState, error) {
	now := time.Now()
	token := uuid.NewString()
	lease := now.Add(tagWorkerLease)
	state := &models.UserFileTagState{
		UFID: ufID, UserID: userID, Status: models.TagStateRunning,
		RunToken: token, LeaseExpires: &lease, UpdatedAt: now,
	}
	err := s.factory.DB().WithContext(s.ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "uf_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"user_id": userID, "status": models.TagStateRunning, "last_error": "",
			"next_retry_at": nil, "run_token": token, "lease_expires_at": lease, "updated_at": now,
		}),
	}).Create(state).Error
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (s *TagService) jobVersionCurrent(job *models.TagRebuildJob) bool {
	if job.ScopeType == models.TagRuleScopeGlobal {
		runtime := s.globalRuntime.Load()
		return runtime != nil && runtime.snapshot != nil && runtime.snapshot.GlobalVersion == job.TargetVersion
	}
	personal, err := s.loadActiveRuleSet(s.ctx, models.TagRuleScopeUser, job.ScopeID)
	return err == nil && personal.Version == job.TargetVersion
}

func (s *TagService) jobStillOwned(job *models.TagRebuildJob) bool {
	var count int64
	err := s.factory.DB().Model(&models.TagRebuildJob{}).
		Where("id = ? AND run_token = ? AND status = ?", job.ID, job.RunToken, "running").Count(&count).Error
	return err == nil && count == 1
}

func (s *TagService) renewRebuildLease(job *models.TagRebuildJob) bool {
	now := time.Now()
	result := s.factory.DB().Model(&models.TagRebuildJob{}).
		Where("id = ? AND run_token = ? AND status = ?", job.ID, job.RunToken, "running").
		Updates(map[string]interface{}{"lease_expires_at": now.Add(tagWorkerLease), "updated_at": now})
	if result.Error != nil || result.RowsAffected != 1 {
		return false
	}
	job.LeaseExpires = timePointer(now.Add(tagWorkerLease))
	return true
}

func (s *TagService) finishRebuildJob(job *models.TagRebuildJob, status, message string) {
	now := time.Now()
	_ = s.factory.DB().Model(&models.TagRebuildJob{}).
		Where("id = ? AND run_token = ?", job.ID, job.RunToken).
		Updates(map[string]interface{}{
			"status": status, "last_error": message, "run_token": "", "lease_expires_at": nil,
			"finished_at": now, "updated_at": now,
		}).Error
	duration := time.Duration(0)
	rate := float64(0)
	if job.StartedAt != nil {
		duration = now.Sub(*job.StartedAt)
		if duration > 0 {
			rate = float64(job.Processed) / duration.Seconds()
		}
	}
	logger.LOG.Info("标签重建任务结束", "job_id", job.ID, "status", status, "processed", job.Processed,
		"failed", job.Failed, "duration", duration, "files_per_second", rate)
}

func (s *TagService) pauseRebuildJob(job *models.TagRebuildJob) {
	now := time.Now()
	_ = s.factory.DB().Model(&models.TagRebuildJob{}).
		Where("id = ? AND run_token = ? AND status = ?", job.ID, job.RunToken, "running").
		Updates(map[string]interface{}{
			"status": "pending", "run_token": "", "lease_expires_at": nil, "updated_at": now,
		}).Error
	logger.LOG.Info("自动标签已关闭，重建任务暂停", "job_id", job.ID, "processed", job.Processed)
}

func (s *TagService) recordRebuildFailure(jobID, userID, ufID string, rebuildErr error) {
	now := time.Now()
	failure := &models.TagRebuildFailure{
		JobID: jobID, UFID: ufID, UserID: userID, Status: models.TagRebuildFailureFailed,
		Error: rebuildErr.Error(), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.factory.DB().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "job_id"}, {Name: "uf_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"user_id": userID, "status": models.TagRebuildFailureFailed,
			"error_message": rebuildErr.Error(), "updated_at": now,
		}),
	}).Create(failure).Error; err != nil {
		logger.LOG.Warn("记录标签重建失败明细失败", "job_id", jobID, "uf_id", ufID, "error", err)
	}
}

func (s *TagService) resolveRebuildFailure(jobID, ufID string) {
	_ = s.factory.DB().Model(&models.TagRebuildFailure{}).
		Where("job_id = ? AND uf_id = ?", jobID, ufID).
		Updates(map[string]interface{}{
			"status": models.TagRebuildFailureResolved, "error_message": "", "updated_at": time.Now(),
		}).Error
}

func (s *TagService) resolveQueuedRebuildFailures(ufID string) {
	_ = s.factory.DB().Model(&models.TagRebuildFailure{}).
		Where("uf_id = ? AND status IN ?", ufID, []string{models.TagRebuildFailureFailed, models.TagRebuildFailureRetrying}).
		Updates(map[string]interface{}{
			"status": models.TagRebuildFailureResolved, "error_message": "", "updated_at": time.Now(),
		}).Error
}

func (s *TagService) runRulePoller() {
	defer s.wg.Done()
	if err := s.initializeRuntime(s.ctx); err != nil {
		s.degraded.Store(true)
		s.degradedReason.Store(err.Error())
		logger.LOG.Error("异步加载标签规则失败，将继续重试", "error", err)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.globalRuntime.Load() == nil {
				if err := s.initializeRuntime(s.ctx); err != nil {
					s.degraded.Store(true)
					s.degradedReason.Store(err.Error())
					logger.LOG.Error("重试加载标签规则失败", "error", err)
				}
				continue
			}
			if err := s.reloadSettings(s.ctx); err != nil {
				logger.LOG.Warn("刷新标签配置失败", "error", err)
				continue
			}
			if err := s.reloadGlobalRules(s.ctx, false); err != nil {
				logger.LOG.Error("热加载全局标签规则失败，继续使用上一版本", "error", err)
			}
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }
