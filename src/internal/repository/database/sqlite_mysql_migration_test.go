package database

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
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
	if !db.Migrator().HasColumn("user_files", "directory_id") || !db.Migrator().HasTable("user_tag_stat") {
		t.Fatal("快照升级未补齐当前结构")
	}
}

func TestPrepareSQLiteSnapshotDeduplicatesLegacyGroupPowers(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "legacy-group-power-snapshot.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := db.Exec(`CREATE TABLE group_power (
		group_id INTEGER NOT NULL,
		power_id INTEGER NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO group_power(group_id, power_id) VALUES(1, 1)").Error; err != nil {
		t.Fatal(err)
	}

	if err := prepareSQLiteSnapshot(db); err != nil {
		t.Fatal(err)
	}
	var pairCount int64
	if err := db.Table("group_power").Where("group_id = ? AND power_id = ?", 1, 1).Count(&pairCount).Error; err != nil {
		t.Fatal(err)
	}
	if pairCount != 1 {
		t.Fatalf("快照升级后用户组权限(1,1)数量错误: %d", pairCount)
	}
	var duplicatePairs int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
		SELECT group_id, power_id FROM group_power
		GROUP BY group_id, power_id HAVING COUNT(*) > 1
	) duplicated`).Scan(&duplicatePairs).Error; err != nil {
		t.Fatal(err)
	}
	if duplicatePairs != 0 {
		t.Fatalf("快照升级后仍有%d组重复用户组权限", duplicatePairs)
	}
}

func TestMigrateLegacyGroupDefaultsRepairsNullFlags(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "legacy-groups.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := db.Exec(`CREATE TABLE groups (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		group_default INTEGER NULL,
		created_at DATETIME NOT NULL,
		space INTEGER NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO groups(id, name, group_default, created_at, space) VALUES
		(1, '管理员', NULL, '2026-08-14 08:00:00', 0),
		(2, '用户', 1, '2026-08-14 08:00:00', 536870912000)`).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyGroupDefaults(db); err != nil {
		t.Fatal(err)
	}
	var groups []struct {
		ID           int
		GroupDefault int
	}
	if err := db.Table("groups").Select("id", "group_default").Order("id ASC").Scan(&groups).Error; err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].GroupDefault != 0 || groups[1].GroupDefault != 1 {
		t.Fatalf("历史用户组默认标志修复错误: %+v", groups)
	}
}

func TestMigrateLegacyGroupPowerDuplicatesKeepsDistinctGrants(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "legacy-group-power.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := db.Exec(`CREATE TABLE group_power (
		group_id INTEGER NOT NULL,
		power_id INTEGER NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO group_power(group_id, power_id) VALUES
		(1, 1), (1, 1), (1, 2), (1, 2), (2, 1)`).Error; err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 2; run++ {
		if err := migrateLegacyGroupPowerDuplicates(db); err != nil {
			t.Fatalf("第%d次清理重复用户组权限失败: %v", run, err)
		}
	}
	var grants []struct {
		GroupID int
		PowerID int
	}
	if err := db.Table("group_power").Order("group_id ASC, power_id ASC").Scan(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if len(grants) != 3 ||
		grants[0].GroupID != 1 || grants[0].PowerID != 1 ||
		grants[1].GroupID != 1 || grants[1].PowerID != 2 ||
		grants[2].GroupID != 2 || grants[2].PowerID != 1 {
		t.Fatalf("重复用户组权限清理错误: %+v", grants)
	}
}

func TestPreflightSQLiteUniqueIndexesCollectsAllDuplicates(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "preflight-duplicates.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := db.Exec(`CREATE TABLE sample (
		id INTEGER NOT NULL,
		code TEXT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO sample(id, code) VALUES
		(1, 'A'), (1, 'B'), (2, 'C'), (3, NULL), (4, NULL), (5, 'C')`).Error; err != nil {
		t.Fatal(err)
	}
	issues := &migrationPreflightIssues{issues: make(map[string]*migrationPreflightIssue)}
	indexes := []migrationUniqueIndex{
		{Name: "PRIMARY", Columns: []string{"id"}},
		{Name: "uk_sample_code", Columns: []string{"code"}},
	}
	if err := preflightSQLiteUniqueIndexes(db, "sample", indexes, issues); err != nil {
		t.Fatal(err)
	}
	primaryIssue := issues.issues["sample\x00唯一索引PRIMARY"]
	codeIssue := issues.issues["sample\x00唯一索引uk_sample_code"]
	if primaryIssue == nil || primaryIssue.Count != 2 {
		t.Fatalf("主键重复预检结果错误: %+v", primaryIssue)
	}
	if codeIssue == nil || codeIssue.Count != 2 {
		t.Fatalf("唯一索引重复预检结果错误: %+v", codeIssue)
	}
	if len(issues.issues) != 2 {
		t.Fatalf("NULL不应被MySQL唯一索引重复规则拦截: %+v", issues.issues)
	}
}

func TestPreflightMigrationTableAggregatesAndRollsBack(t *testing.T) {
	tempDir := t.TempDir()
	source, err := openSQLiteForMigration(filepath.Join(tempDir, "preflight-source.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(source)
	target, err := openSQLiteForMigration(filepath.Join(tempDir, "preflight-target.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(target)
	if err := source.Exec("CREATE TABLE sample(id INTEGER, required_value INTEGER NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Exec("INSERT INTO sample(id, required_value) VALUES(1, NULL), (2, 10), (2, 20)").Error; err != nil {
		t.Fatal(err)
	}
	if err := target.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY, required_value INTEGER NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	columns := []migrationColumn{
		{Name: "id", DataType: "integer", Nullable: false},
		{Name: "required_value", DataType: "integer", Nullable: false},
	}
	issues := &migrationPreflightIssues{issues: make(map[string]*migrationPreflightIssue)}
	if err := preflightMigrationTable(
		context.Background(), source, target, "sample", columns, []string{"id"}, time.UTC,
		100, 3, nil, issues,
	); err != nil {
		t.Fatal(err)
	}
	if len(issues.issues) != 2 {
		t.Fatalf("应汇总非空与重复主键两类问题: %s", issues.Error())
	}
	if !strings.Contains(issues.Error(), "数据转换") || !strings.Contains(issues.Error(), "写入约束") {
		t.Fatalf("预检问题分类错误: %s", issues.Error())
	}
	var targetRows int64
	if err := target.Table("sample").Count(&targetRows).Error; err != nil {
		t.Fatal(err)
	}
	if targetRows != 0 {
		t.Fatalf("预检事务未回滚，目标残留%d行", targetRows)
	}
}

func TestPreflightSourceRelationshipsCollectsAllOrphans(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "preflight-relationships.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := prepareSQLiteSnapshot(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_info(
		id, name, user_name, password, email, phone, group_id, created_at, space, file_password, free_space, state
	) VALUES('orphan-user', '悬空用户', 'orphan', 'hash', 'orphan@example.com', '13800000000', 999,
		'2026-08-14 08:00:00', 0, '', 0, 0)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO group_power(group_id, power_id) VALUES(999, 999)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_info(
		id, name, user_name, password, email, phone, group_id, created_at, space, file_password, free_space, state
	) VALUES('active-file-user', '活动文件用户', 'active-file-user', 'hash', 'active@example.com', '13800000001', 1,
		'2026-08-14 08:00:00', 0, '', 0, 0)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_files(
		user_id, file_id, file_name, directory_id, public, created_at, deleted_at, uf_id
	) VALUES('active-file-user', 'missing-file', '缺失实体.txt', 0, 0, '2026-08-14 08:00:00', NULL, 'active-orphan')`).Error; err != nil {
		t.Fatal(err)
	}
	issues := &migrationPreflightIssues{issues: make(map[string]*migrationPreflightIssue)}
	if err := preflightSourceRelationships(db, issues); err != nil {
		t.Fatal(err)
	}
	if issues.issues["关联完整性\x00用户组引用"] == nil ||
		issues.issues["关联完整性\x00用户组权限引用"] == nil ||
		issues.issues["关联完整性\x00活动用户文件实体引用"] == nil {
		t.Fatalf("未汇总全部悬空引用: %s", issues.Error())
	}
}

func TestPreflightSourceRelationshipsAllowsDeletedUserFileWithoutEntity(t *testing.T) {
	db, err := openSQLiteForMigration(filepath.Join(t.TempDir(), "preflight-deleted-file.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeGormDB(db)
	if err := prepareSQLiteSnapshot(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_info(
		id, name, user_name, password, email, phone, group_id, created_at, space, file_password, free_space, state
	) VALUES('deleted-file-user', '回收站用户', 'deleted-file-user', 'hash', 'deleted@example.com', '13800000002', 1,
		'2026-08-14 08:00:00', 0, '', 0, 0)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO user_files(
		user_id, file_id, file_name, directory_id, public, created_at, deleted_at, uf_id
	) VALUES('deleted-file-user', 'missing-deleted-file', '已删除文件.txt', 999, 0,
		'2026-08-14 08:00:00', '2026-08-14 09:00:00', 'deleted-orphan')`).Error; err != nil {
		t.Fatal(err)
	}
	issues := &migrationPreflightIssues{issues: make(map[string]*migrationPreflightIssue)}
	if err := preflightSourceRelationships(db, issues); err != nil {
		t.Fatal(err)
	}
	if issue := issues.issues["关联完整性\x00活动用户文件实体引用"]; issue != nil {
		t.Fatalf("软删除用户文件缺少实体不应阻止迁移: %+v", issue)
	}
	if issue := issues.issues["关联完整性\x00活动文件目录引用"]; issue != nil {
		t.Fatalf("软删除用户文件的历史目录缺失不应阻止迁移: %+v", issue)
	}
}

func TestMigrationPreflightIssuesAggregatesDeterministically(t *testing.T) {
	issues := &migrationPreflightIssues{issues: make(map[string]*migrationPreflightIssue)}
	issues.add("groups", "数据转换", "主键[id=1]: 非空列不能写入NULL")
	issues.addCount("group_power", "唯一索引PRIMARY", 2, "group_id=1,power_id=1")
	message := issues.Error()
	for _, expected := range []string{
		"发现2类不兼容问题，累计命中3次数据校验",
		"表group_power / 唯一索引PRIMARY: 2次",
		"表groups / 数据转换: 1次",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("预检汇总缺少%q: %s", expected, message)
		}
	}
	if strings.Index(message, "表group_power") > strings.Index(message, "表groups") {
		t.Fatalf("预检汇总顺序不稳定: %s", message)
	}
}

func TestMigrationPreflightErrorCategorySeparatesDataAndRuntimeErrors(t *testing.T) {
	category, isDataIssue := migrationPreflightErrorCategory(&mysqldriver.MySQLError{Number: 1062, Message: "duplicate"})
	if !isDataIssue || category != "MySQL错误1062" {
		t.Fatalf("重复键应归类为数据问题: category=%s data=%v", category, isDataIssue)
	}
	category, isDataIssue = migrationPreflightErrorCategory(&mysqldriver.MySQLError{Number: 1205, Message: "lock timeout"})
	if isDataIssue || category != "" {
		t.Fatalf("锁等待超时不应归类为数据问题: category=%s data=%v", category, isDataIssue)
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

func TestNormalizeMigrationValueRejectsNullForRequiredColumn(t *testing.T) {
	_, err := normalizeMigrationValue(nil, migrationColumn{Name: "group_default", DataType: "int", Nullable: false}, time.UTC)
	if err == nil || !strings.Contains(err.Error(), "非空列不能写入NULL") {
		t.Fatalf("非空列NULL应在预检阶段失败，实际错误: %v", err)
	}
	value, err := normalizeMigrationValue(nil, migrationColumn{Name: "space", DataType: "bigint", Nullable: true}, time.UTC)
	if err != nil || value != nil {
		t.Fatalf("可空列NULL应保留，value=%v err=%v", value, err)
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
