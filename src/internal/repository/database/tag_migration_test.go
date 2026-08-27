package database

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

type tagMigrationTestUserFile struct {
	UserID      string     `gorm:"column:user_id"`
	FileID      string     `gorm:"column:file_id"`
	FileName    string     `gorm:"column:file_name"`
	DirectoryID int        `gorm:"column:directory_id"`
	IsPublic    bool       `gorm:"column:public"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	UFID        string     `gorm:"column:uf_id"`
}

func (tagMigrationTestUserFile) TableName() string { return "user_files" }

func TestMigrateTaggingSchemaIsIdempotentAndGrantsExistingPreviewGroups(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.SysConfig{}, &models.Power{}, &models.GroupPower{}, &tagMigrationTestUserFile{},
		&models.TagCategory{}, &models.TagDefinition{}, &models.UserFileTag{},
		&models.UserDirectoryTag{}, &models.UserFileTagExclusion{}, &models.RecycledDirectoryTag{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	customCategory := models.TagCategory{
		ID: "custom", Code: "custom", Name: "自定义", Color: "#123456", Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&customCategory).Error; err != nil {
		t.Fatal(err)
	}
	preexistingCinemaTag := models.TagDefinition{
		ID: "existing-cinema", Name: models.TagNameCinemaMode, NormalizedName: models.TagNameCinemaMode,
		CategoryID: customCategory.ID, CreatedAt: now,
	}
	if err := db.Create(&preexistingCinemaTag).Error; err != nil {
		t.Fatal(err)
	}
	numericTag := models.TagDefinition{
		ID: "numeric", Name: "２０２３", NormalizedName: "2023",
		CategoryID: customCategory.ID, CreatedAt: now,
	}
	if err := db.Create(&numericTag).Error; err != nil {
		t.Fatal(err)
	}
	normalTag := models.TagDefinition{
		ID: "normal", Name: "电影", NormalizedName: "电影",
		CategoryID: customCategory.ID, CreatedAt: now,
	}
	if err := db.Create(&normalTag).Error; err != nil {
		t.Fatal(err)
	}
	for _, record := range []interface{}{
		&tagMigrationTestUserFile{UserID: "user-1", FileID: "file-1", UFID: "uf-1", FileName: "测试.mp4", DirectoryID: 1, IsPublic: false, CreatedAt: now},
		&models.UserFileTag{ID: "numeric-file", UserID: "user-1", UFID: "uf-1", TagID: numericTag.ID, SourceType: models.TagSourceFilename, SourceKey: "gse", CreatedAt: now},
		&models.UserFileTag{ID: "normal-file", UserID: "user-1", UFID: "uf-1", TagID: normalTag.ID, SourceType: models.TagSourceManual, SourceKey: "user", CreatedAt: now},
		&models.UserDirectoryTag{ID: "numeric-directory", UserID: "user-1", DirectoryID: 1, TagID: numericTag.ID, CreatedBy: "user-1", CreatedAt: now},
		&models.UserFileTagExclusion{UserID: "user-1", UFID: "uf-1", TagID: numericTag.ID, CreatedAt: now},
		&models.RecycledDirectoryTag{RecycledID: "recycled-1", OriginalDirID: 1, TagID: numericTag.ID},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	preview := models.Power{ID: 1, Name: "文件预览", Description: "", Characteristic: "file:preview", CreatedAt: custom_type.Now()}
	if err := db.Create(&preview).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.GroupPower{GroupID: 7, PowerID: preview.ID}).Error; err != nil {
		t.Fatal(err)
	}

	for run := 0; run < 2; run++ {
		if err := migrateTaggingSchema(db); err != nil {
			t.Fatalf("第%d次标签迁移失败: %v", run+1, err)
		}
	}

	var categoryCount, activeCount, permissionCount, grantCount int64
	if err := db.Model(&models.TagCategory{}).Count(&categoryCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TagRuleSet{}).Where("status = ?", models.TagRuleSetActive).Count(&activeCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Power{}).Where("characteristic = ?", "file:tag").Count(&permissionCount).Error; err != nil {
		t.Fatal(err)
	}
	var tagPower models.Power
	if err := db.Where("characteristic = ?", "file:tag").First(&tagPower).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.GroupPower{}).Where("group_id = ? AND power_id = ?", 7, tagPower.ID).Count(&grantCount).Error; err != nil {
		t.Fatal(err)
	}
	if categoryCount != int64(len(builtinTagCategories)+1) || activeCount != 1 || permissionCount != 1 || grantCount != 1 {
		t.Fatalf("迁移结果不符合预期: categories=%d active=%d permission=%d grant=%d", categoryCount, activeCount, permissionCount, grantCount)
	}
	for _, model := range []interface{}{&models.UserFileTagState{}, &models.UserDirectoryTag{}, &models.FileMetadata{}, &models.TagRebuildJob{}, &models.TagRebuildFailure{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("迁移后缺少表 %T", model)
		}
	}
	if db.Migrator().HasTable("user_tag_preference") {
		t.Fatal("用户标签偏好表应被物理删除")
	}
	for _, item := range []struct{ table, column string }{
		{"user_file_tag_state", "user_version"},
		{"tag_rule_set", "scope_type"}, {"tag_rule_set", "scope_id"},
		{"tag_rebuild_job", "scope_type"}, {"tag_rebuild_job", "scope_id"},
	} {
		if db.Migrator().HasColumn(item.table, item.column) {
			t.Fatalf("迁移后仍存在废弃字段 %s.%s", item.table, item.column)
		}
	}
	if !db.Migrator().HasIndex(&models.TagDefinition{}, "idx_tag_definition_system_code") {
		t.Fatal("迁移后缺少系统标签编码唯一索引")
	}
	if !db.Migrator().HasTable(&models.UserTagStat{}) {
		t.Fatal("缺少用户标签统计表")
	}
	if !db.Migrator().HasIndex(&models.UserTagStat{}, "idx_user_tag_stat_count") {
		t.Fatal("缺少用户标签统计计数索引")
	}
	var cinemaTag models.TagDefinition
	if err := db.Where("system_code = ?", models.TagSystemCodeCinemaMode).First(&cinemaTag).Error; err != nil {
		t.Fatalf("迁移后缺少影视模式标签: %v", err)
	}
	if cinemaTag.ID != preexistingCinemaTag.ID || cinemaTag.Name != models.TagNameCinemaMode || !cinemaTag.Builtin {
		t.Fatalf("影视模式标签属性异常: %+v", cinemaTag)
	}
	var numericDefinitionCount int64
	if err := db.Model(&models.TagDefinition{}).Where("id = ?", numericTag.ID).Count(&numericDefinitionCount).Error; err != nil {
		t.Fatal(err)
	}
	if numericDefinitionCount != 0 {
		t.Fatal("迁移后仍存在纯数字标签定义")
	}
	var cleanupMigrationCount int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", pureNumericTagCleanupVersion).Count(&cleanupMigrationCount).Error; err != nil {
		t.Fatal(err)
	}
	if cleanupMigrationCount != 1 {
		t.Fatalf("纯数字标签清理迁移记录数量异常: %d", cleanupMigrationCount)
	}
	var statBackfillCount int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", userTagStatBackfillVersion).Count(&statBackfillCount).Error; err != nil {
		t.Fatal(err)
	}
	if statBackfillCount != 1 {
		t.Fatalf("标签统计回填迁移记录数量异常: %d", statBackfillCount)
	}
	var globalMigrationCount, migrationJobCount int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", globalOnlyTagMigrationVersion).Count(&globalMigrationCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TagRebuildJob{}).Where("id = ?", globalOnlyTagRebuildJobID).Count(&migrationJobCount).Error; err != nil {
		t.Fatal(err)
	}
	if globalMigrationCount != 1 || migrationJobCount != 1 {
		t.Fatalf("全局标签迁移或固定重建任务不幂等: migration=%d job=%d", globalMigrationCount, migrationJobCount)
	}
	var stat models.UserTagStat
	if err := db.Where("user_id = ? AND tag_id = ?", "user-1", normalTag.ID).First(&stat).Error; err != nil {
		t.Fatalf("标签统计回填缺少普通标签: %v", err)
	}
	if stat.FileCount != 1 {
		t.Fatalf("标签统计回填计数异常: %+v", stat)
	}
	var numericStatCount int64
	if err := db.Model(&models.UserTagStat{}).Where("tag_id = ?", numericTag.ID).Count(&numericStatCount).Error; err != nil {
		t.Fatal(err)
	}
	if numericStatCount != 0 {
		t.Fatalf("纯数字标签不应保留统计: %d", numericStatCount)
	}
	for _, model := range []interface{}{
		&models.UserFileTag{}, &models.UserDirectoryTag{}, &models.UserFileTagExclusion{}, &models.RecycledDirectoryTag{},
	} {
		var count int64
		if err := db.Model(model).Where("tag_id = ?", numericTag.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("迁移后仍存在纯数字标签关联 %T", model)
		}
	}
}

func TestMigrateTaggingSchemaRemovesPersonalDataAndLegacyColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.SysConfig{}, &models.Power{}, &models.GroupPower{}, &tagMigrationTestUserFile{},
		&models.TagCategory{}, &models.TagDefinition{}, &models.UserFileTag{}, &models.UserDirectoryTag{},
		&models.UserFileTagExclusion{}, &models.UserTagStat{}, &models.UserFileTagState{},
		&models.FileMetadata{}, &models.FileMetadataState{}, &models.TagRuleSet{}, &models.TagRule{},
		&models.TagRebuildJob{}, &models.TagRebuildFailure{}, &schemaMigration{},
	); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE user_file_tag_state ADD COLUMN user_version BIGINT NOT NULL DEFAULT 0",
		"ALTER TABLE tag_rule_set ADD COLUMN scope_type VARCHAR(16) NOT NULL DEFAULT 'global'",
		"ALTER TABLE tag_rule_set ADD COLUMN scope_id VARCHAR(64) NOT NULL DEFAULT ''",
		"CREATE INDEX idx_tag_rule_scope ON tag_rule_set(scope_type, scope_id, status, version)",
		"ALTER TABLE tag_rebuild_job ADD COLUMN scope_type VARCHAR(16) NOT NULL DEFAULT 'global'",
		"ALTER TABLE tag_rebuild_job ADD COLUMN scope_id VARCHAR(64) NOT NULL DEFAULT ''",
		"CREATE INDEX idx_tag_rebuild_scope ON tag_rebuild_job(scope_type, scope_id)",
		`CREATE TABLE user_tag_preference (
			user_id VARCHAR(64) NOT NULL,
			tag_id VARCHAR(64) NOT NULL,
			hidden BOOLEAN NOT NULL DEFAULT FALSE,
			display_name VARCHAR(255),
			normalized_display_name VARCHAR(191),
			display_category_id VARCHAR(64),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY(user_id, tag_id)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	global := models.TagRuleSet{ID: "legacy-global", Version: 5, Revision: 1, Status: models.TagRuleSetActive, CreatedAt: now, UpdatedAt: now}
	personal := models.TagRuleSet{ID: "legacy-personal", Version: 3, Revision: 1, Status: models.TagRuleSetActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&global).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&personal).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("tag_rule_set").Where("id = ?", personal.ID).Updates(map[string]interface{}{"scope_type": "user", "scope_id": "user-1"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, rule := range []models.TagRule{
		{ID: "global-rule", RuleSetID: global.ID, Type: models.TagRuleTypeWord, Pattern: "全局词", CategoryID: "other", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "personal-rule", RuleSetID: personal.ID, Type: models.TagRuleTypeWord, Pattern: "个人词", CategoryID: "other", Enabled: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
	}
	jobs := []models.TagRebuildJob{
		{ID: "global-history", TargetVersion: 4, Status: "completed", CreatedAt: now, UpdatedAt: now},
		{ID: "global-running", TargetVersion: 5, Status: "running", CreatedAt: now, UpdatedAt: now},
		{ID: "personal-running", TargetVersion: 3, Status: "running", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("tag_rebuild_job").Where("id = ?", "personal-running").Updates(map[string]interface{}{"scope_type": "user", "scope_id": "user-1"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, failure := range []models.TagRebuildFailure{
		{JobID: "global-history", UFID: "global-file", UserID: "user-1", Status: models.TagRebuildFailureFailed, CreatedAt: now, UpdatedAt: now},
		{JobID: "personal-running", UFID: "personal-file", UserID: "user-1", Status: models.TagRebuildFailureFailed, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&failure).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.UserFileTagState{UFID: "state-1", UserID: "user-1", GlobalVersion: 5, Status: models.TagStateReady, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE user_file_tag_state SET user_version = 7 WHERE uf_id = ?", "state-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_tag_preference(user_id, tag_id, hidden, display_name, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)`, "user-1", "normal", true, "个人显示名", now, now).Error; err != nil {
		t.Fatal(err)
	}

	for run := 0; run < 2; run++ {
		if err := migrateTaggingSchema(db); err != nil {
			t.Fatalf("第%d次旧标签结构迁移失败: %v", run+1, err)
		}
	}

	if db.Migrator().HasTable("user_tag_preference") {
		t.Fatal("个人标签偏好表未删除")
	}
	for _, item := range []struct{ table, column string }{
		{"user_file_tag_state", "user_version"},
		{"tag_rule_set", "scope_type"}, {"tag_rule_set", "scope_id"},
		{"tag_rebuild_job", "scope_type"}, {"tag_rebuild_job", "scope_id"},
	} {
		if db.Migrator().HasColumn(item.table, item.column) {
			columns, _ := db.Migrator().ColumnTypes(item.table)
			names := make([]string, 0, len(columns))
			for _, column := range columns {
				names = append(names, column.Name())
			}
			t.Fatalf("废弃字段未删除: %s.%s columns=%v", item.table, item.column, names)
		}
	}
	var personalRuleSets, personalRules, personalJobs, personalFailures int64
	db.Model(&models.TagRuleSet{}).Where("id = ?", personal.ID).Count(&personalRuleSets)
	db.Model(&models.TagRule{}).Where("id = ?", "personal-rule").Count(&personalRules)
	db.Model(&models.TagRebuildJob{}).Where("id = ?", "personal-running").Count(&personalJobs)
	db.Model(&models.TagRebuildFailure{}).Where("job_id = ?", "personal-running").Count(&personalFailures)
	if personalRuleSets+personalRules+personalJobs+personalFailures != 0 {
		t.Fatalf("个人历史数据未清空: sets=%d rules=%d jobs=%d failures=%d", personalRuleSets, personalRules, personalJobs, personalFailures)
	}
	var globalRuleCount, globalJobCount, globalFailureCount, migrationJobCount int64
	db.Model(&models.TagRule{}).Where("id = ?", "global-rule").Count(&globalRuleCount)
	db.Model(&models.TagRebuildJob{}).Where("id IN ?", []string{"global-history", "global-running"}).Count(&globalJobCount)
	db.Model(&models.TagRebuildFailure{}).Where("job_id = ?", "global-history").Count(&globalFailureCount)
	db.Model(&models.TagRebuildJob{}).Where("id = ?", globalOnlyTagRebuildJobID).Count(&migrationJobCount)
	if globalRuleCount != 1 || globalJobCount != 2 || globalFailureCount != 1 || migrationJobCount != 1 {
		t.Fatalf("全局历史或固定迁移任务异常: rule=%d jobs=%d failure=%d migration_job=%d", globalRuleCount, globalJobCount, globalFailureCount, migrationJobCount)
	}
	var superseded models.TagRebuildJob
	if err := db.Where("id = ?", "global-running").First(&superseded).Error; err != nil {
		t.Fatal(err)
	}
	if superseded.Status != "superseded" {
		t.Fatalf("旧未完成全局任务未终止: %+v", superseded)
	}
	var state models.UserFileTagState
	if err := db.Where("uf_id = ?", "state-1").First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.GlobalVersion != 5 {
		t.Fatalf("删除个人版本时损坏了全局状态: %+v", state)
	}
}
