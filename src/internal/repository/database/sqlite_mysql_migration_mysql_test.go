package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

// TestSQLiteToMySQLMigrationIntegration 需要可重建的专用MySQL测试库。
func TestSQLiteToMySQLMigrationIntegration(t *testing.T) {
	dsn := os.Getenv("MYOBJ_TEST_MYSQL_DSN")
	if dsn == "" || os.Getenv("MYOBJ_TEST_MYSQL_ALLOW_RESET") != "1" {
		t.Skip("未配置专用MySQL迁移测试库")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(config.DBName), "myobj_test") {
		t.Fatal("MYOBJ_TEST_MYSQL_DSN必须指向名称以myobj_test开头的专用测试库")
	}
	resetMigrationMySQLTestDatabase(t, dsn)
	defer resetMigrationMySQLTestDatabase(t, dsn)

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.db")
	source, err := openSQLiteForMigration(sourcePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareSQLiteSnapshot(source); err != nil {
		closeGormDB(source)
		t.Fatal(err)
	}
	now := custom_type.JsonTime(time.Date(2026, 8, 14, 1, 2, 3, 123456700, time.UTC))
	fixture := []interface{}{
		&models.Group{ID: 7, Name: "迁移用户组😀", GroupDefault: 1, Space: 8 << 40, CreatedAt: now},
		&models.UserInfo{ID: "user-migration", Name: "迁移用户😀", UserName: "migration", Password: "hash", Email: "user@example.com", Phone: "13800000000", GroupID: 7, CreatedAt: now, Space: 8 << 40, FreeSpace: 7 << 40},
		&models.VirtualDirectory{ID: 10, UserID: "user-migration", Name: "", ParentID: 0, CreatedAt: now, UpdatedAt: now},
		&models.FileInfo{ID: "file-migration", Name: "中文视频😀.mp4", RandomName: "random.mp4", Size: 5 << 30, Mime: "video/mp4", Path: "obj_data/random.mp4", FileHash: strings.Repeat("a", 64), IsChunk: false, EncPath: "", CreatedAt: now, UpdatedAt: now},
		&models.UserFiles{UserID: "user-migration", FileID: "file-migration", FileName: "中文视频😀.mp4", DirectoryID: 10, CreatedAt: now, UfID: "uf-migration"},
	}
	for _, item := range fixture {
		if err := source.Create(item).Error; err != nil {
			closeGormDB(source)
			t.Fatalf("创建SQLite迁移夹具失败: %v", err)
		}
	}
	closeGormDB(source)

	snapshotPath := filepath.Join(tempDir, "snapshot.db")
	report, err := MigrateSQLiteToMySQL(context.Background(), SQLiteToMySQLOptions{
		SourcePath: sourcePath, SnapshotPath: snapshotPath, MySQLDSN: dsn,
		BatchSize: 100, Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) != len(currentMigrationTables()) {
		t.Fatalf("迁移报告表数错误: %d", len(report.Tables))
	}
	if _, err := VerifySQLiteToMySQL(context.Background(), SQLiteToMySQLOptions{
		SourcePath: snapshotPath, MySQLDSN: dsn, BatchSize: 100, Timezone: "Asia/Shanghai",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = MigrateSQLiteToMySQL(context.Background(), SQLiteToMySQLOptions{
		SourcePath: sourcePath, SnapshotPath: filepath.Join(tempDir, "second.db"), MySQLDSN: dsn,
		BatchSize: 100, Timezone: "Asia/Shanghai",
	})
	if err == nil || !strings.Contains(err.Error(), "目标MySQL不是空库") {
		t.Fatalf("重复迁移应被空库保护拒绝，实际错误: %v", err)
	}
}

// TestSQLiteToMySQLPreflightAggregatesAndCleansTarget 需要可重建的专用MySQL测试库。
func TestSQLiteToMySQLPreflightAggregatesAndCleansTarget(t *testing.T) {
	dsn := os.Getenv("MYOBJ_TEST_MYSQL_DSN")
	if dsn == "" || os.Getenv("MYOBJ_TEST_MYSQL_ALLOW_RESET") != "1" {
		t.Skip("未配置专用MySQL迁移测试库")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(config.DBName), "myobj_test") {
		t.Fatal("MYOBJ_TEST_MYSQL_DSN必须指向名称以myobj_test开头的专用测试库")
	}
	resetMigrationMySQLTestDatabase(t, dsn)
	defer resetMigrationMySQLTestDatabase(t, dsn)

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "invalid-source.db")
	source, err := openSQLiteForMigration(sourcePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareSQLiteSnapshot(source); err != nil {
		closeGormDB(source)
		t.Fatal(err)
	}
	now := custom_type.JsonTime(time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC))
	if err := source.Create(&models.Group{
		ID: 7, Name: strings.Repeat("组", 256), GroupDefault: 0, CreatedAt: now,
	}).Error; err != nil {
		closeGormDB(source)
		t.Fatal(err)
	}
	if err := source.Create(&models.UserInfo{
		ID: "orphan-user", Name: "悬空用户", UserName: "orphan", Password: "hash",
		Email: "orphan@example.com", Phone: "13800000000", GroupID: 999, CreatedAt: now,
	}).Error; err != nil {
		closeGormDB(source)
		t.Fatal(err)
	}
	closeGormDB(source)

	_, err = MigrateSQLiteToMySQL(context.Background(), SQLiteToMySQLOptions{
		SourcePath: sourcePath, SnapshotPath: filepath.Join(tempDir, "invalid-snapshot.db"), MySQLDSN: dsn,
		BatchSize: 100, Timezone: "Asia/Shanghai",
	})
	if err == nil {
		t.Fatal("不兼容数据应在预检阶段失败")
	}
	for _, expected := range []string{"迁移预检失败", "表groups", "MySQL错误1406", "关联完整性", "已自动清理目标结构"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("预检错误缺少%q: %v", expected, err)
		}
	}

	target, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(target)
	var tableCount int64
	if err := target.Raw(`SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`).Scan(&tableCount).Error; err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatalf("预检失败后目标库仍有%d张表", tableCount)
	}
}

func resetMigrationMySQLTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	config, _ := mysqldriver.ParseDSN(dsn)
	if err := db.Exec("ALTER DATABASE " + quoteIdentifier(config.DBName, '`') + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		t.Fatal(err)
	}
	var tables []string
	if err := db.Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'", config.DBName).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if err := db.Exec("DROP TABLE " + quoteIdentifier(table, '`')).Error; err != nil {
			t.Fatal(fmt.Errorf("删除测试表%s失败: %w", table, err))
		}
	}
	if err := db.Exec("SET FOREIGN_KEY_CHECKS = 1").Error; err != nil {
		t.Fatal(err)
	}
}
