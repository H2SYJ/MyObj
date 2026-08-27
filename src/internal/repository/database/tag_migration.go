package database

import (
	"errors"
	"fmt"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
	"strings"
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
const userTagStatBackfillVersion = "20260813_user_tag_stat_backfill"
const globalOnlyTagMigrationVersion = "20260827_global_only_tag_rules"
const globalOnlyTagRebuildJobID = "migration-global-tag-rebuild-20260827"

func migrateTaggingSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&schemaMigration{},
		&models.TagCategory{},
		&models.TagDefinition{},
		&models.UserFileTag{},
		&models.UserDirectoryTag{},
		&models.UserFileTagExclusion{},
		&models.UserTagStat{},
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
	if err := migrateGlobalOnlyTagSchema(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&models.UserFileTagState{}, &models.TagRuleSet{}, &models.TagRebuildJob{}); err != nil {
		return fmt.Errorf("重建全局标签索引失败: %w", err)
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
		if err := backfillUserTagStats(tx, now); err != nil {
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

		_, version, err := seedInitialGlobalTagRules(tx, now)
		if err != nil {
			return err
		}
		if err := seedFileTagPermission(tx); err != nil {
			return err
		}
		if err := finalizeGlobalOnlyTagMigration(tx, now, version); err != nil {
			return err
		}
		return nil
	})
}

// migrateGlobalOnlyTagSchema 先删除所有个人范围数据，再物理移除个人规则和展示偏好结构。
// 每一步都依据当前表结构判断，MySQL DDL 中断后也可以从下一次启动继续执行。
func migrateGlobalOnlyTagSchema(db *gorm.DB) error {
	if db.Migrator().HasTable("tag_rule_set") && db.Migrator().HasColumn("tag_rule_set", "scope_type") {
		if db.Migrator().HasTable("tag_rule") {
			if err := db.Exec(`DELETE FROM tag_rule WHERE rule_set_id IN (
				SELECT id FROM tag_rule_set WHERE scope_type <> ? OR scope_id <> ?
			)`, "global", "").Error; err != nil {
				return fmt.Errorf("删除个人标签规则失败: %w", err)
			}
		}
		if err := db.Exec("DELETE FROM tag_rule_set WHERE scope_type <> ? OR scope_id <> ?", "global", "").Error; err != nil {
			return fmt.Errorf("删除个人标签规则集失败: %w", err)
		}
	}

	if db.Migrator().HasTable("tag_rebuild_job") && db.Migrator().HasColumn("tag_rebuild_job", "scope_type") {
		if db.Migrator().HasTable("tag_rebuild_failure") {
			if err := db.Exec(`DELETE FROM tag_rebuild_failure WHERE job_id IN (
				SELECT id FROM tag_rebuild_job WHERE scope_type <> ? OR scope_id <> ?
			)`, "global", "").Error; err != nil {
				return fmt.Errorf("删除个人标签重建失败明细失败: %w", err)
			}
		}
		if err := db.Exec("DELETE FROM tag_rebuild_job WHERE scope_type <> ? OR scope_id <> ?", "global", "").Error; err != nil {
			return fmt.Errorf("删除个人标签重建任务失败: %w", err)
		}
	}

	if db.Migrator().HasTable("user_tag_preference") {
		if err := db.Migrator().DropTable("user_tag_preference"); err != nil {
			return fmt.Errorf("删除用户标签偏好表失败: %w", err)
		}
	}
	if db.Dialector.Name() == "sqlite" {
		return rebuildSQLiteGlobalTagTables(db)
	}
	for _, obsolete := range []struct {
		table   string
		column  string
		indexes []string
	}{
		{table: "user_file_tag_state", column: "user_version"},
		{table: "tag_rule_set", column: "scope_type", indexes: []string{"idx_tag_rule_scope"}},
		{table: "tag_rule_set", column: "scope_id", indexes: []string{"idx_tag_rule_scope"}},
		{table: "tag_rebuild_job", column: "scope_type", indexes: []string{"idx_tag_rebuild_scope", "idx_tag_rebuild_job_scope_type"}},
		{table: "tag_rebuild_job", column: "scope_id", indexes: []string{"idx_tag_rebuild_scope", "idx_tag_rebuild_job_scope_id"}},
	} {
		if err := dropObsoleteTagColumn(db, obsolete.table, obsolete.column, obsolete.indexes); err != nil {
			return err
		}
	}
	return nil
}

type sqliteTagTableRebuild struct {
	table     string
	createSQL string
	columns   string
	obsolete  []string
}

func rebuildSQLiteGlobalTagTables(db *gorm.DB) error {
	rebuilds := []sqliteTagTableRebuild{
		{
			table: "user_file_tag_state", obsolete: []string{"user_version"},
			columns: "uf_id,user_id,global_version,metadata_version,status,last_error,retry_count,next_retry_at,run_token,lease_expires_at,generated_at,updated_at",
			createSQL: `CREATE TABLE user_file_tag_state__global_only_new (
				uf_id varchar(64) PRIMARY KEY,
				user_id varchar(64) NOT NULL,
				global_version bigint NOT NULL DEFAULT 0,
				metadata_version bigint NOT NULL DEFAULT 0,
				status varchar(32) NOT NULL,
				last_error text,
				retry_count integer NOT NULL DEFAULT 0,
				next_retry_at datetime,
				run_token varchar(64) NOT NULL DEFAULT '',
				lease_expires_at datetime,
				generated_at datetime,
				updated_at datetime NOT NULL
			)`,
		},
		{
			table: "tag_rule_set", obsolete: []string{"scope_type", "scope_id"},
			columns: "id,version,revision,status,based_on_version,created_by,created_at,updated_at,published_at",
			createSQL: `CREATE TABLE tag_rule_set__global_only_new (
				id varchar(64) PRIMARY KEY,
				version bigint NOT NULL,
				revision integer NOT NULL DEFAULT 1,
				status varchar(16) NOT NULL,
				based_on_version bigint NOT NULL DEFAULT 0,
				created_by varchar(64) NOT NULL DEFAULT '',
				created_at datetime NOT NULL,
				updated_at datetime NOT NULL,
				published_at datetime
			)`,
		},
		{
			table: "tag_rebuild_job", obsolete: []string{"scope_type", "scope_id"},
			columns: "id,target_version,status,cursor_value,total,processed,succeeded,failed,last_error,run_token,lease_expires_at,requested_by,started_at,finished_at,created_at,updated_at",
			createSQL: `CREATE TABLE tag_rebuild_job__global_only_new (
				id varchar(64) PRIMARY KEY,
				target_version bigint NOT NULL,
				status varchar(32) NOT NULL,
				cursor_value varchar(64) NOT NULL DEFAULT '',
				total bigint NOT NULL DEFAULT 0,
				processed bigint NOT NULL DEFAULT 0,
				succeeded bigint NOT NULL DEFAULT 0,
				failed bigint NOT NULL DEFAULT 0,
				last_error text,
				run_token varchar(64) NOT NULL DEFAULT '',
				lease_expires_at datetime,
				requested_by varchar(64) NOT NULL DEFAULT '',
				started_at datetime,
				finished_at datetime,
				created_at datetime NOT NULL,
				updated_at datetime NOT NULL
			)`,
		},
	}
	for _, rebuild := range rebuilds {
		needsRebuild := false
		for _, column := range rebuild.obsolete {
			exists, err := databaseColumnExists(db, rebuild.table, column)
			if err != nil {
				return err
			}
			needsRebuild = needsRebuild || exists
		}
		if !needsRebuild {
			continue
		}
		temporary := rebuild.table + "__global_only_new"
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("DROP TABLE IF EXISTS `" + temporary + "`").Error; err != nil {
				return err
			}
			if err := tx.Exec(rebuild.createSQL).Error; err != nil {
				return err
			}
			if err := tx.Exec(fmt.Sprintf("INSERT INTO `%s` (%s) SELECT %s FROM `%s`", temporary, rebuild.columns, rebuild.columns, rebuild.table)).Error; err != nil {
				return err
			}
			if err := tx.Exec("DROP TABLE `" + rebuild.table + "`").Error; err != nil {
				return err
			}
			return tx.Exec("ALTER TABLE `" + temporary + "` RENAME TO `" + rebuild.table + "`").Error
		}); err != nil {
			return fmt.Errorf("安全重建SQLite表%s失败: %w", rebuild.table, err)
		}
	}
	return nil
}

func databaseColumnExists(db *gorm.DB, table, column string) (bool, error) {
	columnTypes, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false, fmt.Errorf("读取%s字段失败: %w", table, err)
	}
	for _, columnType := range columnTypes {
		if strings.EqualFold(columnType.Name(), column) {
			return true, nil
		}
	}
	return false, nil
}

func dropObsoleteTagColumn(db *gorm.DB, table, column string, indexes []string) error {
	model := obsoleteTagTableModel(table)
	found, err := databaseColumnExists(db, table, column)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for _, index := range indexes {
		if db.Migrator().HasIndex(model, index) {
			if err := db.Migrator().DropIndex(model, index); err != nil {
				return fmt.Errorf("删除标签旧索引%s失败: %w", index, err)
			}
		}
	}
	if err := db.Exec(fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table, column)).Error; err != nil {
		return fmt.Errorf("删除%s.%s字段失败: %w", table, column, err)
	}
	return nil
}

func obsoleteTagTableModel(table string) interface{} {
	switch table {
	case "user_file_tag_state":
		return &models.UserFileTagState{}
	case "tag_rule_set":
		return &models.TagRuleSet{}
	case "tag_rebuild_job":
		return &models.TagRebuildJob{}
	default:
		return table
	}
}

func finalizeGlobalOnlyTagMigration(tx *gorm.DB, now time.Time, version int64) error {
	var applied int64
	if err := tx.Model(&schemaMigration{}).Where("version = ?", globalOnlyTagMigrationVersion).Count(&applied).Error; err != nil {
		return fmt.Errorf("查询全局标签迁移状态失败: %w", err)
	}
	if applied > 0 {
		return nil
	}
	if err := tx.Model(&models.TagRebuildJob{}).
		Where("id <> ? AND status IN ?", globalOnlyTagRebuildJobID, []string{"pending", "running"}).
		Updates(map[string]interface{}{
			"status": "superseded", "finished_at": now, "updated_at": now,
			"run_token": "", "lease_expires_at": nil,
		}).Error; err != nil {
		return fmt.Errorf("终止旧全局标签重建任务失败: %w", err)
	}
	var total int64
	if err := tx.Model(&models.UserFiles{}).Where("deleted_at IS NULL").Count(&total).Error; err != nil {
		return fmt.Errorf("统计全局标签重建文件失败: %w", err)
	}
	job := models.TagRebuildJob{
		ID: globalOnlyTagRebuildJobID, TargetVersion: version, Status: "pending", Total: total,
		RequestedBy: "system", CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&job).Error; err != nil {
		return fmt.Errorf("创建全局标签迁移重建任务失败: %w", err)
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaMigration{
		Version: globalOnlyTagMigrationVersion, AppliedAt: now,
	}).Error; err != nil {
		return fmt.Errorf("记录全局标签迁移状态失败: %w", err)
	}
	return nil
}

type userTagStatBackfillRow struct {
	UserID    string
	TagID     string
	FileCount int64
}

func backfillUserTagStats(tx *gorm.DB, now time.Time) error {
	var applied int64
	if err := tx.Model(&schemaMigration{}).Where("version = ?", userTagStatBackfillVersion).Count(&applied).Error; err != nil {
		return fmt.Errorf("查询标签统计回填状态失败: %w", err)
	}
	if applied > 0 {
		return nil
	}

	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.UserTagStat{}).Error; err != nil {
		return fmt.Errorf("清空标签统计表失败: %w", err)
	}
	var rows []userTagStatBackfillRow
	if err := tx.Table("user_file_tag AS uft").
		Select("uft.user_id, uft.tag_id, COUNT(DISTINCT uft.uf_id) AS file_count").
		Joins("JOIN user_files uf ON uf.uf_id = uft.uf_id AND uf.user_id = uft.user_id AND uf.deleted_at IS NULL").
		Where(`NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e
			WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)`).
		Group("uft.user_id, uft.tag_id").
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("聚合标签统计失败: %w", err)
	}
	for _, row := range rows {
		if row.UserID == "" || row.TagID == "" || row.FileCount <= 0 {
			continue
		}
		stat := models.UserTagStat{UserID: row.UserID, TagID: row.TagID, FileCount: row.FileCount, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "tag_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"file_count": row.FileCount, "updated_at": now,
			}),
		}).Create(&stat).Error; err != nil {
			return fmt.Errorf("写入标签统计失败: %w", err)
		}
	}
	if err := tx.Create(&schemaMigration{Version: userTagStatBackfillVersion, AppliedAt: now}).Error; err != nil {
		return fmt.Errorf("记录标签统计回填状态失败: %w", err)
	}
	return nil
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
	err := tx.Where("status = ?", models.TagRuleSetActive).
		Order("version DESC").First(&active).Error
	if err == nil {
		return false, active.Version, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return false, 0, err
	}

	var maxVersion int64
	if err := tx.Model(&models.TagRuleSet{}).Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return false, 0, err
	}
	version := maxVersion + 1
	ruleSet := &models.TagRuleSet{
		ID: uuid.NewString(), Version: version,
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
	const legacyDescription = "维护文件标签和个人分词词典"
	const currentDescription = "维护文件与目录标签"
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
			ID: maxID + 1, Name: "文件标签", Description: currentDescription,
			Characteristic: "file:tag", CreatedAt: custom_type.Now(),
		}
		if err := tx.Create(&tagPower).Error; err != nil {
			return fmt.Errorf("初始化文件标签权限失败: %w", err)
		}
	} else if tagPower.Description == legacyDescription {
		if err := tx.Model(&tagPower).Update("description", currentDescription).Error; err != nil {
			return fmt.Errorf("更新文件标签权限说明失败: %w", err)
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
