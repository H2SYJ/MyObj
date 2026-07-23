package database

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openVirtualDirectoryMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func createLegacyDirectoryTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE virtual_path (id INTEGER PRIMARY KEY, user_id TEXT NOT NULL, path TEXT NOT NULL, parent_level TEXT, created_time DATETIME, update_time DATETIME)`,
		`CREATE TABLE user_files (user_id TEXT NOT NULL, file_id TEXT NOT NULL, file_name TEXT NOT NULL, virtual_path TEXT NOT NULL, public BOOLEAN NOT NULL, created_at DATETIME NOT NULL, deleted_at DATETIME, uf_id TEXT NOT NULL)`,
		`CREATE TABLE download_task (id TEXT PRIMARY KEY, type INTEGER NOT NULL, virtual_path TEXT)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrateVirtualDirectorySchemaPreservesIDsAndCanonicalizesPaths(t *testing.T) {
	db := openVirtualDirectoryMigrationTestDB(t)
	createLegacyDirectoryTables(t, db)
	now := time.Now()
	for _, values := range [][]interface{}{
		{1, "user-a", "/", "", now, now},
		{2, "user-a", "/频道", "1", now, now},
		{3, "user-a", "2026", "2", now, now},
	} {
		if err := db.Exec(`INSERT INTO virtual_path(id,user_id,path,parent_level,created_time,update_time) VALUES(?,?,?,?,?,?)`, values...).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO user_files(user_id,file_id,file_name,virtual_path,public,created_at,uf_id) VALUES(?,?,?,?,?,?,?)`, "user-a", "file-1", "示例.mp4", "3", false, now, "uf-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO download_task(id,type,virtual_path) VALUES(?,?,?)`, "download-1", 0, "离线下载/").Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateVirtualDirectorySchema(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("virtual_path") {
		t.Fatal("迁移完成后不应保留virtual_path表")
	}
	var directories []struct {
		ID       int
		Name     string
		ParentID int
	}
	if err := db.Table("virtual_directory").Order("id").Scan(&directories).Error; err != nil {
		t.Fatal(err)
	}
	if len(directories) != 3 || directories[0].ID != 1 || directories[0].Name != "" || directories[1].Name != "频道" || directories[2].ParentID != 2 {
		t.Fatalf("目录迁移结果异常: %#v", directories)
	}
	var directoryID int
	if err := db.Table("user_files").Select("directory_id").Where("uf_id = ?", "uf-1").Scan(&directoryID).Error; err != nil {
		t.Fatal(err)
	}
	if directoryID != 3 {
		t.Fatalf("用户文件目录引用未保留: %d", directoryID)
	}
	var savePath string
	if err := db.Table("download_task").Select("save_path").Where("id = ?", "download-1").Scan(&savePath).Error; err != nil {
		t.Fatal(err)
	}
	if savePath != "/离线下载" {
		t.Fatalf("下载保存路径未规范化: %q", savePath)
	}
	if err := migrateVirtualDirectorySchema(db); err != nil {
		t.Fatalf("迁移重复执行失败: %v", err)
	}
}

func TestMigrateVirtualDirectorySchemaPreflightDoesNotWriteOnConflict(t *testing.T) {
	db := openVirtualDirectoryMigrationTestDB(t)
	createLegacyDirectoryTables(t, db)
	now := time.Now()
	for _, values := range [][]interface{}{
		{1, "user-a", "/", "", now, now},
		{2, "user-a", "/频道", "1", now, now},
		{3, "user-a", "频道", "1", now, now},
	} {
		if err := db.Exec(`INSERT INTO virtual_path(id,user_id,path,parent_level,created_time,update_time) VALUES(?,?,?,?,?,?)`, values...).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateVirtualDirectorySchema(db); err == nil || !strings.Contains(err.Error(), "规范化同名") {
		t.Fatalf("期望规范化同名错误，实际为%v", err)
	}
	if db.Migrator().HasTable("virtual_directory") {
		t.Fatal("预检失败前不应创建新目录表")
	}
	if !db.Migrator().HasColumn("user_files", "virtual_path") || db.Migrator().HasColumn("user_files", "directory_id") {
		t.Fatal("预检失败前不应修改引用列")
	}
}

func TestMigrateVirtualDirectorySchemaAllowsSoftDeletedDanglingFileReference(t *testing.T) {
	db := openVirtualDirectoryMigrationTestDB(t)
	createLegacyDirectoryTables(t, db)
	now := time.Now()
	if err := db.Exec(`INSERT INTO virtual_path(id,user_id,path,parent_level,created_time,update_time) VALUES(?,?,?,?,?,?)`, 1, "user-a", "/", "", now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_files(user_id,file_id,file_name,virtual_path,public,created_at,deleted_at,uf_id) VALUES(?,?,?,?,?,?,?,?)`, "user-a", "file-1", "回收站文件.mp4", "17", false, now, now, "uf-1").Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateVirtualDirectorySchema(db); err != nil {
		t.Fatalf("软删除文件的历史目录引用不应阻止迁移: %v", err)
	}
	var directoryID int
	if err := db.Table("user_files").Select("directory_id").Where("uf_id = ?", "uf-1").Scan(&directoryID).Error; err != nil {
		t.Fatal(err)
	}
	if directoryID != 17 {
		t.Fatalf("软删除文件的历史目录ID应保留，实际为%d", directoryID)
	}
}

func TestMigrateVirtualDirectorySchemaRejectsActiveDanglingFileReference(t *testing.T) {
	db := openVirtualDirectoryMigrationTestDB(t)
	createLegacyDirectoryTables(t, db)
	now := time.Now()
	if err := db.Exec(`INSERT INTO virtual_path(id,user_id,path,parent_level,created_time,update_time) VALUES(?,?,?,?,?,?)`, 1, "user-a", "/", "", now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_files(user_id,file_id,file_name,virtual_path,public,created_at,uf_id) VALUES(?,?,?,?,?,?,?)`, "user-a", "file-1", "在线文件.mp4", "17", false, now, "uf-1").Error; err != nil {
		t.Fatal(err)
	}

	err := migrateVirtualDirectorySchema(db)
	if err == nil || !strings.Contains(err.Error(), "表user_files存在无效目录引用") {
		t.Fatalf("在线文件的悬空目录引用应阻止迁移，实际为%v", err)
	}
	if db.Migrator().HasTable("virtual_directory") {
		t.Fatal("预检失败前不应创建新目录表")
	}
}

func TestMigrateVirtualDirectorySchemaRepairsDanglingUploadReferencesToRoot(t *testing.T) {
	db := openVirtualDirectoryMigrationTestDB(t)
	createLegacyDirectoryTables(t, db)
	now := time.Now()
	for _, values := range [][]interface{}{
		{1, "user-a", "/", "", now, now},
		{2, "user-a", "保留目录", "1", now, now},
	} {
		if err := db.Exec(`INSERT INTO virtual_path(id,user_id,path,parent_level,created_time,update_time) VALUES(?,?,?,?,?,?)`, values...).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`CREATE TABLE upload_task (id TEXT PRIMARY KEY, user_id TEXT, path_id TEXT, status TEXT)`,
		`CREATE TABLE upload_chunk (chunk_id INTEGER PRIMARY KEY, user_id TEXT, path_id TEXT)`,
		`INSERT INTO upload_task(id,user_id,path_id,status) VALUES('task-invalid','user-a','22','uploading'),('task-null','user-a',NULL,'failed'),('task-valid','user-a','2','pending')`,
		`INSERT INTO upload_chunk(chunk_id,user_id,path_id) VALUES(1,'user-a','23')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateVirtualDirectorySchema(db); err != nil {
		t.Fatalf("无效上传目录引用应自动迁到根目录: %v", err)
	}
	var tasks []struct {
		ID          string
		DirectoryID int
	}
	if err := db.Table("upload_task").Select("id, directory_id").Order("id").Scan(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 || tasks[0].ID != "task-invalid" || tasks[0].DirectoryID != 1 || tasks[1].ID != "task-null" || tasks[1].DirectoryID != 1 || tasks[2].ID != "task-valid" || tasks[2].DirectoryID != 2 {
		t.Fatalf("上传任务目录迁移结果异常: %#v", tasks)
	}
	var chunkDirectoryID int
	if err := db.Table("upload_chunk").Select("directory_id").Where("chunk_id = ?", 1).Scan(&chunkDirectoryID).Error; err != nil {
		t.Fatal(err)
	}
	if chunkDirectoryID != 1 {
		t.Fatalf("上传分片应迁到根目录，实际为%d", chunkDirectoryID)
	}
}

func TestPreflightLegacyDirectoriesRejectsCycleAndBadReference(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name string
		rows []legacyVirtualPath
		want string
	}{
		{name: "循环", rows: []legacyVirtualPath{{ID: 1, UserID: "u", Path: "/", CreatedTime: now}, {ID: 2, UserID: "u", Path: "甲", ParentLevel: "3"}, {ID: 3, UserID: "u", Path: "乙", ParentLevel: "2"}}, want: "循环"},
		{name: "坏引用", rows: []legacyVirtualPath{{ID: 1, UserID: "u", Path: "/", CreatedTime: now}, {ID: 2, UserID: "u", Path: "甲", ParentLevel: "99"}}, want: "父目录不存在"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := preflightLegacyDirectories(test.rows); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("期望错误包含%q，实际为%v", test.want, err)
			}
		})
	}
}

func TestMarkIncompatiblePluginsDisablesAPIV1Subscriptions(t *testing.T) {
	db := openVirtualDirectoryMigrationTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE installed_plugin (id TEXT PRIMARY KEY, api_version TEXT NOT NULL, enabled BOOLEAN NOT NULL, updated_at DATETIME)`,
		`CREATE TABLE subscription (id TEXT PRIMARY KEY, plugin_id TEXT NOT NULL, enabled BOOLEAN NOT NULL, status TEXT, last_error TEXT, next_run_at DATETIME, updated_at DATETIME)`,
		`INSERT INTO installed_plugin(id,api_version,enabled) VALUES('v1','1',TRUE),('v2','2',TRUE)`,
		`INSERT INTO subscription(id,plugin_id,enabled,status) VALUES('sub-v1','v1',TRUE,'ready'),('sub-v2','v2',TRUE,'ready')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := markIncompatiblePlugins(db); err != nil {
		t.Fatal(err)
	}
	var v1 struct {
		Enabled bool
		Status  string
	}
	if err := db.Table("subscription").Select("enabled, status").Where("id = ?", "sub-v1").Scan(&v1).Error; err != nil {
		t.Fatal(err)
	}
	if v1.Enabled || v1.Status != "incompatible_api" {
		t.Fatalf("API v1订阅未停用: %#v", v1)
	}
	var v2 struct {
		Enabled bool
		Status  string
	}
	if err := db.Table("subscription").Select("enabled, status").Where("id = ?", "sub-v2").Scan(&v2).Error; err != nil {
		t.Fatal(err)
	}
	if !v2.Enabled || v2.Status != "ready" {
		t.Fatalf("API v2订阅不应受影响: %#v", v2)
	}
}
