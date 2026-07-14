package preview

import (
	"math"
	"testing"
)

func TestCalculateVideoCaptureTime(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		want     float64
	}{
		{name: "一秒视频取中点", duration: 1, want: 0.5},
		{name: "两秒视频取中点", duration: 2, want: 1},
		{name: "超过两秒取百分之十", duration: 2.1, want: 0.21},
		{name: "五十秒最多取第五秒", duration: 50, want: 5},
		{name: "长视频最多取第五秒", duration: 100, want: 5},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CalculateVideoCaptureTime(test.duration)
			if err != nil {
				t.Fatalf("CalculateVideoCaptureTime() error = %v", err)
			}
			if math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("CalculateVideoCaptureTime() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCalculateVideoCaptureTimeRejectsInvalidDuration(t *testing.T) {
	invalidDurations := []float64{0, -1, math.NaN(), math.Inf(1)}
	for _, duration := range invalidDurations {
		if _, err := CalculateVideoCaptureTime(duration); err == nil {
			t.Fatalf("CalculateVideoCaptureTime(%v) expected error", duration)
		}
	}
}

func TestCalculateVideoThumbnailDimensions(t *testing.T) {
	tests := []struct {
		name       string
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{name: "横屏", width: 1920, height: 1080, wantWidth: 300, wantHeight: 169},
		{name: "竖屏", width: 1080, height: 1920, wantWidth: 169, wantHeight: 300},
		{name: "小图不放大", width: 200, height: 100, wantWidth: 200, wantHeight: 100},
		{name: "极窄画面保持最小一像素", width: 1, height: 1000, wantWidth: 1, wantHeight: 300},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height, err := CalculateVideoThumbnailDimensions(test.width, test.height)
			if err != nil {
				t.Fatalf("CalculateVideoThumbnailDimensions() error = %v", err)
			}
			if width != test.wantWidth || height != test.wantHeight {
				t.Fatalf("CalculateVideoThumbnailDimensions() = %dx%d, want %dx%d", width, height, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func TestVideoAndImageThumbnailUseSameJPEGQuality(t *testing.T) {
	if thumbnailJPEGQuality != 90 {
		t.Fatalf("thumbnailJPEGQuality = %d, want 90", thumbnailJPEGQuality)
	}
}
