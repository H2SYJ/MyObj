package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
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
}
