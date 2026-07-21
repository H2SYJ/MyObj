package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	pluginThumbnailMaxInput  = int64(5 * 1024 * 1024)
	pluginThumbnailMaxPixels = int64(40_000_000)
	pluginThumbnailMaxOutput = 1024 * 1024
	pluginThumbnailMaxSide   = 1000
)

type thumbnailDownloadError struct {
	err       error
	transient bool
}

func (e *thumbnailDownloadError) Error() string { return e.err.Error() }

func (s *SubscriptionService) recoverInterruptedThumbnails() {
	now := time.Now()
	_ = s.factory.DB().Model(&models.SubscriptionItem{}).
		Where("thumbnail_status = ?", "processing").
		Updates(map[string]interface{}{
			"thumbnail_status":        "retry_wait",
			"thumbnail_next_retry_at": now,
			"thumbnail_error":         "服务重启后恢复缩略图任务",
			"updated_at":              now,
		}).Error
}

func (s *SubscriptionService) thumbnailLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.dispatchThumbnails()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.dispatchThumbnails()
		}
	}
}

func (s *SubscriptionService) dispatchThumbnails() {
	var items []models.SubscriptionItem
	now := time.Now()
	if err := s.factory.DB().Where("thumbnail_url <> '' AND thumbnail_status IN ? AND (thumbnail_next_retry_at IS NULL OR thumbnail_next_retry_at <= ?)",
		[]string{"waiting_file", "retry_wait"}, now).Order("updated_at ASC").Limit(10).Find(&items).Error; err != nil {
		return
	}
	for _, item := range items {
		if item.DownloadTaskID == "" {
			continue
		}
		var task models.DownloadTask
		if err := s.factory.DB().Where("id = ? AND state = ?", item.DownloadTaskID, enum.DownloadTaskStateFinished.Value()).First(&task).Error; err != nil {
			continue
		}
		itemID := item.ID
		go s.processThumbnailItem(itemID)
	}
}

func (s *SubscriptionService) processThumbnailItem(itemID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Now()
	claimed := s.factory.DB().Model(&models.SubscriptionItem{}).
		Where("id = ? AND thumbnail_status IN ? AND (thumbnail_next_retry_at IS NULL OR thumbnail_next_retry_at <= ?)", itemID, []string{"waiting_file", "retry_wait"}, now).
		Updates(map[string]interface{}{"thumbnail_status": "processing", "thumbnail_error": "", "updated_at": now})
	if claimed.Error != nil || claimed.RowsAffected != 1 {
		return
	}
	var item models.SubscriptionItem
	if err := s.factory.DB().Where("id = ?", itemID).First(&item).Error; err != nil {
		return
	}
	var subscription models.Subscription
	if err := s.factory.DB().Where("id = ?", item.SubscriptionID).First(&subscription).Error; err != nil {
		s.failThumbnail(&item, &thumbnailDownloadError{err: fmt.Errorf("订阅不存在")})
		return
	}
	var task models.DownloadTask
	if err := s.factory.DB().Where("id = ? AND user_id = ? AND state = ?", item.DownloadTaskID, subscription.UserID, enum.DownloadTaskStateFinished.Value()).First(&task).Error; err != nil {
		s.factory.DB().Model(&item).Updates(map[string]interface{}{"thumbnail_status": "waiting_file", "updated_at": time.Now()})
		return
	}
	var owned int64
	if err := s.factory.DB().Model(&models.UserFiles{}).Where("user_id = ? AND file_id = ? AND deleted_at IS NULL", subscription.UserID, task.FileID).Count(&owned).Error; err != nil || owned == 0 {
		s.failThumbnail(&item, &thumbnailDownloadError{err: fmt.Errorf("文件不存在或无权访问")})
		return
	}
	fileInfo, err := s.factory.FileInfo().GetByID(ctx, task.FileID)
	if err != nil || fileInfo.IsEnc {
		s.failThumbnail(&item, &thumbnailDownloadError{err: fmt.Errorf("文件不存在或不支持缩略图")})
		return
	}
	content, fetchErr := s.downloadPluginThumbnail(ctx, item.ThumbnailURL)
	if fetchErr != nil {
		s.failThumbnail(&item, fetchErr)
		return
	}
	encoded, encodeErr := normalizePluginThumbnail(content)
	if encodeErr != nil {
		s.failThumbnail(&item, &thumbnailDownloadError{err: encodeErr})
		return
	}
	targetPath, err := thumbnailTargetPath(fileInfo)
	if err != nil {
		s.failThumbnail(&item, &thumbnailDownloadError{err: err})
		return
	}
	tempDir, err := os.MkdirTemp(filepath.Dir(targetPath), ".subscription-thumbnail-*")
	if err != nil {
		s.failThumbnail(&item, &thumbnailDownloadError{err: err, transient: true})
		return
	}
	defer os.RemoveAll(tempDir)
	stagedPath := filepath.Join(tempDir, "thumbnail.jpg")
	if err := os.WriteFile(stagedPath, encoded, 0644); err != nil {
		s.failThumbnail(&item, &thumbnailDownloadError{err: err, transient: true})
		return
	}
	lockValue, _ := thumbnailUpdateLocks.LoadOrStore(fileInfo.ID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := replaceThumbnailAndUpdate(stagedPath, targetPath, func() error {
		return s.factory.FileInfo().UpdateThumbnailPath(ctx, fileInfo.ID, targetPath)
	}); err != nil {
		s.failThumbnail(&item, &thumbnailDownloadError{err: err, transient: true})
		return
	}
	s.factory.DB().Model(&item).Updates(map[string]interface{}{
		"thumbnail_status": "success", "thumbnail_error": "", "thumbnail_next_retry_at": nil, "updated_at": time.Now(),
	})
}

func (s *SubscriptionService) downloadPluginThumbnail(ctx context.Context, rawURL string) ([]byte, *thumbnailDownloadError) {
	if err := download.ValidatePublicHTTPURL(rawURL); err != nil {
		return nil, &thumbnailDownloadError{err: fmt.Errorf("缩略图地址不是有效公网URL")}
	}
	client, err := download.NewPublicHTTPClient(s.downloadService.networkPolicy.ProxyURL(), s.downloadService.networkPolicy.DownloadLimiter())
	if err != nil {
		return nil, &thumbnailDownloadError{err: err, transient: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &thumbnailDownloadError{err: fmt.Errorf("缩略图地址无效")}
	}
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp,image/gif")
	resp, err := client.Do(req)
	if err != nil {
		return nil, &thumbnailDownloadError{err: fmt.Errorf("下载缩略图失败: %s", download.RedactErrorForLog(err)), transient: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		transient := resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, &thumbnailDownloadError{err: fmt.Errorf("缩略图服务器返回%d", resp.StatusCode), transient: transient}
	}
	if resp.ContentLength > pluginThumbnailMaxInput {
		return nil, &thumbnailDownloadError{err: fmt.Errorf("缩略图超过5 MiB限制")}
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, pluginThumbnailMaxInput+1))
	if err != nil {
		return nil, &thumbnailDownloadError{err: fmt.Errorf("读取缩略图失败: %w", err), transient: true}
	}
	if len(content) == 0 || int64(len(content)) > pluginThumbnailMaxInput {
		return nil, &thumbnailDownloadError{err: fmt.Errorf("缩略图大小无效")}
	}
	return content, nil
}

func normalizePluginThumbnail(content []byte) ([]byte, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp" && format != "gif") {
		return nil, fmt.Errorf("缩略图格式仅支持JPEG、PNG、WebP和GIF")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > pluginThumbnailMaxPixels {
		return nil, fmt.Errorf("缩略图解码像素超过4000万限制")
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("缩略图解码失败")
	}
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	if width > pluginThumbnailMaxSide || height > pluginThumbnailMaxSide {
		ratio := float64(pluginThumbnailMaxSide) / float64(width)
		if height > width {
			ratio = float64(pluginThumbnailMaxSide) / float64(height)
		}
		width, height = max(1, int(float64(width)*ratio)), max(1, int(float64(height)*ratio))
	}
	for width > 0 && height > 0 {
		destination := image.NewRGBA(image.Rect(0, 0, width, height))
		stddraw.Draw(destination, destination.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stddraw.Src)
		draw.CatmullRom.Scale(destination, destination.Bounds(), source, source.Bounds(), stddraw.Over, nil)
		for quality := 90; quality >= 40; quality -= 10 {
			var output bytes.Buffer
			if err := jpeg.Encode(&output, destination, &jpeg.Options{Quality: quality}); err != nil {
				return nil, err
			}
			if output.Len() <= pluginThumbnailMaxOutput {
				return output.Bytes(), nil
			}
		}
		width, height = int(float64(width)*0.8), int(float64(height)*0.8)
	}
	return nil, fmt.Errorf("缩略图无法压缩到1 MiB以内")
}

func (s *SubscriptionService) failThumbnail(item *models.SubscriptionItem, failure *thumbnailDownloadError) {
	updates := map[string]interface{}{"thumbnail_error": failure.Error(), "updated_at": time.Now()}
	if failure.transient && item.ThumbnailRetryCount < 3 {
		delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}
		updates["thumbnail_status"] = "retry_wait"
		updates["thumbnail_retry_count"] = item.ThumbnailRetryCount + 1
		updates["thumbnail_next_retry_at"] = time.Now().Add(delays[item.ThumbnailRetryCount])
	} else {
		updates["thumbnail_status"] = "failed"
		updates["thumbnail_next_retry_at"] = nil
	}
	s.factory.DB().Model(item).Updates(updates)
}

func (s *SubscriptionService) RetryThumbnail(ctx context.Context, userID, itemID string) error {
	result := s.factory.DB().WithContext(ctx).Model(&models.SubscriptionItem{}).
		Where("subscription_item.id = ? AND subscription_item.thumbnail_url <> '' AND EXISTS (SELECT 1 FROM subscription WHERE subscription.id = subscription_item.subscription_id AND subscription.user_id = ?)", itemID, userID).
		Updates(map[string]interface{}{"thumbnail_status": "waiting_file", "thumbnail_retry_count": 0, "thumbnail_next_retry_at": nil, "thumbnail_error": "", "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("订阅条目不存在或没有缩略图")
	}
	go s.processThumbnailItem(itemID)
	return nil
}
