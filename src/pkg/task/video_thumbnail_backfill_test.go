package task

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"myobj/src/pkg/models"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

type fakeVideoFileRepository struct {
	mu        sync.Mutex
	files     []*models.FileInfo
	updateErr error
	updates   map[string]string
}

func (r *fakeVideoFileRepository) ListUnencryptedVideosAfter(_ context.Context, afterID string, limit int) ([]*models.FileInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	files := make([]*models.FileInfo, 0, limit)
	sorted := append([]*models.FileInfo(nil), r.files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, file := range sorted {
		if file.ID <= afterID || file.IsEnc || len(file.Mime) < 6 || file.Mime[:6] != "video/" {
			continue
		}
		files = append(files, file)
		if len(files) == limit {
			break
		}
	}
	return files, nil
}

func (r *fakeVideoFileRepository) UpdateThumbnailPath(_ context.Context, id, thumbnailPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	if r.updates == nil {
		r.updates = make(map[string]string)
	}
	r.updates[id] = thumbnailPath
	for _, file := range r.files {
		if file.ID == id {
			file.ThumbnailImg = thumbnailPath
		}
	}
	return nil
}

type fakeVideoChunkRepository struct {
	chunks map[string][]*models.FileChunk
}

func (r *fakeVideoChunkRepository) GetByFileID(_ context.Context, fileID string) ([]*models.FileChunk, error) {
	chunks := append([]*models.FileChunk(nil), r.chunks[fileID]...)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIndex < chunks[j].ChunkIndex })
	return chunks, nil
}

type fakeVideoThumbnailGenerator struct {
	mu       sync.Mutex
	calls    int
	input    []byte
	generate func(inputPath, outputPath string) error
}

func (g *fakeVideoThumbnailGenerator) Generate(_ context.Context, inputPath, outputPath string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	g.input = append([]byte(nil), content...)
	if g.generate != nil {
		return g.generate(inputPath, outputPath)
	}
	return writeTestVideoThumbnail(outputPath)
}

func TestVideoThumbnailBackfillGeneratesAndIsIdempotent(t *testing.T) {
	storageDir := t.TempDir()
	sourcePath := filepath.Join(storageDir, "video.data")
	if err := os.WriteFile(sourcePath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	file := &models.FileInfo{ID: "001", RandomName: "video", Mime: "video/mp4", Path: sourcePath}
	fileRepo := &fakeVideoFileRepository{files: []*models.FileInfo{file}}
	generator := &fakeVideoThumbnailGenerator{}
	backfiller := NewVideoThumbnailBackfiller(fileRepo, &fakeVideoChunkRepository{}, generator)

	stats, err := backfiller.Run(context.Background(), VideoThumbnailBackfillOptions{TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Generated != 1 || stats.Failed != 0 {
		t.Fatalf("first stats = %+v", stats)
	}
	if file.ThumbnailImg == "" {
		t.Fatal("thumbnail path was not updated")
	}
	if _, err := os.Stat(file.ThumbnailImg); err != nil {
		t.Fatalf("thumbnail was not generated: %v", err)
	}

	stats, err = backfiller.Run(context.Background(), VideoThumbnailBackfillOptions{TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 1 || generator.calls != 1 {
		t.Fatalf("second stats = %+v, generator calls = %d", stats, generator.calls)
	}
}

func TestVideoThumbnailBackfillDryRunDoesNotWrite(t *testing.T) {
	storageDir := t.TempDir()
	sourcePath := filepath.Join(storageDir, "video.data")
	if err := os.WriteFile(sourcePath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	file := &models.FileInfo{ID: "001", RandomName: "video", Mime: "video/mp4", Path: sourcePath}
	fileRepo := &fakeVideoFileRepository{files: []*models.FileInfo{file}}
	backfiller := NewVideoThumbnailBackfiller(fileRepo, &fakeVideoChunkRepository{}, nil)

	stats, err := backfiller.Run(context.Background(), VideoThumbnailBackfillOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != 1 || stats.Generated != 0 || len(fileRepo.updates) != 0 {
		t.Fatalf("stats = %+v, updates = %+v", stats, fileRepo.updates)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "video.jpg")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created thumbnail, stat error = %v", err)
	}
}

func TestVideoThumbnailBackfillMergesChunksInOrderAndCleansTempFile(t *testing.T) {
	storageDir := t.TempDir()
	firstChunk := filepath.Join(storageDir, "video_0.data")
	secondChunk := filepath.Join(storageDir, "video_1.data")
	if err := os.WriteFile(firstChunk, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondChunk, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	file := &models.FileInfo{ID: "001", RandomName: "video", Mime: "video/mp4", Path: firstChunk, IsChunk: true}
	fileRepo := &fakeVideoFileRepository{files: []*models.FileInfo{file}}
	chunkRepo := &fakeVideoChunkRepository{chunks: map[string][]*models.FileChunk{
		file.ID: {
			{FileID: file.ID, ChunkPath: secondChunk, ChunkIndex: 1},
			{FileID: file.ID, ChunkPath: firstChunk, ChunkIndex: 0},
		},
	}}
	generator := &fakeVideoThumbnailGenerator{}
	tempDir := t.TempDir()
	backfiller := NewVideoThumbnailBackfiller(fileRepo, chunkRepo, generator)

	stats, err := backfiller.Run(context.Background(), VideoThumbnailBackfillOptions{TempDir: tempDir})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Generated != 1 || string(generator.input) != "firstsecond" {
		t.Fatalf("stats = %+v, merged input = %q", stats, generator.input)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary merge files were not cleaned: %v", entries)
	}
}

func TestVideoThumbnailBackfillReusesOrphanAfterDatabaseFailure(t *testing.T) {
	storageDir := t.TempDir()
	sourcePath := filepath.Join(storageDir, "video.data")
	if err := os.WriteFile(sourcePath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	file := &models.FileInfo{ID: "001", RandomName: "video", Mime: "video/mp4", Path: sourcePath}
	fileRepo := &fakeVideoFileRepository{files: []*models.FileInfo{file}, updateErr: errors.New("database unavailable")}
	generator := &fakeVideoThumbnailGenerator{}
	backfiller := NewVideoThumbnailBackfiller(fileRepo, &fakeVideoChunkRepository{}, generator)

	stats, err := backfiller.Run(context.Background(), VideoThumbnailBackfillOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 1 || generator.calls != 1 {
		t.Fatalf("first stats = %+v, calls = %d", stats, generator.calls)
	}
	fileRepo.updateErr = nil
	stats, err = backfiller.Run(context.Background(), VideoThumbnailBackfillOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Reused != 1 || generator.calls != 1 {
		t.Fatalf("second stats = %+v, calls = %d", stats, generator.calls)
	}
}

func writeTestVideoThumbnail(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	imageValue := image.NewRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			imageValue.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	encodeErr := jpeg.Encode(file, imageValue, &jpeg.Options{Quality: 90})
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}
