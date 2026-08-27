package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

// 本文件负责把文件名和元数据转换为自动标签。
func (s *TagService) GenerateUserFile(ctx context.Context, userID, ufID, runToken string, targetVersion int64) error {
	return s.generateUserFile(ctx, userID, ufID, runToken, targetVersion, nil)
}

func (s *TagService) generateUserFile(ctx context.Context, userID, ufID, runToken string, targetVersion int64, rebuildGuard *tagRebuildGuard) error {
	_, err := s.generateUserFileWithStats(ctx, userID, ufID, runToken, targetVersion, rebuildGuard, true)
	return err
}

func (s *TagService) generateUserFileForRebuild(ctx context.Context, userID, ufID, runToken string, targetVersion int64, rebuildGuard *tagRebuildGuard) ([]string, error) {
	return s.generateUserFileWithStats(ctx, userID, ufID, runToken, targetVersion, rebuildGuard, false)
}

func (s *TagService) generateUserFileWithStats(ctx context.Context, userID, ufID, runToken string, targetVersion int64, rebuildGuard *tagRebuildGuard, refreshStatsImmediately bool) ([]string, error) {
	if !s.autoEnabled.Load() {
		return nil, errAutoTagDisabled
	}
	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	if targetVersion > 0 && snapshot.GlobalVersion != targetVersion {
		return nil, errStaleTagGeneration
	}

	var userFile models.UserFiles
	if err := s.factory.DB().WithContext(ctx).Where("user_id = ? AND uf_id = ? AND deleted_at IS NULL", userID, ufID).First(&userFile).Error; err != nil {
		return nil, err
	}
	var fileInfo models.FileInfo
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", userFile.FileID).First(&fileInfo).Error; err != nil {
		return nil, err
	}
	metadata, metadataVersion, metadataPartial := s.loadMetadata(ctx, userFile.FileID, fileInfo.IsEnc)
	candidates := snapshot.Generate(tagging.Input{
		Filename: userFile.FileName, MIME: fileInfo.Mime, Size: int64(fileInfo.Size),
		IsEnc: fileInfo.IsEnc, Metadata: metadata,
	})

	status := models.TagStateReady
	if metadataPartial {
		status = models.TagStatePartial
	}
	now := time.Now()
	var affectedTagIDs []string
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if runToken != "" {
			var count int64
			if err := tx.Model(&models.UserFileTagState{}).
				Where("uf_id = ? AND run_token = ?", ufID, runToken).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return errStaleTagGeneration
			}
		}
		if err := validateTagRebuildGuard(tx, rebuildGuard); err != nil {
			return err
		}
		if current := s.globalRuntime.Load(); current == nil || current.snapshot.GlobalVersion != snapshot.GlobalVersion {
			return errStaleTagGeneration
		}
		var activeGlobalVersion int64
		if err := tx.Model(&models.TagRuleSet{}).
			Where("status = ?", models.TagRuleSetActive).
			Select("COALESCE(MAX(version), 0)").Scan(&activeGlobalVersion).Error; err != nil {
			return err
		}
		if activeGlobalVersion != snapshot.GlobalVersion {
			return errStaleTagGeneration
		}
		var oldAutomaticTagIDs []string
		if err := tx.Model(&models.UserFileTag{}).
			Where("user_id = ? AND uf_id = ? AND source_type <> ?", userID, ufID, models.TagSourceManual).
			Distinct("tag_id").Pluck("tag_id", &oldAutomaticTagIDs).Error; err != nil {
			return err
		}
		affectedTagIDs = append(affectedTagIDs, oldAutomaticTagIDs...)
		if err := tx.Where("user_id = ? AND uf_id = ? AND source_type <> ?", userID, ufID, models.TagSourceManual).
			Delete(&models.UserFileTag{}).Error; err != nil {
			return err
		}
		var excluded []string
		if err := tx.Model(&models.UserFileTagExclusion{}).Where("user_id = ? AND uf_id = ?", userID, ufID).
			Pluck("tag_id", &excluded).Error; err != nil {
			return err
		}
		excludedSet := make(map[string]struct{}, len(excluded))
		for _, id := range excluded {
			excludedSet[id] = struct{}{}
		}
		for _, candidate := range candidates {
			tag, tagErr := ensureTagDefinition(tx, candidate.Name, candidate.CategoryID)
			if tagErr != nil {
				return tagErr
			}
			affectedTagIDs = append(affectedTagIDs, tag.ID)
			if _, suppressed := excludedSet[tag.ID]; suppressed {
				continue
			}
			binding := &models.UserFileTag{
				ID: uuid.NewString(), UserID: userID, UFID: ufID, TagID: tag.ID,
				SourceType: candidate.SourceType, SourceKey: candidate.SourceKey,
				RuleVersion: snapshot.GlobalVersion, Visibility: models.TagVisibilityInherit,
				CreatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(binding).Error; err != nil {
				return err
			}
		}
		updates := map[string]interface{}{
			"global_version":   snapshot.GlobalVersion,
			"metadata_version": metadataVersion, "status": status, "last_error": "",
			"retry_count": 0, "next_retry_at": nil, "run_token": "", "lease_expires_at": nil,
			"generated_at": now, "updated_at": now,
		}
		query := tx.Model(&models.UserFileTagState{}).Where("uf_id = ?", ufID)
		if runToken != "" {
			query = query.Where("run_token = ?", runToken)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errStaleTagGeneration
		}
		if refreshStatsImmediately {
			return s.refreshUserTagStats(ctx, tx, userID, affectedTagIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	logger.LOG.Info("自动标签生成完成", "uf_id", ufID, "global_version", snapshot.GlobalVersion,
		"generated", len(candidates), "status", status)
	return uniqueTagStrings(affectedTagIDs), nil
}

func validateTagRebuildGuard(tx *gorm.DB, guard *tagRebuildGuard) error {
	if guard == nil {
		return nil
	}
	var count int64
	if err := tx.Model(&models.TagRebuildJob{}).
		Where("id = ? AND run_token = ? AND status = ?", guard.jobID, guard.runToken, "running").
		Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errStaleTagGeneration
	}
	return nil
}

func (s *TagService) loadMetadata(ctx context.Context, fileID string, encrypted bool) (map[string]string, int64, bool) {
	var entries []models.FileMetadata
	_ = s.factory.DB().WithContext(ctx).Where("file_id = ?", fileID).Find(&entries).Error
	metadata := make(map[string]string, len(entries))
	var version int64
	for _, entry := range entries {
		metadata[entry.Key] = entry.Value
		version = max(version, entry.Version)
	}
	var state models.FileMetadataState
	err := s.factory.DB().WithContext(ctx).Where("file_id = ?", fileID).First(&state).Error
	partial := encrypted && len(entries) == 0
	if err == nil {
		version = max(version, state.Version)
		partial = state.Status == models.TagStatePartial || state.Status == models.TagStateFailed
	}
	return metadata, version, partial
}
