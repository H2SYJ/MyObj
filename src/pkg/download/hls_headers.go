package download

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/time/rate"
)

const (
	hlsMaxRequestHeaders = 32
	hlsMaxHeaderHosts    = 32
	hlsMaxHeaderBytes    = 32 * 1024
)

var blockedHLSHeaders = map[string]struct{}{
	"accept-encoding": {}, "connection": {}, "content-length": {}, "forwarded": {},
	"host": {}, "if-match": {}, "if-modified-since": {}, "if-none-match": {},
	"if-range": {}, "if-unmodified-since": {}, "keep-alive": {}, "proxy-authenticate": {},
	"proxy-authorization": {}, "proxy-connection": {}, "range": {}, "te": {},
	"trailer": {}, "transfer-encoding": {}, "upgrade": {}, "x-forwarded-for": {},
	"x-forwarded-host": {}, "x-forwarded-proto": {},
}

// NormalizeHLSRequestConfig 校验并规范化HLS自定义请求头和精确主机白名单。
func NormalizeHLSRequestConfig(sourceURL string, rawHeaders map[string]string, extraHosts []string) (map[string]string, []string, error) {
	if len(rawHeaders) > hlsMaxRequestHeaders {
		return nil, nil, fmt.Errorf("自定义请求头不能超过%d个", hlsMaxRequestHeaders)
	}
	if len(extraHosts) > hlsMaxHeaderHosts {
		return nil, nil, fmt.Errorf("额外请求头主机不能超过%d个", hlsMaxHeaderHosts)
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, nil, fmt.Errorf("HLS地址缺少有效主机名")
	}
	headers := make(map[string]string, len(rawHeaders))
	totalBytes := 0
	seenHeaders := make(map[string]struct{}, len(rawHeaders))
	for name, value := range rawHeaders {
		name = strings.TrimSpace(name)
		if !validHTTPHeaderName(name) {
			return nil, nil, fmt.Errorf("无效的请求头名称: %s", name)
		}
		lowerName := strings.ToLower(name)
		if _, blocked := blockedHLSHeaders[lowerName]; blocked || strings.HasPrefix(lowerName, "proxy-") {
			return nil, nil, fmt.Errorf("请求头%s由下载器管理，不能自定义", name)
		}
		if _, exists := seenHeaders[lowerName]; exists {
			return nil, nil, fmt.Errorf("请求头名称重复: %s", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, nil, fmt.Errorf("请求头%s的值不能包含换行符", name)
		}
		totalBytes += len(name) + len(value)
		if totalBytes > hlsMaxHeaderBytes {
			return nil, nil, fmt.Errorf("自定义请求头总大小不能超过32 KiB")
		}
		canonicalName := http.CanonicalHeaderKey(name)
		headers[canonicalName] = strings.TrimSpace(value)
		seenHeaders[lowerName] = struct{}{}
	}

	hostSet := map[string]struct{}{strings.ToLower(parsed.Hostname()): {}}
	for _, rawHost := range extraHosts {
		host, hostErr := normalizeExactHost(rawHost)
		if hostErr != nil {
			return nil, nil, hostErr
		}
		hostSet[host] = struct{}{}
	}
	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
	}
	return headers, hosts, nil
}

func normalizeExactHost(rawHost string) (string, error) {
	rawHost = strings.TrimSpace(strings.ToLower(rawHost))
	if rawHost == "" || strings.ContainsAny(rawHost, "*/\\?#@") {
		return "", fmt.Errorf("请求头主机必须是精确主机名，不能包含通配符、路径或端口: %s", rawHost)
	}
	parsed, err := url.Parse("https://" + rawHost)
	if err != nil || parsed.Hostname() == "" || parsed.Port() != "" || parsed.Hostname() != rawHost {
		return "", fmt.Errorf("无效的请求头主机: %s", rawHost)
	}
	return rawHost, nil
}

func validHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char > unicode.MaxASCII || !(unicode.IsLetter(char) || unicode.IsDigit(char) || strings.ContainsRune("!#$%&'*+-.^_`|~", char)) {
			return false
		}
	}
	return true
}

// EncodeHLSHeaderHosts 将已规范化的主机列表编码为UTF-8 JSON。
func EncodeHLSHeaderHosts(hosts []string) (string, error) {
	data, err := json.Marshal(hosts)
	if err != nil {
		return "", fmt.Errorf("编码请求头主机失败: %w", err)
	}
	return string(data), nil
}

// DecodeHLSHeaderHosts 解码请求头主机列表。
func DecodeHLSHeaderHosts(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var hosts []string
	if err := json.Unmarshal([]byte(value), &hosts); err != nil {
		return nil, fmt.Errorf("解码请求头主机失败: %w", err)
	}
	return hosts, nil
}

// EncryptHLSRequestHeaders 使用服务端密钥加密HLS请求头。
func EncryptHLSRequestHeaders(secret, taskID, userID string, headers map[string]string) (string, error) {
	if len(headers) == 0 {
		return "", nil
	}
	plaintext, err := json.Marshal(headers)
	if err != nil {
		return "", fmt.Errorf("编码HLS请求头失败: %w", err)
	}
	aead, err := newHLSHeaderAEAD(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("生成HLS请求头随机数失败: %w", err)
	}
	aad := []byte(taskID + "\x00" + userID)
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	payload := append(nonce, ciphertext...)
	return "v1:" + base64.RawStdEncoding.EncodeToString(payload), nil
}

// DecryptHLSRequestHeaders 解密HLS请求头。
func DecryptHLSRequestHeaders(secret, taskID, userID, encrypted string) (map[string]string, error) {
	if encrypted == "" {
		return nil, nil
	}
	if !strings.HasPrefix(encrypted, "v1:") {
		return nil, fmt.Errorf("不支持的HLS请求头密文版本")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encrypted, "v1:"))
	if err != nil {
		return nil, fmt.Errorf("解码HLS请求头密文失败: %w", err)
	}
	aead, err := newHLSHeaderAEAD(secret)
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize() {
		return nil, fmt.Errorf("HLS请求头密文长度无效")
	}
	nonce, ciphertext := payload[:aead.NonceSize()], payload[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(taskID+"\x00"+userID))
	if err != nil {
		return nil, fmt.Errorf("HLS请求头解密失败: %w", err)
	}
	var headers map[string]string
	if err := json.Unmarshal(plaintext, &headers); err != nil {
		return nil, fmt.Errorf("解析HLS请求头失败: %w", err)
	}
	return headers, nil
}

func newHLSHeaderAEAD(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, fmt.Errorf("服务端认证密钥为空，无法保护HLS请求头")
	}
	reader := hkdf.New(sha256.New, []byte(secret), []byte("myobj-hls-header-salt-v1"), []byte("offline-download-request-headers-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("派生HLS请求头密钥失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建HLS请求头加密器失败: %w", err)
	}
	return cipher.NewGCM(block)
}

func newHLSHTTPClient(proxyAddress string, limiter *rate.Limiter, headers map[string]string, allowedHosts []string) (*http.Client, error) {
	client, err := newPublicHTTPClient(proxyAddress, limiter)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		allowed[strings.ToLower(host)] = struct{}{}
	}
	client.Transport = &hlsHeaderRoundTripper{base: client.Transport, headers: headers, allowedHosts: allowed}
	return client, nil
}

type hlsHeaderRoundTripper struct {
	base         http.RoundTripper
	headers      map[string]string
	allowedHosts map[string]struct{}
}

func (t *hlsHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, allowed := t.allowedHosts[strings.ToLower(req.URL.Hostname())]; allowed {
		cloned := req.Clone(req.Context())
		cloned.Header = req.Header.Clone()
		for name, value := range t.headers {
			cloned.Header.Set(name, value)
		}
		req = cloned
	}
	return t.base.RoundTrip(req)
}
