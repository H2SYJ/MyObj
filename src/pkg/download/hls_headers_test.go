package download

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type hlsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f hlsRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHLSRequestHeadersEncryptRoundTrip(t *testing.T) {
	headers, hosts, err := NormalizeHLSRequestConfig("https://93.184.216.34/video/index.m3u8", map[string]string{
		"authorization": "Bearer secret",
		"Referer":       "https://example.com/",
	}, []string{"cdn.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := EncryptHLSRequestHeaders("01234567890123456789012345678901", "task", "user", headers)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || strings.Contains(encrypted, "secret") {
		t.Fatalf("请求头未正确加密: %s", encrypted)
	}
	decrypted, err := DecryptHLSRequestHeaders("01234567890123456789012345678901", "task", "user", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted["Authorization"] != "Bearer secret" || len(hosts) != 2 {
		t.Fatalf("请求头或主机恢复错误: %#v %#v", decrypted, hosts)
	}
	if _, err := DecryptHLSRequestHeaders("different-secret-0123456789012345", "task", "user", encrypted); err == nil {
		t.Fatal("使用不同服务端密钥时不应解密成功")
	}
}

func TestHLSRequestHeaderValidation(t *testing.T) {
	if _, _, err := NormalizeHLSRequestConfig("https://93.184.216.34/a.m3u8", map[string]string{"Range": "bytes=0-1"}, nil); err == nil {
		t.Fatal("不应允许覆盖下载器管理的Range头")
	}
	if _, _, err := NormalizeHLSRequestConfig("https://93.184.216.34/a.m3u8", map[string]string{"X-Test": "a\r\nb"}, nil); err == nil {
		t.Fatal("不应允许请求头注入换行符")
	}
	if _, _, err := NormalizeHLSRequestConfig("https://93.184.216.34/a.m3u8", nil, []string{"*.example.com"}); err == nil {
		t.Fatal("不应允许主机通配符")
	}
}

func TestHLSHeaderRoundTripperScopesAllCustomHeaders(t *testing.T) {
	seen := make(map[string]string)
	base := hlsRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen[req.URL.Hostname()] = req.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})
	transport := &hlsHeaderRoundTripper{
		base: base, headers: map[string]string{"Authorization": "Bearer secret"},
		allowedHosts: map[string]struct{}{"allowed.example.com": {}},
	}
	client := &http.Client{Transport: transport}
	for _, rawURL := range []string{"https://allowed.example.com/a", "https://blocked.example.com/a"} {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
		if _, err := client.Do(req); err != nil {
			t.Fatal(err)
		}
	}
	if seen["allowed.example.com"] != "Bearer secret" || seen["blocked.example.com"] != "" {
		t.Fatalf("请求头主机作用域错误: %#v", seen)
	}
}

func TestHeaderRoundTripperStripsRedirectCopiedCredentials(t *testing.T) {
	base := hlsRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "blocked.example.com" && req.Header.Get("Cookie") != "" {
			t.Fatal("跨主机重定向请求携带了插件Cookie")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})
	transport := &hlsHeaderRoundTripper{base: base, headers: map[string]string{"Cookie": "session=secret"}, allowedHosts: map[string]struct{}{"allowed.example.com": {}}}
	req, _ := http.NewRequest(http.MethodGet, "https://blocked.example.com/file", nil)
	// 模拟net/http从上一跳复制过来的敏感头。
	req.Header.Set("Cookie", "session=secret")
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
}

func TestRequestHeaderRejectsPrefixesAndExtraIP(t *testing.T) {
	if _, _, err := NormalizeRequestConfig("https://93.184.216.34/file", map[string]string{"X-Forwarded-Custom": "bad"}, nil); err == nil {
		t.Fatal("不应允许X-Forwarded-*请求头")
	}
	if _, _, err := NormalizeRequestConfig("https://93.184.216.34/file", map[string]string{"Cookie": "ok"}, []string{"8.8.8.8"}); err == nil {
		t.Fatal("额外白名单只允许精确域名，不允许IP")
	}
	if _, _, err := NormalizeRequestConfig("https://93.184.216.34/file", map[string]string{"If-None-Match": "etag"}, nil); err != nil {
		t.Fatalf("业务条件头不应被扩大禁止范围: %v", err)
	}
}
