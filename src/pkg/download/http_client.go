package download

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
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

func newPublicHTTPClient(timeoutSeconds int) *http.Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("解析目标地址失败: %w", err)
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("解析目标地址失败: %w", err)
		}
		for _, item := range addresses {
			if !isPublicDownloadIP(item.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
		}
		return nil, fmt.Errorf("目标地址没有可用公网IP")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			return ValidatePublicHTTPURL(req.URL.String())
		},
	}
}
