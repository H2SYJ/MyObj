package upload

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func encodeTestImage(t *testing.T, format string, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&buffer, img)
	}
	if err != nil {
		t.Fatalf("编码测试图片失败: %v", err)
	}
	return buffer.Bytes()
}

func TestSaveVideoThumbnail(t *testing.T) {
	tempDir := t.TempDir()
	thumbnail := encodeTestImage(t, "jpeg", 300, 200)

	savedPath, err := SaveVideoThumbnail(bytes.NewReader(thumbnail), int64(len(thumbnail)), tempDir)
	if err != nil {
		t.Fatalf("保存有效缩略图失败: %v", err)
	}
	if savedPath != TempVideoThumbnailPath(tempDir) {
		t.Fatalf("缩略图路径不正确: %s", savedPath)
	}
	if _, err := os.Stat(savedPath); err != nil {
		t.Fatalf("缩略图文件不存在: %v", err)
	}
}

func TestSaveVideoThumbnailRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name         string
		content      []byte
		declaredSize int64
	}{
		{
			name:         "非JPEG图片",
			content:      encodeTestImage(t, "png", 300, 200),
			declaredSize: 1,
		},
		{
			name:         "尺寸超限",
			content:      encodeTestImage(t, "jpeg", MaxVideoThumbnailDimension+1, 1),
			declaredSize: 1,
		},
		{
			name:         "声明大小超限",
			content:      []byte("invalid"),
			declaredSize: MaxVideoThumbnailSize + 1,
		},
		{
			name:         "实际大小超限",
			content:      make([]byte, MaxVideoThumbnailSize+1),
			declaredSize: MaxVideoThumbnailSize,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := SaveVideoThumbnail(bytes.NewReader(test.content), test.declaredSize, t.TempDir())
			if err == nil {
				t.Fatal("无效缩略图未被拒绝")
			}
		})
	}
}
