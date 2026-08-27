package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

// 本文件负责文件人工标签、自动标签屏蔽和状态读取。
func (s *TagService) TagStates(ctx context.Context, ufIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(ufIDs))
	if len(ufIDs) == 0 {
		return result, nil
	}
	var states []models.UserFileTagState
	if err := s.factory.DB().WithContext(ctx).Where("uf_id IN ?", ufIDs).Find(&states).Error; err != nil {
		return nil, err
	}
	for _, state := range states {
		result[state.UFID] = state.Status
	}
	return result, nil
}

type detailedTagRow struct {
	ID           string
	Name         string
	CategoryID   string
	CategoryCode string
	CategoryName string
	Color        string
	SourceType   string
	Visibility   string
}

func (s *TagService) GetFileTags(ctx context.Context, userID, ufID string) (*response.FileTagsResponse, error) {
	if err := s.ensureOwnership(ctx, userID, []string{ufID}); err != nil {
		return nil, err
	}
	var rows []detailedTagRow
	err := s.factory.DB().WithContext(ctx).Table("user_file_tag AS uft").
		Select("td.id, td.name, tc.id AS category_id, tc.code AS category_code, tc.name AS category_name, tc.color, uft.source_type, uft.visibility").
		Joins("JOIN tag_definition td ON td.id = uft.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("uft.user_id = ? AND uft.uf_id = ?", userID, ufID).
		Order("tc.sort_order ASC, td.name ASC, td.id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	aggregated := map[string]*response.TagView{}
	for _, row := range rows {
		view := aggregated[row.ID]
		if view == nil {
			view = &response.TagView{
				ID: row.ID, Name: row.Name,
				Category:   response.TagCategoryView{ID: row.CategoryID, Code: row.CategoryCode, Name: row.CategoryName, Color: row.Color},
				Visibility: row.Visibility,
			}
			aggregated[row.ID] = view
		}
		view.Sources = appendUnique(view.Sources, row.SourceType)
		if row.SourceType != models.TagSourceManual {
			view.Automatic = true
		}
		if row.SourceType == models.TagSourceManual {
			view.Visibility = row.Visibility
		}
	}
	responseData := &response.FileTagsResponse{FileID: ufID, Tags: make([]response.TagView, 0, len(aggregated)), Suppressed: []response.TagView{}}
	for _, tag := range aggregated {
		responseData.Tags = append(responseData.Tags, *tag)
	}
	sort.Slice(responseData.Tags, func(i, j int) bool { return responseData.Tags[i].Name < responseData.Tags[j].Name })

	var suppressed []detailedTagRow
	if err := s.factory.DB().WithContext(ctx).Table("user_file_tag_exclusion AS e").
		Select("td.id, td.name, tc.id AS category_id, tc.code AS category_code, tc.name AS category_name, tc.color").
		Joins("JOIN tag_definition td ON td.id = e.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("e.user_id = ? AND e.uf_id = ?", userID, ufID).
		Scan(&suppressed).Error; err != nil {
		return nil, err
	}
	for _, row := range suppressed {
		responseData.Suppressed = append(responseData.Suppressed, response.TagView{
			ID: row.ID, Name: row.Name, Automatic: true, Suppressed: true,
			Category: response.TagCategoryView{ID: row.CategoryID, Code: row.CategoryCode, Name: row.CategoryName, Color: row.Color},
		})
	}
	var state models.UserFileTagState
	if err := s.factory.DB().WithContext(ctx).Where("uf_id = ?", ufID).First(&state).Error; err == nil {
		responseData.State = state.Status
		responseData.LastError = state.LastError
		responseData.UpdatedAt = state.UpdatedAt
	}
	return responseData, nil
}

func (s *TagService) UpdateManualTags(ctx context.Context, userID string, fileIDs []string, add []request.ManualTagInput, removeTagIDs []string) error {
	if len(fileIDs) == 0 || len(fileIDs) > 100 {
		return errors.New("文件数量必须在1到100之间")
	}
	if err := s.ensureOwnership(ctx, userID, fileIDs); err != nil {
		return err
	}
	return s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		affectedTagIDs := append([]string(nil), removeTagIDs...)
		for _, ufID := range fileIDs {
			if len(removeTagIDs) > 0 {
				if err := tx.Where("user_id = ? AND uf_id = ? AND source_type = ? AND tag_id IN ?", userID, ufID, models.TagSourceManual, removeTagIDs).
					Delete(&models.UserFileTag{}).Error; err != nil {
					return err
				}
			}
			for _, input := range add {
				visibility := input.Visibility
				if visibility == "" {
					visibility = models.TagVisibilityPrivate
				}
				if visibility != models.TagVisibilityPrivate && visibility != models.TagVisibilityPublic {
					return errors.New("手工标签公开范围无效")
				}
				tag, err := ensureTagDefinition(tx, input.Name, input.CategoryID)
				if err != nil {
					return err
				}
				affectedTagIDs = append(affectedTagIDs, tag.ID)
				binding := models.UserFileTag{
					ID: uuid.NewString(), UserID: userID, UFID: ufID, TagID: tag.ID,
					SourceType: models.TagSourceManual, SourceKey: "user", Visibility: visibility,
					CreatedBy: userID, CreatedAt: time.Now(),
				}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "uf_id"}, {Name: "tag_id"}, {Name: "source_type"}, {Name: "source_key"}},
					DoUpdates: clause.Assignments(map[string]interface{}{"visibility": visibility}),
				}).Create(&binding).Error; err != nil {
					return err
				}
			}
			var manualCount int64
			if err := tx.Model(&models.UserFileTag{}).Where("user_id = ? AND uf_id = ? AND source_type = ?", userID, ufID, models.TagSourceManual).
				Distinct("tag_id").Count(&manualCount).Error; err != nil {
				return err
			}
			if manualCount > 100 {
				return errors.New("每个文件最多允许100个手工标签")
			}
		}
		return s.refreshUserTagStats(ctx, tx, userID, affectedTagIDs)
	})
}

func (s *TagService) UpdateExclusions(ctx context.Context, userID, ufID string, suppress, restore []string) error {
	if err := s.ensureOwnership(ctx, userID, []string{ufID}); err != nil {
		return err
	}
	affectedTagIDs := uniqueTagStrings(append(append([]string(nil), suppress...), restore...))
	err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, tagID := range suppress {
			var autoCount int64
			if err := tx.Model(&models.UserFileTag{}).Where("user_id = ? AND uf_id = ? AND tag_id = ? AND source_type <> ?", userID, ufID, tagID, models.TagSourceManual).Count(&autoCount).Error; err != nil {
				return err
			}
			if autoCount == 0 {
				continue
			}
			exclusion := models.UserFileTagExclusion{UserID: userID, UFID: ufID, TagID: tagID, CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&exclusion).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ? AND uf_id = ? AND tag_id = ? AND source_type <> ?", userID, ufID, tagID, models.TagSourceManual).
				Delete(&models.UserFileTag{}).Error; err != nil {
				return err
			}
		}
		if len(restore) > 0 {
			if err := tx.Where("user_id = ? AND uf_id = ? AND tag_id IN ?", userID, ufID, restore).
				Delete(&models.UserFileTagExclusion{}).Error; err != nil {
				return err
			}
			if err := tagging.QueueUserFile(ctx, tx, userID, ufID); err != nil {
				return err
			}
		}
		return s.refreshUserTagStats(ctx, tx, userID, affectedTagIDs)
	})
	if err == nil && len(restore) > 0 {
		s.notifyPending()
	}
	return err
}

func (s *TagService) ensureOwnership(ctx context.Context, userID string, ufIDs []string) error {
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.UserFiles{}).
		Where("user_id = ? AND uf_id IN ? AND deleted_at IS NULL", userID, uniqueTagStrings(ufIDs)).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueTagStrings(ufIDs))) {
		return errors.New("文件不存在或无权访问")
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func uniqueTagStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
