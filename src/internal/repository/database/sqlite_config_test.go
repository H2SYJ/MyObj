package database

import (
	"io"
	"log/slog"
	"myobj/src/config"
	"myobj/src/pkg/logger"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteEnablesWALBusyTimeoutAndImmediateTransactions(t *testing.T) {
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

	firstTx := sqliteDatabase.database.Begin()
	if firstTx.Error != nil {
		t.Fatalf("开启第一个事务失败: %v", firstTx.Error)
	}
	defer firstTx.Rollback()

	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondTx := sqliteDatabase.database.Begin()
		if secondTx.Error != nil {
			secondResult <- secondTx.Error
			return
		}
		secondResult <- secondTx.Rollback().Error
	}()
	<-secondStarted

	select {
	case err := <-secondResult:
		t.Fatalf("第二个写事务未等待第一个事务结束: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := firstTx.Commit().Error; err != nil {
		t.Fatalf("提交第一个事务失败: %v", err)
	}
	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatalf("第一个事务提交后开启第二个事务失败: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("第一个事务提交后第二个事务仍未继续")
	}
}
