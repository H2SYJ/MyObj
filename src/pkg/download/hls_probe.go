package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"golang.org/x/time/rate"
)

const hlsMaxPlaylistSize = 8 * 1024 * 1024

var hlsContentTypes = map[string]struct{}{
	"application/vnd.apple.mpegurl": {},
	"application/x-mpegurl":         {},
	"audio/mpegurl":                 {},
	"audio/x-mpegurl":               {},
}

// LooksLikeHLSURL 根据URL路径后缀判断是否为m3u8地址。
func LooksLikeHLSURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && strings.EqualFold(filepath.Ext(parsed.Path), ".m3u8")
}

// DetectHLSContentType 通过安全HEAD请求探测HLS响应类型。
func DetectHLSContentType(ctx context.Context, rawURL, proxyAddress string, limiter *rate.Limiter, headers map[string]string, allowedHosts []string) (bool, error) {
	if err := ValidatePublicHTTPURL(rawURL); err != nil {
		return false, err
	}
	client, err := newHLSHTTPClient(proxyAddress, limiter, headers, allowedHosts)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, &CredentialsRequiredError{StatusCode: resp.StatusCode, URL: rawURL}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		_, ok := hlsContentTypes[contentType]
		return ok, nil
	}
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false, err
	}
	getReq.Header.Set("Range", "bytes=0-0")
	getResp, err := client.Do(getReq)
	if err != nil {
		return false, err
	}
	defer getResp.Body.Close()
	if getResp.StatusCode == http.StatusUnauthorized || getResp.StatusCode == http.StatusForbidden {
		return false, &CredentialsRequiredError{StatusCode: getResp.StatusCode, URL: rawURL}
	}
	if getResp.StatusCode < 200 || getResp.StatusCode >= 400 {
		return false, fmt.Errorf("探测HLS类型失败，状态码: %d", getResp.StatusCode)
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(getResp.Header.Get("Content-Type"), ";")[0]))
	_, ok := hlsContentTypes[contentType]
	return ok, nil
}

// ProbeHLSPlaylist 对播放列表做有界预检，网络失败不会创建任何临时文件。
func ProbeHLSPlaylist(ctx context.Context, rawURL, proxyAddress string, limiter *rate.Limiter, headers map[string]string, allowedHosts []string) error {
	client, err := newHLSHTTPClient(proxyAddress, limiter, headers, allowedHosts)
	if err != nil {
		return err
	}
	data, err := fetchHLSBytes(ctx, client, rawURL, 0, 0, hlsMaxPlaylistSize)
	if err != nil {
		return err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "#EXTM3U") {
		return fmt.Errorf("目标内容不是有效的m3u8播放列表")
	}
	return nil
}

// NormalizeHLSOutputFileName 校验并生成MP4输出文件名。
func NormalizeHLSOutputFileName(requested, rawURL, taskID string) (string, error) {
	name := strings.TrimSpace(requested)
	if name == "" {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			name = filepath.Base(parsed.Path)
		}
		if name == "" || name == "." || name == "/" {
			name = "hls_" + taskID
		}
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") {
		return "", fmt.Errorf("HLS输出文件名不能包含路径或非法字符")
	}
	name = sanitizeFileName(name)
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ".mp4"
	if name == ".mp4" || len([]byte(name)) > 255 {
		return "", fmt.Errorf("HLS输出文件名无效或过长")
	}
	return name, nil
}

func fetchHLSBytes(ctx context.Context, client *http.Client, rawURL string, rangeOffset, rangeLength int64, maxBytes int64) ([]byte, error) {
	if err := ValidatePublicHTTPURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if rangeLength > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeOffset, rangeOffset+rangeLength-1))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &CredentialsRequiredError{StatusCode: resp.StatusCode, URL: rawURL}
	}
	if rangeLength > 0 {
		if resp.StatusCode != http.StatusPartialContent {
			return nil, fmt.Errorf("HLS Byte Range请求未返回206，状态码: %d", resp.StatusCode)
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取HLS资源失败，状态码: %d", resp.StatusCode)
	}
	limit := maxBytes
	if rangeLength > 0 {
		limit = rangeLength
	}
	if limit <= 0 {
		limit = hlsMaxPlaylistSize
	}
	reader := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("HLS资源超过允许大小")
	}
	if rangeLength > 0 && int64(len(data)) != rangeLength {
		return nil, fmt.Errorf("HLS Byte Range长度不一致: 期望%d字节，实际%d字节", rangeLength, len(data))
	}
	return data, nil
}
