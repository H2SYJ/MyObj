package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

type tagLifecycleTestUserFile struct {
	UserID      string     `gorm:"column:user_id"`
	FileID      string     `gorm:"column:file_id"`
	FileName    string     `gorm:"column:file_name"`
	DirectoryID int        `gorm:"column:directory_id"`
	IsPublic    bool       `gorm:"column:public"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	UFID        string     `gorm:"column:uf_id"`
}

func (tagLifecycleTestUserFile) TableName() string { return "user_files" }

func TestCreateAndRollbackUserFileKeepsTagStateAtomic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&tagLifecycleTestUserFile{}, &models.UserFileTagState{}, &models.UserFileTag{},
		&models.UserFileTagExclusion{}, &models.TagRebuildFailure{},
	); err != nil {
		t.Fatal(err)
	}
	fileService := &FileService{factory: impl.NewRepositoryFactory(db)}
	userFile := &models.UserFiles{
		UserID: "user-1", FileID: "file-1", UfID: "uf-1", FileName: "测试.mp4",
		DirectoryID: 1, CreatedAt: custom_type.Now(),
	}
	if err := fileService.createUserFileWithTagState(context.Background(), userFile); err != nil {
		t.Fatal(err)
	}
	var state models.UserFileTagState
	if err := db.First(&state, "uf_id = ?", userFile.UfID).Error; err != nil {
		t.Fatal(err)
	}
	if state.Status != models.TagStatePending || state.UserID != userFile.UserID {
		t.Fatalf("秒传文件没有在同一事务创建标签任务: %+v", state)
	}
	if err := fileService.rollbackUserFileWithTagState(context.Background(), userFile.UserID, userFile.UfID); err != nil {
		t.Fatal(err)
	}
	var userFileCount, stateCount int64
	if err := db.Unscoped().Model(&tagLifecycleTestUserFile{}).Where("uf_id = ?", userFile.UfID).Count(&userFileCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.UserFileTagState{}).Where("uf_id = ?", userFile.UfID).Count(&stateCount).Error; err != nil {
		t.Fatal(err)
	}
	if userFileCount != 0 || stateCount != 0 {
		t.Fatalf("秒传回滚遗留用户文件或标签状态: user_files=%d tag_state=%d", userFileCount, stateCount)
	}
}
