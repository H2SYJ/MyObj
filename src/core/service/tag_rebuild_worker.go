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

const tagRebuildBatch = 100

// 本文件负责规则变更后的全量标签重建。
func (s *TagService) runRebuildWorker() {
	defer s.wg.Done()
	if !s.waitForRuntime() {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	runDispatch := func() {
		next, err := s.dispatchRebuild()
		delay := schedulerWakeDelay(time.Now(), next)
		if err != nil {
			logger.LOG.Error("调度标签重建任务失败", "worker", "rebuild", "error", err, "retry_after", schedulerErrorRetry)
			delay = schedulerErrorRetry
		} else if next != nil {
			logger.LOG.Debug("标签重建任务已安排下次唤醒", "worker", "rebuild", "next_wake_at", *next)
		}
		resetSchedulerTimer(timer, delay)
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.rebuildWake:
			runDispatch()
		case <-timer.C:
			runDispatch()
		}
	}
}

func (s *TagService) dispatchRebuild() (*time.Time, error) {
	for s.autoEnabled.Load() {
		job, ok, err := s.claimRebuildJob()
		if err != nil {
			return nil, err
		}
		if !ok {
			return s.nextRebuildWakeAt(time.Now())
		}
		s.processRebuildJob(job)
		if s.ctx.Err() != nil {
			return nil, nil
		}
	}
	return nil, nil
}

func (s *TagService) claimRebuildJob() (*models.TagRebuildJob, bool, error) {
	now := time.Now()
	var job models.TagRebuildJob
	query := s.factory.DB().WithContext(s.ctx).
		Where("status = ? OR (status = ? AND lease_expires_at < ?)", "pending", "running", now).
		Order("created_at ASC").Limit(1).Find(&job)
	if query.Error != nil {
		return nil, false, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, false, nil
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
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, false, nil
	}
	job.Status = "running"
	job.RunToken = token
	job.LeaseExpires = timePointer(now.Add(tagWorkerLease))
	if startedNow {
		job.StartedAt = timePointer(now)
	}
	return &job, true, nil
}

func (s *TagService) nextRebuildWakeAt(now time.Time) (*time.Time, error) {
	var job models.TagRebuildJob
	query := s.factory.DB().WithContext(s.ctx).Select("lease_expires_at").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at > ?", "running", now).
		Order("lease_expires_at ASC").Limit(1).Find(&job)
	if query.Error != nil {
		return nil, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, nil
	}
	return job.LeaseExpires, nil
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
		batchAffectedTagIDs := make(map[string][]string)
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
			affectedTagIDs, err := s.generateUserFileForRebuild(s.ctx, file.UserID, file.UfID, state.RunToken, job.TargetVersion, &tagRebuildGuard{
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
				batchAffectedTagIDs[file.UserID] = append(batchAffectedTagIDs[file.UserID], affectedTagIDs...)
				s.resolveRebuildFailure(job.ID, file.UfID)
			}
		}
		nextProcessed := job.Processed + int64(len(files))
		nextSucceeded := job.Succeeded + batchSucceeded
		nextFailed := job.Failed + batchFailed
		now := time.Now()
		leaseExpires := now.Add(tagWorkerLease)
		err := s.factory.DB().WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
			// 重建期间先汇总本批受影响标签，再统一刷新统计并原子推进批次游标。
			if err := s.refreshUserTagStatsBatch(s.ctx, tx, batchAffectedTagIDs); err != nil {
				return err
			}
			result := tx.Model(&models.TagRebuildJob{}).
				Where("id = ? AND run_token = ? AND status = ?", job.ID, job.RunToken, "running").
				Updates(map[string]interface{}{
					"cursor_value": lastCursor, "processed": nextProcessed,
					"succeeded": nextSucceeded, "failed": nextFailed, "last_error": job.LastError,
					"lease_expires_at": leaseExpires, "updated_at": now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errStaleTagGeneration
			}
			return nil
		})
		if err != nil {
			if !errors.Is(err, errStaleTagGeneration) {
				logger.LOG.Error("提交标签重建批次失败", "job_id", job.ID, "cursor", lastCursor, "error", err)
			}
			return
		}
		job.Cursor = lastCursor
		job.Processed = nextProcessed
		job.Succeeded = nextSucceeded
		job.Failed = nextFailed
		job.LeaseExpires = timePointer(leaseExpires)
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
	runtime := s.globalRuntime.Load()
	return runtime != nil && runtime.snapshot != nil && runtime.snapshot.GlobalVersion == job.TargetVersion
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

func timePointer(value time.Time) *time.Time { return &value }
