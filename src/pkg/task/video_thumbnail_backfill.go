package task

import (
	"context"
	"fmt"
	"io"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/preview"
	"os"
	"path/filepath"
	"sync"
)

const defaultVideoThumbnailBatchSize = 100

// VideoThumbnailBackfillOptions 控制历史视频缩略图补齐行为。
type VideoThumbnailBackfillOptions struct {
	DryRun      bool
	Concurrency int
	TempDir     string
	BatchSize   int
}

// VideoThumbnailBackfillStats 是补齐任务汇总。
type VideoThumbnailBackfillStats struct {
	Scanned   int
	Generated int
	Reused    int
	Pending   int
	Skipped   int
	Failed    int
}

// VideoThumbnailFileRepository 是补齐任务所需的最小文件仓储接口。
type VideoThumbnailFileRepository interface {
	ListUnencryptedVideosAfter(ctx context.Context, afterID string, limit int) ([]*models.FileInfo, error)
	UpdateThumbnailPath(ctx context.Context, id, thumbnailPath string) error
}

// VideoThumbnailChunkRepository 是补齐任务所需的最小分片仓储接口。
type VideoThumbnailChunkRepository interface {
	GetByFileID(ctx context.Context, fileID string) ([]*models.FileChunk, error)
}

// VideoThumbnailBackfiller 为历史未加密视频补齐缩略图。
type VideoThumbnailBackfiller struct {
	fileRepo  VideoThumbnailFileRepository
	chunkRepo VideoThumbnailChunkRepository
	generator preview.VideoThumbnailGenerator
}

// NewVideoThumbnailBackfiller 创建历史视频缩略图补齐任务。
func NewVideoThumbnailBackfiller(
	fileRepo VideoThumbnailFileRepository,
	chunkRepo VideoThumbnailChunkRepository,
	generator preview.VideoThumbnailGenerator,
) *VideoThumbnailBackfiller {
	return &VideoThumbnailBackfiller{
		fileRepo:  fileRepo,
		chunkRepo: chunkRepo,
		generator: generator,
	}
}

// Run 扫描所有未加密视频并补齐缩略图。
func (b *VideoThumbnailBackfiller) Run(ctx context.Context, options VideoThumbnailBackfillOptions) (VideoThumbnailBackfillStats, error) {
	options = normalizeVideoThumbnailBackfillOptions(options)
	stats := VideoThumbnailBackfillStats{}
	cursor := ""

	for {
		files, err := b.fileRepo.ListUnencryptedVideosAfter(ctx, cursor, options.BatchSize)
		if err != nil {
			return stats, fmt.Errorf("查询历史视频失败: %w", err)
		}
		if len(files) == 0 {
			break
		}
		cursor = files[len(files)-1].ID

		batchStats := b.processBatch(ctx, files, options)
		stats.add(batchStats)
		if err := ctx.Err(); err != nil {
			return stats, err
		}
	}

	return stats, nil
}

func normalizeVideoThumbnailBackfillOptions(options VideoThumbnailBackfillOptions) VideoThumbnailBackfillOptions {
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}
	if options.BatchSize <= 0 {
		options.BatchSize = defaultVideoThumbnailBatchSize
	}
	if options.TempDir == "" {
		options.TempDir = os.TempDir()
	}
	return options
}

func (b *VideoThumbnailBackfiller) processBatch(
	ctx context.Context,
	files []*models.FileInfo,
	options VideoThumbnailBackfillOptions,
) VideoThumbnailBackfillStats {
	jobs := make(chan *models.FileInfo)
	results := make(chan videoThumbnailBackfillResult, len(files))
	workerCount := min(options.Concurrency, len(files))
	var workers sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for file := range jobs {
				results <- b.processFile(ctx, file, options)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, file := range files {
			select {
			case jobs <- file:
			case <-ctx.Done():
				return
			}
		}
	}()

	workers.Wait()
	close(results)

	stats := VideoThumbnailBackfillStats{}
	for result := range results {
		stats.Scanned++
		switch result.status {
		case videoThumbnailGenerated:
			stats.Generated++
		case videoThumbnailReused:
			stats.Reused++
		case videoThumbnailPending:
			stats.Pending++
		case videoThumbnailSkipped:
			stats.Skipped++
		case videoThumbnailFailed:
			stats.Failed++
			if logger.LOG != nil {
				logger.LOG.Warn("补齐视频缩略图失败", "fileID", result.fileID, "error", result.err)
			}
		}
	}
	return stats
}

type videoThumbnailBackfillStatus int

const (
	videoThumbnailGenerated videoThumbnailBackfillStatus = iota
	videoThumbnailReused
	videoThumbnailPending
	videoThumbnailSkipped
	videoThumbnailFailed
)

type videoThumbnailBackfillResult struct {
	fileID string
	status videoThumbnailBackfillStatus
	err    error
}

func (b *VideoThumbnailBackfiller) processFile(
	ctx context.Context,
	file *models.FileInfo,
	options VideoThumbnailBackfillOptions,
) videoThumbnailBackfillResult {
	result := videoThumbnailBackfillResult{fileID: file.ID}
	if file.ThumbnailImg != "" && preview.ValidateVideoThumbnail(file.ThumbnailImg) == nil {
		result.status = videoThumbnailSkipped
		return result
	}

	targetPath, err := videoThumbnailTargetPath(file)
	if err != nil {
		return failedVideoThumbnailResult(file.ID, err)
	}
	if preview.ValidateVideoThumbnail(targetPath) == nil {
		if options.DryRun {
			result.status = videoThumbnailReused
			return result
		}
		if err := b.fileRepo.UpdateThumbnailPath(ctx, file.ID, targetPath); err != nil {
			return failedVideoThumbnailResult(file.ID, fmt.Errorf("更新复用缩略图路径失败: %w", err))
		}
		result.status = videoThumbnailReused
		return result
	}

	if options.DryRun {
		if err := b.validateVideoSource(ctx, file); err != nil {
			return failedVideoThumbnailResult(file.ID, err)
		}
		result.status = videoThumbnailPending
		return result
	}

	inputPath, cleanup, err := b.prepareVideoSource(ctx, file, options.TempDir)
	if err != nil {
		return failedVideoThumbnailResult(file.ID, err)
	}
	defer cleanup()

	tempThumbnail, err := os.CreateTemp(filepath.Dir(targetPath), ".video-thumbnail-backfill-*.jpg")
	if err != nil {
		return failedVideoThumbnailResult(file.ID, fmt.Errorf("创建缩略图临时文件失败: %w", err))
	}
	tempThumbnailPath := tempThumbnail.Name()
	if err := tempThumbnail.Close(); err != nil {
		os.Remove(tempThumbnailPath)
		return failedVideoThumbnailResult(file.ID, fmt.Errorf("关闭缩略图临时文件失败: %w", err))
	}
	defer os.Remove(tempThumbnailPath)

	if err := b.generator.Generate(ctx, inputPath, tempThumbnailPath); err != nil {
		return failedVideoThumbnailResult(file.ID, err)
	}
	if err := preview.ValidateVideoThumbnail(tempThumbnailPath); err != nil {
		return failedVideoThumbnailResult(file.ID, fmt.Errorf("生成的缩略图校验失败: %w", err))
	}
	if err := replaceVideoThumbnail(tempThumbnailPath, targetPath); err != nil {
		return failedVideoThumbnailResult(file.ID, fmt.Errorf("保存缩略图失败: %w", err))
	}
	if err := b.fileRepo.UpdateThumbnailPath(ctx, file.ID, targetPath); err != nil {
		return failedVideoThumbnailResult(file.ID, fmt.Errorf("更新缩略图路径失败: %w", err))
	}

	result.status = videoThumbnailGenerated
	return result
}

func videoThumbnailTargetPath(file *models.FileInfo) (string, error) {
	if file.Path == "" {
		return "", fmt.Errorf("视频存储路径为空")
	}
	if file.RandomName == "" {
		return "", fmt.Errorf("视频随机文件名为空")
	}
	return filepath.Join(filepath.Dir(file.Path), file.RandomName+".jpg"), nil
}

func (b *VideoThumbnailBackfiller) validateVideoSource(ctx context.Context, file *models.FileInfo) error {
	if !file.IsChunk {
		return validateRegularFile(file.Path)
	}
	chunks, err := b.chunkRepo.GetByFileID(ctx, file.ID)
	if err != nil {
		return fmt.Errorf("查询视频分片失败: %w", err)
	}
	if len(chunks) == 0 {
		return fmt.Errorf("未找到视频分片")
	}
	for _, chunk := range chunks {
		if err := validateRegularFile(chunk.ChunkPath); err != nil {
			return fmt.Errorf("视频分片 %d 不可用: %w", chunk.ChunkIndex, err)
		}
	}
	return nil
}

func (b *VideoThumbnailBackfiller) prepareVideoSource(
	ctx context.Context,
	file *models.FileInfo,
	tempDir string,
) (string, func(), error) {
	if !file.IsChunk {
		if err := validateRegularFile(file.Path); err != nil {
			return "", func() {}, err
		}
		return file.Path, func() {}, nil
	}

	chunks, err := b.chunkRepo.GetByFileID(ctx, file.ID)
	if err != nil {
		return "", func() {}, fmt.Errorf("查询视频分片失败: %w", err)
	}
	if len(chunks) == 0 {
		return "", func() {}, fmt.Errorf("未找到视频分片")
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", func() {}, fmt.Errorf("创建视频合并临时目录失败: %w", err)
	}
	merged, err := os.CreateTemp(tempDir, "video-thumbnail-merge-*.data")
	if err != nil {
		return "", func() {}, fmt.Errorf("创建视频合并临时文件失败: %w", err)
	}
	mergedPath := merged.Name()
	cleanup := func() { os.Remove(mergedPath) }

	for _, chunk := range chunks {
		if err := copyVideoChunk(merged, chunk); err != nil {
			merged.Close()
			cleanup()
			return "", func() {}, err
		}
	}
	if err := merged.Sync(); err != nil {
		merged.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("同步合并视频失败: %w", err)
	}
	if err := merged.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("关闭合并视频失败: %w", err)
	}
	return mergedPath, cleanup, nil
}

func copyVideoChunk(output *os.File, chunk *models.FileChunk) error {
	input, err := os.Open(chunk.ChunkPath)
	if err != nil {
		return fmt.Errorf("打开视频分片 %d 失败: %w", chunk.ChunkIndex, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := input.Close()
	if copyErr != nil {
		return fmt.Errorf("合并视频分片 %d 失败: %w", chunk.ChunkIndex, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭视频分片 %d 失败: %w", chunk.ChunkIndex, closeErr)
	}
	return nil
}

func validateRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("视频文件不可用: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("视频文件无效: %s", path)
	}
	return nil
}

func replaceVideoThumbnail(sourcePath, targetPath string) error {
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(sourcePath, targetPath)
}

func failedVideoThumbnailResult(fileID string, err error) videoThumbnailBackfillResult {
	return videoThumbnailBackfillResult{
		fileID: fileID,
		status: videoThumbnailFailed,
		err:    err,
	}
}

func (s *VideoThumbnailBackfillStats) add(other VideoThumbnailBackfillStats) {
	s.Scanned += other.Scanned
	s.Generated += other.Generated
	s.Reused += other.Reused
	s.Pending += other.Pending
	s.Skipped += other.Skipped
	s.Failed += other.Failed
}
