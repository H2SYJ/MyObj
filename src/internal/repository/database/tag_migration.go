package database

import (
	"errors"
	"fmt"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var builtinTagCategories = []models.TagCategory{
	{ID: "title", Code: "title", Name: "标题", Color: "#409eff", SortOrder: 10, Enabled: true, Builtin: true},
	{ID: "file_type", Code: "file_type", Name: "文件类型", Color: "#67c23a", SortOrder: 20, Enabled: true, Builtin: true},
	{ID: "year", Code: "year", Name: "年份", Color: "#e6a23c", SortOrder: 30, Enabled: true, Builtin: true},
	{ID: "season_episode", Code: "season_episode", Name: "季集", Color: "#f56c6c", SortOrder: 40, Enabled: true, Builtin: true},
	{ID: "resolution", Code: "resolution", Name: "分辨率", Color: "#909399", SortOrder: 50, Enabled: true, Builtin: true},
	{ID: "codec", Code: "codec", Name: "编码", Color: "#7b61ff", SortOrder: 60, Enabled: true, Builtin: true},
	{ID: "source", Code: "source", Name: "来源", Color: "#13ce66", SortOrder: 70, Enabled: true, Builtin: true},
	{ID: "language", Code: "language", Name: "语言", Color: "#ff8a00", SortOrder: 80, Enabled: true, Builtin: true},
	{ID: "other", Code: "other", Name: "其他", Color: "#909399", SortOrder: 90, Enabled: true, Builtin: true},
}

const pureNumericTagCleanupVersion = "20260804_pure_numeric_tag_cleanup"

func migrateTaggingSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&schemaMigration{},
		&models.TagCategory{},
		&models.TagDefinition{},
		&models.UserTagPreference{},
		&models.UserFileTag{},
		&models.UserDirectoryTag{},
		&models.UserFileTagExclusion{},
		&models.UserFileTagState{},
		&models.FileMetadata{},
		&models.FileMetadataState{},
		&models.TagRuleSet{},
		&models.TagRule{},
		&models.TagRebuildJob{},
		&models.TagRebuildFailure{},
	); err != nil {
		return fmt.Errorf("创建标签数据表失败: %w", err)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		for _, item := range builtinTagCategories {
			item.CreatedAt = now
			item.UpdatedAt = now
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "code"}},
				DoNothing: true,
			}).Create(&item).Error; err != nil {
				return fmt.Errorf("初始化标签分类%s失败: %w", item.Code, err)
			}
		}
		if err := seedCinemaModeTag(tx, now); err != nil {
			return err
		}
		if err := migratePureNumericTags(tx, now); err != nil {
			return err
		}

		for key, value := range map[string]string{
			"auto_tag_enabled": "true",
			"auto_tag_limit":   "20",
		} {
			var count int64
			if err := tx.Model(&models.SysConfig{}).Where("`key` = ?", key).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := tx.Create(&models.SysConfig{Key: key, Value: value}).Error; err != nil {
					return fmt.Errorf("初始化标签配置%s失败: %w", key, err)
				}
			}
		}

		seeded, version, err := seedInitialGlobalTagRules(tx, now)
		if err != nil {
			return err
		}
		if err := seedFileTagPermission(tx); err != nil {
			return err
		}
		if seeded {
			var total int64
			if err := tx.Model(&models.UserFiles{}).Where("deleted_at IS NULL").Count(&total).Error; err != nil {
				return err
			}
			if total > 0 {
				job := &models.TagRebuildJob{
					ID: uuid.NewString(), ScopeType: models.TagRuleScopeGlobal,
					TargetVersion: version, Status: "pending", Total: total,
					CreatedAt: now, UpdatedAt: now,
				}
				if err := tx.Create(job).Error; err != nil {
					return fmt.Errorf("创建首次标签重建任务失败: %w", err)
				}
			}
		}
		return nil
	})
}

func migratePureNumericTags(tx *gorm.DB, now time.Time) error {
	var applied int64
	if err := tx.Model(&schemaMigration{}).Where("version = ?", pureNumericTagCleanupVersion).Count(&applied).Error; err != nil {
		return fmt.Errorf("查询纯数字标签清理状态失败: %w", err)
	}
	if applied > 0 {
		return nil
	}

	var definitions []models.TagDefinition
	if err := tx.Select("id", "name").Find(&definitions).Error; err != nil {
		return fmt.Errorf("查询纯数字标签失败: %w", err)
	}
	tagIDs := make([]string, 0)
	for _, definition := range definitions {
		if tagging.IsPureNumericTagName(definition.Name) {
			tagIDs = append(tagIDs, definition.ID)
		}
	}
	if len(tagIDs) > 0 {
		for _, model := range []interface{}{
			&models.UserFileTag{},
			&models.UserDirectoryTag{},
			&models.UserFileTagExclusion{},
			&models.RecycledDirectoryTag{},
		} {
			if !tx.Migrator().HasTable(model) {
				continue
			}
			if err := tx.Where("tag_id IN ?", tagIDs).Delete(model).Error; err != nil {
				return fmt.Errorf("删除纯数字标签关联失败: %w", err)
			}
		}
		if err := tx.Where("id IN ?", tagIDs).Delete(&models.TagDefinition{}).Error; err != nil {
			return fmt.Errorf("删除纯数字标签定义失败: %w", err)
		}
	}
	if err := tx.Create(&schemaMigration{Version: pureNumericTagCleanupVersion, AppliedAt: now}).Error; err != nil {
		return fmt.Errorf("记录纯数字标签清理状态失败: %w", err)
	}
	return nil
}

func seedCinemaModeTag(tx *gorm.DB, now time.Time) error {
	const categoryID = "other"
	var tag models.TagDefinition
	err := tx.Where("system_code = ?", models.TagSystemCodeCinemaMode).First(&tag).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = tx.Where("normalized_name = ?", tagging.Normalize(models.TagNameCinemaMode)).
			Order("CASE WHEN category_id = 'other' THEN 0 ELSE 1 END, created_at ASC, id ASC").First(&tag).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询影视模式标签失败: %w", err)
	}
	if err == gorm.ErrRecordNotFound {
		code := models.TagSystemCodeCinemaMode
		tag = models.TagDefinition{
			ID: uuid.NewString(), Name: models.TagNameCinemaMode, NormalizedName: tagging.Normalize(models.TagNameCinemaMode),
			CategoryID: categoryID, SystemCode: &code, Builtin: true, CreatedAt: now,
		}
		if err := tx.Create(&tag).Error; err != nil {
			return fmt.Errorf("初始化影视模式标签失败: %w", err)
		}
		return nil
	}
	return tx.Model(&models.TagDefinition{}).Where("id = ?", tag.ID).
		Updates(map[string]interface{}{"system_code": models.TagSystemCodeCinemaMode, "builtin": true}).Error
}

func seedInitialGlobalTagRules(tx *gorm.DB, now time.Time) (bool, int64, error) {
	var active models.TagRuleSet
	err := tx.Where("scope_type = ? AND scope_id = '' AND status = ?", models.TagRuleScopeGlobal, models.TagRuleSetActive).
		Order("version DESC").First(&active).Error
	if err == nil {
		return false, active.Version, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return false, 0, err
	}

	var maxVersion int64
	if err := tx.Model(&models.TagRuleSet{}).Where("scope_type = ? AND scope_id = ''", models.TagRuleScopeGlobal).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return false, 0, err
	}
	version := maxVersion + 1
	ruleSet := &models.TagRuleSet{
		ID: uuid.NewString(), ScopeType: models.TagRuleScopeGlobal, Version: version,
		Revision: 1, Status: models.TagRuleSetActive, CreatedBy: "system",
		CreatedAt: now, UpdatedAt: now, PublishedAt: &now,
	}
	if err := tx.Create(ruleSet).Error; err != nil {
		return false, 0, fmt.Errorf("初始化全局标签规则集失败: %w", err)
	}

	rules := []models.TagRule{
		{Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?:^|[^0-9])((?:19|20)\d{2})(?:[^0-9]|$)`, Replacement: "$1", CategoryID: "year", Priority: 90, Weight: 1},
		{Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(?:^|[^a-z0-9])(2160p|4k|1080p|720p|8k)(?:[^a-z0-9]|$)`, Replacement: "$1", CategoryID: "resolution", Priority: 100, Weight: 1},
		{Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(?:^|[^a-z0-9])(h\.?264|h\.?265|x264|x265|hevc|av1)(?:[^a-z0-9]|$)`, Replacement: "$1", CategoryID: "codec", Priority: 95, Weight: 1},
		{Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(?:^|[^a-z0-9])(web[- .]?dl|web[- .]?rip|blu[- .]?ray|bdrip|hdtv)(?:[^a-z0-9]|$)`, Replacement: "$1", CategoryID: "source", Priority: 90, Weight: 1},
		{Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(?:^|[^a-z0-9])(s\d{1,2}e\d{1,3})(?:[^a-z0-9]|$)`, Replacement: "$1", CategoryID: "season_episode", Priority: 100, Weight: 1},
		{Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(国语|粤语|日语|英语|中文字幕|中英字幕)`, Replacement: "$1", CategoryID: "language", Priority: 90, Weight: 1},
	}
	for index := range rules {
		rules[index].ID = uuid.NewString()
		rules[index].RuleSetID = ruleSet.ID
		rules[index].Enabled = true
		rules[index].CreatedAt = now
		rules[index].UpdatedAt = now
	}
	if err := tx.Create(&rules).Error; err != nil {
		return false, 0, fmt.Errorf("初始化全局标签规则失败: %w", err)
	}
	return true, version, nil
}

func seedFileTagPermission(tx *gorm.DB) error {
	var tagPower models.Power
	err := tx.Where("characteristic = ?", "file:tag").First(&tagPower).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		var maxID int
		if err := tx.Model(&models.Power{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
			return err
		}
		tagPower = models.Power{
			ID: maxID + 1, Name: "文件标签", Description: "维护文件标签和个人分词词典",
			Characteristic: "file:tag", CreatedAt: custom_type.Now(),
		}
		if err := tx.Create(&tagPower).Error; err != nil {
			return fmt.Errorf("初始化文件标签权限失败: %w", err)
		}
	}

	var previewPower models.Power
	if err := tx.Where("characteristic = ?", "file:preview").First(&previewPower).Error; err != nil {
		return nil
	}
	var groupIDs []int
	if err := tx.Table("group_power").Where("power_id = ?", previewPower.ID).Pluck("group_id", &groupIDs).Error; err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		grant := models.GroupPower{GroupID: groupID, PowerID: tagPower.ID}
		var count int64
		if err := tx.Model(&models.GroupPower{}).Where("group_id = ? AND power_id = ?", groupID, tagPower.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := tx.Create(&grant).Error; err != nil {
				return fmt.Errorf("授予用户组%d文件标签权限失败: %w", groupID, err)
			}
		}
	}
	return nil
}
