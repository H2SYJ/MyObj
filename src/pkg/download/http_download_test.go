package download

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newDownloadTestFactory(t *testing.T, task *models.DownloadTask) *impl.RepositoryFactory {
	t.Helper()
	logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.DownloadTask{}); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	if task != nil {
		if err := db.Create(task).Error; err != nil {
			t.Fatalf("创建测试任务失败: %v", err)
		}
	}
	return impl.NewRepositoryFactory(db)
}

func TestValidatePublicHTTPURLRejectsPrivateTargets(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/file",
		"http://localhost/file",
		"http://10.0.0.1/file",
		"ftp://example.com/file",
	}
	for _, rawURL := range tests {
		if err := ValidatePublicHTTPURL(rawURL); err == nil {
			t.Fatalf("应拒绝地址: %s", rawURL)
		}
	}
}

func TestGetFileInfoFallsBackToRangeGET(t *testing.T) {
	content := []byte("hello")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-0/5")
		w.Header().Set("Content-Disposition", `attachment; filename="测试.txt"`)
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[:1])
	}))
	defer server.Close()

	info, supportRange, err := GetFileInfoWithClient(context.Background(), server.URL+"/file", server.Client())
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}
	if !supportRange || info.FileSize != int64(len(content)) || info.FileName != "测试.txt" {
		t.Fatalf("文件信息不正确: %#v, supportRange=%v", info, supportRange)
	}
}

func TestRangeResumeUsesManifestInsteadOfPreallocatedSize(t *testing.T) {
	data := []byte(strings.Repeat("0123456789abcdef", 256))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeValue := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.Split(rangeValue, "-")
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end, _ := strconv.ParseInt(parts[1], 10, 64)
		w.Header().Set("ETag", `"stable"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer server.Close()

	task := &models.DownloadTask{ID: "task", UserID: "user", Type: 0, State: 1, RunToken: "run"}
	factory := newDownloadTestFactory(t, task)
	directory := t.TempDir()
	filePath := filepath.Join(directory, "file.bin")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(data[:1024], 0); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	manifest := &downloadManifest{
		Version:  1,
		URL:      server.URL,
		FileSize: int64(len(data)),
		ETag:     `"stable"`,
		Chunks: []manifestChunk{
			{Index: 0, Start: 0, End: 1023, Done: true},
			{Index: 1, Start: 1024, End: 2047},
			{Index: 2, Start: 2048, End: 3071},
			{Index: 3, Start: 3072, End: int64(len(data) - 1)},
		},
	}
	if err := saveDownloadManifest(filePath+".manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	info := &FileInfoResult{FileName: "file.bin", FileSize: int64(len(data)), ETag: `"stable"`}
	progress := newDownloadProgress(task.ID, info.FileSize, factory, task.RunToken)
	opts := &HTTPDownloadOptions{ChunkSize: 1024, MaxConcurrent: 2, MaxRetries: 0}
	if err := downloadWithRange(context.Background(), server.URL, filePath, info, opts, progress, server.Client()); err != nil {
		t.Fatalf("恢复下载失败: %v", err)
	}
	actual, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(data) {
		t.Fatal("恢复后的文件内容不一致")
	}
}

func TestRangeProbeRejectsServerReturning200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("all"))
	}))
	defer server.Close()
	err := probeRangeSupport(context.Background(), server.URL, 3, server.Client())
	if err == nil || !strings.Contains(err.Error(), "不支持可靠Range") {
		t.Fatalf("应拒绝忽略Range的服务器，实际错误: %v", err)
	}
}

func TestManifestRejectsChangedResource(t *testing.T) {
	manifest := &downloadManifest{Version: 1, URL: "https://example.com/file", FileSize: 10, ETag: `"v1"`}
	info := &FileInfoResult{FileSize: 10, ETag: `"v2"`}
	if manifestMatches(manifest, manifest.URL, info) {
		t.Fatal("远端资源变化后不应复用旧清单")
	}
}

func TestUnknownSizeDownloadReservesSpaceInBlocks(t *testing.T) {
	var reservations []int64
	progress := &downloadProgress{
		TotalSize: -1,
		ReserveSpace: func(size int64) (int64, error) {
			reservations = append(reservations, size)
			return size, nil
		},
	}
	if err := progress.ensureUnknownSizeReservation(1); err != nil {
		t.Fatal(err)
	}
	if err := progress.ensureUnknownSizeReservation(65 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 2 || reservations[0] != 64*1024*1024 || reservations[1] != 128*1024*1024 {
		t.Fatalf("增量预留不正确: %#v", reservations)
	}
}
