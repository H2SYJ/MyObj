package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

type tagCloudRow struct {
	ID                  string
	Name                string
	BaseName            string
	BaseCategoryID      string
	BaseCategoryCode    string
	BaseCategoryName    string
	BaseColor           string
	DisplayCategoryID   string
	DisplayCategoryCode string
	DisplayCategoryName string
	DisplayColor        string
	FileCount           int64
	Hidden              bool
	Builtin             bool
	SystemCode          string
}

func cloudItemFromRow(row tagCloudRow) response.TagCloudItem {
	category := response.TagCategoryView{ID: row.BaseCategoryID, Code: row.BaseCategoryCode, Name: row.BaseCategoryName, Color: row.BaseColor}
	if row.DisplayCategoryID != "" {
		category = response.TagCategoryView{ID: row.DisplayCategoryID, Code: row.DisplayCategoryCode, Name: row.DisplayCategoryName, Color: row.DisplayColor}
	}
	return response.TagCloudItem{
		ID: row.ID, Name: row.Name, BaseName: row.BaseName, Category: category,
		BaseCategory: response.TagCategoryView{ID: row.BaseCategoryID, Code: row.BaseCategoryCode, Name: row.BaseCategoryName, Color: row.BaseColor},
		FileCount:    row.FileCount, Hidden: row.Hidden, System: row.Builtin || row.SystemCode != "", SystemCode: row.SystemCode,
	}
}

func (s *TagService) tagCloudQuery(ctx context.Context, userID string) *gorm.DB {
	return s.factory.DB().WithContext(ctx).Table("tag_definition AS td").
		Select(`td.id, COALESCE(pref.display_name, td.name) AS name, td.name AS base_name, td.builtin, td.system_code,
			base.id AS base_category_id, base.code AS base_category_code, base.name AS base_category_name, base.color AS base_color,
			display.id AS display_category_id, display.code AS display_category_code, display.name AS display_category_name, display.color AS display_color,
			COALESCE(pref.hidden, false) AS hidden, COALESCE(stat.file_count, 0) AS file_count`).
		Joins("JOIN tag_category base ON base.id = td.category_id").
		Joins("LEFT JOIN user_tag_preference pref ON pref.tag_id = td.id AND pref.user_id = ?", userID).
		Joins("LEFT JOIN user_tag_stat stat ON stat.tag_id = td.id AND stat.user_id = ?", userID).
		Joins("LEFT JOIN tag_category display ON display.id = pref.display_category_id AND display.enabled = ?", true).
		Where("COALESCE(pref.hidden, false) = ? OR COALESCE(stat.file_count, 0) > 0", true)
}

func (s *TagService) TagCloud(ctx context.Context, userID string) (*response.TagCloudResponse, error) {
	var rows []tagCloudRow
	if err := s.tagCloudQuery(ctx, userID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := &response.TagCloudResponse{Tags: []response.TagCloudItem{}, Hidden: []response.TagCloudItem{}}
	for _, row := range rows {
		item := cloudItemFromRow(row)
		if item.Hidden {
			result.Hidden = append(result.Hidden, item)
		} else if item.FileCount > 0 {
			result.Tags = append(result.Tags, item)
		}
	}
	less := func(items []response.TagCloudItem, i, j int) bool {
		if items[i].FileCount != items[j].FileCount {
			return items[i].FileCount > items[j].FileCount
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	}
	sort.Slice(result.Tags, func(i, j int) bool { return less(result.Tags, i, j) })
	sort.Slice(result.Hidden, func(i, j int) bool { return less(result.Hidden, i, j) })
	return result, nil
}

func (s *TagService) tagCloudItem(ctx context.Context, userID, tagID string) (response.TagCloudItem, error) {
	var row tagCloudRow
	if err := s.tagCloudQuery(ctx, userID).Where("td.id = ?", tagID).Scan(&row).Error; err != nil {
		return response.TagCloudItem{}, err
	}
	if row.ID == "" {
		return response.TagCloudItem{}, errors.New("标签不存在或当前用户未使用")
	}
	return cloudItemFromRow(row), nil
}

func (s *TagService) TagCloudEditor(ctx context.Context, userID, tagID string) (*response.TagCloudEditorResponse, error) {
	item, err := s.tagCloudItem(ctx, userID, tagID)
	if err != nil {
		return nil, err
	}
	set, err := s.PersonalDictionary(ctx, userID)
	if err != nil {
		return nil, err
	}
	aliases := make([]string, 0)
	for _, rule := range set.Rules {
		if rule.Type == models.TagRuleTypeAlias && tagging.Normalize(rule.Replacement) == tagging.Normalize(item.BaseName) && rule.CategoryID == item.BaseCategory.ID {
			aliases = append(aliases, rule.Pattern)
		}
	}
	sort.Strings(aliases)
	return &response.TagCloudEditorResponse{Tag: item, Aliases: aliases}, nil
}

func normalizedAliases(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = tagging.DisplayName(value)
		if !tagging.ValidTagName(value) {
			return nil, errors.New("别名无效")
		}
		key := tagging.Normalize(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *TagService) UpdateTagCloudItem(ctx context.Context, userID, tagID string, input request.UpdateTagCloudItemRequest) (*response.TagCloudEditorResponse, *models.TagRebuildJob, error) {
	current, err := s.TagCloudEditor(ctx, userID, tagID)
	if err != nil {
		return nil, nil, err
	}
	if current.Tag.System {
		return nil, nil, errors.New("系统标签不允许编辑")
	}
	displayName := tagging.DisplayName(input.DisplayName)
	if !tagging.ValidTagName(displayName) {
		return nil, nil, errors.New("标签名称无效")
	}
	normalizedName := tagging.Normalize(displayName)
	var duplicateCount int64
	if err := s.factory.DB().WithContext(ctx).Table("tag_definition AS td").
		Joins("LEFT JOIN user_tag_preference pref ON pref.tag_id = td.id AND pref.user_id = ?", userID).
		Where("td.id <> ? AND COALESCE(pref.normalized_display_name, td.normalized_name) = ?", tagID, normalizedName).
		Where(`COALESCE(pref.hidden, false) = ? OR EXISTS (SELECT 1
			FROM user_file_tag uft
			JOIN user_files uf ON uf.uf_id = uft.uf_id AND uf.user_id = uft.user_id AND uf.deleted_at IS NULL
			WHERE uft.user_id = ? AND uft.tag_id = td.id
			AND NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e
				WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id))`, true, userID).
		Count(&duplicateCount).Error; err != nil {
		return nil, nil, err
	}
	if duplicateCount > 0 {
		return nil, nil, errors.New("标签名称已存在")
	}
	var normalizedDisplayName *string
	if normalizedName == tagging.Normalize(current.Tag.BaseName) {
		displayName = ""
	} else {
		normalizedDisplayName = &normalizedName
	}
	displayCategoryID := strings.TrimSpace(input.DisplayCategoryID)
	if displayCategoryID == current.Tag.BaseCategory.ID {
		displayCategoryID = ""
	}
	if displayCategoryID != "" {
		var count int64
		if err := s.factory.DB().WithContext(ctx).Model(&models.TagCategory{}).Where("id = ? AND enabled = ?", displayCategoryID, true).Count(&count).Error; err != nil {
			return nil, nil, err
		}
		if count != 1 {
			return nil, nil, errors.New("显示分类不存在或已停用")
		}
	}
	aliases, err := normalizedAliases(input.Aliases)
	if err != nil {
		return nil, nil, err
	}
	currentAliases := append([]string(nil), current.Aliases...)
	sort.Strings(currentAliases)
	aliasesChanged := !sameStrings(currentAliases, aliases)
	var inputs []request.TagRuleInput
	if aliasesChanged {
		set, loadErr := s.PersonalDictionary(ctx, userID)
		if loadErr != nil {
			return nil, nil, loadErr
		}
		inputs = make([]request.TagRuleInput, 0, len(set.Rules)+len(aliases))
		for _, rule := range set.Rules {
			if rule.Type == models.TagRuleTypeAlias && tagging.Normalize(rule.Replacement) == tagging.Normalize(current.Tag.BaseName) && rule.CategoryID == current.Tag.BaseCategory.ID {
				continue
			}
			inputs = append(inputs, request.TagRuleInput{ID: rule.ID, Type: rule.Type, TargetField: rule.TargetField, Pattern: rule.Pattern, Replacement: rule.Replacement, CategoryID: rule.CategoryID, Priority: rule.Priority, Weight: rule.Weight, Enabled: rule.Enabled})
		}
		for _, alias := range aliases {
			inputs = append(inputs, request.TagRuleInput{Type: models.TagRuleTypeAlias, TargetField: "basename", Pattern: alias, Replacement: current.Tag.BaseName, CategoryID: current.Tag.BaseCategory.ID, Weight: 1, Enabled: true})
		}
	}
	now := time.Now()
	pref := models.UserTagPreference{UserID: userID, TagID: tagID, UpdatedAt: now, CreatedAt: now}
	if displayName != "" {
		pref.DisplayName = &displayName
		pref.NormalizedDisplayName = normalizedDisplayName
	}
	if displayCategoryID != "" {
		pref.DisplayCategoryID = &displayCategoryID
	}
	var job *models.TagRebuildJob
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if aliasesChanged {
			if _, createdJob, saveErr := s.savePersonalDictionary(ctx, userID, inputs, tx); saveErr != nil {
				return saveErr
			} else {
				job = createdJob
			}
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "tag_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"display_name": pref.DisplayName, "normalized_display_name": pref.NormalizedDisplayName,
				"display_category_id": pref.DisplayCategoryID, "updated_at": now,
			}),
		}).Create(&pref).Error; err != nil {
			return err
		}
		return s.refreshUserTagStats(ctx, tx, userID, []string{tagID})
	})
	if err != nil {
		return nil, nil, err
	}
	if aliasesChanged {
		s.afterPersonalDictionarySaved(userID)
	}
	updated, err := s.TagCloudEditor(ctx, userID, tagID)
	return updated, job, err
}

func (s *TagService) HideTagCloudItem(ctx context.Context, userID, tagID string) error {
	item, err := s.tagCloudItem(ctx, userID, tagID)
	if err != nil {
		return err
	}
	if item.System {
		return errors.New("系统标签不允许隐藏")
	}
	now := time.Now()
	pref := models.UserTagPreference{UserID: userID, TagID: tagID, Hidden: true, CreatedAt: now, UpdatedAt: now}
	return s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "tag_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"hidden": true, "updated_at": now}),
		}).Create(&pref).Error; err != nil {
			return err
		}
		return s.refreshUserTagStats(ctx, tx, userID, []string{tagID})
	})
}

func (s *TagService) RestoreTagCloudItem(ctx context.Context, userID, tagID string) (*models.TagRebuildJob, error) {
	var definition models.TagDefinition
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", tagID).First(&definition).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("标签不存在或当前用户未使用")
		}
		return nil, err
	}
	if definition.Builtin || definition.SystemCode != nil {
		return nil, errors.New("系统标签无需恢复")
	}
	var preference models.UserTagPreference
	err := s.factory.DB().WithContext(ctx).Where("user_id = ? AND tag_id = ?", userID, tagID).First(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if _, itemErr := s.tagCloudItem(ctx, userID, tagID); itemErr != nil {
			return nil, itemErr
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !preference.Hidden {
		return nil, nil
	}
	var targetVersion int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagRuleSet{}).
		Where("scope_type = ? AND scope_id = ? AND status = ?", models.TagRuleScopeUser, userID, models.TagRuleSetActive).
		Select("COALESCE(MAX(version), 0)").Scan(&targetVersion).Error; err != nil {
		return nil, err
	}
	var job *models.TagRebuildJob
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.UserTagPreference{}).
			Where("user_id = ? AND tag_id = ? AND hidden = ?", userID, tagID, true).
			Updates(map[string]interface{}{"hidden": false, "updated_at": time.Now()})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		createdJob, createErr := s.createRebuildJob(ctx, models.TagRuleScopeUser, userID, targetVersion, userID, tx)
		if createErr != nil {
			return createErr
		}
		job = createdJob
		return s.refreshUserTagStats(ctx, tx, userID, []string{tagID})
	})
	if err != nil {
		return nil, err
	}
	if job != nil {
		s.notifyRebuild()
	}
	return job, nil
}

type tagStatCountRow struct {
	UserID    string
	TagID     string
	FileCount int64
}

type tagStatKey struct {
	UserID string
	TagID  string
}

func (s *TagService) tagIDsForUserFile(ctx context.Context, tx *gorm.DB, userID, ufID string) ([]string, error) {
	return tagIDsForUserFile(ctx, tx, userID, ufID)
}

func tagIDsForUserFile(ctx context.Context, tx *gorm.DB, userID, ufID string) ([]string, error) {
	var tagIDs []string
	if err := tx.WithContext(ctx).Model(&models.UserFileTag{}).
		Where("user_id = ? AND uf_id = ?", userID, ufID).
		Distinct("tag_id").Pluck("tag_id", &tagIDs).Error; err != nil {
		return nil, err
	}
	var excluded []string
	if err := tx.WithContext(ctx).Model(&models.UserFileTagExclusion{}).
		Where("user_id = ? AND uf_id = ?", userID, ufID).
		Distinct("tag_id").Pluck("tag_id", &excluded).Error; err != nil {
		return nil, err
	}
	return uniqueTagStrings(append(tagIDs, excluded...)), nil
}

func (s *TagService) refreshUserTagStats(ctx context.Context, tx *gorm.DB, userID string, tagIDs []string) error {
	return refreshUserTagStats(ctx, tx, userID, tagIDs)
}

func (s *TagService) refreshUserTagStatsBatch(ctx context.Context, tx *gorm.DB, affectedTagIDs map[string][]string) error {
	return refreshUserTagStatsBatch(ctx, tx, affectedTagIDs)
}

func refreshUserTagStats(ctx context.Context, tx *gorm.DB, userID string, tagIDs []string) error {
	return refreshUserTagStatsBatch(ctx, tx, map[string][]string{userID: tagIDs})
}

func refreshUserTagStatsBatch(ctx context.Context, tx *gorm.DB, affectedTagIDs map[string][]string) error {
	userIDs := make([]string, 0, len(affectedTagIDs))
	normalizedTagIDs := make(map[string][]string, len(affectedTagIDs))
	for userID := range affectedTagIDs {
		tagIDs := uniqueTagStrings(affectedTagIDs[userID])
		if userID == "" || len(tagIDs) == 0 {
			continue
		}
		sort.Strings(tagIDs)
		normalizedTagIDs[userID] = tagIDs
		userIDs = append(userIDs, userID)
	}
	if len(userIDs) == 0 {
		return nil
	}
	sort.Strings(userIDs)
	conditions := make([]string, 0, len(userIDs))
	conditionArgs := make([]interface{}, 0, len(userIDs)*2)
	for _, userID := range userIDs {
		conditions = append(conditions, "(uft.user_id = ? AND uft.tag_id IN ?)")
		conditionArgs = append(conditionArgs, userID, normalizedTagIDs[userID])
	}
	var rows []tagStatCountRow
	if err := tx.WithContext(ctx).Table("user_file_tag AS uft").
		Select("uft.user_id, uft.tag_id, COUNT(DISTINCT uft.uf_id) AS file_count").
		Joins("JOIN user_files uf ON uf.uf_id = uft.uf_id AND uf.user_id = uft.user_id AND uf.deleted_at IS NULL").
		Where("("+strings.Join(conditions, " OR ")+")", conditionArgs...).
		Where(`NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e
			WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)`).
		Group("uft.user_id, uft.tag_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	counts := make(map[tagStatKey]int64, len(rows))
	for _, row := range rows {
		counts[tagStatKey{UserID: row.UserID, TagID: row.TagID}] = row.FileCount
	}
	now := time.Now()
	for _, userID := range userIDs {
		for _, tagID := range normalizedTagIDs[userID] {
			count := counts[tagStatKey{UserID: userID, TagID: tagID}]
			if count <= 0 {
				if err := tx.WithContext(ctx).Where("user_id = ? AND tag_id = ?", userID, tagID).Delete(&models.UserTagStat{}).Error; err != nil {
					return err
				}
				continue
			}
			stat := models.UserTagStat{UserID: userID, TagID: tagID, FileCount: count, UpdatedAt: now}
			if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "user_id"}, {Name: "tag_id"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"file_count": count, "updated_at": now,
				}),
			}).Create(&stat).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
