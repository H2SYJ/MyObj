package database

import (
	"testing"

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
	if err := db.AutoMigrate(&models.SysConfig{}, &models.Power{}, &models.GroupPower{}, &models.UserFiles{}); err != nil {
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
	if categoryCount != int64(len(builtinTagCategories)) || activeCount != 1 || permissionCount != 1 || grantCount != 1 {
		t.Fatalf("迁移结果不符合预期: categories=%d active=%d permission=%d grant=%d", categoryCount, activeCount, permissionCount, grantCount)
	}
	for _, model := range []interface{}{&models.UserFileTagState{}, &models.FileMetadata{}, &models.TagRebuildJob{}, &models.TagRebuildFailure{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("迁移后缺少表 %T", model)
		}
	}
}
