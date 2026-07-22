package service

import (
	"context"
	"myobj/src/config"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRefreshExistingItemRebasesOnlyRetryableUnsubmittedItem(t *testing.T) {
	previousConfig := config.CONFIG
	config.CONFIG = &config.MyObjConfig{Auth: config.Auth{Secret: "subscription-save-path-test-secret"}}
	t.Cleanup(func() { config.CONFIG = previousConfig })

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SubscriptionItem{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	items := []models.SubscriptionItem{
		{ID: "deferred", SubscriptionID: "subscription", SourceGeneration: 1, ItemKey: "deferred", URL: "https://example.com/deferred", DownloadType: "http", SavePath: "/旧目录", Status: "deferred", ThumbnailStatus: "none", CreatedAt: now, UpdatedAt: now},
		{ID: "submitted", SubscriptionID: "subscription", SourceGeneration: 1, ItemKey: "submitted", URL: "https://example.com/submitted", DownloadType: "http", SavePath: "/旧目录", Status: "submitted", ThumbnailStatus: "none", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	service := &SubscriptionService{factory: impl.NewRepositoryFactory(db)}
	subscription := &models.Subscription{ID: "subscription", UserID: "user", DefaultPath: "/保存目录"}
	manifest := &pluginpkg.Manifest{}

	for index := range items {
		pluginItem := pluginpkg.DownloadableItem{URL: items[index].URL, DownloadType: items[index].DownloadType, SavePath: "/频道"}
		if err := service.refreshExistingItem(context.Background(), subscription, manifest, &items[index], pluginItem, map[string]bool{}); err != nil {
			t.Fatal(err)
		}
	}

	var deferred models.SubscriptionItem
	if err := db.First(&deferred, "id = ?", "deferred").Error; err != nil {
		t.Fatal(err)
	}
	if deferred.SavePath != "/保存目录/频道" {
		t.Fatalf("未提交条目未按新保存目录刷新: %q", deferred.SavePath)
	}
	var submitted models.SubscriptionItem
	if err := db.First(&submitted, "id = ?", "submitted").Error; err != nil {
		t.Fatal(err)
	}
	if submitted.SavePath != "/旧目录" {
		t.Fatalf("已提交条目的保存目录不应改变: %q", submitted.SavePath)
	}
}
