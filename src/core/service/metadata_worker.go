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

const metadataWorkerLease = 45 * time.Second

func (s *TagService) runMetadataWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		s.seedMissingMetadataStates()
		for index := 0; index < 10; index++ {
			state, ok := s.claimMetadataState()
			if !ok {
				break
			}
			if err := s.extractMetadata(state); err != nil {
				s.failMetadataState(state, err)
			}
		}
	}
}

func (s *TagService) seedMissingMetadataStates() {
	var files []models.FileInfo
	err := s.factory.DB().WithContext(s.ctx).
		Where("NOT EXISTS (SELECT 1 FROM file_metadata_state fms WHERE fms.file_id = file_info.id)").
		Order("created_at ASC").Limit(100).Find(&files).Error
	if err != nil {
		return
	}
	now := time.Now()
	for _, file := range files {
		state := models.FileMetadataState{FileID: file.ID, Status: models.TagStatePending, UpdatedAt: now}
		_ = s.factory.DB().WithContext(s.ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error
	}
}

func (s *TagService) claimMetadataState() (*models.FileMetadataState, bool) {
	now := time.Now()
	var state models.FileMetadataState
	err := s.factory.DB().WithContext(s.ctx).
		Where("status = ? OR (status = ? AND next_retry_at <= ?) OR (status = ? AND lease_expires_at < ?)",
			models.TagStatePending, models.TagStateFailed, now, models.TagStateRunning, now).
		Order("updated_at ASC").First(&state).Error
	if err != nil {
		return nil, false
	}
	token := uuid.NewString()
	result := s.factory.DB().WithContext(s.ctx).Model(&models.FileMetadataState{}).
		Where("file_id = ? AND status = ? AND run_token = ?", state.FileID, state.Status, state.RunToken).
		Updates(map[string]interface{}{
			"status": models.TagStateRunning, "run_token": token,
			"lease_expires_at": now.Add(metadataWorkerLease), "updated_at": now,
		})
	if result.Error != nil || result.RowsAffected != 1 {
		return nil, false
	}
	state.Status = models.TagStateRunning
	state.RunToken = token
	return &state, true
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
		s.Notify()
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
