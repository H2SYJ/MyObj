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
		&models.TagCategory{}, &models.TagDefinition{},
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
}
