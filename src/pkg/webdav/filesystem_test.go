package webdav

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
)

func newDirectoryTestFileSystem(t *testing.T) *MyObjFileSystem {
	t.Helper()
	logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.VirtualDirectory{}); err != nil {
		t.Fatal(err)
	}
	now := custom_type.Now()
	if err := db.Create(&models.VirtualDirectory{ID: 1, UserID: "user-a", Name: "", ParentID: 0, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	return NewMyObjFileSystem(&models.UserInfo{ID: "user-a"}, factory).(*MyObjFileSystem)
}

func TestWebDAVNestedDirectoryCreateRenameMoveAndDelete(t *testing.T) {
	fs := newDirectoryTestFileSystem(t)
	ctx := context.Background()
	if err := fs.Mkdir(ctx, "/频道", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(ctx, "/频道/2026", 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.resolveDirectory(ctx, "/频道/2026"); err != nil {
		t.Fatalf("多级目录解析失败: %v", err)
	}
	if err := fs.Rename(ctx, "/频道/2026", "/归档"); err != nil {
		t.Fatalf("目录移动并重命名失败: %v", err)
	}
	if _, err := fs.Stat(ctx, "/归档"); err != nil {
		t.Fatalf("移动后的目录不存在: %v", err)
	}
	if err := fs.RemoveAll(ctx, "/频道"); err != nil {
		t.Fatalf("删除空目录失败: %v", err)
	}
	if _, err := fs.Stat(ctx, "/频道"); !os.IsNotExist(err) {
		t.Fatalf("目录删除后仍可访问: %v", err)
	}
}

func TestWebDAVRejectsMovingDirectoryIntoDescendant(t *testing.T) {
	fs := newDirectoryTestFileSystem(t)
	ctx := context.Background()
	if err := fs.Mkdir(ctx, "/频道", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(ctx, "/频道/2026", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rename(ctx, "/频道", "/频道/2026/频道"); err == nil {
		t.Fatal("将目录移动到其子目录应失败")
	}
}
