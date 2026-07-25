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
	"time"
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

func TestBuildHLSManifestRecursivelyLoadsSelectedChildPlaylists(t *testing.T) {
	base := "https://93.184.216.34/"
	fixtures := map[string]string{
		base + "root.m3u8":         "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=4000000,RESOLUTION=1920x1080\nlevel-1.m3u8\n",
		base + "level-1.m3u8":      "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"中文\",DEFAULT=YES,URI=\"audio-master.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=3000000,AUDIO=\"audio\"\nlevel-2.m3u8\n",
		base + "level-2.m3u8":      "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nvideo.ts\n#EXT-X-ENDLIST\n",
		base + "audio-master.m3u8": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=128000\naudio-media.m3u8\n",
		base + "audio-media.m3u8":  "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\naudio.aac\n#EXT-X-ENDLIST\n",
	}
	manifest, err := buildHLSManifest(context.Background(), newHLSFixtureClient(fixtures, 0), base+"root.m3u8", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Renditions) != 2 || manifest.Renditions[0].PlaylistURL != base+"level-2.m3u8" ||
		manifest.Renditions[1].PlaylistURL != base+"audio-media.m3u8" {
		t.Fatalf("未递归解析到最终视频和音轨清单: %#v", manifest.Renditions)
	}
}

func TestBuildHLSManifestRejectsPlaylistCycleAndDepthOverflow(t *testing.T) {
	base := "https://93.184.216.34/"
	cycle := map[string]string{
		base + "a.m3u8?token=old":     "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nb.m3u8?token=current\n",
		base + "b.m3u8?token=current": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\na.m3u8?token=new\n",
	}
	if _, err := buildHLSManifest(context.Background(), newHLSFixtureClient(cycle, 0), base+"a.m3u8?token=old", "video.mp4"); err == nil || !strings.Contains(err.Error(), "循环引用") {
		t.Fatalf("循环播放列表应被拒绝: %v", err)
	}
	deep := make(map[string]string)
	for index := 0; index <= hlsMaxPlaylistDepth; index++ {
		deep[fmt.Sprintf("%s%d.m3u8", base, index)] = fmt.Sprintf("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\n%d.m3u8\n", index+1)
	}
	if _, err := buildHLSManifest(context.Background(), newHLSFixtureClient(deep, 0), base+"0.m3u8", "video.mp4"); err == nil || !strings.Contains(err.Error(), "嵌套") {
		t.Fatalf("超深播放列表应被拒绝: %v", err)
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

func TestHLSManifestCompletionReuseAndStructureChange(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
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
			Version: hlsManifestVersion, SourceURL: "https://93.184.216.34/index.m3u8", OutputName: "video.mp4", CapturedAt: time.Now().UTC(),
			Renditions: []hlsRendition{{Kind: hlsVideoRendition, Sequence: 0, Segments: segments}},
		}
	}

	initial := newManifest("https://93.184.216.34/segment.ts?token=old", 1)
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

	resumed := newManifest("https://93.184.216.34/segment.ts?token=new", 1)
	if !hlsManifestStructureMatches(initial, resumed) {
		t.Fatal("只有签名参数变化时结构应保持一致")
	}
	copyHLSCompletion(initial, resumed)
	validateHLSCompletedFiles(sessionDir, resumed)
	if !resumed.Renditions[0].Segments[0].Done || !strings.Contains(resumed.Renditions[0].Segments[0].URL, "token=new") {
		t.Fatalf("签名URL更新后未复用已完成分片: %#v", resumed.Renditions[0].Segments[0])
	}
	if err := os.Remove(segmentPath); err != nil {
		t.Fatal(err)
	}
	missing := newManifest("https://93.184.216.34/segment.ts?token=newer", 1)
	copyHLSCompletion(initial, missing)
	validateHLSCompletedFiles(sessionDir, missing)
	if missing.Renditions[0].Segments[0].Done {
		t.Fatal("已完成分片缺失时不应继续标记为完成")
	}

	changed := newManifest("https://93.184.216.34/segment.ts?token=latest", 2)
	if hlsManifestStructureMatches(initial, changed) {
		t.Fatal("分片结构变化后不应复用旧完成状态")
	}
}

func TestHLSCredentialsRequired(t *testing.T) {
	client := newHLSFixtureClient(map[string]string{"https://93.184.216.34/a.m3u8": ""}, http.StatusUnauthorized)
	_, err := fetchHLSBytes(context.Background(), client, "https://93.184.216.34/a.m3u8", 0, 0, hlsMaxPlaylistSize)
	if !IsHLSCredentialsRequired(err) {
		t.Fatalf("401应转换为凭据待更新错误: %v", err)
	}
}

func TestPreparedHLSSnapshotUsesChildManifestAndEncryptedCachedKey(t *testing.T) {
	base := "https://93.184.216.34/"
	key := []byte("0123456789abcdef")
	plain := []byte("快照下载不再访问远程m3u8和密钥")
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := hlsAES128IV("", 0)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	fixtures := map[string]string{
		base + "master.m3u8": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nmedia.m3u8\n",
		base + "media.m3u8":  "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n",
		base + "key.bin":     string(key),
	}
	tempDir := t.TempDir()
	prepared, err := PrepareHLSSnapshot(context.Background(), "task", base+"master.m3u8", "user", "video.mp4", tempDir,
		"snapshot-test-secret", &HLSPrepareOptions{Client: newHLSFixtureClient(fixtures, 0), MaxRetries: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	keyFile, err := os.ReadFile(filepath.Join(prepared.finalDir, hlsKeyCacheFileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(keyFile, key) || bytes.Contains(keyFile, []byte(base+"key.bin")) {
		t.Fatal("HLS密钥缓存泄露了明文密钥或地址")
	}
	manifest, err := loadPreparedHLSManifest(prepared.finalDir, base+"master.m3u8", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := loadHLSKeyCache(prepared.finalDir, "snapshot-test-secret", "task", "user", manifest)
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]string, 0)
	segmentClient := &http.Client{Transport: hlsRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		if req.URL.String() != base+"segment.ts" {
			return &http.Response{StatusCode: http.StatusGone, Body: io.NopCloser(strings.NewReader("expired")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(ciphertext)), Header: make(http.Header), ContentLength: int64(len(ciphertext))}, nil
	})}
	opts := &HLSDownloadOptions{MaxRetries: 0, MaxConcurrent: 1, ProgressReporter: func(context.Context, int64, int64, int) (bool, error) {
		return true, nil
	}}
	progress := newHLSProgress(context.Background(), "task", "run", nil, manifest, opts)
	progress.sampleInterval = time.Hour
	if err := downloadHLSManifest(context.Background(), segmentClient, prepared.finalDir, manifest, keys, progress, opts); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0] != base+"segment.ts" {
		t.Fatalf("worker访问了快照之外的清单或密钥: %v", requests)
	}
	result, err := os.ReadFile(filepath.Join(prepared.finalDir, manifest.Renditions[0].Segments[0].LocalName))
	if err != nil || !bytes.Equal(result, plain) {
		t.Fatalf("缓存密钥解密结果错误: %q %v", result, err)
	}
}

func TestPrepareHLSSnapshotFailureRemovesStagingDirectory(t *testing.T) {
	base := "https://93.184.216.34/"
	fixtures := map[string]string{
		base + "master.m3u8": "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nmissing.m3u8\n",
	}
	tempDir := t.TempDir()
	if _, err := PrepareHLSSnapshot(context.Background(), "failed", base+"master.m3u8", "user", "video.mp4", tempDir,
		"snapshot-test-secret", &HLSPrepareOptions{Client: newHLSFixtureClient(fixtures, 0), MaxRetries: 0}); err == nil {
		t.Fatal("子级清单缺失时不应生成HLS快照")
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("HLS快照失败后残留临时目录: %v", entries)
	}
}

func TestLoadPreparedHLSManifestRejectsHistoricalTask(t *testing.T) {
	_, err := loadPreparedHLSManifest(t.TempDir(), "https://93.184.216.34/a.m3u8", "video.mp4")
	if err == nil || !strings.Contains(err.Error(), "任务缺少HLS快照") {
		t.Fatalf("历史无快照任务应直接失败: %v", err)
	}
}
