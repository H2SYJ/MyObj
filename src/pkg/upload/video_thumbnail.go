package upload

import (
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
)

const (
	videoThumbnailFileName     = "video_thumbnail.jpg"
	MaxVideoThumbnailSize      = int64(1024 * 1024)
	MaxVideoThumbnailDimension = 1000
)

// TempVideoThumbnailPath 返回上传任务临时目录中的视频缩略图路径。
func TempVideoThumbnailPath(tempDir string) string {
	return filepath.Join(tempDir, videoThumbnailFileName)
}

// SaveVideoThumbnail 校验并保存上传的视频缩略图。
func SaveVideoThumbnail(input io.Reader, declaredSize int64, tempDir string) (string, error) {
	if input == nil {
		return "", fmt.Errorf("缩略图内容为空")
	}
	if declaredSize <= 0 {
		return "", fmt.Errorf("缩略图大小无效")
	}
	if declaredSize > MaxVideoThumbnailSize {
		return "", fmt.Errorf("缩略图超过 %d 字节限制", MaxVideoThumbnailSize)
	}

	tempFile, err := os.CreateTemp(tempDir, "video-thumbnail-*.tmp")
	if err != nil {
		return "", fmt.Errorf("创建缩略图临时文件失败: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	written, copyErr := io.Copy(tempFile, io.LimitReader(input, MaxVideoThumbnailSize+1))
	closeErr := tempFile.Close()
	if copyErr != nil {
		return "", fmt.Errorf("保存缩略图失败: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("关闭缩略图临时文件失败: %w", closeErr)
	}
	if written == 0 || written > MaxVideoThumbnailSize {
		return "", fmt.Errorf("缩略图实际大小无效")
	}

	thumbnailFile, err := os.Open(tempPath)
	if err != nil {
		return "", fmt.Errorf("打开缩略图失败: %w", err)
	}
	imageConfig, decodeErr := jpeg.DecodeConfig(thumbnailFile)
	closeErr = thumbnailFile.Close()
	if decodeErr != nil {
		return "", fmt.Errorf("缩略图不是有效的 JPEG 图片: %w", decodeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("关闭缩略图文件失败: %w", closeErr)
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 ||
		imageConfig.Width > MaxVideoThumbnailDimension || imageConfig.Height > MaxVideoThumbnailDimension {
		return "", fmt.Errorf("缩略图尺寸必须在 1 到 %d 像素之间", MaxVideoThumbnailDimension)
	}

	finalPath := TempVideoThumbnailPath(tempDir)
	if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("替换旧缩略图失败: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("保存缩略图失败: %w", err)
	}

	return finalPath, nil
}
