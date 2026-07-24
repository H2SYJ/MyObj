package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestNormalizePluginThumbnailConvertsAndLimits(t *testing.T) {
	input := image.NewRGBA(image.Rect(0, 0, 1600, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 1600; x++ {
			input.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, input); err != nil {
		t.Fatal(err)
	}
	output, err := normalizePluginThumbnail(source.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(output) > pluginThumbnailMaxOutput {
		t.Fatalf("输出超过1 MiB: %d", len(output))
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(output))
	if err != nil || format != "jpeg" || config.Width > pluginThumbnailMaxSide || config.Height > pluginThumbnailMaxSide {
		t.Fatalf("输出格式或尺寸错误: format=%s size=%dx%d err=%v", format, config.Width, config.Height, err)
	}
}

func TestNormalizePluginThumbnailRejectsPixelBomb(t *testing.T) {
	_, err := normalizePluginThumbnail(pngHeader(7000, 6000))
	if err == nil {
		t.Fatal("超过4000万像素的图片未被拒绝")
	}
}

func TestRecoverInterruptedThumbnails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SubscriptionItem{}); err != nil {
		t.Fatal(err)
	}
	item := models.SubscriptionItem{ID: "item-1", SubscriptionID: "subscription-1", SourceGeneration: 1, ItemKey: "key-1", URL: "https://example.com/file", DownloadType: "http", SavePath: "/", Status: "submitted", ThumbnailURL: "https://example.com/cover.jpg", ThumbnailStatus: "processing", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	service := &SubscriptionService{factory: impl.NewRepositoryFactory(db)}
	service.recoverInterruptedThumbnails()
	if err := db.First(&item, "id = ?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.ThumbnailStatus != "retry_wait" || item.ThumbnailNextRetryAt == nil {
		t.Fatalf("中断缩略图任务未恢复: status=%s next=%v", item.ThumbnailStatus, item.ThumbnailNextRetryAt)
	}
}

func TestListRunnableThumbnailItemsJoinsFinishedDownloads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SubscriptionItem{}, &models.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	due := now.Add(-time.Second)
	future := now.Add(time.Minute)
	for _, task := range []models.DownloadTask{
		{ID: "finished-1", State: enum.DownloadTaskStateFinished.Value()},
		{ID: "finished-2", State: enum.DownloadTaskStateFinished.Value()},
		{ID: "downloading", State: enum.DownloadTaskStateDownloading.Value()},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	items := []models.SubscriptionItem{
		{ID: "waiting-finished", SubscriptionID: "s", SourceGeneration: 1, ItemKey: "a", URL: "https://example.com/a", DownloadType: "http", SavePath: "/", DownloadTaskID: "finished-1", Status: "submitted", ThumbnailURL: "https://example.com/a.jpg", ThumbnailStatus: "waiting_file", CreatedAt: now.Add(-4 * time.Minute), UpdatedAt: now.Add(-4 * time.Minute)},
		{ID: "retry-due", SubscriptionID: "s", SourceGeneration: 1, ItemKey: "b", URL: "https://example.com/b", DownloadType: "http", SavePath: "/", DownloadTaskID: "finished-2", Status: "submitted", ThumbnailURL: "https://example.com/b.jpg", ThumbnailStatus: "retry_wait", ThumbnailNextRetryAt: &due, CreatedAt: now.Add(-3 * time.Minute), UpdatedAt: now.Add(-3 * time.Minute)},
		{ID: "waiting-downloading", SubscriptionID: "s", SourceGeneration: 1, ItemKey: "c", URL: "https://example.com/c", DownloadType: "http", SavePath: "/", DownloadTaskID: "downloading", Status: "submitted", ThumbnailURL: "https://example.com/c.jpg", ThumbnailStatus: "waiting_file", CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)},
		{ID: "retry-future", SubscriptionID: "s", SourceGeneration: 1, ItemKey: "d", URL: "https://example.com/d", DownloadType: "http", SavePath: "/", DownloadTaskID: "finished-2", Status: "submitted", ThumbnailURL: "https://example.com/d.jpg", ThumbnailStatus: "retry_wait", ThumbnailNextRetryAt: &future, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	service := &SubscriptionService{factory: impl.NewRepositoryFactory(db)}
	candidates, err := service.listRunnableThumbnailItems(now, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].ID != "waiting-finished" || candidates[1].ID != "retry-due" {
		t.Fatalf("缩略图JOIN筛选结果不符合预期: %#v", candidates)
	}
	claimed, err := service.claimThumbnailItem("waiting-finished", now)
	if err != nil || !claimed {
		t.Fatalf("首次认领缩略图任务失败: claimed=%v err=%v", claimed, err)
	}
	claimed, err = service.claimThumbnailItem("waiting-finished", now)
	if err != nil || claimed {
		t.Fatalf("重复认领缩略图任务未被阻止: claimed=%v err=%v", claimed, err)
	}
	next, err := service.nextThumbnailRetryAt(now)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.Sub(future) > time.Millisecond || future.Sub(*next) > time.Millisecond {
		t.Fatalf("缩略图最早重试时间=%v，期望%v", next, future)
	}
}

func pngHeader(width, height uint32) []byte {
	output := append([]byte{}, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}...)
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8], data[9], data[10], data[11], data[12] = 8, 2, 0, 0, 0
	chunkType := []byte("IHDR")
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	output = append(output, length...)
	output = append(output, chunkType...)
	output = append(output, data...)
	crc := crc32.ChecksumIEEE(append(chunkType, data...))
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)
	return append(output, crcBytes...)
}
