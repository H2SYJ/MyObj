package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Eyevinn/hls-m3u8/m3u8"
)

const (
	hlsManifestVersion  = 2
	hlsMaxVariants      = 256
	hlsMaxSegments      = 50000
	hlsMaxPlaylistDepth = 8
	hlsMaxPlaylistCount = 32
	hlsManifestFileName = "manifest.json"
	hlsVideoRendition   = "video"
	hlsAudioRendition   = "audio"
	hlsNoMap            = -1
)

type hlsManifest struct {
	Version    int            `json:"version"`
	SourceURL  string         `json:"source_url"`
	OutputName string         `json:"output_name"`
	CapturedAt time.Time      `json:"captured_at"`
	Renditions []hlsRendition `json:"renditions"`
}

type hlsPlaylistTraversal struct {
	count         int
	totalSegments int
	path          map[string]struct{}
}

type hlsRendition struct {
	Kind           string       `json:"kind"`
	PlaylistURL    string       `json:"playlist_url"`
	Sequence       uint64       `json:"sequence"`
	TargetDuration uint         `json:"target_duration"`
	Maps           []hlsMap     `json:"maps"`
	Segments       []hlsSegment `json:"segments"`
}

type hlsMap struct {
	URL         string     `json:"url"`
	URLIdentity string     `json:"url_identity"`
	Offset      int64      `json:"offset"`
	Length      int64      `json:"length"`
	LocalName   string     `json:"local_name"`
	Key         hlsKeySpec `json:"key"`
	Done        bool       `json:"done"`
	Size        int64      `json:"size"`
	SHA256      string     `json:"sha256"`
}

type hlsSegment struct {
	Sequence      uint64     `json:"sequence"`
	URL           string     `json:"url"`
	URLIdentity   string     `json:"url_identity"`
	Duration      float64    `json:"duration"`
	Offset        int64      `json:"offset"`
	Length        int64      `json:"length"`
	LocalName     string     `json:"local_name"`
	Discontinuity bool       `json:"discontinuity"`
	MapIndex      int        `json:"map_index"`
	Key           hlsKeySpec `json:"key"`
	Done          bool       `json:"done"`
	Size          int64      `json:"size"`
	SHA256        string     `json:"sha256"`
}

type hlsKeySpec struct {
	Method      string `json:"method"`
	URL         string `json:"url"`
	URLIdentity string `json:"url_identity"`
	IV          string `json:"iv"`
}

func buildHLSManifest(ctx context.Context, client *http.Client, sourceURL, outputName string) (*hlsManifest, error) {
	traversal := &hlsPlaylistTraversal{path: make(map[string]struct{})}
	video, audio, err := loadHLSRenditionChain(ctx, client, sourceURL, hlsVideoRendition, true, 0, traversal)
	if err != nil {
		return nil, err
	}
	manifest := &hlsManifest{
		Version: hlsManifestVersion, SourceURL: sourceURL, OutputName: outputName, CapturedAt: time.Now().UTC(),
		Renditions: []hlsRendition{*video},
	}
	if audio != nil {
		manifest.Renditions = append(manifest.Renditions, *audio)
	}
	return manifest, nil
}

func loadHLSRenditionChain(ctx context.Context, client *http.Client, playlistURL, kind string, allowAudio bool, depth int,
	traversal *hlsPlaylistTraversal) (*hlsRendition, *hlsRendition, error) {
	if depth > hlsMaxPlaylistDepth {
		return nil, nil, fmt.Errorf("HLS子级播放列表嵌套不能超过%d层", hlsMaxPlaylistDepth)
	}
	playlistIdentity := stableHLSURLIdentity(playlistURL)
	if _, exists := traversal.path[playlistIdentity]; exists {
		return nil, nil, fmt.Errorf("HLS播放列表存在循环引用")
	}
	traversal.count++
	if traversal.count > hlsMaxPlaylistCount {
		return nil, nil, fmt.Errorf("HLS播放列表总数不能超过%d个", hlsMaxPlaylistCount)
	}
	traversal.path[playlistIdentity] = struct{}{}
	defer delete(traversal.path, playlistIdentity)

	data, err := fetchHLSBytes(ctx, client, playlistURL, 0, 0, hlsMaxPlaylistSize)
	if err != nil {
		return nil, nil, err
	}
	playlist, listType, err := m3u8.DecodeFrom(bytes.NewReader(data), true)
	if err != nil {
		return nil, nil, fmt.Errorf("解析%s播放列表失败: %w", kind, err)
	}
	switch listType {
	case m3u8.MEDIA:
		media, ok := playlist.(*m3u8.MediaPlaylist)
		if !ok {
			return nil, nil, fmt.Errorf("%s播放列表类型无效", kind)
		}
		if rangeErr := applyImplicitHLSByteRanges(media, data); rangeErr != nil {
			return nil, nil, rangeErr
		}
		rendition, renditionErr := buildHLSRendition(playlistURL, kind, media)
		if renditionErr != nil {
			return nil, nil, renditionErr
		}
		traversal.totalSegments += len(rendition.Segments)
		if traversal.totalSegments > hlsMaxSegments {
			return nil, nil, fmt.Errorf("HLS视频和音轨分片总数不能超过%d个", hlsMaxSegments)
		}
		return rendition, nil, nil
	case m3u8.MASTER:
		master, ok := playlist.(*m3u8.MasterPlaylist)
		if !ok {
			return nil, nil, fmt.Errorf("%s主播放列表类型无效", kind)
		}
		variant, selectErr := selectHighestHLSVariant(master)
		if selectErr != nil {
			return nil, nil, selectErr
		}
		variantURL, resolveErr := resolveHLSURL(playlistURL, variant.URI)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		var audioAlternative *m3u8.Alternative
		if kind == hlsVideoRendition && allowAudio {
			audioAlternative = selectDefaultHLSAudio(variant)
		}
		primary, nestedAudio, loadErr := loadHLSRenditionChain(ctx, client, variantURL, kind,
			allowAudio && audioAlternative == nil, depth+1, traversal)
		if loadErr != nil {
			return nil, nil, loadErr
		}
		if kind != hlsVideoRendition {
			return primary, nil, nil
		}
		if audioAlternative != nil && audioAlternative.URI != "" {
			audioURL, audioResolveErr := resolveHLSURL(playlistURL, audioAlternative.URI)
			if audioResolveErr != nil {
				return nil, nil, audioResolveErr
			}
			audio, _, audioErr := loadHLSRenditionChain(ctx, client, audioURL, hlsAudioRendition, false, depth+1, traversal)
			if audioErr != nil {
				return nil, nil, audioErr
			}
			return primary, audio, nil
		}
		return primary, nestedAudio, nil
	default:
		return nil, nil, fmt.Errorf("无法识别%s播放列表类型", kind)
	}
}

func buildHLSRendition(playlistURL, kind string, media *m3u8.MediaPlaylist) (*hlsRendition, error) {
	if !media.Closed {
		return nil, fmt.Errorf("首版仅支持包含#EXT-X-ENDLIST的点播HLS，不支持直播录制")
	}
	if media.Iframe {
		return nil, fmt.Errorf("不支持#EXT-X-I-FRAMES-ONLY播放列表")
	}
	segments := media.GetAllSegments()
	if len(segments) == 0 {
		return nil, fmt.Errorf("HLS播放列表没有可下载分片")
	}
	rendition := &hlsRendition{Kind: kind, PlaylistURL: playlistURL, Sequence: media.SeqNo, TargetDuration: media.TargetDuration}
	mapIndexes := make(map[string]int)
	activeKey, err := normalizeHLSKey(playlistURL, media.Keys)
	if err != nil {
		return nil, err
	}
	activeMap := media.Map
	for index, item := range segments {
		if item == nil || item.URI == "" || item.Gap || item.Duration <= 0 {
			return nil, fmt.Errorf("HLS分片%d缺少可用URI或被标记为GAP", index)
		}
		segmentURL, err := resolveHLSURL(playlistURL, item.URI)
		if err != nil {
			return nil, err
		}
		if len(item.Keys) > 0 {
			activeKey, err = normalizeHLSKey(playlistURL, item.Keys)
			if err != nil {
				return nil, fmt.Errorf("HLS分片%d密钥无效: %w", index, err)
			}
		}
		key := activeKey
		if item.Map != nil {
			activeMap = item.Map
		}
		mapIndex := hlsNoMap
		if activeMap != nil {
			mapURL, mapErr := resolveHLSURL(playlistURL, activeMap.URI)
			if mapErr != nil {
				return nil, mapErr
			}
			mapKey := key
			if mapKey.Method == "AES-128" && mapKey.IV == "" {
				return nil, fmt.Errorf("AES-128加密的EXT-X-MAP必须提供显式IV")
			}
			identity := stableHLSURLIdentity(mapURL)
			dedupeKey := fmt.Sprintf("%s|%d|%d|%s|%s", identity, activeMap.Offset, activeMap.Limit, mapKey.URLIdentity, mapKey.IV)
			if existing, found := mapIndexes[dedupeKey]; found {
				mapIndex = existing
			} else {
				mapIndex = len(rendition.Maps)
				mapIndexes[dedupeKey] = mapIndex
				rendition.Maps = append(rendition.Maps, hlsMap{
					URL: mapURL, URLIdentity: identity, Offset: activeMap.Offset, Length: activeMap.Limit,
					LocalName: fmt.Sprintf("%s_map_%05d.bin", kind, mapIndex), Key: mapKey,
				})
			}
		}
		rendition.Segments = append(rendition.Segments, hlsSegment{
			Sequence: item.SeqId, URL: segmentURL, URLIdentity: stableHLSURLIdentity(segmentURL),
			Duration: item.Duration, Offset: item.Offset, Length: item.Limit,
			LocalName:     fmt.Sprintf("%s_segment_%05d.bin", kind, index),
			Discontinuity: item.Discontinuity, MapIndex: mapIndex, Key: key,
		})
	}
	return rendition, nil
}

func applyImplicitHLSByteRanges(media *m3u8.MediaPlaylist, data []byte) error {
	segments := media.GetAllSegments()
	type parsedRange struct {
		length   int64
		offset   int64
		explicit bool
		uri      string
	}
	ranges := make([]parsedRange, 0, len(segments))
	var pending *parsedRange
	for _, rawLine := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"))
			parts := strings.SplitN(value, "@", 2)
			length, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil || length <= 0 {
				return fmt.Errorf("解析HLS Byte Range长度失败: %s", value)
			}
			pending = &parsedRange{length: length}
			if len(parts) == 2 {
				offset, offsetErr := strconv.ParseInt(parts[1], 10, 64)
				if offsetErr != nil || offset < 0 {
					return fmt.Errorf("解析HLS Byte Range偏移失败: %s", value)
				}
				pending.offset, pending.explicit = offset, true
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		item := parsedRange{uri: line}
		if pending != nil {
			item = *pending
			item.uri = line
		}
		ranges = append(ranges, item)
		pending = nil
	}
	if len(ranges) != len(segments) {
		return fmt.Errorf("HLS Byte Range与分片数量不一致")
	}
	var previousEnd int64
	previousURI := ""
	previousHadRange := false
	for index, item := range ranges {
		if item.length <= 0 {
			previousHadRange = false
			continue
		}
		if !item.explicit {
			if !previousHadRange || item.uri != previousURI {
				return fmt.Errorf("HLS隐式Byte Range必须紧跟同一资源的前一个Byte Range")
			}
			item.offset = previousEnd
		}
		segments[index].Limit = item.length
		segments[index].Offset = item.offset
		previousEnd = item.offset + item.length
		previousURI = item.uri
		previousHadRange = true
	}
	return nil
}

func selectHighestHLSVariant(master *m3u8.MasterPlaylist) (*m3u8.Variant, error) {
	if len(master.Variants) == 0 || len(master.Variants) > hlsMaxVariants {
		return nil, fmt.Errorf("HLS清晰度数量必须在1到%d之间", hlsMaxVariants)
	}
	variants := make([]*m3u8.Variant, 0, len(master.Variants))
	for _, variant := range master.Variants {
		if variant != nil && variant.URI != "" && !variant.Iframe {
			variants = append(variants, variant)
		}
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("Master Playlist没有可用的视频变体")
	}
	sort.SliceStable(variants, func(i, j int) bool {
		leftAverage := variants[i].AverageBandwidth
		rightAverage := variants[j].AverageBandwidth
		if leftAverage != rightAverage {
			return leftAverage > rightAverage
		}
		if variants[i].Bandwidth != variants[j].Bandwidth {
			return variants[i].Bandwidth > variants[j].Bandwidth
		}
		return hlsResolutionPixels(variants[i].Resolution) > hlsResolutionPixels(variants[j].Resolution)
	})
	return variants[0], nil
}

func selectDefaultHLSAudio(variant *m3u8.Variant) *m3u8.Alternative {
	var first, autoselect *m3u8.Alternative
	for _, alternative := range variant.Alternatives {
		if alternative == nil || alternative.Type != "AUDIO" || alternative.GroupId != variant.Audio || alternative.URI == "" {
			continue
		}
		if first == nil {
			first = alternative
		}
		if alternative.Default {
			return alternative
		}
		if autoselect == nil && alternative.Autoselect {
			autoselect = alternative
		}
	}
	if autoselect != nil {
		return autoselect
	}
	return first
}

func normalizeHLSKey(playlistURL string, keys []m3u8.Key) (hlsKeySpec, error) {
	for _, key := range keys {
		method := strings.ToUpper(strings.TrimSpace(key.Method))
		if method == "" || method == "NONE" {
			continue
		}
		if method != "AES-128" {
			return hlsKeySpec{}, fmt.Errorf("不支持的HLS加密方式: %s", method)
		}
		if key.Keyformat != "" && !strings.EqualFold(key.Keyformat, "identity") {
			return hlsKeySpec{}, fmt.Errorf("不支持的HLS KEYFORMAT: %s", key.Keyformat)
		}
		keyURL, err := resolveHLSURL(playlistURL, key.URI)
		if err != nil {
			return hlsKeySpec{}, err
		}
		return hlsKeySpec{Method: method, URL: keyURL, URLIdentity: stableHLSURLIdentity(keyURL), IV: key.IV}, nil
	}
	return hlsKeySpec{}, nil
}

func resolveHLSURL(baseURL, reference string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("解析HLS基础URL失败: %w", err)
	}
	ref, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return "", fmt.Errorf("解析HLS引用URL失败: %w", err)
	}
	resolved := base.ResolveReference(ref).String()
	if err := ValidatePublicHTTPURL(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func stableHLSURLIdentity(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return strings.ToLower(parsed.Scheme+"://"+parsed.Host) + parsed.EscapedPath()
}

func hlsResolutionPixels(resolution string) int64 {
	parts := strings.Split(strings.ToLower(resolution), "x")
	if len(parts) != 2 {
		return 0
	}
	width, _ := strconv.ParseInt(parts[0], 10, 64)
	height, _ := strconv.ParseInt(parts[1], 10, 64)
	return width * height
}

func loadHLSManifest(path string) (*hlsManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest hlsManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析HLS下载清单失败: %w", err)
	}
	return &manifest, nil
}

func saveHLSManifest(path string, manifest *hlsManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("编码HLS下载清单失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("创建HLS下载清单目录失败: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("写入HLS下载清单失败: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("提交HLS下载清单失败: %w", err)
	}
	return nil
}

func hlsManifestStructureMatches(old, fresh *hlsManifest) bool {
	if old == nil || old.Version != hlsManifestVersion || stableHLSURLIdentity(old.SourceURL) != stableHLSURLIdentity(fresh.SourceURL) ||
		old.OutputName != fresh.OutputName || len(old.Renditions) != len(fresh.Renditions) {
		return false
	}
	for renditionIndex := range fresh.Renditions {
		left, right := &old.Renditions[renditionIndex], &fresh.Renditions[renditionIndex]
		if left.Kind != right.Kind || left.Sequence != right.Sequence || len(left.Maps) != len(right.Maps) || len(left.Segments) != len(right.Segments) {
			return false
		}
		for index := range right.Maps {
			if !sameHLSMapStructure(left.Maps[index], right.Maps[index]) {
				return false
			}
		}
		for index := range right.Segments {
			if !sameHLSSegmentStructure(left.Segments[index], right.Segments[index]) {
				return false
			}
		}
	}
	return true
}

func sameHLSMapStructure(left, right hlsMap) bool {
	return left.URLIdentity == right.URLIdentity && left.Offset == right.Offset && left.Length == right.Length &&
		left.Key.Method == right.Key.Method && left.Key.URLIdentity == right.Key.URLIdentity && left.Key.IV == right.Key.IV
}

func sameHLSSegmentStructure(left, right hlsSegment) bool {
	return left.Sequence == right.Sequence && left.URLIdentity == right.URLIdentity && math.Abs(left.Duration-right.Duration) < 0.001 &&
		left.Offset == right.Offset && left.Length == right.Length && left.Discontinuity == right.Discontinuity && left.MapIndex == right.MapIndex &&
		left.Key.Method == right.Key.Method && left.Key.URLIdentity == right.Key.URLIdentity && left.Key.IV == right.Key.IV
}

func copyHLSCompletion(old, fresh *hlsManifest) {
	for renditionIndex := range fresh.Renditions {
		for index := range fresh.Renditions[renditionIndex].Maps {
			fresh.Renditions[renditionIndex].Maps[index].Done = old.Renditions[renditionIndex].Maps[index].Done
			fresh.Renditions[renditionIndex].Maps[index].Size = old.Renditions[renditionIndex].Maps[index].Size
			fresh.Renditions[renditionIndex].Maps[index].SHA256 = old.Renditions[renditionIndex].Maps[index].SHA256
		}
		for index := range fresh.Renditions[renditionIndex].Segments {
			fresh.Renditions[renditionIndex].Segments[index].Done = old.Renditions[renditionIndex].Segments[index].Done
			fresh.Renditions[renditionIndex].Segments[index].Size = old.Renditions[renditionIndex].Segments[index].Size
			fresh.Renditions[renditionIndex].Segments[index].SHA256 = old.Renditions[renditionIndex].Segments[index].SHA256
		}
	}
}

func validateHLSCompletedFiles(sessionDir string, manifest *hlsManifest) {
	for renditionIndex := range manifest.Renditions {
		rendition := &manifest.Renditions[renditionIndex]
		for index := range rendition.Maps {
			validateHLSCompletedFile(sessionDir, &rendition.Maps[index].Done, rendition.Maps[index].Size, rendition.Maps[index].SHA256, rendition.Maps[index].LocalName)
		}
		for index := range rendition.Segments {
			validateHLSCompletedFile(sessionDir, &rendition.Segments[index].Done, rendition.Segments[index].Size, rendition.Segments[index].SHA256, rendition.Segments[index].LocalName)
		}
	}
}

func validateHLSCompletedFile(sessionDir string, done *bool, expectedSize int64, expectedHash, localName string) {
	if !*done {
		return
	}
	path := filepath.Join(sessionDir, localName)
	stat, err := os.Stat(path)
	if err != nil || stat.Size() != expectedSize {
		*done = false
		return
	}
	hash, _, err := hashHLSFile(path)
	if err != nil || hash != expectedHash {
		*done = false
	}
}

func hashHLSFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
