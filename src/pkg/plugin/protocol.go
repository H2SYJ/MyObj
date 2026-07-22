package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type InvocationRequest struct {
	Action string                 `json:"action"`
	Config map[string]interface{} `json:"config,omitempty"`
	Now    time.Time              `json:"now"`
}

type DownloadableItem struct {
	ID           string     `json:"id,omitempty"`
	Title        string     `json:"title,omitempty"`
	URL          string     `json:"url"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	DownloadType string     `json:"download_type"`
	FileName     string     `json:"file_name,omitempty"`
	// SavePath 是订阅保存目录下以 / 开头的根相对目录。
	SavePath       string            `json:"save_path,omitempty"`
	ThumbnailURL   string            `json:"thumbnail_url,omitempty"`
	RequestHeaders map[string]string `json:"request_headers,omitempty"`
	HeaderHosts    []string          `json:"header_hosts,omitempty"`
}

func (item *DownloadableItem) UnmarshalJSON(data []byte) error {
	type itemAlias DownloadableItem
	var decoded itemAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw struct {
		RequestHeaders json.RawMessage `json:"request_headers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.RequestHeaders) > 0 && string(raw.RequestHeaders) != "null" {
		headers, err := decodeUniqueStringMap(raw.RequestHeaders)
		if err != nil {
			return fmt.Errorf("request_headers无效: %w", err)
		}
		decoded.RequestHeaders = headers
	}
	*item = DownloadableItem(decoded)
	return nil
}

func decodeUniqueStringMap(data []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("必须是字符串键值对象")
	}
	result := map[string]string{}
	seen := map[string]bool{}
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, fmt.Errorf("请求头名称必须是字符串")
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			return nil, fmt.Errorf("请求头名称重复: %s", name)
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("请求头值必须是字符串")
		}
		seen[lower] = true
		result[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, fmt.Errorf("请求头对象后包含多余内容")
	}
	return result, nil
}

type InvocationResponse struct {
	OK      bool               `json:"ok"`
	Error   string             `json:"error,omitempty"`
	Items   []DownloadableItem `json:"items,omitempty"`
	Message string             `json:"message,omitempty"`
}

type HTTPRequest struct {
	Method           string            `json:"method"`
	URL              string            `json:"url"`
	Headers          map[string]string `json:"headers,omitempty"`
	Body             string            `json:"body_base64,omitempty"`
	MaxResponseBytes int               `json:"max_response_bytes,omitempty"`
}

type HTTPResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body_base64,omitempty"`
	Error      string              `json:"error,omitempty"`
}

type FileQueryRequest struct {
	Operation string `json:"operation,omitempty"`
	UFID      string `json:"uf_id,omitempty"`
	// Path 是订阅保存目录下的根相对目录。
	Path          string     `json:"path,omitempty"`
	Recursive     bool       `json:"recursive,omitempty"`
	NameEquals    string     `json:"name_equals,omitempty"`
	NameContains  string     `json:"name_contains,omitempty"`
	MIMEPrefix    string     `json:"mime_prefix,omitempty"`
	IsEncrypted   *bool      `json:"is_encrypted,omitempty"`
	IsPublic      *bool      `json:"is_public,omitempty"`
	HasThumbnail  *bool      `json:"has_thumbnail,omitempty"`
	CreatedAfter  *time.Time `json:"created_after,omitempty"`
	CreatedBefore *time.Time `json:"created_before,omitempty"`
	UpdatedAfter  *time.Time `json:"updated_after,omitempty"`
	UpdatedBefore *time.Time `json:"updated_before,omitempty"`
	Cursor        string     `json:"cursor,omitempty"`
	Limit         int        `json:"limit,omitempty"`
}

type SafeFileInfo struct {
	UFID         string    `json:"uf_id"`
	FileName     string    `json:"file_name"`
	VirtualPath  string    `json:"virtual_path"`
	FileSize     int64     `json:"file_size"`
	MIMEType     string    `json:"mime_type"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	IsEncrypted  bool      `json:"is_encrypted"`
	IsPublic     bool      `json:"is_public"`
	HasThumbnail bool      `json:"has_thumbnail"`
}

type FileQueryResponse struct {
	Files      []SafeFileInfo `json:"files,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Error      string         `json:"error,omitempty"`
}
