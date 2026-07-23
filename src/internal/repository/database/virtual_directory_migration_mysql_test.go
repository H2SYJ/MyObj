package database

import (
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMigrateVirtualDirectorySchemaMySQL 需要专用测试库，并且仅在显式允许重建时运行。
// 示例：MYOBJ_TEST_MYSQL_DSN=user:pass@tcp(127.0.0.1:3306)/myobj_test?parseTime=true
//
//	MYOBJ_TEST_MYSQL_ALLOW_RESET=1
func TestMigrateVirtualDirectorySchemaMySQL(t *testing.T) {
	dsn := os.Getenv("MYOBJ_TEST_MYSQL_DSN")
	if dsn == "" || os.Getenv("MYOBJ_TEST_MYSQL_ALLOW_RESET") != "1" {
		t.Skip("未配置专用MySQL迁移测试库")
	}
	if !strings.Contains(strings.ToLower(dsn), "/myobj_test") {
		t.Fatal("MYOBJ_TEST_MYSQL_DSN必须指向名称以myobj_test开头的专用测试库")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"schema_migration", "virtual_directory", "virtual_path", "user_files", "download_task"} {
		if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	createLegacyDirectoryTables(t, db)
	now := time.Now()
	if err := db.Exec(`INSERT INTO virtual_path(id,user_id,path,parent_level,created_time,update_time) VALUES(1,'user-a','/','',?,?),(2,'user-a','/频道','1',?,?)`, now, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_files(user_id,file_id,file_name,virtual_path,public,created_at,uf_id) VALUES('user-a','file-1','示例.mp4','2',FALSE,?,'uf-1')`, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO download_task(id,type,virtual_path) VALUES('download-1',0,'/离线下载/')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateVirtualDirectorySchema(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("virtual_path") || db.Migrator().HasColumn("user_files", "virtual_path") {
		t.Fatal("MySQL迁移后仍存在旧目录结构")
	}
	if !db.Migrator().HasColumn("user_files", "directory_id") || !db.Migrator().HasIndex("virtual_directory", "uk_virtual_directory_sibling") {
		t.Fatal("MySQL迁移后缺少目录字段或唯一索引")
	}
	if err := migrateVirtualDirectorySchema(db); err != nil {
		t.Fatalf("MySQL迁移重复执行失败: %v", err)
	}
}
