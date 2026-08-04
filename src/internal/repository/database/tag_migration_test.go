package database

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

func TestMigrateTaggingSchemaIsIdempotentAndGrantsExistingPreviewGroups(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.SysConfig{}, &models.Power{}, &models.GroupPower{}, &models.UserFiles{},
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
	for _, record := range []interface{}{
		&models.UserFileTag{ID: "numeric-file", UserID: "user-1", UFID: "uf-1", TagID: numericTag.ID, SourceType: models.TagSourceFilename, SourceKey: "gse", CreatedAt: now},
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
	if err := db.Model(&models.TagRuleSet{}).Where("scope_type = ? AND status = ?", models.TagRuleScopeGlobal, models.TagRuleSetActive).Count(&activeCount).Error; err != nil {
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
	if !db.Migrator().HasIndex(&models.TagDefinition{}, "idx_tag_definition_system_code") {
		t.Fatal("迁移后缺少系统标签编码唯一索引")
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
