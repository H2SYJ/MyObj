package download

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/time/rate"
)

const hlsKeyCacheFileName = "keys.enc"

type HLSPrepareOptions struct {
	ProxyURL        string
	DownloadLimiter *rate.Limiter
	RequestHeaders  map[string]string
	HeaderHosts     []string
	MaxRetries      int
	Client          *http.Client
}

// PreparedHLSSnapshot 表示已经完整写入暂存目录、尚未切换为任务正式快照的HLS快照。
type PreparedHLSSnapshot struct {
	stagingDir string
	finalDir   string
	backupDir  string
	active     bool
}

// PrepareHLSSnapshot 抓取入口及所选播放链路的全部子级清单和AES-128密钥。
func PrepareHLSSnapshot(ctx context.Context, taskID, sourceURL, userID, outputName, tempDir, secret string,
	opts *HLSPrepareOptions) (*PreparedHLSSnapshot, error) {
	if taskID == "" || userID == "" || outputName == "" {
		return nil, fmt.Errorf("准备HLS快照所需的任务信息不完整")
	}
	if err := ValidatePublicHTTPURL(sourceURL); err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &HLSPrepareOptions{MaxRetries: 3}
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	client := opts.Client
	if client == nil {
		var err error
		client, err = newHLSHTTPClient(opts.ProxyURL, opts.DownloadLimiter, opts.RequestHeaders, opts.HeaderHosts)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return nil, fmt.Errorf("创建HLS临时目录失败: %w", err)
	}
	stagingDir, err := os.MkdirTemp(tempDir, fmt.Sprintf("hls_%s.staging-", taskID))
	if err != nil {
		return nil, fmt.Errorf("创建HLS快照暂存目录失败: %w", err)
	}
	prepared := &PreparedHLSSnapshot{
		stagingDir: stagingDir,
		finalDir:   filepath.Join(tempDir, fmt.Sprintf("hls_%s", taskID)),
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	manifest, err := buildHLSManifest(ctx, client, sourceURL, outputName)
	if err != nil {
		return nil, err
	}
	keys, err := fetchHLSKeys(ctx, client, manifest, opts.MaxRetries)
	if err != nil {
		return nil, err
	}
	if old, loadErr := loadHLSManifest(filepath.Join(prepared.finalDir, hlsManifestFileName)); loadErr == nil &&
		hlsManifestStructureMatches(old, manifest) {
		copyHLSCompletion(old, manifest)
		validateHLSCompletedFiles(prepared.finalDir, manifest)
		if err := copyHLSCompletedFiles(prepared.finalDir, stagingDir, manifest); err != nil {
			return nil, err
		}
	}
	if err := saveHLSManifest(filepath.Join(stagingDir, hlsManifestFileName), manifest); err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		if err := saveHLSKeyCache(filepath.Join(stagingDir, hlsKeyCacheFileName), secret, taskID, userID, keys); err != nil {
			return nil, err
		}
	}
	success = true
	return prepared, nil
}

// Activate 将暂存快照切换为任务正式快照；后续必须调用Finalize或Rollback。
func (p *PreparedHLSSnapshot) Activate() error {
	if p == nil || p.stagingDir == "" || p.active {
		return fmt.Errorf("HLS快照状态无效")
	}
	if _, err := os.Stat(p.finalDir); err == nil {
		p.backupDir = p.finalDir + ".backup-" + filepath.Base(p.stagingDir)
		if err := os.Rename(p.finalDir, p.backupDir); err != nil {
			return fmt.Errorf("备份原HLS快照失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查原HLS快照失败: %w", err)
	}
	if err := os.Rename(p.stagingDir, p.finalDir); err != nil {
		if p.backupDir != "" {
			if restoreErr := os.Rename(p.backupDir, p.finalDir); restoreErr != nil {
				return fmt.Errorf("提交HLS快照失败: %v；恢复原HLS快照失败: %w", err, restoreErr)
			}
			p.backupDir = ""
		}
		return fmt.Errorf("提交HLS快照失败: %w", err)
	}
	p.active = true
	return nil
}

func (p *PreparedHLSSnapshot) Finalize() error {
	if p == nil {
		return nil
	}
	if p.backupDir != "" {
		if err := os.RemoveAll(p.backupDir); err != nil {
			return fmt.Errorf("清理旧HLS快照失败: %w", err)
		}
	}
	p.backupDir = ""
	return nil
}

func (p *PreparedHLSSnapshot) Rollback() error {
	if p == nil {
		return nil
	}
	if !p.active {
		return p.Discard()
	}
	if err := os.RemoveAll(p.finalDir); err != nil {
		return fmt.Errorf("回滚HLS快照失败: %w", err)
	}
	if p.backupDir != "" {
		if err := os.Rename(p.backupDir, p.finalDir); err != nil {
			return fmt.Errorf("恢复原HLS快照失败: %w", err)
		}
	}
	p.active = false
	p.backupDir = ""
	return nil
}

func (p *PreparedHLSSnapshot) Discard() error {
	if p == nil || p.stagingDir == "" || p.active {
		return nil
	}
	return os.RemoveAll(p.stagingDir)
}

func HasHLSSnapshot(tempDir, taskID string) bool {
	manifest, err := loadHLSManifest(filepath.Join(tempDir, fmt.Sprintf("hls_%s", taskID), hlsManifestFileName))
	return err == nil && manifest.Version == hlsManifestVersion && !manifest.CapturedAt.IsZero()
}

func loadPreparedHLSManifest(sessionDir, sourceURL, outputName string) (*hlsManifest, error) {
	manifest, err := loadHLSManifest(filepath.Join(sessionDir, hlsManifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("任务缺少HLS快照，请重新创建")
		}
		return nil, err
	}
	if manifest.Version != hlsManifestVersion || manifest.CapturedAt.IsZero() {
		return nil, fmt.Errorf("HLS快照版本无效，请重新创建任务")
	}
	if manifest.SourceURL != sourceURL || manifest.OutputName != outputName {
		return nil, fmt.Errorf("HLS快照与任务信息不一致，请重新创建任务")
	}
	if err := validatePreparedHLSManifest(manifest); err != nil {
		return nil, err
	}
	validateHLSCompletedFiles(sessionDir, manifest)
	if err := saveHLSManifest(filepath.Join(sessionDir, hlsManifestFileName), manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func validatePreparedHLSManifest(manifest *hlsManifest) error {
	if manifest == nil || len(manifest.Renditions) == 0 || len(manifest.Renditions) > 2 {
		return fmt.Errorf("HLS快照播放轨道数量无效")
	}
	totalSegments := 0
	for renditionIndex := range manifest.Renditions {
		rendition := &manifest.Renditions[renditionIndex]
		if (renditionIndex == 0 && rendition.Kind != hlsVideoRendition) ||
			(renditionIndex == 1 && rendition.Kind != hlsAudioRendition) || len(rendition.Segments) == 0 {
			return fmt.Errorf("HLS快照播放轨道结构无效")
		}
		totalSegments += len(rendition.Segments)
		if totalSegments > hlsMaxSegments {
			return fmt.Errorf("HLS快照分片总数超过限制")
		}
		for index := range rendition.Maps {
			item := &rendition.Maps[index]
			if err := validatePreparedHLSItem(item.URL, item.LocalName, item.Key); err != nil {
				return err
			}
		}
		for index := range rendition.Segments {
			item := &rendition.Segments[index]
			if err := validatePreparedHLSItem(item.URL, item.LocalName, item.Key); err != nil {
				return err
			}
			if item.MapIndex < hlsNoMap || item.MapIndex >= len(rendition.Maps) {
				return fmt.Errorf("HLS快照分片Map索引无效")
			}
		}
	}
	return nil
}

func validatePreparedHLSItem(rawURL, localName string, key hlsKeySpec) error {
	if err := ValidatePublicHTTPURL(rawURL); err != nil {
		return fmt.Errorf("HLS快照资源地址无效: %w", err)
	}
	if localName == "" || filepath.Base(localName) != localName || strings.ContainsAny(localName, "/\\\x00") {
		return fmt.Errorf("HLS快照本地文件名无效")
	}
	if key.Method != "" && key.Method != "AES-128" {
		return fmt.Errorf("HLS快照包含不支持的加密方式")
	}
	if key.Method == "AES-128" {
		if err := ValidatePublicHTTPURL(key.URL); err != nil {
			return fmt.Errorf("HLS快照密钥地址无效: %w", err)
		}
	}
	return nil
}

func copyHLSCompletedFiles(sourceDir, destinationDir string, manifest *hlsManifest) error {
	for renditionIndex := range manifest.Renditions {
		rendition := &manifest.Renditions[renditionIndex]
		for index := range rendition.Maps {
			if rendition.Maps[index].Done {
				if err := linkOrCopyHLSFile(sourceDir, destinationDir, rendition.Maps[index].LocalName); err != nil {
					return err
				}
			}
		}
		for index := range rendition.Segments {
			if rendition.Segments[index].Done {
				if err := linkOrCopyHLSFile(sourceDir, destinationDir, rendition.Segments[index].LocalName); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func linkOrCopyHLSFile(sourceDir, destinationDir, name string) error {
	source := filepath.Join(sourceDir, name)
	destination := filepath.Join(destinationDir, name)
	if err := os.Link(source, destination); err == nil {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = output.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
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

func saveHLSKeyCache(path, secret, taskID, userID string, keys map[string][]byte) error {
	plaintext, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("编码HLS密钥缓存失败: %w", err)
	}
	aead, err := newHLSKeyCacheAEAD(secret)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("生成HLS密钥缓存随机数失败: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(taskID+"\x00"+userID))
	payload := "v1:" + base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...))
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		return fmt.Errorf("写入HLS密钥缓存失败: %w", err)
	}
	return nil
}

func loadHLSKeyCache(sessionDir, secret, taskID, userID string, manifest *hlsManifest) (map[string][]byte, error) {
	required := collectHLSKeyURLs(manifest)
	if len(required) == 0 {
		return map[string][]byte{}, nil
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, hlsKeyCacheFileName))
	if err != nil {
		return nil, fmt.Errorf("读取HLS密钥缓存失败: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if !strings.HasPrefix(value, "v1:") {
		return nil, fmt.Errorf("HLS密钥缓存版本无效")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil {
		return nil, fmt.Errorf("解码HLS密钥缓存失败: %w", err)
	}
	aead, err := newHLSKeyCacheAEAD(secret)
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize() {
		return nil, fmt.Errorf("HLS密钥缓存长度无效")
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte(taskID+"\x00"+userID))
	if err != nil {
		return nil, fmt.Errorf("HLS密钥缓存解密失败: %w", err)
	}
	var keys map[string][]byte
	if err := json.Unmarshal(plaintext, &keys); err != nil {
		return nil, fmt.Errorf("解析HLS密钥缓存失败: %w", err)
	}
	for keyURL := range required {
		if len(keys[keyURL]) != aes.BlockSize {
			return nil, fmt.Errorf("HLS密钥缓存缺少有效的AES-128密钥")
		}
	}
	return keys, nil
}

func collectHLSKeyURLs(manifest *hlsManifest) map[string]struct{} {
	required := make(map[string]struct{})
	for _, rendition := range manifest.Renditions {
		for _, item := range rendition.Maps {
			if item.Key.Method == "AES-128" {
				required[item.Key.URL] = struct{}{}
			}
		}
		for _, item := range rendition.Segments {
			if item.Key.Method == "AES-128" {
				required[item.Key.URL] = struct{}{}
			}
		}
	}
	return required
}

func newHLSKeyCacheAEAD(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, fmt.Errorf("服务端认证密钥为空，无法保护HLS密钥缓存")
	}
	reader := hkdf.New(sha256.New, []byte(secret), []byte("myobj-hls-key-cache-salt-v1"), []byte("offline-download-hls-keys-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("派生HLS密钥缓存密钥失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建HLS密钥缓存加密器失败: %w", err)
	}
	return cipher.NewGCM(block)
}
