package database

import (
	"io"
	"log/slog"
	"myobj/src/config"
	"myobj/src/pkg/logger"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteEnablesWALAndBusyTimeout(t *testing.T) {
	previousConfig := config.CONFIG
	previousLogger := logger.LOG
	defer func() {
		config.CONFIG = previousConfig
		logger.LOG = previousLogger
	}()
	logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	config.CONFIG = &config.MyObjConfig{
		Database: config.Database{
			Host:    filepath.Join(t.TempDir(), "test.db"),
			MaxOpen: 2,
			MaxIdle: 1,
		},
		Log: config.Log{Level: "error"},
	}

	sqliteDatabase := new(SQLite)
	sqliteDatabase.InitDatabase()
	defer func() {
		sqlDB, _ := sqliteDatabase.database.DB()
		_ = sqlDB.Close()
	}()

	var journalMode string
	if err := sqliteDatabase.database.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("SQLite日志模式不是WAL: %s", journalMode)
	}
	var busyTimeout int
	if err := sqliteDatabase.database.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("SQLite busy_timeout错误: %d", busyTimeout)
	}
}
