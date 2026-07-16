package upload

import (
	"context"
	"math"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/models"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSelectBestDiskUsesRealAvailableSpace(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Disk{}); err != nil {
		t.Fatalf("创建磁盘表失败: %v", err)
	}

	disk := &models.Disk{
		ID:       "disk-1",
		Size:     1,
		DiskPath: t.TempDir(),
		DataPath: t.TempDir(),
	}
	if err := db.Create(disk).Error; err != nil {
		t.Fatalf("写入磁盘数据失败: %v", err)
	}
	factory := impl.NewRepositoryFactory(db)

	selected, err := SelectBestDisk(context.Background(), factory, 1)
	if err != nil {
		t.Fatalf("真实磁盘空间充足时不应失败: %v", err)
	}
	if selected.ID != disk.ID {
		t.Fatalf("选中的磁盘错误: got=%s want=%s", selected.ID, disk.ID)
	}

	_, err = SelectBestDisk(context.Background(), factory, math.MaxInt64)
	if err == nil || !strings.Contains(err.Error(), "没有足够空间的磁盘") {
		t.Fatalf("空间不足时应返回明确错误，实际为%v", err)
	}
}

func TestSelectBestDiskRejectsNegativeFileSize(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	_, err = SelectBestDisk(context.Background(), impl.NewRepositoryFactory(db), -1)
	if err == nil || !strings.Contains(err.Error(), "文件大小不能为负数") {
		t.Fatalf("负数文件大小应返回明确错误，实际为%v", err)
	}
}
