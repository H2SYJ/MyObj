package database

import (
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateRecycledSchemaPreservesLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE recycled (
		id TEXT PRIMARY KEY,
		file_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		created_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO recycled (id, file_id, user_id, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)", "r1", "uf1", "u1").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateRecycledSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []interface{}{&models.RecycledDirectoryNode{}, &models.RecycledDirectoryFile{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("回收站迁移缺少表: %T", model)
		}
	}
	var record models.Recycled
	if err := db.Where("id = ?", "r1").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.ItemType != models.RecycledItemTypeFile || record.ItemCount != 1 || record.FileID != "uf1" {
		t.Fatalf("旧回收站记录迁移异常: %#v", record)
	}
}
