package download

import (
	"context"
	"testing"
	"time"
)

func TestHTTPProgressFlushesLatestSnapshotBeforeTerminalState(t *testing.T) {
	writes := 0
	lastDownloaded := int64(0)
	progress := &downloadProgress{
		Context:    context.Background(),
		TotalSize:  100,
		LastUpdate: time.Now(),
		ProgressReporter: func(_ context.Context, downloadedSize, _ int64, _ int) (bool, error) {
			writes++
			lastDownloaded = downloadedSize
			return true, nil
		},
	}
	progress.updateProgress(75)
	if writes != 0 {
		t.Fatalf("一秒内HTTP进度写入次数=%d，期望0", writes)
	}
	if err := progress.flushProgress(); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || lastDownloaded != 75 {
		t.Fatalf("终态刷新未写入最新快照: writes=%d downloaded=%d", writes, lastDownloaded)
	}
}
