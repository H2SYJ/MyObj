package download

import (
	"github.com/anacrolix/torrent"
	"golang.org/x/time/rate"
)

// applySharedTorrentLimiters 只注入限流器，不改变Torrent的代理、DHT、Tracker或Peer网络配置。
func applySharedTorrentLimiters(cfg *torrent.ClientConfig, downloadLimiter, uploadLimiter *rate.Limiter) {
	if downloadLimiter != nil {
		cfg.DownloadRateLimiter = downloadLimiter
	}
	if uploadLimiter != nil {
		cfg.UploadRateLimiter = uploadLimiter
	}
}
