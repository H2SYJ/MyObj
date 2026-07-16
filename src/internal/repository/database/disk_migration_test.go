package database

import (
	"myobj/src/pkg/models"
	"myobj/src/pkg/util"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateLegacyDiskSizes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Disk{}); err != nil {
		t.Fatalf("创建磁盘表失败: %v", err)
	}

	legacyDisk := &models.Disk{ID: "legacy", Size: 447, DiskPath: "legacy", DataPath: "legacy"}
	byteDisk := &models.Disk{ID: "bytes", Size: 8 * util.DiskByte, DiskPath: "bytes", DataPath: "bytes"}
	if err := db.Create([]*models.Disk{legacyDisk, byteDisk}).Error; err != nil {
		t.Fatalf("写入测试数据失败: %v", err)
	}

	migrated, err := migrateLegacyDiskSizes(db)
	if err != nil {
		t.Fatalf("迁移磁盘容量失败: %v", err)
	}
	if migrated != 1 {
		t.Fatalf("应迁移1条旧数据，实际迁移%d条", migrated)
	}

	var disks []models.Disk
	if err := db.Order("id").Find(&disks).Error; err != nil {
		t.Fatalf("读取迁移结果失败: %v", err)
	}
	if disks[0].Size != 8*util.DiskByte {
		t.Fatalf("字节数据不应重复转换，实际为%d", disks[0].Size)
	}
	if disks[1].Size != 447*util.DiskByte {
		t.Fatalf("旧GB数据转换错误，实际为%d", disks[1].Size)
	}

	migrated, err = migrateLegacyDiskSizes(db)
	if err != nil {
		t.Fatalf("重复执行迁移失败: %v", err)
	}
	if migrated != 0 {
		t.Fatalf("重复执行迁移应保持幂等，实际又迁移%d条", migrated)
	}
}

func TestMigrateLegacyDiskSizesWithoutTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	migrated, err := migrateLegacyDiskSizes(db)
	if err != nil {
		t.Fatalf("磁盘表不存在时不应报错: %v", err)
	}
	if migrated != 0 {
		t.Fatalf("磁盘表不存在时迁移数应为0，实际为%d", migrated)
	}
}
