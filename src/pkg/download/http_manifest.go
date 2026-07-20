package download

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type manifestChunk struct {
	Index int   `json:"index"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  bool  `json:"done"`
}

type downloadManifest struct {
	Version      int             `json:"version"`
	URL          string          `json:"url"`
	FileSize     int64           `json:"file_size"`
	ETag         string          `json:"etag"`
	LastModified string          `json:"last_modified"`
	Chunks       []manifestChunk `json:"chunks"`
}

func loadDownloadManifest(path string) (*downloadManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest downloadManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析下载清单失败: %w", err)
	}
	return &manifest, nil
}

func saveDownloadManifest(path string, manifest *downloadManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("编码下载清单失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建下载清单目录失败: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("写入下载清单失败: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("提交下载清单失败: %w", err)
	}
	return nil
}

func manifestMatches(manifest *downloadManifest, rawURL string, info *FileInfoResult) bool {
	if manifest == nil || manifest.Version != 1 || manifest.URL != rawURL || manifest.FileSize != info.FileSize {
		return false
	}
	// 没有资源校验标识时不能确认远端内容未变化，必须从零开始。
	if info.ETag == "" && info.LastModified == "" {
		return false
	}
	if manifest.ETag != info.ETag || manifest.LastModified != info.LastModified || len(manifest.Chunks) == 0 {
		return false
	}
	var nextStart int64
	for index, chunk := range manifest.Chunks {
		if chunk.Index != index || chunk.Start != nextStart || chunk.End < chunk.Start || chunk.End >= manifest.FileSize {
			return false
		}
		nextStart = chunk.End + 1
	}
	return nextStart == manifest.FileSize
}
