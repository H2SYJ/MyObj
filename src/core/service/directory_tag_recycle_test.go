package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

type directoryRecycleTestUserFile struct {
	UserID      string     `gorm:"column:user_id"`
	FileID      string     `gorm:"column:file_id"`
	FileName    string     `gorm:"column:file_name"`
	DirectoryID int        `gorm:"column:directory_id"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	UFID        string     `gorm:"column:uf_id"`
}

func (directoryRecycleTestUserFile) TableName() string { return "user_files" }

func TestDirectoryTagsSurviveRecycleAndRestoreIDRemap(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.VirtualDirectory{}, &directoryRecycleTestUserFile{}, &models.FileInfo{},
		&models.Recycled{}, &models.RecycledDirectoryNode{}, &models.RecycledDirectoryFile{}, &models.RecycledDirectoryTag{},
		&models.UserDirectoryTag{},
	); err != nil {
		t.Fatal(err)
	}
	root := models.VirtualDirectory{ID: 1, UserID: "user-1", Name: "home", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	child := models.VirtualDirectory{ID: 2, UserID: "user-1", Name: "影视库", ParentID: 1, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	if err := db.Create(&[]models.VirtualDirectory{root, child}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserDirectoryTag{
		ID: uuid.NewString(), UserID: "user-1", DirectoryID: child.ID, TagID: "cinema",
		CreatedBy: "user-1", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	if _, err := recycleDirectoryTree(context.Background(), factory, db, "user-1", child.ID); err != nil {
		t.Fatal(err)
	}
	var activeCount, snapshotCount int64
	if err := db.Model(&models.UserDirectoryTag{}).Count(&activeCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.RecycledDirectoryTag{}).Count(&snapshotCount).Error; err != nil {
		t.Fatal(err)
	}
	if activeCount != 0 || snapshotCount != 1 {
		t.Fatalf("目录进入回收站后标签状态异常: active=%d snapshot=%d", activeCount, snapshotCount)
	}
	occupier := models.VirtualDirectory{UserID: "user-1", Name: "临时目录", ParentID: root.ID, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	if err := db.Create(&occupier).Error; err != nil {
		t.Fatal(err)
	}
	var recycled models.Recycled
	if err := db.Where("user_id = ? AND item_type = ?", "user-1", models.RecycledItemTypeFolder).First(&recycled).Error; err != nil {
		t.Fatal(err)
	}
	recycledService := NewRecycledService(factory, nil)
	if _, err := recycledService.restoreDirectory(context.Background(), &recycled); err != nil {
		t.Fatal(err)
	}
	var restored models.VirtualDirectory
	if err := db.Where("user_id = ? AND parent_id = ? AND name = ?", "user-1", root.ID, child.Name).First(&restored).Error; err != nil {
		t.Fatal(err)
	}
	var restoredTag models.UserDirectoryTag
	if err := db.Where("user_id = ? AND directory_id = ?", "user-1", restored.ID).First(&restoredTag).Error; err != nil {
		t.Fatal(err)
	}
	if restored.ID == child.ID || restoredTag.TagID != "cinema" {
		t.Fatalf("恢复目录未按新ID重建标签: directory=%+v tag=%+v", restored, restoredTag)
	}
	if _, err := recycleDirectoryTree(context.Background(), factory, db, "user-1", restored.ID); err != nil {
		t.Fatal(err)
	}
	var recycledAgain models.Recycled
	if err := db.Where("user_id = ? AND item_type = ?", "user-1", models.RecycledItemTypeFolder).First(&recycledAgain).Error; err != nil {
		t.Fatal(err)
	}
	if err := recycledService.deleteDirectoryRecycled(context.Background(), &recycledAgain); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.RecycledDirectoryTag{}).Where("recycled_id = ?", recycledAgain.ID).Count(&snapshotCount).Error; err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 0 {
		t.Fatalf("永久删除后仍残留目录标签快照: count=%d", snapshotCount)
	}
}
