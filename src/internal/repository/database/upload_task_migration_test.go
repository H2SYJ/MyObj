package database

import (
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateUploadTaskSchemaAddsAsyncProcessingColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.Exec(`CREATE TABLE upload_task (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		file_name TEXT NOT NULL,
		file_size INTEGER NOT NULL,
		chunk_size INTEGER NOT NULL,
		total_chunks INTEGER NOT NULL,
		uploaded_chunks INTEGER DEFAULT 0,
		status TEXT DEFAULT 'pending'
	)`).Error; err != nil {
		t.Fatalf("创建旧上传任务表失败: %v", err)
	}

	if err := migrateUploadTaskSchema(db); err != nil {
		t.Fatalf("迁移上传任务表失败: %v", err)
	}
	for _, field := range []string{"DiskID", "IsEnc", "ProcessingStage", "ResultFileID", "FirstChunkHash"} {
		if !db.Migrator().HasColumn(&models.UploadTask{}, field) {
			t.Fatalf("迁移后缺少字段 %s", field)
		}
	}
	if err := migrateUploadTaskSchema(db); err != nil {
		t.Fatalf("重复执行迁移应保持幂等: %v", err)
	}
}
