package download

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

const (
	OfflineDownloadProxyConfigKey              = "offline_download_proxy"
	OfflineDownloadSpeedLimitConfigKey         = "offline_download_speed_limit_mb_per_sec"
	OfflineDownloadBTUploadSpeedLimitConfigKey = "offline_download_bt_upload_speed_limit_mb_per_sec"

	bytesPerMB           = 1024 * 1024
	downloadLimiterBurst = 64 * 1024
	btUploadLimiterBurst = 1024 * 1024
)

// NetworkSettings 表示可动态生效的离线下载网络配置。
type NetworkSettings struct {
	ProxyURL                   string
	DownloadSpeedLimitMBPerSec float64
	BTUploadSpeedLimitMBPerSec float64
}

// NetworkPolicy 保存当前代理快照和跨任务共享的动态限流器。
type NetworkPolicy struct {
	mu       sync.RWMutex
	proxyURL string

	downloadLimiter *rate.Limiter
	btUploadLimiter *rate.Limiter
}

func NewNetworkPolicy() *NetworkPolicy {
	return &NetworkPolicy{
		downloadLimiter: rate.NewLimiter(rate.Inf, downloadLimiterBurst),
		btUploadLimiter: rate.NewLimiter(rate.Inf, btUploadLimiterBurst),
	}
}

// NormalizeProxyURL 校验并规范化HTTP直链下载代理地址，空字符串表示关闭代理。
func NormalizeProxyURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("代理地址格式错误: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	switch parsed.Scheme {
	case "http", "https", "socks5":
	default:
		return "", fmt.Errorf("代理地址仅支持http、https和socks5协议")
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("代理地址缺少主机名")
	}
	if parsed.Opaque != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("代理地址不能包含路径、查询参数或片段")
	}
	parsed.Path = ""
	return parsed.String(), nil
}

// ValidateSpeedLimitMBPerSec 校验MB/s限速值是否可安全换算为字节每秒。
func ValidateSpeedLimitMBPerSec(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("离线下载限速必须是大于等于0的有限数值")
	}
	if value > float64(math.MaxInt64)/bytesPerMB {
		return fmt.Errorf("离线下载限速数值过大")
	}
	return nil
}

// Apply 校验并应用配置。代理只供新HTTP客户端读取，限流器则原地更新以影响活动任务。
func (p *NetworkPolicy) Apply(settings NetworkSettings) error {
	proxyURL, err := NormalizeProxyURL(settings.ProxyURL)
	if err != nil {
		return err
	}
	if err := ValidateSpeedLimitMBPerSec(settings.DownloadSpeedLimitMBPerSec); err != nil {
		return err
	}
	if err := ValidateSpeedLimitMBPerSec(settings.BTUploadSpeedLimitMBPerSec); err != nil {
		return err
	}

	p.mu.Lock()
	p.proxyURL = proxyURL
	p.mu.Unlock()

	p.downloadLimiter.SetLimit(speedLimit(settings.DownloadSpeedLimitMBPerSec))
	p.btUploadLimiter.SetLimit(speedLimit(settings.BTUploadSpeedLimitMBPerSec))
	return nil
}

func speedLimit(value float64) rate.Limit {
	if value == 0 {
		return rate.Inf
	}
	return rate.Limit(value * bytesPerMB)
}

func (p *NetworkPolicy) ProxyURL() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.proxyURL
}

func (p *NetworkPolicy) DownloadLimiter() *rate.Limiter {
	return p.downloadLimiter
}

func (p *NetworkPolicy) BTUploadLimiter() *rate.Limiter {
	return p.btUploadLimiter
}

func (p *NetworkPolicy) Settings() NetworkSettings {
	return NetworkSettings{
		ProxyURL:                   p.ProxyURL(),
		DownloadSpeedLimitMBPerSec: limiterMBPerSec(p.downloadLimiter),
		BTUploadSpeedLimitMBPerSec: limiterMBPerSec(p.btUploadLimiter),
	}
}

func limiterMBPerSec(limiter *rate.Limiter) float64 {
	if limiter == nil || limiter.Limit() == rate.Inf {
		return 0
	}
	return float64(limiter.Limit()) / bytesPerMB
}
