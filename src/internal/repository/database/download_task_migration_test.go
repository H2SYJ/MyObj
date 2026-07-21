package database

import (
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateDownloadTaskSchemaAddsSchedulerColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE download_task (id TEXT PRIMARY KEY, user_id TEXT, type INTEGER, state INTEGER, create_time DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateDownloadTaskSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"run_token", "worker_id", "lease_expires_at", "retry_count", "next_retry_at", "batch_id", "reserved_size", "request_headers_encrypted", "header_hosts_json", "requires_headers"} {
		if !db.Migrator().HasColumn(&models.DownloadTask{}, column) {
			t.Fatalf("缺少迁移字段: %s", column)
		}
	}
	for _, index := range []string{"idx_download_batch_id", "idx_download_run_token", "idx_download_user_type_state_create", "idx_download_schedule"} {
		if !db.Migrator().HasIndex(&models.DownloadTask{}, index) {
			t.Fatalf("缺少迁移索引: %s", index)
		}
	}
}
