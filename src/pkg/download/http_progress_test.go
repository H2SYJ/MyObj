package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPProgressFlushesLatestSnapshotBeforeTerminalState(t *testing.T) {
	writes := 0
	lastDownloaded := int64(0)
	progress := &downloadProgress{
		Context:   context.Background(),
		TotalSize: 100,
		ProgressReporter: func(_ context.Context, downloadedSize, _ int64, _ int) (bool, error) {
			writes++
			lastDownloaded = downloadedSize
			return true, nil
		},
	}
	progress.recordTransfer(75, 75)
	if writes != 0 {
		t.Fatalf("未启动采样器时HTTP进度写入次数=%d，期望0", writes)
	}
	if err := progress.flushProgress(0); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || lastDownloaded != 75 {
		t.Fatalf("终态刷新未写入最新快照: writes=%d downloaded=%d", writes, lastDownloaded)
	}
}

func TestHTTPTransferCounterRemainsMonotonicWhenProgressRollsBack(t *testing.T) {
	progress := &downloadProgress{TotalSize: 200}
	progress.recordTransfer(100, 100)
	progress.setDownloadedSize(20)
	progress.recordTransfer(70, 50)

	downloadedSize, progressValue := progress.snapshot()
	if downloadedSize != 70 || progressValue != 35 {
		t.Fatalf("回滚后的有效进度异常: downloaded=%d progress=%d", downloadedSize, progressValue)
	}
	if transferred := progress.transferredBytes.Load(); transferred != 150 {
		t.Fatalf("回滚不应减少传输计数，实际%d，期望150", transferred)
	}
}

func TestHTTPDirectDownloadReportsSampledSpeed(t *testing.T) {
	const chunkSize = 4 * 1024
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		flusher, _ := writer.(http.Flusher)
		for index := 0; index < 5; index++ {
			_, _ = writer.Write(make([]byte, chunkSize))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(15 * time.Millisecond)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var positiveReports atomic.Int32
	progress := &downloadProgress{
		Context: ctx, Cancel: cancel, TotalSize: 5 * chunkSize, sampleInterval: 10 * time.Millisecond,
		ProgressReporter: func(_ context.Context, _ int64, speed int64, _ int) (bool, error) {
			if speed > 0 {
				positiveReports.Add(1)
			}
			return true, nil
		},
	}
	filePath := filepath.Join(t.TempDir(), "direct.bin")
	if err := downloadDirect(ctx, server.URL, filePath, server.Client(), progress); err != nil {
		t.Fatal(err)
	}
	if err := progress.stopSampler(); err != nil {
		t.Fatal(err)
	}
	if positiveReports.Load() == 0 {
		t.Fatal("受控HTTP下载期间没有产生正速度样本")
	}
}
