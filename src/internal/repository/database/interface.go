package database

import (
	"fmt"
	"myobj/src/config"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/util"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// databasePool 全局数据库连接池实例
var databasePool *gorm.DB

// SQL 数据库接口定义
// 所有数据库实现(MySQL/SQLite)都需要实现此接口
type SQL interface {
	// GetDB 获取数据库连接实例
	GetDB() *gorm.DB
	// Ping 测试数据库连接是否可用
	Ping() error
	// InitDatabase 初始化数据库连接
	InitDatabase()
}

// InitDataBase 初始化数据库连接
// 根据配置文件中的数据库类型(mysql/sqlite)选择对应的数据库驱动进行初始化
func InitDataBase() {
	dbType := config.CONFIG.Database.Type
	logger.LOG.Info("[数据库] 开始初始化数据库连接", "type", dbType)

	switch dbType {
	case "mysql":
		initMySQL()
	case "sqlite":
		initSQLite()
	default:
		logger.LOG.Error("[数据库] 不支持的数据库类型", "type", dbType)
		panic(fmt.Sprintf("不支持的数据库类型: %s", dbType))
	}
	migratedDisks, err := migrateCurrentSchema(databasePool)
	if err != nil {
		logger.LOG.Error("迁移数据库结构失败", "error", err)
		panic(fmt.Sprintf("迁移数据库结构失败: %v", err))
	}
	if migratedDisks > 0 {
		logger.LOG.Info("磁盘容量单位迁移完成", "count", migratedDisks)
	}

	logger.LOG.Info("[数据库] 数据库连接池初始化成功 ✓")
}

// migrateCurrentSchema 在已建立的连接上执行当前版本的增量迁移。
// 服务启动和 SQLite→MySQL 迁移 CLI 共用此入口，避免两套迁移顺序发生偏差。
func migrateCurrentSchema(db *gorm.DB) (int64, error) {
	if err := autoMigrateMissingModels(db); err != nil {
		return 0, fmt.Errorf("补齐缺失数据表失败: %w", err)
	}
	if err := migrateVirtualDirectorySchema(db); err != nil {
		return 0, fmt.Errorf("迁移虚拟目录结构失败: %w", err)
	}
	if err := migrateUserFileSearchIndexes(db); err != nil {
		return 0, fmt.Errorf("迁移文件搜索索引失败: %w", err)
	}
	migratedDisks, err := migrateLegacyDiskSizes(db)
	if err != nil {
		return 0, fmt.Errorf("迁移磁盘容量单位失败: %w", err)
	}
	if err := migrateUploadTaskSchema(db); err != nil {
		return 0, fmt.Errorf("迁移上传任务表失败: %w", err)
	}
	if err := migrateDownloadTaskSchema(db); err != nil {
		return 0, fmt.Errorf("迁移下载任务表失败: %w", err)
	}
	if err := migrateSubscriptionSchema(db); err != nil {
		return 0, fmt.Errorf("迁移插件订阅表失败: %w", err)
	}
	if err := migrateRecycledSchema(db); err != nil {
		return 0, fmt.Errorf("迁移回收站目录结构失败: %w", err)
	}
	if err := migrateTaggingSchema(db); err != nil {
		return 0, fmt.Errorf("迁移文件标签结构失败: %w", err)
	}
	if err := migrateLegacyGroupDefaults(db); err != nil {
		return 0, fmt.Errorf("迁移用户组默认标志失败: %w", err)
	}
	if err := migrateDefaultAccessData(db); err != nil {
		return 0, fmt.Errorf("迁移默认用户组和权限失败: %w", err)
	}
	if err := migrateLegacyGroupPowerDuplicates(db); err != nil {
		return 0, fmt.Errorf("清理重复用户组权限失败: %w", err)
	}
	return migratedDisks, nil
}

// migrateLegacyGroupDefaults 将旧 SQLite 宽松结构中的空标志还原为 Go 模型的零值。
// group_default 为 0 表示非默认组，已有的默认组标志 1 保持不变。
func migrateLegacyGroupDefaults(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Group{}) || !db.Migrator().HasColumn(&models.Group{}, "GroupDefault") {
		return nil
	}
	return db.Model(&models.Group{}).
		Where("group_default IS NULL").
		Update("group_default", 0).Error
}

// migrateLegacyGroupPowerDuplicates 清理旧 SQLite 表缺少联合唯一约束时产生的重复授权。
// 组权限是集合关系，同一 group_id、power_id 只保留一条不会损失业务信息。
func migrateLegacyGroupPowerDuplicates(db *gorm.DB) error {
	if db.Dialector.Name() != "sqlite" || !db.Migrator().HasTable(&models.GroupPower{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&models.GroupPower{}, "GroupID") ||
		!db.Migrator().HasColumn(&models.GroupPower{}, "PowerID") {
		return nil
	}
	return db.Exec(`DELETE FROM group_power
		WHERE rowid NOT IN (
			SELECT MIN(rowid) FROM group_power GROUP BY group_id, power_id
		)`).Error
}

// migrateUserFileSearchIndexes 为搜索最先使用的用户、公开状态和目录范围补齐复合索引。
// 文件名仍保留包含匹配语义，先通过这些等值条件缩小需要扫描的活跃文件范围。
func migrateUserFileSearchIndexes(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.UserFiles{}) {
		return nil
	}
	for _, index := range []string{
		"idx_user_files_user_active",
		"idx_user_files_public_active",
		"idx_user_files_user_directory_active",
	} {
		if db.Migrator().HasIndex(&models.UserFiles{}, index) {
			continue
		}
		if err := db.Migrator().CreateIndex(&models.UserFiles{}, index); err != nil {
			return fmt.Errorf("创建文件搜索索引%s失败: %w", index, err)
		}
	}
	return nil
}

// migrateRecycledSchema 补齐整目录回收所需字段和关联表，旧文件记录保持兼容。
func migrateRecycledSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.Recycled{},
		&models.RecycledDirectoryNode{},
		&models.RecycledDirectoryFile{},
		&models.RecycledDirectoryTag{},
	); err != nil {
		return err
	}
	return db.Model(&models.Recycled{}).
		Where("item_type IS NULL OR item_type = ''").
		Updates(map[string]interface{}{
			"item_type":  models.RecycledItemTypeFile,
			"item_count": 1,
		}).Error
}

func migrateSubscriptionSchema(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.InstalledPlugin{},
		&models.Subscription{},
		&models.SubscriptionRun{},
		&models.SubscriptionItem{},
		&models.PluginAuditLog{},
	)
}

// migrateDownloadTaskSchema 为可靠下载调度补齐字段和索引。
func migrateDownloadTaskSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.DownloadTask{}) {
		return nil
	}
	columns := []string{
		"BatchID", "RunToken", "WorkerID", "LeaseExpiresAt",
		"RetryCount", "NextRetryAt", "ReservedSize",
		"RequestHeadersEncrypted", "HeaderHostsJSON", "RequiresHeaders",
	}
	for _, column := range columns {
		if !db.Migrator().HasColumn(&models.DownloadTask{}, column) {
			if err := db.Migrator().AddColumn(&models.DownloadTask{}, column); err != nil {
				return fmt.Errorf("新增下载任务字段%s失败: %w", column, err)
			}
		}
	}
	indexes := []string{
		"idx_download_batch_id",
		"idx_download_run_token",
		"idx_download_lease_expires",
		"idx_download_next_retry",
		"idx_download_user_type_state_create",
		"idx_download_schedule",
	}
	for _, index := range indexes {
		if !db.Migrator().HasIndex(&models.DownloadTask{}, index) {
			if err := db.Migrator().CreateIndex(&models.DownloadTask{}, index); err != nil {
				return fmt.Errorf("新增下载任务索引%s失败: %w", index, err)
			}
		}
	}
	return nil
}

// migrateUploadTaskSchema 为异步文件处理补齐字段。AutoMigrate 只增加缺失列，兼容 SQLite 和 MySQL。
func migrateUploadTaskSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.UploadTask{}) {
		return nil
	}
	return db.AutoMigrate(&models.UploadTask{})
}

// migrateLegacyDiskSizes 将旧版本按GB保存的磁盘容量转换为字节。
// 管理端历史输入上限为999999GB，因此小于等于该值的正数可判定为旧数据。
func migrateLegacyDiskSizes(db *gorm.DB) (int64, error) {
	const maxLegacyDiskSizeGB int64 = 999999
	if !db.Migrator().HasTable(&models.Disk{}) {
		return 0, nil
	}
	result := db.Model(&models.Disk{}).
		Where("size > 0 AND size <= ?", maxLegacyDiskSizeGB).
		Update("size", gorm.Expr("size * ?", util.DiskByte))
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// initMySQL 初始化MySQL数据库连接
func initMySQL() {
	logger.LOG.Info("[数据库] 正在连接MySQL数据库...",
		"host", config.CONFIG.Database.Host,
		"port", config.CONFIG.Database.Port,
		"database", config.CONFIG.Database.DBName)

	mysql := new(Mysql)
	mysql.InitDatabase()

	if err := mysql.Ping(); err != nil {
		logger.LOG.Error("[数据库] MySQL连接测试失败", "error", err)
		panic(fmt.Sprintf("MySQL数据库连接失败: %v", err))
	}

	databasePool = mysql.GetDB()
	logger.LOG.Info("[数据库] MySQL连接成功")
}

// initSQLite 初始化SQLite数据库连接
func initSQLite() {
	logger.LOG.Info("[数据库] 正在连接SQLite数据库...", "path", config.CONFIG.Database.Host)

	sqlite := new(SQLite)
	sqlite.InitDatabase()

	if err := sqlite.Ping(); err != nil {
		logger.LOG.Error("[数据库] SQLite连接测试失败", "error", err)
		panic(fmt.Sprintf("SQLite数据库连接失败: %v", err))
	}

	databasePool = sqlite.GetDB()
	logger.LOG.Info("[数据库] SQLite连接成功")
}

// GetDB 获取全局数据库连接池实例
// 返回已初始化的GORM数据库连接对象
func GetDB() *gorm.DB {
	return databasePool
}

// logLevel 将日志级别字符串转换为GORM日志级别
// 根据应用配置的日志级别返回对应的GORM日志级别
func logLevel(level string) gormlogger.LogLevel {
	switch level {
	case "debug":
		return gormlogger.Info // debug模式下显示SQL详细信息
	case "error":
		return gormlogger.Error
	case "warn":
		return gormlogger.Warn
	default:
		return gormlogger.Info
	}
}
