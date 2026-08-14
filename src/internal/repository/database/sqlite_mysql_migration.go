package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"myobj/src/pkg/models"
)

const (
	defaultMigrationBatchSize = 1000
	minimumMigrationBatchSize = 100
	maximumMigrationBatchSize = 10000
)

// SQLiteToMySQLOptions 定义一次 SQLite→MySQL 迁移的输入和安全边界。
type SQLiteToMySQLOptions struct {
	SourcePath   string
	SnapshotPath string
	MySQLDSN     string
	BatchSize    int
	Timezone     string
	DryRun       bool
	Progress     func(MigrationProgress)
}

// MigrationProgress 是迁移 CLI 使用的无敏感信息进度事件。
type MigrationProgress struct {
	Stage     string
	Table     string
	Completed int64
	Total     int64
	Message   string
}

// TableMigrationReport 记录单表迁移与校验结果。
type TableMigrationReport struct {
	Table        string
	SourceRows   int64
	TargetRows   int64
	SourceDigest string
	TargetDigest string
}

// SQLiteToMySQLReport 是迁移或只读校验的最终报告。
type SQLiteToMySQLReport struct {
	SourcePath        string
	SnapshotPath      string
	TargetDescription string
	DryRun            bool
	Tables            []TableMigrationReport
}

type migrationTable struct {
	Name  string
	Model interface{}
}

type migrationColumn struct {
	Name              string
	DataType          string
	Nullable          bool
	DatetimePrecision int
}

type fileState struct {
	Exists  bool
	Size    int64
	ModTime time.Time
}

// MigrateSQLiteToMySQL 从只读源库生成快照，并将升级后的快照迁移到空 MySQL。
func MigrateSQLiteToMySQL(ctx context.Context, options SQLiteToMySQLOptions) (*SQLiteToMySQLReport, error) {
	options, location, err := validateSQLiteToMySQLOptions(options)
	if err != nil {
		return nil, err
	}

	sourcePath, err := filepath.Abs(options.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("解析源SQLite路径失败: %w", err)
	}
	snapshotPath := options.SnapshotPath
	if snapshotPath == "" {
		snapshotPath = fmt.Sprintf("%s.mysql-migration-%s.snapshot.db", sourcePath, time.Now().Format("20060102-150405"))
	}
	snapshotPath, err = filepath.Abs(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("解析快照路径失败: %w", err)
	}
	if sameFilePath(sourcePath, snapshotPath) {
		return nil, errors.New("快照路径不能与源SQLite路径相同")
	}

	emitMigrationProgress(options.Progress, MigrationProgress{Stage: "snapshot", Message: "正在创建只读一致性快照"})
	if err := createSQLiteSnapshot(ctx, sourcePath, snapshotPath); err != nil {
		return nil, err
	}

	snapshotDB, err := openSQLiteForMigration(snapshotPath, false)
	if err != nil {
		return nil, fmt.Errorf("打开SQLite快照失败: %w", err)
	}
	defer closeGormDB(snapshotDB)

	emitMigrationProgress(options.Progress, MigrationProgress{Stage: "upgrade", Message: "正在升级SQLite快照结构"})
	if err := prepareSQLiteSnapshot(snapshotDB); err != nil {
		return nil, fmt.Errorf("升级SQLite快照失败: %w", err)
	}
	if err := validateSourceTables(snapshotDB); err != nil {
		return nil, err
	}

	targetDB, targetDescription, err := openMigrationMySQL(options.MySQLDSN, location)
	if err != nil {
		return nil, err
	}
	defer closeGormDB(targetDB)
	if err := validateTargetDatabase(targetDB, true); err != nil {
		return nil, err
	}

	report := &SQLiteToMySQLReport{
		SourcePath:        sourcePath,
		SnapshotPath:      snapshotPath,
		TargetDescription: targetDescription,
		DryRun:            options.DryRun,
	}
	if options.DryRun {
		report.Tables, err = collectDryRunCounts(snapshotDB)
		if err != nil {
			return nil, err
		}
		return report, nil
	}

	emitMigrationProgress(options.Progress, MigrationProgress{Stage: "schema", Message: "正在创建MySQL目标结构"})
	if err := autoMigrateCurrentModels(targetDB); err != nil {
		return nil, fmt.Errorf("创建MySQL目标结构失败，目标库需要重建后重试: %w", err)
	}

	report.Tables, err = copyAllMigrationTables(ctx, snapshotDB, targetDB, location, options.BatchSize, options.Progress)
	if err != nil {
		return nil, fmt.Errorf("复制数据失败，目标库已包含部分数据，需要重建后重试: %w", err)
	}
	if _, err := migrateCurrentSchema(targetDB); err != nil {
		return nil, fmt.Errorf("执行MySQL增量迁移失败，目标库需要重建后重试: %w", err)
	}

	emitMigrationProgress(options.Progress, MigrationProgress{Stage: "verify", Message: "正在校验迁移结果"})
	report.Tables, err = verifyMigrationData(ctx, snapshotDB, targetDB, location)
	if err != nil {
		return nil, fmt.Errorf("迁移校验失败: %w", err)
	}
	if err := verifyTargetRelationships(targetDB); err != nil {
		return nil, fmt.Errorf("关联完整性校验失败: %w", err)
	}
	if err := verifyAutoIncrementValues(targetDB); err != nil {
		return nil, fmt.Errorf("自增值校验失败: %w", err)
	}
	return report, nil
}

// VerifySQLiteToMySQL 对已存在的目标库执行只读校验，不创建快照或写入数据。
func VerifySQLiteToMySQL(ctx context.Context, options SQLiteToMySQLOptions) (*SQLiteToMySQLReport, error) {
	options, location, err := validateSQLiteToMySQLOptions(options)
	if err != nil {
		return nil, err
	}
	sourcePath, err := filepath.Abs(options.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("解析SQLite快照路径失败: %w", err)
	}
	sourceDB, err := openSQLiteForMigration(sourcePath, true)
	if err != nil {
		return nil, fmt.Errorf("打开SQLite快照失败: %w", err)
	}
	defer closeGormDB(sourceDB)
	if err := validateSourceTables(sourceDB); err != nil {
		return nil, err
	}
	targetDB, targetDescription, err := openMigrationMySQL(options.MySQLDSN, location)
	if err != nil {
		return nil, err
	}
	defer closeGormDB(targetDB)
	if err := validateTargetDatabase(targetDB, false); err != nil {
		return nil, err
	}
	tables, err := verifyMigrationData(ctx, sourceDB, targetDB, location)
	if err != nil {
		return nil, err
	}
	if err := verifyTargetRelationships(targetDB); err != nil {
		return nil, err
	}
	if err := verifyAutoIncrementValues(targetDB); err != nil {
		return nil, err
	}
	return &SQLiteToMySQLReport{
		SourcePath:        sourcePath,
		SnapshotPath:      sourcePath,
		TargetDescription: targetDescription,
		Tables:            tables,
	}, nil
}

// RedactMySQLDSN 返回可安全展示的目标地址。
func RedactMySQLDSN(dsn string) string {
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return "MySQL DSN格式无效"
	}
	user := config.User
	if user == "" {
		user = "<empty>"
	}
	return fmt.Sprintf("%s:***@%s(%s)/%s", user, config.Net, config.Addr, config.DBName)
}

func validateSQLiteToMySQLOptions(options SQLiteToMySQLOptions) (SQLiteToMySQLOptions, *time.Location, error) {
	if strings.TrimSpace(options.SourcePath) == "" {
		return options, nil, errors.New("必须指定源SQLite路径")
	}
	if strings.TrimSpace(options.MySQLDSN) == "" {
		return options, nil, errors.New("环境变量MYOBJ_MIGRATE_MYSQL_DSN不能为空")
	}
	if options.BatchSize == 0 {
		options.BatchSize = defaultMigrationBatchSize
	}
	if options.BatchSize < minimumMigrationBatchSize || options.BatchSize > maximumMigrationBatchSize {
		return options, nil, fmt.Errorf("batch-size必须在%d到%d之间", minimumMigrationBatchSize, maximumMigrationBatchSize)
	}
	if strings.TrimSpace(options.Timezone) == "" {
		options.Timezone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(options.Timezone)
	if err != nil {
		return options, nil, fmt.Errorf("无效时区%s: %w", options.Timezone, err)
	}
	return options, location, nil
}

func createSQLiteSnapshot(ctx context.Context, sourcePath, snapshotPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("读取源SQLite失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("源SQLite路径不是普通文件")
	}
	if _, err := os.Stat(snapshotPath); err == nil {
		return fmt.Errorf("快照文件已存在，拒绝覆盖: %s", snapshotPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查快照路径失败: %w", err)
	}
	parent := filepath.Dir(snapshotPath)
	if parentInfo, err := os.Stat(parent); err != nil || !parentInfo.IsDir() {
		return fmt.Errorf("快照目录不存在或不可用: %s", parent)
	}

	before := captureSQLiteFileStates(sourcePath)
	db, err := openSQLiteForMigration(sourcePath, true)
	if err != nil {
		return fmt.Errorf("只读打开源SQLite失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		closeGormDB(db)
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		closeGormDB(db)
		return err
	}
	defer func() {
		_ = conn.Close()
		closeGormDB(db)
	}()

	var quickCheck string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quickCheck); err != nil {
		return fmt.Errorf("执行SQLite完整性检查失败: %w", err)
	}
	if !strings.EqualFold(quickCheck, "ok") {
		return fmt.Errorf("SQLite完整性检查失败: %s", quickCheck)
	}
	var dataVersionBefore, dataVersionAfter int64
	if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&dataVersionBefore); err != nil {
		return err
	}
	escapedSnapshot := strings.ReplaceAll(filepath.ToSlash(snapshotPath), "'", "''")
	if _, err := conn.ExecContext(ctx, "VACUUM INTO '"+escapedSnapshot+"'"); err != nil {
		return fmt.Errorf("创建SQLite一致性快照失败: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&dataVersionAfter); err != nil {
		return err
	}
	after := captureSQLiteFileStates(sourcePath)
	if dataVersionBefore != dataVersionAfter || !sameSQLiteFileStates(before, after) {
		return fmt.Errorf("生成快照期间源SQLite发生变化，请停止MyObj、WebDAV和后台任务后重试；已生成快照保留在%s", snapshotPath)
	}
	return nil
}

func openSQLiteForMigration(path string, readOnly bool) (*gorm.DB, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := absPath
	if readOnly {
		dsn = "file:" + filepath.ToSlash(absPath) + "?mode=ro"
	}
	return gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
}

func openMigrationMySQL(dsn string, location *time.Location) (*gorm.DB, string, error) {
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, "", fmt.Errorf("解析MYOBJ_MIGRATE_MYSQL_DSN失败: %w", err)
	}
	if strings.TrimSpace(config.DBName) == "" {
		return nil, "", errors.New("MySQL DSN必须明确指定目标数据库名")
	}
	config.ParseTime = true
	config.Loc = location
	if config.Params == nil {
		config.Params = make(map[string]string)
	}
	config.Params["charset"] = "utf8mb4"
	config.Collation = "utf8mb4_unicode_ci"
	precision := 6
	dialector := mysql.New(mysql.Config{
		DSN:                      config.FormatDSN(),
		DefaultDatetimePrecision: &precision,
	})
	db, err := gorm.Open(dialector, &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, "", fmt.Errorf("连接目标%s失败: %w", RedactMySQLDSN(dsn), err)
	}
	return db, RedactMySQLDSN(dsn), nil
}

func prepareSQLiteSnapshot(db *gorm.DB) error {
	if err := autoMigrateMissingModels(db); err != nil {
		return err
	}
	if _, err := migrateCurrentSchema(db); err != nil {
		return err
	}
	return autoMigrateMissingModels(db)
}

// autoMigrateMissingModels 只补建不存在的表，不用当前约束重建历史基础表。
// 这样既能初始化全新数据库，也能保留历史 SQLite 中可能存在的宽松 NULL 数据。
func autoMigrateMissingModels(db *gorm.DB) error {
	for _, table := range currentMigrationTables() {
		if db.Migrator().HasTable(table.Name) {
			continue
		}
		if err := db.AutoMigrate(table.Model); err != nil {
			return fmt.Errorf("补建数据表%s失败: %w", table.Name, err)
		}
	}
	return nil
}

func autoMigrateCurrentModels(db *gorm.DB) error {
	modelsToMigrate := make([]interface{}, 0, len(currentMigrationTables()))
	for _, table := range currentMigrationTables() {
		modelsToMigrate = append(modelsToMigrate, table.Model)
	}
	return db.AutoMigrate(modelsToMigrate...)
}

func currentMigrationTables() []migrationTable {
	return []migrationTable{
		{Name: "groups", Model: &models.Group{}},
		{Name: "power", Model: &models.Power{}},
		{Name: "group_power", Model: &models.GroupPower{}},
		{Name: "user_info", Model: &models.UserInfo{}},
		{Name: "api_key", Model: &models.ApiKey{}},
		{Name: "disk", Model: &models.Disk{}},
		{Name: "file_info", Model: &models.FileInfo{}},
		{Name: "virtual_directory", Model: &models.VirtualDirectory{}},
		{Name: "user_files", Model: &models.UserFiles{}},
		{Name: "file_chunk", Model: &models.FileChunk{}},
		{Name: "upload_task", Model: &models.UploadTask{}},
		{Name: "upload_chunk", Model: &models.UploadChunk{}},
		{Name: "download_task", Model: &models.DownloadTask{}},
		{Name: "installed_plugin", Model: &models.InstalledPlugin{}},
		{Name: "subscription", Model: &models.Subscription{}},
		{Name: "subscription_run", Model: &models.SubscriptionRun{}},
		{Name: "subscription_item", Model: &models.SubscriptionItem{}},
		{Name: "plugin_audit_log", Model: &models.PluginAuditLog{}},
		{Name: "shares", Model: &models.Share{}},
		{Name: "recycled", Model: &models.Recycled{}},
		{Name: "recycled_directory_node", Model: &models.RecycledDirectoryNode{}},
		{Name: "recycled_directory_file", Model: &models.RecycledDirectoryFile{}},
		{Name: "recycled_directory_tag", Model: &models.RecycledDirectoryTag{}},
		{Name: "tag_category", Model: &models.TagCategory{}},
		{Name: "tag_definition", Model: &models.TagDefinition{}},
		{Name: "user_tag_preference", Model: &models.UserTagPreference{}},
		{Name: "user_file_tag", Model: &models.UserFileTag{}},
		{Name: "user_directory_tag", Model: &models.UserDirectoryTag{}},
		{Name: "user_file_tag_exclusion", Model: &models.UserFileTagExclusion{}},
		{Name: "user_tag_stat", Model: &models.UserTagStat{}},
		{Name: "user_file_tag_state", Model: &models.UserFileTagState{}},
		{Name: "file_metadata", Model: &models.FileMetadata{}},
		{Name: "file_metadata_state", Model: &models.FileMetadataState{}},
		{Name: "tag_rule_set", Model: &models.TagRuleSet{}},
		{Name: "tag_rule", Model: &models.TagRule{}},
		{Name: "tag_rebuild_job", Model: &models.TagRebuildJob{}},
		{Name: "tag_rebuild_failure", Model: &models.TagRebuildFailure{}},
		{Name: "sys_config", Model: &models.SysConfig{}},
		{Name: "schema_migration", Model: &schemaMigration{}},
	}
}

func validateSourceTables(db *gorm.DB) error {
	var names []string
	if err := db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name").Scan(&names).Error; err != nil {
		return fmt.Errorf("读取SQLite表清单失败: %w", err)
	}
	expected := make(map[string]struct{}, len(currentMigrationTables())+1)
	for _, table := range currentMigrationTables() {
		expected[table.Name] = struct{}{}
	}
	expected["sqlite_sequence"] = struct{}{}
	for _, name := range names {
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("SQLite快照包含未知表%s，拒绝静默遗漏", name)
		}
	}
	for _, table := range currentMigrationTables() {
		if !db.Migrator().HasTable(table.Name) {
			return fmt.Errorf("SQLite快照缺少表%s", table.Name)
		}
	}
	return nil
}

func validateTargetDatabase(db *gorm.DB, requireEmpty bool) error {
	var databaseName, charset, collation string
	if err := db.Raw(`SELECT s.SCHEMA_NAME, s.DEFAULT_CHARACTER_SET_NAME, s.DEFAULT_COLLATION_NAME
		FROM information_schema.SCHEMATA s WHERE s.SCHEMA_NAME = DATABASE()`).Row().Scan(&databaseName, &charset, &collation); err != nil {
		return fmt.Errorf("读取MySQL数据库属性失败: %w", err)
	}
	if databaseName == "" {
		return errors.New("MySQL DSN未选中目标数据库")
	}
	if !strings.EqualFold(charset, "utf8mb4") || !strings.EqualFold(collation, "utf8mb4_unicode_ci") {
		return fmt.Errorf("目标数据库必须使用utf8mb4/utf8mb4_unicode_ci，当前为%s/%s", charset, collation)
	}
	var tables []string
	if err := db.Raw(`SELECT TABLE_NAME FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`).Scan(&tables).Error; err != nil {
		return err
	}
	if requireEmpty && len(tables) > 0 {
		return fmt.Errorf("目标MySQL不是空库，发现表: %s", strings.Join(tables, ", "))
	}
	if !requireEmpty && len(tables) == 0 {
		return errors.New("目标MySQL为空，无法执行迁移校验")
	}
	return nil
}

func collectDryRunCounts(source *gorm.DB) ([]TableMigrationReport, error) {
	reports := make([]TableMigrationReport, 0, len(currentMigrationTables()))
	for _, table := range currentMigrationTables() {
		var count int64
		if err := source.Table(table.Name).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("统计表%s失败: %w", table.Name, err)
		}
		reports = append(reports, TableMigrationReport{Table: table.Name, SourceRows: count})
	}
	return reports, nil
}

func copyAllMigrationTables(ctx context.Context, source, target *gorm.DB, location *time.Location, batchSize int, progress func(MigrationProgress)) ([]TableMigrationReport, error) {
	reports := make([]TableMigrationReport, 0, len(currentMigrationTables()))
	for _, table := range currentMigrationTables() {
		columns, err := loadMySQLColumns(target, table.Name)
		if err != nil {
			return nil, err
		}
		if err := ensureSQLiteColumns(source, table.Name, columns); err != nil {
			return nil, err
		}
		primaryKeys, err := loadMySQLPrimaryKeys(target, table.Name)
		if err != nil {
			return nil, err
		}
		var total int64
		if err := source.Table(table.Name).Count(&total).Error; err != nil {
			return nil, err
		}
		emitMigrationProgress(progress, MigrationProgress{Stage: "copy", Table: table.Name, Total: total})
		report, err := copyMigrationTable(ctx, source, target, table.Name, columns, primaryKeys, location, batchSize, total, progress)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func copyMigrationTable(ctx context.Context, source, target *gorm.DB, table string, columns []migrationColumn, primaryKeys []string, location *time.Location, batchSize int, total int64, progress func(MigrationProgress)) (TableMigrationReport, error) {
	columnNames := migrationColumnNames(columns)
	selectSQL := "SELECT " + quoteIdentifiers(columnNames, '"') + " FROM " + quoteIdentifier(table, '"')
	if len(primaryKeys) > 0 {
		selectSQL += " ORDER BY " + quoteIdentifiers(primaryKeys, '"')
	}
	rows, err := source.Raw(selectSQL).Rows()
	if err != nil {
		return TableMigrationReport{}, fmt.Errorf("读取表%s失败: %w", table, err)
	}
	defer rows.Close()

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	insertSQL := "INSERT INTO " + quoteIdentifier(table, '`') + " (" + quoteIdentifiers(columnNames, '`') + ") VALUES (" + placeholders + ")"
	hash := sha256.New()
	var copied int64
	for {
		tx := target.Begin()
		if tx.Error != nil {
			return TableMigrationReport{}, tx.Error
		}
		stmt, err := tx.Raw("SELECT 1").Statement.ConnPool.PrepareContext(ctx, insertSQL)
		if err != nil {
			tx.Rollback()
			return TableMigrationReport{}, fmt.Errorf("准备表%s写入语句失败: %w", table, err)
		}
		batchRows := 0
		for batchRows < batchSize && rows.Next() {
			rawValues, err := scanRowValues(rows, len(columns))
			if err != nil {
				stmt.Close()
				tx.Rollback()
				return TableMigrationReport{}, fmt.Errorf("读取表%s第%d行失败: %w", table, copied+1, err)
			}
			values, err := normalizeRowValues(rawValues, columns, location)
			if err != nil {
				stmt.Close()
				tx.Rollback()
				return TableMigrationReport{}, fmt.Errorf("转换表%s第%d行失败: %w", table, copied+1, err)
			}
			if _, err := stmt.ExecContext(ctx, values...); err != nil {
				key := migrationRowKey(columnNames, primaryKeys, values)
				stmt.Close()
				tx.Rollback()
				return TableMigrationReport{}, fmt.Errorf("写入表%s主键[%s]失败: %w", table, key, err)
			}
			writeCanonicalRow(hash, columns, values)
			copied++
			batchRows++
		}
		if err := rows.Err(); err != nil {
			stmt.Close()
			tx.Rollback()
			return TableMigrationReport{}, fmt.Errorf("遍历表%s失败: %w", table, err)
		}
		if err := stmt.Close(); err != nil {
			tx.Rollback()
			return TableMigrationReport{}, err
		}
		if batchRows == 0 {
			tx.Rollback()
			break
		}
		if err := tx.Commit().Error; err != nil {
			return TableMigrationReport{}, fmt.Errorf("提交表%s批次失败: %w", table, err)
		}
		emitMigrationProgress(progress, MigrationProgress{Stage: "copy", Table: table, Completed: copied, Total: total})
		if copied >= total {
			break
		}
	}
	return TableMigrationReport{Table: table, SourceRows: copied, SourceDigest: hex.EncodeToString(hash.Sum(nil))}, nil
}

func verifyMigrationData(ctx context.Context, source, target *gorm.DB, location *time.Location) ([]TableMigrationReport, error) {
	reports := make([]TableMigrationReport, 0, len(currentMigrationTables()))
	for _, table := range currentMigrationTables() {
		columns, err := loadMySQLColumns(target, table.Name)
		if err != nil {
			return nil, err
		}
		if err := ensureSQLiteColumns(source, table.Name, columns); err != nil {
			return nil, err
		}
		primaryKeys, err := loadMySQLPrimaryKeys(target, table.Name)
		if err != nil {
			return nil, err
		}
		sourceRows, sourceDigest, err := digestTable(ctx, source, table.Name, columns, primaryKeys, location, '"')
		if err != nil {
			return nil, err
		}
		targetRows, targetDigest, err := digestTable(ctx, target, table.Name, columns, primaryKeys, location, '`')
		if err != nil {
			return nil, err
		}
		report := TableMigrationReport{Table: table.Name, SourceRows: sourceRows, TargetRows: targetRows, SourceDigest: sourceDigest, TargetDigest: targetDigest}
		reports = append(reports, report)
		if sourceRows != targetRows {
			return nil, fmt.Errorf("表%s行数不一致: SQLite=%d MySQL=%d", table.Name, sourceRows, targetRows)
		}
		if sourceDigest != targetDigest {
			difference, differenceErr := locateFirstTableDifference(ctx, source, target, table.Name, columns, primaryKeys, location)
			if differenceErr != nil {
				return nil, fmt.Errorf("表%s内容摘要不一致，定位差异失败: %w", table.Name, differenceErr)
			}
			return nil, fmt.Errorf("表%s内容摘要不一致: %s", table.Name, difference)
		}
	}
	return reports, nil
}

func locateFirstTableDifference(ctx context.Context, source, target *gorm.DB, table string, columns []migrationColumn, primaryKeys []string, location *time.Location) (string, error) {
	columnNames := migrationColumnNames(columns)
	orderSQLite := "SELECT " + quoteIdentifiers(columnNames, '"') + " FROM " + quoteIdentifier(table, '"') + " ORDER BY " + quoteIdentifiers(primaryKeys, '"')
	orderMySQL := "SELECT " + quoteIdentifiers(columnNames, '`') + " FROM " + quoteIdentifier(table, '`') + " ORDER BY " + quoteIdentifiers(primaryKeys, '`')
	sourceRows, err := source.WithContext(ctx).Raw(orderSQLite).Rows()
	if err != nil {
		return "", err
	}
	defer sourceRows.Close()
	targetRows, err := target.WithContext(ctx).Raw(orderMySQL).Rows()
	if err != nil {
		return "", err
	}
	defer targetRows.Close()
	var rowNumber int64
	for {
		hasSource := sourceRows.Next()
		hasTarget := targetRows.Next()
		if !hasSource || !hasTarget {
			if hasSource != hasTarget {
				return fmt.Sprintf("第%d行存在性不同", rowNumber+1), nil
			}
			break
		}
		sourceRaw, err := scanRowValues(sourceRows, len(columns))
		if err != nil {
			return "", err
		}
		targetRaw, err := scanRowValues(targetRows, len(columns))
		if err != nil {
			return "", err
		}
		sourceValues, err := normalizeRowValues(sourceRaw, columns, location)
		if err != nil {
			return "", err
		}
		targetValues, err := normalizeRowValues(targetRaw, columns, location)
		if err != nil {
			return "", err
		}
		key := migrationRowKey(columnNames, primaryKeys, sourceValues)
		for index, column := range columns {
			if canonicalMigrationValue(sourceValues[index]) != canonicalMigrationValue(targetValues[index]) {
				return fmt.Sprintf("第%d行主键[%s]列%s不同（SQLite类型%T，MySQL类型%T）", rowNumber+1, key, column.Name, sourceRaw[index], targetRaw[index]), nil
			}
		}
		rowNumber++
	}
	if err := sourceRows.Err(); err != nil {
		return "", err
	}
	if err := targetRows.Err(); err != nil {
		return "", err
	}
	return "未定位到逐列差异", nil
}

func canonicalMigrationValue(value interface{}) string {
	if value == nil {
		return "<NULL>"
	}
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return fmt.Sprint(value)
}

func digestTable(ctx context.Context, db *gorm.DB, table string, columns []migrationColumn, primaryKeys []string, location *time.Location, quote rune) (int64, string, error) {
	columnNames := migrationColumnNames(columns)
	query := "SELECT " + quoteIdentifiers(columnNames, quote) + " FROM " + quoteIdentifier(table, quote)
	if len(primaryKeys) > 0 {
		query += " ORDER BY " + quoteIdentifiers(primaryKeys, quote)
	}
	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return 0, "", fmt.Errorf("读取表%s用于校验失败: %w", table, err)
	}
	defer rows.Close()
	hash := sha256.New()
	var count int64
	for rows.Next() {
		rawValues, err := scanRowValues(rows, len(columns))
		if err != nil {
			return 0, "", err
		}
		values, err := normalizeRowValues(rawValues, columns, location)
		if err != nil {
			return 0, "", fmt.Errorf("规范化表%s第%d行失败: %w", table, count+1, err)
		}
		writeCanonicalRow(hash, columns, values)
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return count, hex.EncodeToString(hash.Sum(nil)), nil
}

func loadMySQLColumns(db *gorm.DB, table string) ([]migrationColumn, error) {
	rows, err := db.Raw(`SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COALESCE(DATETIME_PRECISION, 0)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, table).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []migrationColumn
	for rows.Next() {
		var column migrationColumn
		var nullable string
		if err := rows.Scan(&column.Name, &column.DataType, &nullable, &column.DatetimePrecision); err != nil {
			return nil, err
		}
		column.Nullable = strings.EqualFold(nullable, "YES")
		columns = append(columns, column)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("MySQL目标缺少表%s", table)
	}
	return columns, rows.Err()
}

func loadMySQLPrimaryKeys(db *gorm.DB, table string) ([]string, error) {
	var keys []string
	err := db.Raw(`SELECT COLUMN_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = 'PRIMARY'
		ORDER BY SEQ_IN_INDEX`, table).Scan(&keys).Error
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("MySQL表%s缺少主键，无法稳定迁移", table)
	}
	return keys, nil
}

func ensureSQLiteColumns(db *gorm.DB, table string, columns []migrationColumn) error {
	rows, err := db.Raw("PRAGMA table_info(" + quoteIdentifier(table, '"') + ")").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	available := make(map[string]struct{})
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		available[name] = struct{}{}
	}
	for _, column := range columns {
		if _, ok := available[column.Name]; !ok {
			return fmt.Errorf("SQLite表%s缺少目标列%s", table, column.Name)
		}
	}
	return nil
}

func scanRowValues(rows *sql.Rows, count int) ([]interface{}, error) {
	values := make([]interface{}, count)
	destinations := make([]interface{}, count)
	for index := range values {
		destinations[index] = &values[index]
	}
	if err := rows.Scan(destinations...); err != nil {
		return nil, err
	}
	return values, nil
}

func normalizeRowValues(values []interface{}, columns []migrationColumn, location *time.Location) ([]interface{}, error) {
	normalized := make([]interface{}, len(values))
	for index, value := range values {
		converted, err := normalizeMigrationValue(value, columns[index], location)
		if err != nil {
			return nil, fmt.Errorf("列%s: %w", columns[index].Name, err)
		}
		normalized[index] = converted
	}
	return normalized, nil
}

func normalizeMigrationValue(value interface{}, column migrationColumn, location *time.Location) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	switch strings.ToLower(column.DataType) {
	case "datetime", "timestamp", "date":
		parsed, zero, err := parseMigrationTime(value, location)
		if err != nil {
			return nil, err
		}
		if zero {
			if column.Nullable {
				return nil, nil
			}
			return nil, errors.New("非空时间列不能写入零时间")
		}
		parsed = parsed.In(location)
		if parsed.Year() < 1000 || parsed.Year() > 9999 {
			return nil, fmt.Errorf("时间年份超出MySQL范围: %d", parsed.Year())
		}
		if strings.EqualFold(column.DataType, "date") {
			return parsed.Format("2006-01-02"), nil
		}
		precision := column.DatetimePrecision
		if precision < 0 {
			precision = 0
		}
		if precision > 6 {
			precision = 6
		}
		if precision == 0 {
			return parsed.Format("2006-01-02 15:04:05"), nil
		}
		fraction := fmt.Sprintf("%09d", parsed.Nanosecond())[:precision]
		return parsed.Format("2006-01-02 15:04:05") + "." + fraction, nil
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint":
		return normalizeIntegerValue(value)
	case "decimal":
		return normalizeDecimalValue(value)
	case "float", "double":
		return normalizeFloatingValue(value)
	case "char", "varchar", "text", "tinytext", "mediumtext", "longtext", "json", "enum", "set":
		if bytes, ok := value.([]byte); ok {
			return string(bytes), nil
		}
		return value, nil
	default:
		return value, nil
	}
}

func normalizeIntegerValue(value interface{}) (interface{}, error) {
	raw := strings.TrimSpace(migrationValueString(value))
	if raw == "" {
		return nil, errors.New("整数值为空")
	}
	if strings.HasPrefix(raw, "-") {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, err
		}
		return strconv.FormatInt(parsed, 10), nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, err
	}
	return strconv.FormatUint(parsed, 10), nil
}

func normalizeDecimalValue(value interface{}) (interface{}, error) {
	raw := strings.TrimSpace(migrationValueString(value))
	if raw == "" {
		return nil, errors.New("小数值为空")
	}
	if strings.ContainsAny(raw, "eE") {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, err
		}
		raw = strconv.FormatFloat(parsed, 'f', -1, 64)
	}
	if strings.Contains(raw, ".") {
		raw = strings.TrimRight(strings.TrimRight(raw, "0"), ".")
	}
	if raw == "" || raw == "-0" {
		raw = "0"
	}
	return raw, nil
}

func normalizeFloatingValue(value interface{}) (interface{}, error) {
	raw := strings.TrimSpace(migrationValueString(value))
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, err
	}
	return strconv.FormatFloat(parsed, 'g', -1, 64), nil
}

func migrationValueString(value interface{}) string {
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return fmt.Sprint(value)
}

func parseMigrationTime(value interface{}, location *time.Location) (time.Time, bool, error) {
	if parsed, ok := value.(time.Time); ok {
		return parsed, parsed.IsZero(), nil
	}
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		return time.Time{}, false, fmt.Errorf("不支持的时间类型%T", value)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "0000-00-00") || strings.HasPrefix(raw, "0001-01-01") {
		return time.Time{}, true, nil
	}
	withZone := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
	}
	for _, layout := range withZone {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, false, nil
		}
	}
	withoutZone := []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range withoutZone {
		if parsed, err := time.ParseInLocation(layout, raw, location); err == nil {
			return parsed, false, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("无法解析时间%s", raw)
}

func writeCanonicalRow(hash interface{ Write([]byte) (int, error) }, columns []migrationColumn, values []interface{}) {
	for index, value := range values {
		_, _ = hash.Write([]byte(columns[index].Name))
		_, _ = hash.Write([]byte{0})
		if value == nil {
			_, _ = hash.Write([]byte{0})
			continue
		}
		_, _ = hash.Write([]byte{1})
		var data []byte
		switch typed := value.(type) {
		case []byte:
			data = typed
		case time.Time:
			data = []byte(typed.Format(time.RFC3339Nano))
		default:
			data = []byte(fmt.Sprint(typed))
		}
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(data)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(data)
	}
}

func verifyTargetRelationships(db *gorm.DB) error {
	checks := []struct {
		name  string
		query string
	}{
		{"用户组引用", "SELECT COUNT(*) FROM user_info u LEFT JOIN groups g ON g.id = u.group_id WHERE g.id IS NULL"},
		{"用户组权限引用", "SELECT COUNT(*) FROM group_power gp LEFT JOIN groups g ON g.id = gp.group_id LEFT JOIN power p ON p.id = gp.power_id WHERE g.id IS NULL OR p.id IS NULL"},
		{"用户文件用户引用", "SELECT COUNT(*) FROM user_files uf LEFT JOIN user_info u ON u.id = uf.user_id WHERE u.id IS NULL"},
		{"用户文件实体引用", "SELECT COUNT(*) FROM user_files uf LEFT JOIN file_info f ON f.id = uf.file_id WHERE f.id IS NULL"},
		{"活动文件目录引用", "SELECT COUNT(*) FROM user_files uf LEFT JOIN virtual_directory d ON d.id = uf.directory_id AND d.user_id = uf.user_id WHERE uf.deleted_at IS NULL AND d.id IS NULL"},
		{"文件分片引用", "SELECT COUNT(*) FROM file_chunk c LEFT JOIN file_info f ON f.id = c.file_id WHERE f.id IS NULL"},
		{"回收站用户引用", "SELECT COUNT(*) FROM recycled r LEFT JOIN user_info u ON u.id = r.user_id WHERE u.id IS NULL"},
		{"文件标签引用", "SELECT COUNT(*) FROM user_file_tag t LEFT JOIN user_files uf ON uf.uf_id = t.uf_id AND uf.user_id = t.user_id LEFT JOIN tag_definition d ON d.id = t.tag_id WHERE uf.uf_id IS NULL OR d.id IS NULL"},
		{"目录标签引用", "SELECT COUNT(*) FROM user_directory_tag t LEFT JOIN virtual_directory d ON d.id = t.directory_id AND d.user_id = t.user_id LEFT JOIN tag_definition td ON td.id = t.tag_id WHERE d.id IS NULL OR td.id IS NULL"},
		{"订阅插件引用", "SELECT COUNT(*) FROM subscription s LEFT JOIN installed_plugin p ON p.id = s.plugin_id WHERE p.id IS NULL"},
		{"订阅条目引用", "SELECT COUNT(*) FROM subscription_item i LEFT JOIN subscription s ON s.id = i.subscription_id WHERE s.id IS NULL"},
	}
	for _, check := range checks {
		var count int64
		if err := db.Raw(check.query).Scan(&count).Error; err != nil {
			return fmt.Errorf("检查%s失败: %w", check.name, err)
		}
		if count != 0 {
			return fmt.Errorf("%s存在%d条悬空记录", check.name, count)
		}
	}
	return nil
}

func verifyAutoIncrementValues(db *gorm.DB) error {
	var tables []string
	if err := db.Raw(`SELECT DISTINCT TABLE_NAME FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND EXTRA LIKE '%auto_increment%' ORDER BY TABLE_NAME`).Scan(&tables).Error; err != nil {
		return err
	}
	for _, table := range tables {
		var column string
		if err := db.Raw(`SELECT COLUMN_NAME FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND EXTRA LIKE '%auto_increment%' LIMIT 1`, table).Scan(&column).Error; err != nil {
			return err
		}
		var maxID int64
		query := "SELECT COALESCE(MAX(" + quoteIdentifier(column, '`') + "), 0) FROM " + quoteIdentifier(table, '`')
		if err := db.Raw(query).Scan(&maxID).Error; err != nil {
			return err
		}
		var next sql.NullInt64
		if err := db.Raw(`SELECT AUTO_INCREMENT FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table).Scan(&next).Error; err != nil {
			return err
		}
		if !next.Valid || next.Int64 <= maxID {
			return fmt.Errorf("表%s的AUTO_INCREMENT无效: next=%v max=%d", table, next, maxID)
		}
	}
	return nil
}

func migrationColumnNames(columns []migrationColumn) []string {
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = column.Name
	}
	return names
}

func migrationRowKey(columns, primaryKeys []string, values []interface{}) string {
	indexes := make(map[string]int, len(columns))
	for index, column := range columns {
		indexes[column] = index
	}
	parts := make([]string, 0, len(primaryKeys))
	for _, key := range primaryKeys {
		parts = append(parts, key+"="+fmt.Sprint(values[indexes[key]]))
	}
	return strings.Join(parts, ",")
}

func quoteIdentifiers(names []string, quote rune) string {
	quoted := make([]string, len(names))
	for index, name := range names {
		quoted[index] = quoteIdentifier(name, quote)
	}
	return strings.Join(quoted, ", ")
}

func quoteIdentifier(name string, quote rune) string {
	quoted := string(quote)
	return quoted + strings.ReplaceAll(name, quoted, quoted+quoted) + quoted
}

func captureSQLiteFileStates(sourcePath string) map[string]fileState {
	states := make(map[string]fileState, 3)
	for _, path := range []string{sourcePath, sourcePath + "-wal", sourcePath + "-shm"} {
		info, err := os.Stat(path)
		if err != nil {
			states[path] = fileState{}
			continue
		}
		states[path] = fileState{Exists: true, Size: info.Size(), ModTime: info.ModTime()}
	}
	return states
}

func sameSQLiteFileStates(left, right map[string]fileState) bool {
	if len(left) != len(right) {
		return false
	}
	for path, leftState := range left {
		rightState, ok := right[path]
		if !ok || leftState.Exists != rightState.Exists || leftState.Size != rightState.Size || !leftState.ModTime.Equal(rightState.ModTime) {
			return false
		}
	}
	return true
}

func sameFilePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if strings.EqualFold(left, right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func emitMigrationProgress(callback func(MigrationProgress), progress MigrationProgress) {
	if callback != nil {
		callback(progress)
	}
}

func closeGormDB(db *gorm.DB) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
