package download

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHLSProgressTracksConcurrentSegmentUpdatesUntilFlush(t *testing.T) {
	writes := 0
	lastDownloaded := int64(0)
	progress := &hlsProgress{
		totalDuration: 100,
		progressReporter: func(_ context.Context, downloadedSize, _ int64, _ int) (bool, error) {
			writes++
			lastDownloaded = downloadedSize
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
	if writes != 0 {
		t.Fatalf("分片完成回调不应直接写入进度，实际%d次", writes)
	}
	if err := progress.flushCurrent(); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("强制刷新后的写入次数=%d，期望1", writes)
	}
	if lastDownloaded != 100 {
		t.Fatalf("最终已下载大小=%d，期望100", lastDownloaded)
	}
}

func TestHLSProgressReportsInFlightTransferBeforeSegmentCompletes(t *testing.T) {
	type report struct {
		downloadedSize int64
		speed          int64
	}
	reports := make(chan report, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := &hlsProgress{
		ctx:            ctx,
		totalDuration:  10,
		sampleInterval: 10 * time.Millisecond,
		progressReporter: func(_ context.Context, downloadedSize, speed int64, _ int) (bool, error) {
			select {
			case reports <- report{downloadedSize: downloadedSize, speed: speed}:
			default:
			}
			return true, nil
		},
	}
	progress.startSampler(cancel)
	writer := &transferCountingWriter{writer: io.Discard, onTransfer: progress.recordTransfer}
	if _, err := writer.Write(make([]byte, 32*1024)); err != nil {
		t.Fatal(err)
	}

	select {
	case current := <-reports:
		if current.speed <= 0 {
			t.Fatalf("未完成分片的速度=%d，期望大于0", current.speed)
		}
		if current.downloadedSize != 0 {
			t.Fatalf("未完成分片不应改变有效下载量，实际%d", current.downloadedSize)
		}
	case <-time.After(time.Second):
		t.Fatal("未完成HLS分片没有产生速度上报")
	}
	if err := progress.stopSampler(); err != nil {
		t.Fatal(err)
	}
}

type progressRoundTripFunc func(*http.Request) (*http.Response, error)

func (f progressRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type slowSegmentReader struct {
	remaining int
	delay     time.Duration
}

func (r *slowSegmentReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	readSize := min(len(buffer), 4*1024, r.remaining)
	for index := 0; index < readSize; index++ {
		buffer[index] = byte(index)
	}
	r.remaining -= readSize
	return readSize, nil
}

func TestHLSItemReportsSpeedWhileSegmentIsStillDownloading(t *testing.T) {
	const totalSize = 5 * 4 * 1024
	client := &http.Client{Transport: progressRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(&slowSegmentReader{remaining: totalSize, delay: 15 * time.Millisecond}),
			ContentLength: totalSize,
			Request:       request,
		}, nil
	})}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var segmentCompleted atomic.Bool
	var reportedBeforeCompletion atomic.Bool
	progress := &hlsProgress{
		ctx: ctx, totalDuration: 10, sampleInterval: 10 * time.Millisecond,
		progressReporter: func(_ context.Context, downloadedSize, speed int64, _ int) (bool, error) {
			if speed > 0 && downloadedSize == 0 && !segmentCompleted.Load() {
				reportedBeforeCompletion.Store(true)
			}
			return true, nil
		},
	}
	progress.startSampler(cancel)
	_, size, err := downloadHLSItem(ctx, client, "https://8.8.8.8/segment.ts", 0, 0,
		filepath.Join(t.TempDir(), "segment.ts"), hlsKeySpec{}, 0, nil, progress.recordTransfer)
	segmentCompleted.Store(true)
	if stopErr := progress.stopSampler(); stopErr != nil {
		t.Fatal(stopErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if size != totalSize {
		t.Fatalf("HLS分片大小=%d，期望%d", size, totalSize)
	}
	if !reportedBeforeCompletion.Load() {
		t.Fatal("HLS分片完成前没有产生速度上报")
	}
}
