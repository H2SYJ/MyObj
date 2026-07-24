package download

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHLSProgressCoalescesConcurrentSegmentUpdates(t *testing.T) {
	var writes atomic.Int32
	var lastDownloaded atomic.Int64
	progress := &hlsProgress{
		totalDuration: 100,
		lastUpdate:    time.Now().Add(-time.Second),
		progressReporter: func(_ context.Context, downloadedSize, _ int64, _ int) (bool, error) {
			writes.Add(1)
			lastDownloaded.Store(downloadedSize)
			return true, nil
		},
	}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := progress.complete(1, 1); err != nil {
				t.Errorf("更新HLS进度失败: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := writes.Load(); got != 1 {
		t.Fatalf("一秒内进度写入次数=%d，期望1", got)
	}
	if err := progress.flushCurrent(); err != nil {
		t.Fatal(err)
	}
	if got := writes.Load(); got != 2 {
		t.Fatalf("强制刷新后的写入次数=%d，期望2", got)
	}
	if got := lastDownloaded.Load(); got != 100 {
		t.Fatalf("最终已下载大小=%d，期望100", got)
	}
}
