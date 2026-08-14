package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/pkg/models"
)

func TestMigrateCurrentSchemaCreatesFreshSQLiteSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 2; run++ {
		if _, err := migrateCurrentSchema(db); err != nil {
			t.Fatalf("全新 SQLite 数据库第%d次迁移失败: %v", run, err)
		}
	}

	for _, table := range currentMigrationTables() {
		if !db.Migrator().HasTable(table.Name) {
			t.Fatalf("全新 SQLite 数据库迁移后缺少表 %s", table.Name)
		}
	}

	var backfillCount int64
	if err := db.Model(&schemaMigration{}).
		Where("version = ?", userTagStatBackfillVersion).
		Count(&backfillCount).Error; err != nil {
		t.Fatal(err)
	}
	if backfillCount != 1 {
		t.Fatalf("标签统计回填迁移记录数量异常: %d", backfillCount)
	}

	var adminGroup, defaultGroup models.Group
	if err := db.Where("id = ?", models.DefaultAdminGroupID).First(&adminGroup).Error; err != nil {
		t.Fatalf("缺少管理员组: %v", err)
	}
	if err := db.Where("group_default = ?", 1).First(&defaultGroup).Error; err != nil {
		t.Fatalf("缺少默认用户组: %v", err)
	}
	if defaultGroup.Space != models.DefaultUserSpaceBytes {
		t.Fatalf("默认用户组空间异常: %d", defaultGroup.Space)
	}

	assertGroupPowerCount(t, db, adminGroup.ID, len(models.DefaultPowerDefinitions))
	expectedDefaultUserPowerCount := 0
	for _, definition := range models.DefaultPowerDefinitions {
		if definition.GrantToDefaultUser {
			expectedDefaultUserPowerCount++
		}
	}
	assertGroupPowerCount(t, db, defaultGroup.ID, expectedDefaultUserPowerCount)
	for _, characteristic := range []string{
		"user:update", "user:update:password", "file:tag", "apikey:create", "apikey:delete",
	} {
		var count int64
		if err := db.Table("group_power AS gp").
			Joins("JOIN power p ON p.id = gp.power_id").
			Where("gp.group_id = ? AND p.characteristic = ?", defaultGroup.ID, characteristic).
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("默认用户组缺少权限 %s", characteristic)
		}
	}
}

func assertGroupPowerCount(t *testing.T, db *gorm.DB, groupID, expected int) {
	t.Helper()
	var count int64
	if err := db.Model(&models.GroupPower{}).Where("group_id = ?", groupID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(expected) {
		t.Fatalf("用户组%d权限数量异常: got=%d want=%d", groupID, count, expected)
	}
}
