package download

import (
	"encoding/base64"
	"fmt"
	"myobj/src/pkg/logger"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
	"golang.org/x/time/rate"
)

type sharedTorrentSession struct {
	content string
	client  *torrent.Client
	torrent *torrent.Torrent
}

var (
	sharedTorrentSessions   = make(map[string]*sharedTorrentSession)
	sharedTorrentSessionsMu sync.Mutex
)

func acquireTorrentSession(content, sessionTempDir, sessionID string, opts *TorrentSingleFileDownloadOptions) (*torrent.Client, *torrent.Torrent, func(), error) {
	sharedTorrentSessionsMu.Lock()
	defer sharedTorrentSessionsMu.Unlock()
	if sessionID != "" {
		if session, exists := sharedTorrentSessions[sessionID]; exists {
			if session.content != content {
				return nil, nil, nil, fmt.Errorf("Torrent批次来源不一致")
			}
			return session.client, session.torrent, func() {}, nil
		}
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = sessionTempDir
	cfg.NoUpload = false
	cfg.Seed = false
	cfg.NoDHT = false
	cfg.DisableIPv6 = true
	cfg.DisableTCP = false
	cfg.DisableUTP = false
	cfg.ListenPort = 0
	cfg.Debug = false
	cfg.EstablishedConnsPerTorrent = 200
	cfg.HalfOpenConnsPerTorrent = 50
	cfg.TorrentPeersHighWater = 200
	cfg.TorrentPeersLowWater = 50
	if opts.MaxConcurrentPeers > 0 {
		cfg.EstablishedConnsPerTorrent = opts.MaxConcurrentPeers
	}
	applySharedTorrentLimiters(cfg, opts.DownloadLimiter, opts.UploadLimiter)
	if opts.DownloadLimiter == nil && opts.DownloadRateMbps > 0 {
		limit := rate.Limit(int64(opts.DownloadRateMbps) * 1024 * 1024 / 8)
		cfg.DownloadRateLimiter = rate.NewLimiter(limit, int(limit))
	}
	if opts.UploadLimiter == nil && opts.UploadRateMbps > 0 {
		limit := rate.Limit(int64(opts.UploadRateMbps) * 1024 * 1024 / 8)
		cfg.UploadRateLimiter = rate.NewLimiter(limit, int(limit))
	}

	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("创建torrent客户端失败: %w", err)
	}
	client.AddDhtNodes([]string{
		"87.98.162.88:6881",
		"82.221.103.244:6881",
		"87.98.162.88:6969",
		"91.121.145.85:6881",
		"67.215.246.10:6881",
		"176.9.47.217:6881",
		"176.9.47.217:6969",
	})

	var taskTorrent *torrent.Torrent
	if strings.HasPrefix(content, "magnet:") {
		taskTorrent, err = client.AddMagnet(content)
		if err == nil {
			taskTorrent.AddTrackers([][]string{{
				"udp://tracker.opentrackr.org:1337/announce",
				"udp://9.rarbg.com:2810/announce",
				"udp://opentracker.i2p.rocks:6969/announce",
				"https://opentracker.i2p.rocks:443/announce",
				"udp://tracker.openbittorrent.com:6969/announce",
				"udp://tracker.torrent.eu.org:451/announce",
				"udp://open.stealth.si:80/announce",
				"udp://exodus.desync.com:6969/announce",
				"http://tracker.opentrackr.org:1337/announce",
			}})
		}
	} else {
		var torrentData []byte
		torrentData, err = base64.StdEncoding.DecodeString(content)
		if err == nil {
			torrentPath := filepath.Join(sessionTempDir, "temp.torrent")
			err = os.WriteFile(torrentPath, torrentData, 0644)
			if err == nil {
				taskTorrent, err = client.AddTorrentFromFile(torrentPath)
			}
		}
	}
	if err != nil {
		client.Close()
		return nil, nil, nil, fmt.Errorf("添加Torrent来源失败: %w", err)
	}

	closeSession := func() { client.Close() }
	if sessionID != "" {
		sharedTorrentSessions[sessionID] = &sharedTorrentSession{content: content, client: client, torrent: taskTorrent}
		closeSession = func() {}
		logger.LOG.Info("创建共享Torrent会话", "batchID", sessionID)
	}
	return client, taskTorrent, closeSession, nil
}

// CloseTorrentSession 关闭批次共享的Torrent客户端。
func CloseTorrentSession(sessionID string) {
	if sessionID == "" {
		return
	}
	sharedTorrentSessionsMu.Lock()
	session := sharedTorrentSessions[sessionID]
	delete(sharedTorrentSessions, sessionID)
	sharedTorrentSessionsMu.Unlock()
	if session != nil {
		session.client.Close()
		logger.LOG.Info("关闭共享Torrent会话", "batchID", sessionID)
	}
}
