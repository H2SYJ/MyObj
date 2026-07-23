package database

import (
	"myobj/src/config"
	"myobj/src/pkg/logger"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type SQLite struct {
	database *gorm.DB
}

func (sql *SQLite) InitDatabase() {
	host := config.CONFIG.Database.Host
	separator := "?"
	if strings.Contains(host, "?") {
		separator = "&"
	}
	// 写事务从开始时就申请写锁，避免 WAL 模式下读事务升级为写事务时触发
	// SQLITE_BUSY_SNAPSHOT（database is locked (517)）。其他写事务会按
	// busy_timeout 排队，普通读查询仍可并发执行。
	host += separator + "_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := gorm.Open(sqlite.Open(host), &gorm.Config{
		Logger: &GormSlogAdapter{
			level: logLevel(config.CONFIG.Log.Level),
		},
	})
	if err != nil {
		logger.LOG.Error("failed to connect database", "err", err)
		panic("failed to connect database")
	}
	sqlDB, dbErr := db.DB()
	if dbErr != nil {
		logger.LOG.Error("failed to configure sqlite connection pool", "err", dbErr)
		panic("failed to configure sqlite connection pool")
	}
	if config.CONFIG.Database.MaxOpen > 0 {
		sqlDB.SetMaxOpenConns(config.CONFIG.Database.MaxOpen)
	}
	if config.CONFIG.Database.MaxIdle > 0 {
		sqlDB.SetMaxIdleConns(config.CONFIG.Database.MaxIdle)
	}
	sql.database = db
}
func (sql *SQLite) GetDB() *gorm.DB {
	return sql.database
}
func (sql *SQLite) Ping() error {
	sqlDB, err := sql.database.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
