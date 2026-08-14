package database

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateSQLiteSnapshotKeepsSourceUnchangedAndIncludesWAL(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.db")
	snapshotPath := filepath.Join(tempDir, "snapshot.db")
	db, err := openSQLiteForMigration(sourcePath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE sample (id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO sample(id, value) VALUES(1, ?)", "中文😀").Error; err != nil {
		t.Fatal(err)
	}
	before := fileSHA256(t, sourcePath)
	if err := createSQLiteSnapshot(context.Background(), sourcePath, snapshotPath); err != nil {
		t.Fatal(err)
	}
	after := fileSHA256(t, sourcePath)
	if before != after {
		t.Fatal("生成快照修改了源SQLite主文件")
	}
	snapshot, err := openSQLiteForMigration(snapshotPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(snapshot)
	var value string
	if err := snapshot.Raw("SELECT value FROM sample WHERE id = 1").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "中文😀" {
		t.Fatalf("快照没有包含WAL数据: %q", value)
	}
}

func TestCreateSQLiteSnapshotAfterSourceClosed(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.db")
	snapshotPath := filepath.Join(tempDir, "snapshot.db")
	db, err := openSQLiteForMigration(sourcePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		closeGormDB(db)
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE sample (id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		closeGormDB(db)
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO sample(id, value) VALUES(1, ?)", "停服快照").Error; err != nil {
		closeGormDB(db)
		t.Fatal(err)
	}
	closeGormDB(db)

	if err := createSQLiteSnapshot(context.Background(), sourcePath, snapshotPath); err != nil {
		t.Fatal(err)
	}
	snapshot, err := openSQLiteForMigration(snapshotPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(snapshot)
	var value string
	if err := snapshot.Raw("SELECT value FROM sample WHERE id = 1").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "停服快照" {
		t.Fatalf("停服后生成的快照数据错误: %q", value)
	}
}

func TestCaptureSQLiteFileStatesIgnoresSharedMemory(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	states := captureSQLiteFileStates(sourcePath)
	if _, exists := states[sourcePath+"-shm"]; exists {
		t.Fatal("SHM协调文件不应参与源数据变化判断")
	}
	if _, exists := states[sourcePath]; !exists {
		t.Fatal("主数据库文件必须参与源数据变化判断")
	}
	if _, exists := states[sourcePath+"-wal"]; !exists {
		t.Fatal("WAL文件必须参与源数据变化判断")
	}
}

func TestSameSQLiteFileStatesDetectsDatabaseAndWALChanges(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	baseTime := time.Unix(1, 0)
	before := map[string]fileState{
		sourcePath:          {Exists: true, Size: 100, ModTime: baseTime},
		sourcePath + "-wal": {Exists: true, Size: 20, ModTime: baseTime},
	}
	for _, changedPath := range []string{sourcePath, sourcePath + "-wal"} {
		after := map[string]fileState{
			sourcePath:          before[sourcePath],
			sourcePath + "-wal": before[sourcePath+"-wal"],
		}
		state := after[changedPath]
		state.Size++
		after[changedPath] = state
		if sameSQLiteFileStates(before, after) {
			t.Fatalf("未检测到持久化文件变化: %s", changedPath)
		}
	}
}

func TestPrepareSQLiteSnapshotCreatesCurrentTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.db")
	db, err := openSQLiteForMigration(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := prepareSQLiteSnapshot(db); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceTables(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn("user_files", "directory_id") || !db.Migrator().HasTable("user_tag_preference") {
		t.Fatal("快照升级未补齐当前结构")
	}
}

func TestValidateSourceTablesRejectsUnknownTable(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "unknown.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := prepareSQLiteSnapshot(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE custom_unknown(id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	err = validateSourceTables(db)
	if err == nil || !strings.Contains(err.Error(), "未知表custom_unknown") {
		t.Fatalf("应拒绝未知表，实际错误: %v", err)
	}
}

func TestNormalizeMigrationTime(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	column := migrationColumn{Name: "created_at", DataType: "datetime", Nullable: true, DatetimePrecision: 6}
	tests := []struct {
		name  string
		value interface{}
		want  interface{}
	}{
		{name: "UTC", value: "2026-08-14 01:02:03.1234567+00:00", want: "2026-08-14 09:02:03.123456"},
		{name: "东八区", value: "2026-08-14T09:02:03.123456+08:00", want: "2026-08-14 09:02:03.123456"},
		{name: "无时区", value: "2026-08-14 09:02:03.123456", want: "2026-08-14 09:02:03.123456"},
		{name: "零时间", value: "0001-01-01 00:00:00+00:00", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeMigrationValue(test.value, column, location)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("时间转换错误: got=%v want=%v", got, test.want)
			}
		})
	}
}

func TestNormalizeMigrationValuePreservesPrimitiveData(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	tests := []struct {
		column migrationColumn
		value  interface{}
		want   interface{}
	}{
		{column: migrationColumn{DataType: "varchar"}, value: []byte("中文😀"), want: "中文😀"},
		{column: migrationColumn{DataType: "bigint"}, value: int64(1<<62 + 7), want: "4611686018427387911"},
		{column: migrationColumn{DataType: "tinyint"}, value: int64(1), want: "1"},
		{column: migrationColumn{DataType: "blob"}, value: []byte{0, 1, 2}, want: []byte{0, 1, 2}},
	}
	for _, test := range tests {
		got, err := normalizeMigrationValue(test.value, test.column, location)
		if err != nil {
			t.Fatal(err)
		}
		switch want := test.want.(type) {
		case []byte:
			gotBytes, ok := got.([]byte)
			if !ok || string(gotBytes) != string(want) {
				t.Fatalf("二进制转换错误: %v", got)
			}
		default:
			if got != want {
				t.Fatalf("值转换错误: got=%v want=%v", got, want)
			}
		}
	}
}

func TestRedactMySQLDSN(t *testing.T) {
	dsn := "myobj:secret-password@tcp(127.0.0.1:3306)/my_obj?parseTime=true"
	redacted := RedactMySQLDSN(dsn)
	if strings.Contains(redacted, "secret-password") || !strings.Contains(redacted, "my_obj") {
		t.Fatalf("DSN脱敏错误: %s", redacted)
	}
}

func TestValidateMigrationOptions(t *testing.T) {
	_, _, err := validateSQLiteToMySQLOptions(SQLiteToMySQLOptions{SourcePath: "source.db", MySQLDSN: "dsn", BatchSize: 99})
	if err == nil {
		t.Fatal("batch-size小于下限时应失败")
	}
	_, _, err = validateSQLiteToMySQLOptions(SQLiteToMySQLOptions{SourcePath: "source.db", MySQLDSN: "dsn", BatchSize: 100, Timezone: "Invalid/Timezone"})
	if err == nil {
		t.Fatal("无效时区应失败")
	}
}

func fileSHA256(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}
