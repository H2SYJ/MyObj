package download

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newHLSFixtureClient(fixtures map[string]string, status int) *http.Client {
	return &http.Client{Transport: hlsRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, found := fixtures[req.URL.String()]
		responseStatus := status
		if responseStatus == 0 {
			responseStatus = http.StatusOK
		}
		if !found {
			responseStatus = http.StatusNotFound
		}
		return &http.Response{StatusCode: responseStatus, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func TestBuildHLSManifestSelectsHighestVariantAndDefaultAudio(t *testing.T) {
	base := "https://93.184.216.34/"
	fixtures := map[string]string{
		base + "master.m3u8": "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"中文\",DEFAULT=YES,AUTOSELECT=YES,URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,AVERAGE-BANDWIDTH=900000,RESOLUTION=640x360,AUDIO=\"audio\"\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=4000000,AVERAGE-BANDWIDTH=3500000,RESOLUTION=1920x1080,AUDIO=\"audio\"\nhigh.m3u8\n",
		base + "high.m3u8":   "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:10\n#EXTINF:4.0,\nvideo/10.ts\n#EXTINF:4.0,\nvideo/11.ts\n#EXT-X-ENDLIST\n",
		base + "audio.m3u8":  "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\naudio/0.aac\n#EXTINF:4.0,\naudio/1.aac\n#EXT-X-ENDLIST\n",
	}
	manifest, err := buildHLSManifest(context.Background(), newHLSFixtureClient(fixtures, 0), base+"master.m3u8", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Renditions) != 2 || manifest.Renditions[0].PlaylistURL != base+"high.m3u8" || manifest.Renditions[1].Kind != hlsAudioRendition {
		t.Fatalf("Master选择结果错误: %#v", manifest.Renditions)
	}
	if manifest.Renditions[0].Segments[0].URL != base+"video/10.ts" {
		t.Fatalf("相对分片URL解析错误: %s", manifest.Renditions[0].Segments[0].URL)
	}
}

func TestBuildHLSManifestRejectsLiveAndDRM(t *testing.T) {
	base := "https://93.184.216.34/"
	fixtures := map[string]string{
		base + "live.m3u8": "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\n0.ts\n",
		base + "drm.m3u8":  "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n#EXTINF:4,\n0.ts\n#EXT-X-ENDLIST\n",
	}
	for _, name := range []string{"live.m3u8", "drm.m3u8"} {
		if _, err := buildHLSManifest(context.Background(), newHLSFixtureClient(fixtures, 0), base+name, "video.mp4"); err == nil {
			t.Fatalf("%s应被拒绝", name)
		}
	}
}

func TestBuildHLSManifestPropagatesKeyAndImplicitByteRange(t *testing.T) {
	base := "https://93.184.216.34/"
	fixtures := map[string]string{
		base + "range.m3u8": "#EXTM3U\n#EXT-X-VERSION:4\n#EXT-X-TARGETDURATION:4\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXT-X-BYTERANGE:16@0\n#EXTINF:4,\nmedia.bin\n#EXT-X-BYTERANGE:16\n#EXTINF:4,\nmedia.bin\n#EXT-X-ENDLIST\n",
	}
	manifest, err := buildHLSManifest(context.Background(), newHLSFixtureClient(fixtures, 0), base+"range.m3u8", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	segments := manifest.Renditions[0].Segments
	if len(segments) != 2 || segments[1].Offset != 16 || segments[1].Length != 16 {
		t.Fatalf("隐式Byte Range解析错误: %#v", segments)
	}
	if segments[0].Key.Method != "AES-128" || segments[1].Key.Method != "AES-128" {
		t.Fatalf("AES-128密钥未传播到后续分片: %#v", segments)
	}
}

func TestBuildHLSManifestParsesFMP4Map(t *testing.T) {
	base := "https://93.184.216.34/"
	fixtures := map[string]string{
		base + "fmp4.m3u8": "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-MAP:URI=\"init.mp4\",BYTERANGE=\"100@0\"\n#EXTINF:4,\nsegment.m4s\n#EXT-X-ENDLIST\n",
	}
	manifest, err := buildHLSManifest(context.Background(), newHLSFixtureClient(fixtures, 0), base+"fmp4.m3u8", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	rendition := manifest.Renditions[0]
	if len(rendition.Maps) != 1 || rendition.Maps[0].URL != base+"init.mp4" ||
		rendition.Maps[0].Offset != 0 || rendition.Maps[0].Length != 100 || rendition.Segments[0].MapIndex != 0 {
		t.Fatalf("EXT-X-MAP解析错误: %#v", rendition)
	}
}

func TestHLSAES128DecryptAndIV(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv, err := hlsAES128IV("", 42)
	if err != nil {
		t.Fatal(err)
	}
	explicitIV, err := hlsAES128IV("0x0000000000000000000000000000002a", 0)
	if err != nil || !bytes.Equal(explicitIV, iv) {
		t.Fatalf("显式AES-128 IV解析错误: %x %v", explicitIV, err)
	}
	plain := []byte("这是一个AES-128 HLS测试分片")
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, _ := aes.NewCipher(key)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	input := filepath.Join(t.TempDir(), "segment.enc")
	output := filepath.Join(t.TempDir(), "segment.bin")
	if err := os.WriteFile(input, ciphertext, 0644); err != nil {
		t.Fatal(err)
	}
	if err := decryptHLSAES128File(input, output, key, iv); err != nil {
		t.Fatal(err)
	}
	decrypted, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(decrypted, plain) {
		t.Fatalf("AES-128解密结果错误: %q %v", decrypted, err)
	}
}

func TestHLSManifestResumeAndStructureReset(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "session")
	newManifest := func(segmentURL string, segmentCount int) *hlsManifest {
		segments := make([]hlsSegment, segmentCount)
		for index := range segments {
			url := segmentURL
			if index > 0 {
				url = strings.Replace(segmentURL, "segment.ts", "segment-2.ts", 1)
			}
			segments[index] = hlsSegment{
				Sequence: uint64(index), URL: url, URLIdentity: stableHLSURLIdentity(url),
				Duration: 4, LocalName: fmt.Sprintf("video_segment_%05d.bin", index), MapIndex: hlsNoMap,
			}
		}
		return &hlsManifest{
			Version: hlsManifestVersion, SourceURL: "https://93.184.216.34/index.m3u8", OutputName: "video.mp4",
			Renditions: []hlsRendition{{Kind: hlsVideoRendition, Sequence: 0, Segments: segments}},
		}
	}

	initial, err := loadOrInitializeHLSManifest(sessionDir, newManifest("https://93.184.216.34/segment.ts?token=old", 1))
	if err != nil {
		t.Fatal(err)
	}
	segmentPath := filepath.Join(sessionDir, initial.Renditions[0].Segments[0].LocalName)
	if err := os.WriteFile(segmentPath, []byte("completed segment"), 0644); err != nil {
		t.Fatal(err)
	}
	hash, size, err := hashHLSFile(segmentPath)
	if err != nil {
		t.Fatal(err)
	}
	initial.Renditions[0].Segments[0].Done = true
	initial.Renditions[0].Segments[0].SHA256 = hash
	initial.Renditions[0].Segments[0].Size = size
	if err := saveHLSManifest(filepath.Join(sessionDir, hlsManifestFileName), initial); err != nil {
		t.Fatal(err)
	}

	resumed, err := loadOrInitializeHLSManifest(sessionDir, newManifest("https://93.184.216.34/segment.ts?token=new", 1))
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Renditions[0].Segments[0].Done || !strings.Contains(resumed.Renditions[0].Segments[0].URL, "token=new") {
		t.Fatalf("签名URL更新后未复用已完成分片: %#v", resumed.Renditions[0].Segments[0])
	}
	if err := os.Remove(segmentPath); err != nil {
		t.Fatal(err)
	}
	missing, err := loadOrInitializeHLSManifest(sessionDir, newManifest("https://93.184.216.34/segment.ts?token=newer", 1))
	if err != nil {
		t.Fatal(err)
	}
	if missing.Renditions[0].Segments[0].Done {
		t.Fatal("已完成分片缺失时不应继续标记为完成")
	}

	orphanPath := filepath.Join(sessionDir, "orphan.bin")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	reset, err := loadOrInitializeHLSManifest(sessionDir, newManifest("https://93.184.216.34/segment.ts?token=latest", 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(reset.Renditions[0].Segments) != 2 {
		t.Fatalf("结构变化后清单未重建: %#v", reset)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("结构变化后旧临时文件未清理: %v", err)
	}
}

func TestHLSCredentialsRequired(t *testing.T) {
	client := newHLSFixtureClient(map[string]string{"https://93.184.216.34/a.m3u8": ""}, http.StatusUnauthorized)
	_, err := fetchHLSBytes(context.Background(), client, "https://93.184.216.34/a.m3u8", 0, 0, hlsMaxPlaylistSize)
	if !IsHLSCredentialsRequired(err) {
		t.Fatalf("401应转换为凭据待更新错误: %v", err)
	}
}
