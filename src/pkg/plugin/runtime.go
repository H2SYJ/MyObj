package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	MaxInvocationOutput       = 2 * 1024 * 1024
	MaxInvocationLog          = 256 * 1024
	MaxHTTPResponse           = 10 * 1024 * 1024
	MaxHTTPResponseHeaderJSON = 64 * 1024
)

type FileQueryFunc func(context.Context, FileQueryRequest) (FileQueryResponse, error)

type InvocationHost struct {
	Permissions     map[string]bool
	HTTPClient      *http.Client
	ValidateHTTPURL func(string) error
	FileQuery       FileQueryFunc
	fileCount       int
	fileResults     int
	mu              sync.Mutex
}

type hostContextKey struct{}

type Runtime struct {
	runtime  wazero.Runtime
	compiled sync.Map
}

func NewRuntime(ctx context.Context) (*Runtime, error) {
	config := wazero.NewRuntimeConfig().WithMemoryLimitPages(1024).WithCloseOnContextDone(true)
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		runtime.Close(ctx)
		return nil, err
	}
	r := &Runtime{runtime: runtime}
	builder := runtime.NewHostModuleBuilder("myobj")
	builder.NewFunctionBuilder().WithFunc(r.hostHTTPRequest).Export("http_request")
	builder.NewFunctionBuilder().WithFunc(r.hostFileGet).Export("file_get")
	builder.NewFunctionBuilder().WithFunc(r.hostFileQuery).Export("files_query")
	if _, err := builder.Instantiate(ctx); err != nil {
		runtime.Close(ctx)
		return nil, err
	}
	return r, nil
}

func (r *Runtime) Close(ctx context.Context) error { return r.runtime.Close(ctx) }

func (r *Runtime) ValidateModule(ctx context.Context, wasm []byte) error {
	digest := sha256.Sum256(wasm)
	_, err := r.compiledModule(ctx, hex.EncodeToString(digest[:]), wasm)
	if err != nil {
		return err
	}
	return nil
}

func (r *Runtime) Invoke(ctx context.Context, cacheKey string, wasm []byte, request InvocationRequest, host *InvocationHost) (*InvocationResponse, string, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, "", err
	}
	compiled, err := r.compiledModule(ctx, cacheKey, wasm)
	if err != nil {
		return nil, "", err
	}
	var stdout, stderr limitedBuffer
	stdout.limit = MaxInvocationOutput
	stderr.limit = MaxInvocationLog
	invokeCtx, cancel := context.WithTimeout(context.WithValue(ctx, hostContextKey{}, host), 60*time.Second)
	defer cancel()
	moduleConfig := wazero.NewModuleConfig().WithName("").WithStdin(bytes.NewReader(payload)).WithStdout(&stdout).WithStderr(&stderr)
	module, err := r.runtime.InstantiateModule(invokeCtx, compiled, moduleConfig)
	if module != nil {
		_ = module.Close(invokeCtx)
	}
	if err != nil {
		return nil, stderr.String(), fmt.Errorf("执行WASM插件失败: %w", err)
	}
	var response InvocationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, stderr.String(), fmt.Errorf("插件输出不是有效JSON: %w", err)
	}
	if !response.OK {
		return &response, stderr.String(), fmt.Errorf("插件执行失败: %s", response.Error)
	}
	return &response, stderr.String(), nil
}

func (r *Runtime) compiledModule(ctx context.Context, key string, wasm []byte) (wazero.CompiledModule, error) {
	if cached, ok := r.compiled.Load(key); ok {
		return cached.(wazero.CompiledModule), nil
	}
	compiled, err := r.runtime.CompileModule(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("编译WASM插件失败: %w", err)
	}
	actual, loaded := r.compiled.LoadOrStore(key, compiled)
	if loaded {
		_ = compiled.Close(ctx)
		return actual.(wazero.CompiledModule), nil
	}
	return compiled, nil
}

func (r *Runtime) hostHTTPRequest(ctx context.Context, module api.Module, requestPtr, requestLen, outputPtr, outputCap uint32) int32 {
	host := hostFromContext(ctx)
	if host == nil || !host.Permissions[PermissionPublicHTTP] || host.HTTPClient == nil {
		return writeHostError(module, outputPtr, outputCap, "permission_denied")
	}
	requestBytes, ok := module.Memory().Read(requestPtr, requestLen)
	if !ok {
		return -1
	}
	var pluginRequest HTTPRequest
	if err := json.Unmarshal(requestBytes, &pluginRequest); err != nil {
		return writeHostError(module, outputPtr, outputCap, "invalid_request")
	}
	if host.ValidateHTTPURL == nil || host.ValidateHTTPURL(pluginRequest.URL) != nil {
		return writeHostError(module, outputPtr, outputCap, "invalid_url")
	}
	method := strings.ToUpper(pluginRequest.Method)
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodPost {
		return writeHostError(module, outputPtr, outputCap, "method_not_allowed")
	}
	body, err := base64.StdEncoding.DecodeString(pluginRequest.Body)
	if err != nil || len(body) > 1024*1024 {
		return writeHostError(module, outputPtr, outputCap, "invalid_body")
	}
	req, err := http.NewRequestWithContext(ctx, method, pluginRequest.URL, bytes.NewReader(body))
	if err != nil {
		return writeHostError(module, outputPtr, outputCap, "invalid_url")
	}
	if len(pluginRequest.Headers) > 32 {
		return writeHostError(module, outputPtr, outputCap, "invalid_header")
	}
	headerBytes := 0
	for name, value := range pluginRequest.Headers {
		headerBytes += len(name) + len(value)
		if headerBytes > 32*1024 || !validPluginHTTPHeaderName(name) || strings.ContainsAny(value, "\r\n") || isBlockedPluginHTTPHeader(name) {
			return writeHostError(module, outputPtr, outputCap, "invalid_header")
		}
		req.Header.Set(name, value)
	}
	responseLimit, ok := pluginHTTPResponseLimit(pluginRequest.MaxResponseBytes)
	if !ok {
		return writeHostError(module, outputPtr, outputCap, "invalid_response_limit")
	}
	resp, err := host.HTTPClient.Do(req)
	if err != nil {
		return writeHostJSON(module, outputPtr, outputCap, HTTPResponse{Error: redactHTTPClientError(err)})
	}
	defer resp.Body.Close()
	if !validPluginHTTPResponseHeaders(resp.Header) {
		return writeHostError(module, outputPtr, outputCap, "response_headers_too_large")
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(responseLimit)+1))
	if err != nil || len(responseBody) > responseLimit {
		return writeHostError(module, outputPtr, outputCap, "response_too_large")
	}
	return writeHostJSON(module, outputPtr, outputCap, HTTPResponse{
		StatusCode: resp.StatusCode, Headers: resp.Header, Body: base64.StdEncoding.EncodeToString(responseBody),
	})
}

func pluginHTTPResponseLimit(requested int) (int, bool) {
	if requested == 0 {
		return MaxHTTPResponse, true
	}
	if requested < 0 || requested > MaxHTTPResponse {
		return 0, false
	}
	return requested, true
}

func validPluginHTTPResponseHeaders(headers http.Header) bool {
	data, err := json.Marshal(headers)
	return err == nil && len(data) <= MaxHTTPResponseHeaderJSON
}

func redactHTTPClientError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		parsed, parseErr := url.Parse(urlErr.URL)
		if parseErr == nil {
			parsed.RawQuery = ""
			parsed.Fragment = ""
			parsed.User = nil
			return fmt.Sprintf("%s %s: %v", urlErr.Op, parsed.String(), urlErr.Err)
		}
		return fmt.Sprintf("%s request failed: %v", urlErr.Op, urlErr.Err)
	}
	return err.Error()
}

func validPluginHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", char)) {
			return false
		}
	}
	return true
}

func (r *Runtime) hostFileQuery(ctx context.Context, module api.Module, requestPtr, requestLen, outputPtr, outputCap uint32) int32 {
	return r.hostFileOperation(ctx, module, requestPtr, requestLen, outputPtr, outputCap, "query")
}

func (r *Runtime) hostFileGet(ctx context.Context, module api.Module, requestPtr, requestLen, outputPtr, outputCap uint32) int32 {
	return r.hostFileOperation(ctx, module, requestPtr, requestLen, outputPtr, outputCap, "get")
}

func (r *Runtime) hostFileOperation(ctx context.Context, module api.Module, requestPtr, requestLen, outputPtr, outputCap uint32, operation string) int32 {
	host := hostFromContext(ctx)
	if host == nil || !host.Permissions[PermissionReadMetadata] || host.FileQuery == nil {
		return writeHostError(module, outputPtr, outputCap, "permission_denied")
	}
	host.mu.Lock()
	host.fileCount++
	count := host.fileCount
	host.mu.Unlock()
	if count > 10 {
		return writeHostError(module, outputPtr, outputCap, "file_query_limit")
	}
	requestBytes, ok := module.Memory().Read(requestPtr, requestLen)
	if !ok {
		return -1
	}
	var request FileQueryRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		return writeHostError(module, outputPtr, outputCap, "invalid_request")
	}
	request.Operation = operation
	if operation == "get" && strings.TrimSpace(request.UFID) == "" {
		return writeHostError(module, outputPtr, outputCap, "invalid_request")
	}
	if request.Limit <= 0 || request.Limit > 100 {
		request.Limit = 100
	}
	response, err := host.FileQuery(ctx, request)
	if err != nil {
		response.Error = err.Error()
	}
	host.mu.Lock()
	host.fileResults += len(response.Files)
	total := host.fileResults
	host.mu.Unlock()
	if total > 500 {
		return writeHostError(module, outputPtr, outputCap, "file_result_limit")
	}
	return writeHostJSON(module, outputPtr, outputCap, response)
}

func hostFromContext(ctx context.Context) *InvocationHost {
	host, _ := ctx.Value(hostContextKey{}).(*InvocationHost)
	return host
}

func writeHostError(module api.Module, ptr, capacity uint32, message string) int32 {
	return writeHostJSON(module, ptr, capacity, map[string]string{"error": message})
}

func writeHostJSON(module api.Module, ptr, capacity uint32, value interface{}) int32 {
	data, err := json.Marshal(value)
	if err != nil || uint32(len(data)) > capacity || !module.Memory().Write(ptr, data) {
		return -1
	}
	return int32(len(data))
}

func isBlockedPluginHTTPHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "x-forwarded-") {
		return true
	}
	switch lower {
	case "host", "connection", "content-length", "transfer-encoding", "keep-alive", "te", "trailer", "upgrade", "forwarded":
		return true
	default:
		return lower == ""
	}
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > b.limit {
		return 0, fmt.Errorf("插件输出超过限制")
	}
	return b.Buffer.Write(data)
}
