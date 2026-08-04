package metadata

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImageProviderExtractsDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "示例.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	img.Set(0, 0, color.White)
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	values, err := (ImageProvider{}).Extract(context.Background(), Input{Path: path, MIME: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"width": "1920", "height": "1080", "resolution": "1080P", "format": "PNG"}
	for _, value := range values {
		delete(want, value.Key)
	}
	if len(want) != 0 {
		t.Fatalf("缺少元数据: %#v", want)
	}
}

func TestExtractMarksOptionalProviderFailurePartial(t *testing.T) {
	result := Extract(context.Background(), Input{Path: "missing.mp4", FileName: "missing.mp4", MIME: "video/mp4"},
		BasicProvider{}, FFProbeProvider{Path: "definitely-missing-ffprobe"})
	if !result.Partial || len(result.Values) == 0 || result.ErrorText() == "" {
		t.Fatalf("预期保留基础元数据并标记部分完成: %#v", result)
	}
}
