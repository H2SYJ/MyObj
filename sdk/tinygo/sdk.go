package myobjplugin

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	maxInputBytes                = 2 * 1024 * 1024
	defaultHTTPResponseBytes     = 2 * 1024 * 1024
	minimumHTTPResponseBytes     = 64 * 1024
	maximumHTTPResponseBytes     = 4 * 1024 * 1024
	maxHTTPResponseMetadataBytes = 128 * 1024
	maxFileOutputBytes           = 2 * 1024 * 1024
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
	// SavePath 是订阅保存目录下以 / 开头的根相对目录，空值或 / 表示保存目录本身。
	SavePath       string            `json:"save_path,omitempty"`
	ThumbnailURL   string            `json:"thumbnail_url,omitempty"`
	RequestHeaders map[string]string `json:"request_headers,omitempty"`
	HeaderHosts    []string          `json:"header_hosts,omitempty"`
}

type InvocationResponse struct {
	OK      bool               `json:"ok"`
	Error   string             `json:"error,omitempty"`
	Items   []DownloadableItem `json:"items,omitempty"`
	Message string             `json:"message,omitempty"`
}

type Handler interface {
	Healthcheck() error
	ValidateConfig(map[string]interface{}) error
	Fetch(InvocationRequest) ([]DownloadableItem, error)
}

// Run 实现ABI v1的stdin/stdout UTF-8 JSON入口。
func Run(handler Handler) {
	requestBytes, err := io.ReadAll(io.LimitReader(os.Stdin, maxInputBytes+1))
	if err != nil || len(requestBytes) == 0 || len(requestBytes) > maxInputBytes {
		writeResponse(InvocationResponse{OK: false, Error: "invalid_input"})
		return
	}
	var request InvocationRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		writeResponse(InvocationResponse{OK: false, Error: "invalid_json"})
		return
	}
	response := InvocationResponse{OK: true}
	switch request.Action {
	case "healthcheck":
		err = handler.Healthcheck()
	case "validate_config":
		err = handler.ValidateConfig(request.Config)
	case "fetch":
		response.Items, err = handler.Fetch(request)
	default:
		err = fmt.Errorf("unsupported_action")
	}
	if err != nil {
		response.OK = false
		response.Error = err.Error()
	}
	writeResponse(response)
}

func writeResponse(response InvocationResponse) {
	writer := bufio.NewWriter(os.Stdout)
	_ = json.NewEncoder(writer).Encode(response)
	_ = writer.Flush()
}

type HTTPRequestInput struct {
	Method           string            `json:"method"`
	URL              string            `json:"url"`
	Headers          map[string]string `json:"headers,omitempty"`
	Body             []byte            `json:"-"`
	MaxResponseBytes int               `json:"-"`
}

type httpRequestWire struct {
	Method           string            `json:"method"`
	URL              string            `json:"url"`
	Headers          map[string]string `json:"headers,omitempty"`
	Body             string            `json:"body_base64,omitempty"`
	MaxResponseBytes int               `json:"max_response_bytes,omitempty"`
}

type HTTPResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	BodyBase64 string              `json:"body_base64,omitempty"`
	Error      string              `json:"error,omitempty"`
}

func (r HTTPResponse) Body() ([]byte, error) {
	return base64.StdEncoding.DecodeString(r.BodyBase64)
}

func HTTPRequest(request HTTPRequestInput) (HTTPResponse, error) {
	responseLimit, outputCapacity, err := httpResponseCapacity(request.MaxResponseBytes)
	if err != nil {
		return HTTPResponse{}, err
	}
	wire := httpRequestWire{
		Method:           request.Method,
		URL:              request.URL,
		Headers:          request.Headers,
		Body:             base64.StdEncoding.EncodeToString(request.Body),
		MaxResponseBytes: responseLimit,
	}
	var response HTTPResponse
	if err := callHTTPRequest(wire, &response, outputCapacity); err != nil {
		return response, err
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

func httpResponseCapacity(requested int) (int, int, error) {
	limit := requested
	if limit == 0 {
		limit = defaultHTTPResponseBytes
	}
	if limit < minimumHTTPResponseBytes || limit > maximumHTTPResponseBytes {
		return 0, 0, fmt.Errorf("http_response_limit_out_of_range")
	}
	encodedBodyBytes := base64.StdEncoding.EncodedLen(limit)
	maxInt := int(^uint(0) >> 1)
	if encodedBodyBytes > maxInt-maxHTTPResponseMetadataBytes {
		return 0, 0, fmt.Errorf("http_response_limit_overflow")
	}
	return limit, encodedBodyBytes + maxHTTPResponseMetadataBytes, nil
}

type FileQuery struct {
	// Path 是订阅保存目录下的根相对目录，空值或 / 表示保存目录本身。
	Path          string     `json:"path,omitempty"`
	Recursive     bool       `json:"recursive,omitempty"`
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

func FileGet(ufID string) (SafeFileInfo, error) {
	var response FileQueryResponse
	if err := callFileGet(map[string]string{"uf_id": ufID}, &response, maxFileOutputBytes); err != nil {
		return SafeFileInfo{}, err
	}
	if response.Error != "" || len(response.Files) != 1 {
		if response.Error == "" {
			response.Error = "not_found"
		}
		return SafeFileInfo{}, fmt.Errorf("%s", response.Error)
	}
	return response.Files[0], nil
}

func FilesQuery(query FileQuery) (FileQueryResponse, error) {
	var response FileQueryResponse
	if err := callFilesQuery(query, &response, maxFileOutputBytes); err != nil {
		return response, err
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}
