package download

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPackageLocalHLSWithFFmpeg(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("当前测试环境未安装ffmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("当前测试环境未安装ffprobe")
	}
	dir := t.TempDir()
	playlist := filepath.Join(dir, "fixture.m3u8")
	generate := exec.Command(ffmpegPath,
		"-v", "error", "-nostdin",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100",
		"-t", "1.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-f", "hls", "-hls_time", "0.5", "-hls_playlist_type", "vod", playlist,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("生成HLS测试资源失败: %v: %s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outputPath, err := packageLocalHLS(ctx, dir, "result.mp4", map[string]string{hlsVideoRendition: playlist})
	if err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(outputPath)
	if err != nil || stat.Size() <= 0 {
		t.Fatalf("HLS封装结果无效: %v", err)
	}
}
