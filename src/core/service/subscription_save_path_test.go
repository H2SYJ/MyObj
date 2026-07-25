package service

import (
	"context"
	"myobj/src/config"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	subscription := &models.Subscription{ID: "subscription", UserID: "user", SavePath: "/保存目录"}
	manifest := &pluginpkg.Manifest{}

	for index := range items {
		pluginItem := pluginpkg.DownloadableItem{URL: items[index].URL, DownloadType: items[index].DownloadType, RelativeSavePath: "频道"}
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

func TestRefreshExistingItemStoresLatestSignedURLWithoutInterruptingDownloadingTask(t *testing.T) {
	previousConfig := config.CONFIG
	config.CONFIG = &config.MyObjConfig{Auth: config.Auth{Secret: "subscription-url-refresh-test-secret"}}
	t.Cleanup(func() { config.CONFIG = previousConfig })

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SubscriptionItem{}, &models.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	requestCount := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		requestCount++
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = writer.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:4,\nsegment.ts?token=new\n#EXT-X-ENDLIST\n"))
	}))
	t.Cleanup(proxy.Close)
	policy := download.NewNetworkPolicy()
	if err := policy.Apply(download.NetworkSettings{ProxyURL: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	downloadService := &DownloadService{factory: factory, tempDir: t.TempDir(), networkPolicy: policy}
	downloadService.manager = NewDownloadManager(factory, downloadService.tempDir, policy)
	service := &SubscriptionService{factory: factory, downloadService: downloadService}

	now := time.Now()
	task := &models.DownloadTask{ID: "task", UserID: "user", Type: enum.DownloadTaskTypeHLS.Value(),
		URL: "http://example.com/video.m3u8?token=old", FileName: "video.mp4", State: enum.DownloadTaskStateDownloading.Value()}
	item := &models.SubscriptionItem{ID: "item", SubscriptionID: "subscription", SourceGeneration: 1, ItemKey: "stable",
		URL: task.URL, DownloadType: "hls", SavePath: "/离线下载", DownloadTaskID: task.ID, Status: "submitted",
		ThumbnailStatus: "none", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	pluginItem := pluginpkg.DownloadableItem{URL: "http://example.com/video.m3u8?token=new", DownloadType: "hls"}
	subscription := &models.Subscription{ID: "subscription", UserID: "user", SavePath: "/离线下载"}
	if err := service.refreshExistingItem(context.Background(), subscription, &pluginpkg.Manifest{}, item, pluginItem, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	var updatedItem models.SubscriptionItem
	if err := db.First(&updatedItem, "id = ?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedItem.URL != pluginItem.URL {
		t.Fatalf("订阅条目未保存最新签名URL: %s", updatedItem.URL)
	}
	var unchangedTask models.DownloadTask
	if err := db.First(&unchangedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchangedTask.URL != task.URL || unchangedTask.State != enum.DownloadTaskStateDownloading.Value() {
		t.Fatalf("下载中的任务不应被刷新打断: %#v", unchangedTask)
	}
	if requestCount != 0 {
		t.Fatalf("下载中的任务不应提前抓取新快照，请求数=%d", requestCount)
	}

	if err := db.Model(&models.DownloadTask{}).Where("id = ?", task.ID).
		Update("state", enum.DownloadTaskStateFailed.Value()).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.refreshExistingItem(context.Background(), subscription, &pluginpkg.Manifest{}, &updatedItem, pluginItem, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	var refreshedTask models.DownloadTask
	if err := db.First(&refreshedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshedTask.URL != pluginItem.URL || refreshedTask.State != enum.DownloadTaskStateInit.Value() {
		t.Fatalf("失败任务未使用条目中已保存的签名URL重新排队: %#v", refreshedTask)
	}
	if requestCount != 1 {
		t.Fatalf("失败任务应抓取一次新快照，请求数=%d", requestCount)
	}
	if !download.HasHLSSnapshot(downloadService.tempDir, task.ID) {
		t.Fatal("失败任务刷新后缺少HLS快照")
	}

	if err := db.Model(&models.DownloadTask{}).Where("id = ?", task.ID).
		Update("state", enum.DownloadTaskStateFailed.Value()).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(downloadService.tempDir, "hls_"+task.ID)); err != nil {
		t.Fatal(err)
	}
	if err := service.refreshExistingItem(context.Background(), subscription, &pluginpkg.Manifest{}, &updatedItem, pluginItem, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&refreshedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshedTask.State != enum.DownloadTaskStateInit.Value() || requestCount != 2 ||
		!download.HasHLSSnapshot(downloadService.tempDir, task.ID) {
		t.Fatalf("URL未变化的失败任务未重建快照并重新排队: task=%#v requests=%d", refreshedTask, requestCount)
	}
}
