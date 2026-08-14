package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

func TestMigrateDefaultAccessDataRepairsLegacyDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&schemaMigration{}, &models.Group{}, &models.Power{}, &models.GroupPower{}); err != nil {
		t.Fatal(err)
	}
	for _, group := range []*models.Group{
		{ID: models.DefaultAdminGroupID, Name: "administer", CreatedAt: custom_type.Now()},
		{ID: models.DefaultUserGroupID, Name: "user", GroupDefault: 1, Space: 500, CreatedAt: custom_type.Now()},
	} {
		if err := db.Create(group).Error; err != nil {
			t.Fatal(err)
		}
	}

	for run := 1; run <= 2; run++ {
		if err := migrateDefaultAccessData(db); err != nil {
			t.Fatalf("第%d次迁移默认权限失败: %v", run, err)
		}
	}

	var defaultGroup models.Group
	if err := db.Where("id = ?", models.DefaultUserGroupID).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	if defaultGroup.Space != models.DefaultUserSpaceBytes {
		t.Fatalf("旧默认用户组空间未修正: %d", defaultGroup.Space)
	}
	assertGroupPowerCount(t, db, models.DefaultAdminGroupID, len(models.DefaultPowerDefinitions))

	expectedDefaultUserPowerCount := 0
	for _, definition := range models.DefaultPowerDefinitions {
		if definition.GrantToDefaultUser {
			expectedDefaultUserPowerCount++
		}
	}
	assertGroupPowerCount(t, db, models.DefaultUserGroupID, expectedDefaultUserPowerCount)

	var migrationCount int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", defaultAccessSeedVersion).Count(&migrationCount).Error; err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 {
		t.Fatalf("默认权限迁移记录数量异常: %d", migrationCount)
	}
}
