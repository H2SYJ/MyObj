package download

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"golang.org/x/time/rate"
)

func TestNormalizeProxyURL(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:7890",
		"https://proxy.example.com",
		"socks5://user:password@localhost:1080/",
		"",
	}
	for _, value := range valid {
		if _, err := NormalizeProxyURL(value); err != nil {
			t.Fatalf("代理地址应合法 %q: %v", value, err)
		}
	}
	invalid := []string{
		"ftp://proxy.example.com:21",
		"http:///missing-host",
		"http://proxy.example.com/path",
		"http://proxy.example.com?mode=1",
	}
	for _, value := range invalid {
		if _, err := NormalizeProxyURL(value); err == nil {
			t.Fatalf("代理地址应被拒绝: %q", value)
		}
	}
}

func TestNetworkPolicyUpdatesSharedLimitersInPlace(t *testing.T) {
	policy := NewNetworkPolicy()
	downloadLimiter := policy.DownloadLimiter()
	uploadLimiter := policy.BTUploadLimiter()
	if err := policy.Apply(NetworkSettings{
		ProxyURL:                   "http://127.0.0.1:7890",
		DownloadSpeedLimitMBPerSec: 2.5,
		BTUploadSpeedLimitMBPerSec: 1.25,
	}); err != nil {
		t.Fatal(err)
	}
	if policy.DownloadLimiter() != downloadLimiter || policy.BTUploadLimiter() != uploadLimiter {
		t.Fatal("更新配置时不应替换活动任务持有的限流器")
	}
	if downloadLimiter.Limit() != rate.Limit(2.5*1024*1024) {
		t.Fatalf("下载限速错误: %v", downloadLimiter.Limit())
	}
	if uploadLimiter.Limit() != rate.Limit(1.25*1024*1024) {
		t.Fatalf("上传限速错误: %v", uploadLimiter.Limit())
	}
	if err := policy.Apply(NetworkSettings{}); err != nil {
		t.Fatal(err)
	}
	if downloadLimiter.Limit() != rate.Inf || uploadLimiter.Limit() != rate.Inf {
		t.Fatal("0应恢复不限速")
	}
}

func TestValidateSpeedLimitRejectsInvalidValues(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		if err := ValidateSpeedLimitMBPerSec(value); err == nil {
			t.Fatalf("应拒绝限速值: %v", value)
		}
	}
}

func TestTorrentPolicyInjectsLimitersWithoutProxy(t *testing.T) {
	policy := NewNetworkPolicy()
	cfg := torrent.NewDefaultClientConfig()
	applySharedTorrentLimiters(cfg, policy.DownloadLimiter(), policy.BTUploadLimiter())
	if cfg.DownloadRateLimiter != policy.DownloadLimiter() || cfg.UploadRateLimiter != policy.BTUploadLimiter() {
		t.Fatal("Torrent客户端未共享全局限流器")
	}
	if cfg.HTTPProxy != nil || cfg.TrackerDialContext != nil || cfg.TrackerListenPacket != nil {
		t.Fatal("Torrent客户端不应注入代理或自定义网络连接")
	}
}

func TestHTTPClientsShareGlobalDownloadLimiter(t *testing.T) {
	policy := NewNetworkPolicy()
	clientA, err := newPublicHTTPClient("", policy.DownloadLimiter())
	if err != nil {
		t.Fatal(err)
	}
	clientB, err := newPublicHTTPClient("", policy.DownloadLimiter())
	if err != nil {
		t.Fatal(err)
	}
	transportA, ok := clientA.Transport.(*rateLimitedRoundTripper)
	if !ok {
		t.Fatal("HTTP客户端未安装限速传输层")
	}
	transportB, ok := clientB.Transport.(*rateLimitedRoundTripper)
	if !ok || transportA.limiter != transportB.limiter || transportA.limiter != policy.DownloadLimiter() {
		t.Fatal("HTTP客户端未共享全局下载限流器")
	}
}

func TestPublicHTTPClientUsesHTTPProxyWithAuthentication(t *testing.T) {
	var proxyAuthorization string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyAuthorization = r.Header.Get("Proxy-Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("代理响应"))
	}))
	defer proxy.Close()

	proxyURL := strings.Replace(proxy.URL, "http://", "http://user:password@", 1)
	client, err := newPublicHTTPClient(proxyURL, rate.NewLimiter(rate.Inf, 64*1024))
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 0 {
		t.Fatalf("客户端不应设置覆盖正文传输的总超时: %v", client.Timeout)
	}
	resp, err := client.Get("http://1.1.1.1/file")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "代理响应" {
		t.Fatalf("代理响应错误: %q", body)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:password"))
	if proxyAuthorization != wantAuth {
		t.Fatalf("代理认证头错误: got=%q want=%q", proxyAuthorization, wantAuth)
	}
}

func TestProxyClientStillRejectsPrivateRedirects(t *testing.T) {
	client, err := newPublicHTTPClient("http://127.0.0.1:7890", nil)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("代理模式下仍应拒绝重定向到私网地址")
	}
}

func TestPublicHTTPClientUsesSOCKS5Proxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		defer conn.Close()
		serverErr <- serveSOCKS5HTTP(conn)
	}()

	client, err := newPublicHTTPClient("socks5://"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://1.1.1.1/file", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "SOCKS5响应" {
		t.Fatalf("SOCKS5响应错误: %q", body)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func serveSOCKS5HTTP(conn net.Conn) error {
	reader := bufio.NewReader(conn)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return err
	}
	requestHeader := make([]byte, 4)
	if _, err := io.ReadFull(reader, requestHeader); err != nil {
		return err
	}
	var addressLength int
	switch requestHeader[3] {
	case 1:
		addressLength = 4
	case 3:
		length, err := reader.ReadByte()
		if err != nil {
			return err
		}
		addressLength = int(length)
	case 4:
		addressLength = 16
	default:
		return fmt.Errorf("未知SOCKS5地址类型: %d", requestHeader[3])
	}
	if _, err := io.CopyN(io.Discard, reader, int64(addressLength+2)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return err
	}
	request, err := http.ReadRequest(reader)
	if err != nil {
		return err
	}
	_ = request.Body.Close()
	_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 12\r\nConnection: close\r\n\r\nSOCKS5响应")
	return err
}
