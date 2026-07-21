package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

var blockedPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// ValidatePublicHTTPURL 校验离线下载地址只访问公网HTTP/HTTPS资源。
func ValidatePublicHTTPURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("下载地址格式错误: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("仅支持HTTP和HTTPS下载")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("下载地址缺少主机名")
	}
	if parsed.User != nil {
		return fmt.Errorf("下载地址不能包含用户名或密码")
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") {
		return fmt.Errorf("不允许访问本机地址")
	}
	return validatePublicHost(context.Background(), parsed.Hostname())
}

// RedactURLForLog 移除查询参数、片段和用户信息，避免日志泄露临时签名或凭据。
func RedactURLForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

// RedactErrorForLog 清除net/http错误中可能携带的完整查询参数。
func RedactErrorForLog(err error) string {
	if err == nil {
		return ""
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Sprintf("%s %s: %v", urlErr.Op, RedactURLForLog(urlErr.URL), urlErr.Err)
	}
	return err.Error()
}

func validatePublicHost(ctx context.Context, host string) error {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("解析下载地址失败: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("下载地址没有可用IP")
	}
	for _, address := range addresses {
		if !isPublicDownloadIP(address.IP) {
			return fmt.Errorf("不允许访问非公网地址: %s", address.IP.String())
		}
	}
	return nil
}

func isPublicDownloadIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	for _, prefix := range blockedPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func newPublicHTTPClient(proxyAddress string, limiter *rate.Limiter) (*http.Client, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	proxyAddress, err := NormalizeProxyURL(proxyAddress)
	if err != nil {
		return nil, err
	}
	if proxyAddress == "" {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return nil, fmt.Errorf("解析目标地址失败: %w", splitErr)
			}
			addresses, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
			if lookupErr != nil {
				return nil, fmt.Errorf("解析目标地址失败: %w", lookupErr)
			}
			for _, item := range addresses {
				if !isPublicDownloadIP(item.IP) {
					continue
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
			}
			return nil, fmt.Errorf("目标地址没有可用公网IP")
		}
	} else {
		proxyURL, parseErr := url.Parse(proxyAddress)
		if parseErr != nil {
			return nil, fmt.Errorf("解析代理地址失败: %w", parseErr)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		// 代理可以部署在本机或内网，目标地址已在请求及重定向阶段单独校验。
		transport.DialContext = dialer.DialContext
	}

	var roundTripper http.RoundTripper = transport
	if limiter != nil {
		roundTripper = &rateLimitedRoundTripper{base: transport, limiter: limiter}
	}
	return &http.Client{
		Transport: roundTripper,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			return ValidatePublicHTTPURL(req.URL.String())
		},
	}, nil
}

// NewPublicHTTPClient 为插件等受控调用方提供与离线下载一致的公网访问策略。
func NewPublicHTTPClient(proxyAddress string, limiter *rate.Limiter) (*http.Client, error) {
	return newPublicHTTPClient(proxyAddress, limiter)
}

type rateLimitedRoundTripper struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		resp.Body = &rateLimitedReadCloser{
			ReadCloser: resp.Body,
			ctx:        req.Context(),
			limiter:    t.limiter,
		}
	}
	return resp, nil
}

type rateLimitedReadCloser struct {
	io.ReadCloser
	ctx     context.Context
	limiter *rate.Limiter
}

func (r *rateLimitedReadCloser) Read(buffer []byte) (int, error) {
	if r.limiter == nil || r.limiter.Limit() == rate.Inf {
		return r.ReadCloser.Read(buffer)
	}
	burst := r.limiter.Burst()
	if burst > 0 && len(buffer) > burst {
		buffer = buffer[:burst]
	}
	n, err := r.ReadCloser.Read(buffer)
	if n == 0 {
		return n, err
	}
	if waitErr := r.limiter.WaitN(r.ctx, n); waitErr != nil {
		return n, waitErr
	}
	return n, err
}
