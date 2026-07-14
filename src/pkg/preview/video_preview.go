package preview

// 视频预览

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// VideoThumbnailMaxDimension 与前端生成视频缩略图时的最长边保持一致。
	VideoThumbnailMaxDimension = 300
	// VideoThumbnailMaxFileSize 与上传接口的视频缩略图大小限制保持一致。
	VideoThumbnailMaxFileSize = int64(1024 * 1024)
)

// VideoMetadata 是生成缩略图所需的视频元数据。
type VideoMetadata struct {
	Duration float64
	Width    int
	Height   int
}

// VideoThumbnailGenerator 定义视频缩略图生成能力，便于批处理测试替换。
type VideoThumbnailGenerator interface {
	Generate(ctx context.Context, inputPath, outputPath string) error
}

// FFmpegVideoThumbnailGenerator 使用 ffprobe 和 FFmpeg 截取视频画面。
type FFmpegVideoThumbnailGenerator struct {
	ffprobePath string
	ffmpegPath  string
}

// NewFFmpegVideoThumbnailGenerator 检查外部工具并创建生成器。
func NewFFmpegVideoThumbnailGenerator() (*FFmpegVideoThumbnailGenerator, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("未找到 ffprobe，请先安装 FFmpeg 并加入 PATH: %w", err)
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("未找到 ffmpeg，请先安装 FFmpeg 并加入 PATH: %w", err)
	}
	return &FFmpegVideoThumbnailGenerator{
		ffprobePath: ffprobePath,
		ffmpegPath:  ffmpegPath,
	}, nil
}

// CalculateVideoCaptureTime 按前端规则计算截帧时间。
func CalculateVideoCaptureTime(duration float64) (float64, error) {
	if math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return 0, fmt.Errorf("视频时长无效: %v", duration)
	}
	if duration <= 2 {
		return duration / 2, nil
	}
	return math.Min(duration*0.1, 5), nil
}

// CalculateVideoThumbnailDimensions 按前端规则计算缩略图尺寸。
func CalculateVideoThumbnailDimensions(width, height int) (int, int, error) {
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("视频画面尺寸无效: %dx%d", width, height)
	}
	scale := math.Min(1, float64(VideoThumbnailMaxDimension)/float64(max(width, height)))
	thumbnailWidth := max(1, int(math.Round(float64(width)*scale)))
	thumbnailHeight := max(1, int(math.Round(float64(height)*scale)))
	return thumbnailWidth, thumbnailHeight, nil
}

// Generate 生成 JPEG 视频缩略图。
func (g *FFmpegVideoThumbnailGenerator) Generate(ctx context.Context, inputPath, outputPath string) error {
	metadata, err := g.probe(ctx, inputPath)
	if err != nil {
		return err
	}
	captureTime, err := CalculateVideoCaptureTime(metadata.Duration)
	if err != nil {
		return err
	}
	width, height, err := CalculateVideoThumbnailDimensions(metadata.Width, metadata.Height)
	if err != nil {
		return err
	}

	var frame bytes.Buffer
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, g.ffmpegPath,
		"-v", "error",
		"-i", inputPath,
		"-ss", strconv.FormatFloat(captureTime, 'f', 6, 64),
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d", width, height),
		"-f", "image2pipe",
		"-vcodec", "png",
		"pipe:1",
	)
	command.Stdout = &frame
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("FFmpeg 截取视频画面失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	imageValue, err := jpegOrPNGDecode(frame.Bytes())
	if err != nil {
		return fmt.Errorf("解析 FFmpeg 输出画面失败: %w", err)
	}
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("创建缩略图失败: %w", err)
	}
	// 复用图片缩略图的 JPEG 编码器，使两类缩略图保持相同质量。
	encodeErr := encodeJPEG(output, imageValue)
	syncErr := output.Sync()
	closeErr := output.Close()
	if encodeErr != nil {
		return fmt.Errorf("编码 JPEG 缩略图失败: %w", encodeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("同步缩略图失败: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭缩略图失败: %w", closeErr)
	}
	return ValidateVideoThumbnail(outputPath)
}

func (g *FFmpegVideoThumbnailGenerator) probe(ctx context.Context, inputPath string) (*VideoMetadata, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, g.ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "json",
		inputPath,
	)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe 读取视频元数据失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var result struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("解析 ffprobe 输出失败: %w", err)
	}
	if len(result.Streams) == 0 {
		return nil, fmt.Errorf("视频中没有可用画面流")
	}
	duration, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil {
		return nil, fmt.Errorf("解析视频时长失败: %w", err)
	}
	return &VideoMetadata{
		Duration: duration,
		Width:    result.Streams[0].Width,
		Height:   result.Streams[0].Height,
	}, nil
}

// ValidateVideoThumbnail 校验批处理生成或遗留的 JPEG 缩略图。
func ValidateVideoThumbnail(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > VideoThumbnailMaxFileSize {
		return fmt.Errorf("缩略图文件大小无效: %d", info.Size())
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	imageConfig, decodeErr := jpeg.DecodeConfig(file)
	closeErr := file.Close()
	if decodeErr != nil {
		return fmt.Errorf("缩略图不是有效的 JPEG 图片: %w", decodeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 ||
		imageConfig.Width > VideoThumbnailMaxDimension || imageConfig.Height > VideoThumbnailMaxDimension {
		return fmt.Errorf("缩略图尺寸无效: %dx%d", imageConfig.Width, imageConfig.Height)
	}
	return nil
}

func jpegOrPNGDecode(content []byte) (image.Image, error) {
	value, _, err := image.Decode(bytes.NewReader(content))
	return value, err
}
