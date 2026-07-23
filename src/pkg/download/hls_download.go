package download

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/upload"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type HLSDownloadOptions struct {
	EnableEncryption bool
	SavePath         string
	MaxRetries       int
	MaxConcurrent    int
	FilePassword     string
	RunToken         string
	ReservedSize     int64
	ProxyURL         string
	DownloadLimiter  *rate.Limiter
	RequestHeaders   map[string]string
	HeaderHosts      []string
	OutputFileName   string
	Client           *http.Client
	ReserveSpace     func(requiredSize int64) (int64, error)
}

type hlsProgress struct {
	mu                sync.Mutex
	taskID            string
	runToken          string
	repo              *impl.RepositoryFactory
	totalDuration     float64
	completedDuration float64
	downloadedBytes   int64
	lastBytes         int64
	lastUpdate        time.Time
	reservedSize      int64
	reserveSpace      func(int64) (int64, error)
}

func DownloadHLSWithContext(ctx context.Context, taskID, rawURL, userID, tempDir string, repoFactory *impl.RepositoryFactory, opts *HLSDownloadOptions) (*HTTPDownloadResult, error) {
	if opts == nil {
		opts = &HLSDownloadOptions{SavePath: "/离线下载", MaxRetries: 3, MaxConcurrent: 4}
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.SavePath == "" {
		opts.SavePath = "/离线下载"
	}
	if opts.OutputFileName == "" {
		return nil, fmt.Errorf("HLS输出文件名不能为空")
	}
	client := opts.Client
	if client == nil {
		var err error
		client, err = newHLSHTTPClient(opts.ProxyURL, opts.DownloadLimiter, opts.RequestHeaders, opts.HeaderHosts)
		if err != nil {
			return nil, err
		}
	}

	sessionDir := filepath.Join(tempDir, fmt.Sprintf("hls_%s", taskID))
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("创建HLS临时目录失败: %w", err)
	}
	freshManifest, err := buildHLSManifest(ctx, client, rawURL, opts.OutputFileName)
	if err != nil {
		return nil, err
	}
	manifest, err := loadOrInitializeHLSManifest(sessionDir, freshManifest)
	if err != nil {
		return nil, err
	}
	updated, err := repoFactory.DownloadTask().UpdateIfRunToken(ctx, taskID, opts.RunToken, map[string]interface{}{
		"file_name": opts.OutputFileName, "file_size": 0, "support_range": true,
	})
	if err != nil {
		return nil, fmt.Errorf("更新HLS任务信息失败: %w", err)
	}
	if !updated {
		return nil, context.Canceled
	}

	progress := newHLSProgress(taskID, opts.RunToken, repoFactory, manifest, opts)
	if err := progress.syncInitial(); err != nil {
		return nil, err
	}
	if err := downloadHLSManifest(ctx, client, sessionDir, manifest, progress, opts); err != nil {
		return nil, err
	}
	if err := progress.markPackaging(); err != nil {
		return nil, err
	}
	localPlaylists, err := writeLocalHLSPlaylists(sessionDir, manifest)
	if err != nil {
		return nil, err
	}
	outputPath, err := packageLocalHLS(ctx, sessionDir, opts.OutputFileName, localPlaylists)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(outputPath)
	if err != nil || stat.Size() <= 0 {
		return nil, fmt.Errorf("检查HLS封装结果失败: %w", err)
	}
	if opts.ReserveSpace != nil {
		reserved, reserveErr := opts.ReserveSpace(stat.Size())
		if reserveErr != nil {
			return nil, reserveErr
		}
		opts.ReservedSize = reserved
	}
	uploadData := &upload.FileUploadData{
		TempFilePath: outputPath, FileName: opts.OutputFileName, FileSize: stat.Size(),
		SavePath: opts.SavePath, UserID: userID, IsEnc: opts.EnableEncryption,
		IsChunk: false, FilePassword: opts.FilePassword, ReservedSize: opts.ReservedSize,
	}
	fileID, err := upload.ProcessUploadedFile(uploadData, repoFactory)
	if err != nil {
		return nil, fmt.Errorf("HLS文件入库失败: %w", err)
	}
	if removeErr := os.RemoveAll(sessionDir); removeErr != nil {
		logger.LOG.Warn("清理HLS临时目录失败", "taskID", taskID, "path", sessionDir, "error", removeErr)
	}
	return &HTTPDownloadResult{FileID: fileID, FileName: opts.OutputFileName, FileSize: stat.Size()}, nil
}

func newHLSProgress(taskID, runToken string, repo *impl.RepositoryFactory, manifest *hlsManifest, opts *HLSDownloadOptions) *hlsProgress {
	progress := &hlsProgress{
		taskID: taskID, runToken: runToken, repo: repo, lastUpdate: time.Now(),
		reservedSize: opts.ReservedSize, reserveSpace: opts.ReserveSpace,
	}
	for _, rendition := range manifest.Renditions {
		for _, item := range rendition.Maps {
			if item.Done {
				progress.downloadedBytes += item.Size
			}
		}
		for _, segment := range rendition.Segments {
			progress.totalDuration += segment.Duration
			if segment.Done {
				progress.completedDuration += segment.Duration
				progress.downloadedBytes += segment.Size
			}
		}
	}
	progress.lastBytes = progress.downloadedBytes
	return progress
}

func (p *hlsProgress) complete(size int64, duration float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.downloadedBytes += size
	p.completedDuration += duration
	if p.reserveSpace != nil && p.downloadedBytes > p.reservedSize {
		const block = int64(64 * 1024 * 1024)
		target := ((p.downloadedBytes + block - 1) / block) * block
		reserved, err := p.reserveSpace(target)
		if err != nil {
			return err
		}
		p.reservedSize = reserved
	}
	now := time.Now()
	elapsed := now.Sub(p.lastUpdate).Seconds()
	speed := int64(0)
	if elapsed > 0 {
		speed = int64(float64(p.downloadedBytes-p.lastBytes) / elapsed)
	}
	progressValue := 0
	if p.totalDuration > 0 {
		progressValue = min(95, int(p.completedDuration/p.totalDuration*95))
	}
	updated, err := p.repo.DownloadTask().UpdateIfRunToken(context.Background(), p.taskID, p.runToken, map[string]interface{}{
		"downloaded_size": p.downloadedBytes, "speed": speed, "progress": progressValue,
	})
	if err != nil {
		return err
	}
	if !updated {
		return context.Canceled
	}
	p.lastBytes = p.downloadedBytes
	p.lastUpdate = now
	return nil
}

func (p *hlsProgress) syncInitial() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	progressValue := 0
	if p.totalDuration > 0 {
		progressValue = min(95, int(p.completedDuration/p.totalDuration*95))
	}
	updated, err := p.repo.DownloadTask().UpdateIfRunToken(context.Background(), p.taskID, p.runToken, map[string]interface{}{
		"downloaded_size": p.downloadedBytes, "speed": 0, "progress": progressValue,
	})
	if err != nil {
		return err
	}
	if !updated {
		return context.Canceled
	}
	return nil
}

func (p *hlsProgress) markPackaging() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	updated, err := p.repo.DownloadTask().UpdateIfRunToken(context.Background(), p.taskID, p.runToken, map[string]interface{}{
		"downloaded_size": p.downloadedBytes, "speed": 0, "progress": 99,
	})
	if err != nil {
		return err
	}
	if !updated {
		return context.Canceled
	}
	return nil
}

func downloadHLSManifest(ctx context.Context, client *http.Client, sessionDir string, manifest *hlsManifest, progress *hlsProgress, opts *HLSDownloadOptions) error {
	keys, err := fetchHLSKeys(ctx, client, manifest, opts.MaxRetries)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(sessionDir, hlsManifestFileName)
	var manifestMu sync.Mutex
	for renditionIndex := range manifest.Renditions {
		rendition := &manifest.Renditions[renditionIndex]
		for mapIndex := range rendition.Maps {
			item := &rendition.Maps[mapIndex]
			if item.Done {
				continue
			}
			hash, size, downloadErr := downloadHLSItemWithRetry(ctx, client, sessionDir, item.URL, item.Offset, item.Length, item.LocalName, item.Key, 0, keys, opts.MaxRetries)
			if downloadErr != nil {
				return downloadErr
			}
			item.Done, item.SHA256, item.Size = true, hash, size
			if err := saveHLSManifest(manifestPath, manifest); err != nil {
				return err
			}
			if err := progress.complete(size, 0); err != nil {
				return err
			}
		}
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type job struct{ renditionIndex, segmentIndex int }
	jobs := make(chan job)
	var workers sync.WaitGroup
	var firstErr error
	var errorOnce sync.Once
	workerCount := opts.MaxConcurrent
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				segment := &manifest.Renditions[item.renditionIndex].Segments[item.segmentIndex]
				hash, size, downloadErr := downloadHLSItemWithRetry(workerCtx, client, sessionDir, segment.URL, segment.Offset, segment.Length,
					segment.LocalName, segment.Key, segment.Sequence, keys, opts.MaxRetries)
				if downloadErr != nil {
					errorOnce.Do(func() { firstErr = downloadErr; cancel() })
					continue
				}
				manifestMu.Lock()
				segment.Done, segment.SHA256, segment.Size = true, hash, size
				saveErr := saveHLSManifest(manifestPath, manifest)
				manifestMu.Unlock()
				if saveErr != nil {
					errorOnce.Do(func() { firstErr = saveErr; cancel() })
					continue
				}
				if progressErr := progress.complete(size, segment.Duration); progressErr != nil {
					errorOnce.Do(func() { firstErr = progressErr; cancel() })
				}
			}
		}()
	}
sendLoop:
	for renditionIndex := range manifest.Renditions {
		for segmentIndex := range manifest.Renditions[renditionIndex].Segments {
			if manifest.Renditions[renditionIndex].Segments[segmentIndex].Done {
				continue
			}
			select {
			case <-workerCtx.Done():
				break sendLoop
			case jobs <- job{renditionIndex: renditionIndex, segmentIndex: segmentIndex}:
			}
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func fetchHLSKeys(ctx context.Context, client *http.Client, manifest *hlsManifest, maxRetries int) (map[string][]byte, error) {
	specs := make(map[string]hlsKeySpec)
	for _, rendition := range manifest.Renditions {
		for _, item := range rendition.Maps {
			if item.Key.Method == "AES-128" {
				specs[item.Key.URL] = item.Key
			}
		}
		for _, segment := range rendition.Segments {
			if segment.Key.Method == "AES-128" {
				specs[segment.Key.URL] = segment.Key
			}
		}
	}
	keys := make(map[string][]byte, len(specs))
	for keyURL := range specs {
		data, err := fetchHLSKeyWithRetry(ctx, client, keyURL, maxRetries)
		if err != nil {
			return nil, err
		}
		keys[keyURL] = data
	}
	return keys, nil
}

func fetchHLSKeyWithRetry(ctx context.Context, client *http.Client, keyURL string, maxRetries int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<(attempt-1)) * time.Second):
			}
		}
		data, err := fetchHLSBytes(ctx, client, keyURL, 0, 0, aes.BlockSize)
		if err == nil {
			if len(data) != aes.BlockSize {
				return nil, fmt.Errorf("AES-128密钥长度必须为16字节，实际%d字节", len(data))
			}
			return data, nil
		}
		if IsHLSCredentialsRequired(err) || !isRetryableHLSItemError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("HLS密钥重试%d次后失败: %w", maxRetries, lastErr)
}

func downloadHLSItemWithRetry(ctx context.Context, client *http.Client, sessionDir, rawURL string, offset, length int64, localName string,
	keySpec hlsKeySpec, sequence uint64, keys map[string][]byte, maxRetries int) (string, int64, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return "", 0, ctx.Err()
			case <-time.After(delay):
			}
		}
		hash, size, err := downloadHLSItem(ctx, client, rawURL, offset, length, filepath.Join(sessionDir, localName), keySpec, sequence, keys)
		if err == nil {
			return hash, size, nil
		}
		if IsHLSCredentialsRequired(err) || errors.Is(err, context.Canceled) || !isRetryableHLSItemError(err) {
			return "", 0, err
		}
		lastErr = err
	}
	return "", 0, fmt.Errorf("HLS资源重试%d次后失败: %w", maxRetries, lastErr)
}

func isRetryableHLSItemError(err error) bool {
	var networkError interface{ Timeout() bool }
	if errors.As(err, &networkError) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "状态码: 408") || strings.Contains(message, "状态码: 429") ||
		strings.Contains(message, "状态码: 500") || strings.Contains(message, "状态码: 502") ||
		strings.Contains(message, "状态码: 503") || strings.Contains(message, "状态码: 504")
}

func downloadHLSItem(ctx context.Context, client *http.Client, rawURL string, offset, length int64, destination string,
	keySpec hlsKeySpec, sequence uint64, keys map[string][]byte) (string, int64, error) {
	if err := ValidatePublicHTTPURL(rawURL); err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	if length > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", 0, &CredentialsRequiredError{StatusCode: resp.StatusCode, URL: rawURL}
	}
	if length > 0 {
		if resp.StatusCode != http.StatusPartialContent {
			return "", 0, fmt.Errorf("HLS Byte Range请求未返回206，状态码: %d", resp.StatusCode)
		}
		start, end, _, parseErr := parseContentRange(resp.Header.Get("Content-Range"))
		if parseErr != nil || start != offset || end != offset+length-1 {
			return "", 0, fmt.Errorf("HLS Byte Range响应范围无效: %s", resp.Header.Get("Content-Range"))
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("下载HLS资源失败，状态码: %d", resp.StatusCode)
	}
	inputPath := destination + ".part"
	if keySpec.Method == "AES-128" {
		inputPath += ".enc"
	}
	_ = os.Remove(inputPath)
	output, err := os.OpenFile(inputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", 0, err
	}
	written, copyErr := io.Copy(output, resp.Body)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(inputPath)
		return "", 0, copyErr
	}
	if syncErr != nil || closeErr != nil {
		_ = os.Remove(inputPath)
		if syncErr != nil {
			return "", 0, syncErr
		}
		return "", 0, closeErr
	}
	if length > 0 && written != length {
		_ = os.Remove(inputPath)
		return "", 0, fmt.Errorf("HLS Byte Range长度不一致: 期望%d字节，实际%d字节", length, written)
	}
	if length == 0 && resp.ContentLength >= 0 && written != resp.ContentLength {
		_ = os.Remove(inputPath)
		return "", 0, fmt.Errorf("HLS资源长度不一致: 期望%d字节，实际%d字节", resp.ContentLength, written)
	}
	finalPart := destination + ".part"
	if keySpec.Method == "AES-128" {
		key := keys[keySpec.URL]
		iv, ivErr := hlsAES128IV(keySpec.IV, sequence)
		if ivErr != nil {
			return "", 0, ivErr
		}
		if decryptErr := decryptHLSAES128File(inputPath, finalPart, key, iv); decryptErr != nil {
			_ = os.Remove(inputPath)
			_ = os.Remove(finalPart)
			return "", 0, decryptErr
		}
		_ = os.Remove(inputPath)
	}
	_ = os.Remove(destination)
	if err := os.Rename(finalPart, destination); err != nil {
		_ = os.Remove(finalPart)
		return "", 0, err
	}
	hash, size, err := hashHLSFile(destination)
	return hash, size, err
}

func hlsAES128IV(rawIV string, sequence uint64) ([]byte, error) {
	if strings.TrimSpace(rawIV) == "" {
		iv := make([]byte, aes.BlockSize)
		binary.BigEndian.PutUint64(iv[8:], sequence)
		return iv, nil
	}
	value := strings.TrimPrefix(strings.TrimSpace(rawIV), "0x")
	if len(value) > aes.BlockSize*2 {
		return nil, fmt.Errorf("AES-128 IV长度超过16字节")
	}
	if len(value)%2 != 0 {
		value = "0" + value
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("解析AES-128 IV失败: %w", err)
	}
	iv := make([]byte, aes.BlockSize)
	copy(iv[aes.BlockSize-len(decoded):], decoded)
	return iv, nil
}

func decryptHLSAES128File(inputPath, outputPath string, key, iv []byte) error {
	if len(key) != aes.BlockSize || len(iv) != aes.BlockSize {
		return fmt.Errorf("AES-128密钥或IV长度无效")
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = output.Close()
		if !success {
			_ = os.Remove(outputPath)
		}
	}()
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	decrypter := cipher.NewCBCDecrypter(block, iv)
	cipherBlock := make([]byte, aes.BlockSize)
	var previousPlain []byte
	for {
		_, readErr := io.ReadFull(input, cipherBlock)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("AES-128密文长度不是16字节的整数倍: %w", readErr)
		}
		plainBlock := make([]byte, aes.BlockSize)
		decrypter.CryptBlocks(plainBlock, cipherBlock)
		if previousPlain != nil {
			if _, err := output.Write(previousPlain); err != nil {
				return err
			}
		}
		previousPlain = plainBlock
	}
	if previousPlain == nil {
		return fmt.Errorf("AES-128密文为空")
	}
	padding := int(previousPlain[len(previousPlain)-1])
	if padding <= 0 || padding > aes.BlockSize || !bytes.Equal(previousPlain[len(previousPlain)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return fmt.Errorf("AES-128分片填充无效")
	}
	if _, err := output.Write(previousPlain[:len(previousPlain)-padding]); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

func writeLocalHLSPlaylists(sessionDir string, manifest *hlsManifest) (map[string]string, error) {
	paths := make(map[string]string, len(manifest.Renditions))
	for _, rendition := range manifest.Renditions {
		var content strings.Builder
		content.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
		targetDuration := rendition.TargetDuration
		for _, segment := range rendition.Segments {
			targetDuration = max(targetDuration, uint(segment.Duration+0.999999))
		}
		content.WriteString("#EXT-X-TARGETDURATION:" + strconv.FormatUint(uint64(targetDuration), 10) + "\n")
		content.WriteString("#EXT-X-MEDIA-SEQUENCE:" + strconv.FormatUint(rendition.Sequence, 10) + "\n")
		content.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
		lastMap := hlsNoMap
		for _, segment := range rendition.Segments {
			if segment.MapIndex != hlsNoMap && segment.MapIndex != lastMap {
				content.WriteString("#EXT-X-MAP:URI=\"" + rendition.Maps[segment.MapIndex].LocalName + "\"\n")
				lastMap = segment.MapIndex
			}
			if segment.Discontinuity {
				content.WriteString("#EXT-X-DISCONTINUITY\n")
			}
			content.WriteString("#EXTINF:" + strconv.FormatFloat(segment.Duration, 'f', 6, 64) + ",\n")
			content.WriteString(segment.LocalName + "\n")
		}
		content.WriteString("#EXT-X-ENDLIST\n")
		path := filepath.Join(sessionDir, rendition.Kind+"_local.m3u8")
		if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
			return nil, fmt.Errorf("写入本地HLS播放列表失败: %w", err)
		}
		paths[rendition.Kind] = path
	}
	return paths, nil
}

func packageLocalHLS(ctx context.Context, sessionDir, outputName string, playlists map[string]string) (string, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("未找到ffmpeg，无法封装HLS视频: %w", err)
	}
	hlsDemuxerOptions := ffmpegHLSDemuxerOptions(ctx, ffmpegPath)
	allowSegmentExtensions := bytes.Contains(hlsDemuxerOptions, []byte("allowed_segment_extensions"))
	disableExtensionPicky := bytes.Contains(hlsDemuxerOptions, []byte("extension_picky"))
	videoPlaylist := playlists[hlsVideoRendition]
	if videoPlaylist == "" {
		return "", fmt.Errorf("缺少本地HLS媒体播放列表")
	}
	outputPath := filepath.Join(sessionDir, outputName)
	tempOutput := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".part.mp4"
	_ = os.Remove(tempOutput)
	args := []string{"-v", "error", "-nostdin"}
	args = append(args, localHLSInputArgs(videoPlaylist, allowSegmentExtensions, disableExtensionPicky)...)
	if audioPlaylist := playlists[hlsAudioRendition]; audioPlaylist != "" {
		args = append(args, localHLSInputArgs(audioPlaylist, allowSegmentExtensions, disableExtensionPicky)...)
		args = append(args, "-map", "0:v?", "-map", "1:a:0?")
	} else {
		args = append(args, "-map", "0:v?", "-map", "0:a?")
	}
	args = append(args, "-c", "copy", "-movflags", "+faststart", "-y", tempOutput)
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, ffmpegPath, args...)
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		_ = os.Remove(tempOutput)
		return "", fmt.Errorf("FFmpeg封装HLS失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := validatePackagedHLS(ctx, tempOutput); err != nil {
		_ = os.Remove(tempOutput)
		return "", err
	}
	_ = os.Remove(outputPath)
	if err := os.Rename(tempOutput, outputPath); err != nil {
		_ = os.Remove(tempOutput)
		return "", fmt.Errorf("提交HLS封装结果失败: %w", err)
	}
	return outputPath, nil
}

func ffmpegHLSDemuxerOptions(ctx context.Context, ffmpegPath string) []byte {
	command := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-h", "demuxer=hls")
	output, err := command.CombinedOutput()
	if err != nil {
		return nil
	}
	return output
}

func localHLSInputArgs(playlist string, allowSegmentExtensions, disableExtensionPicky bool) []string {
	args := []string{"-protocol_whitelist", "file", "-allowed_extensions", "ALL"}
	if allowSegmentExtensions {
		args = append(args, "-allowed_segment_extensions", "ALL")
	}
	if disableExtensionPicky {
		args = append(args, "-extension_picky", "0")
	}
	return append(args, "-i", playlist)
}

func validatePackagedHLS(ctx context.Context, path string) error {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return fmt.Errorf("未找到ffprobe，无法验证HLS封装结果: %w", err)
	}
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, ffprobePath, "-v", "error", "-show_entries", "stream=codec_type", "-of", "json", path)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("ffprobe验证HLS封装结果失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return fmt.Errorf("解析ffprobe结果失败: %w", err)
	}
	for _, stream := range result.Streams {
		if stream.CodecType == "video" || stream.CodecType == "audio" {
			return nil
		}
	}
	return fmt.Errorf("HLS封装结果不包含音频或视频流")
}
