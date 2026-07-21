package service

import (
	"context"
	"myobj/src/config"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRefreshTaskHeadersDoesNotResumeUserPausedOrFailedTask(t *testing.T) {
	previous := config.CONFIG
	config.CONFIG = &config.MyObjConfig{Auth: config.Auth{Secret: "refresh-header-test-secret"}}
	t.Cleanup(func() { config.CONFIG = previous })

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	service := &DownloadService{factory: impl.NewRepositoryFactory(db)}

	for _, state := range []int{enum.DownloadTaskStatePaused.Value(), enum.DownloadTaskStateFailed.Value()} {
		taskID := "task-" + string(rune('0'+state))
		itemID := "item-" + string(rune('0'+state))
		task := models.DownloadTask{ID: taskID, UserID: "user-1", Type: enum.DownloadTaskTypeHttp.Value(), State: state, RequiresHeaders: false}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		itemEncrypted, err := download.EncryptRequestHeaders(config.CONFIG.Auth.Secret, itemID, task.UserID, map[string]string{"Authorization": "Bearer updated"})
		if err != nil {
			t.Fatal(err)
		}
		hostsJSON, err := download.EncodeHeaderHosts([]string{"example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.RefreshTaskHeaders(context.Background(), task.ID, itemID, task.UserID, itemEncrypted, hostsJSON); err != nil {
			t.Fatal(err)
		}
		var updated models.DownloadTask
		if err := db.First(&updated, "id = ?", task.ID).Error; err != nil {
			t.Fatal(err)
		}
		if updated.State != state {
			t.Fatalf("任务状态被意外改变: want=%d got=%d", state, updated.State)
		}
		headers, err := download.DecryptRequestHeaders(config.CONFIG.Auth.Secret, task.ID, task.UserID, updated.RequestHeadersEncrypted)
		if err != nil || headers["Authorization"] != "Bearer updated" {
			t.Fatalf("任务凭据未更新: headers=%v err=%v", headers, err)
		}
	}
}
