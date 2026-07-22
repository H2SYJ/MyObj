package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestHostHTTPRequestHonorsPluginResponseLimit(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		responseLimit int
		headers       http.Header
		wantError     string
		wantRequests  int
	}{
		{name: "接受刚好达到上限的响应", body: "1234", responseLimit: 4, wantRequests: 1},
		{name: "拒绝超过上限的响应", body: "1234", responseLimit: 3, wantError: "response_too_large", wantRequests: 1},
		{name: "拒绝无效上限", body: "1", responseLimit: -1, wantError: "invalid_response_limit"},
		{
			name:          "拒绝过大的响应头",
			body:          "1",
			responseLimit: 1,
			headers:       http.Header{"X-Large": []string{strings.Repeat("x", MaxHTTPResponseHeaderJSON)}},
			wantError:     "response_headers_too_large",
			wantRequests:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestCount := 0
			host := &InvocationHost{
				Permissions: map[string]bool{PermissionPublicHTTP: true},
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					requestCount++
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     test.headers,
						Body:       io.NopCloser(strings.NewReader(test.body)),
					}, nil
				})},
				ValidateHTTPURL: func(string) error { return nil },
			}
			response := invokeHostHTTPRequest(t, host, HTTPRequest{
				Method:           http.MethodGet,
				URL:              "https://example.com/data",
				MaxResponseBytes: test.responseLimit,
			})
			if requestCount != test.wantRequests {
				t.Fatalf("HTTP 请求次数 = %d，期望 %d", requestCount, test.wantRequests)
			}
			if response.Error != test.wantError {
				t.Fatalf("response.Error = %q，期望 %q", response.Error, test.wantError)
			}
		})
	}
}

func TestPluginHTTPResponseLimitKeepsLegacyDefault(t *testing.T) {
	if limit, ok := pluginHTTPResponseLimit(0); !ok || limit != MaxHTTPResponse {
		t.Fatalf("旧插件默认限制 = %d, ok = %v", limit, ok)
	}
	if _, ok := pluginHTTPResponseLimit(MaxHTTPResponse + 1); ok {
		t.Fatal("超过宿主硬上限的请求未被拒绝")
	}
}

func invokeHostHTTPRequest(t *testing.T, host *InvocationHost, request HTTPRequest) HTTPResponse {
	t.Helper()
	ctx := context.Background()
	runtime, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("创建插件运行时失败: %v", err)
	}
	defer runtime.Close(ctx)

	module := instantiateMemoryModule(t, ctx, runtime)
	defer module.Close(ctx)
	requestBytes, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("编码宿主请求失败: %v", err)
	}
	const requestPtr = uint32(0)
	const outputPtr = uint32(4096)
	const outputCapacity = uint32(4096)
	if !module.Memory().Write(requestPtr, requestBytes) {
		t.Fatal("写入WASM请求内存失败")
	}
	written := runtime.hostHTTPRequest(
		context.WithValue(ctx, hostContextKey{}, host),
		module,
		requestPtr,
		uint32(len(requestBytes)),
		outputPtr,
		outputCapacity,
	)
	if written <= 0 {
		t.Fatalf("宿主调用返回 %d", written)
	}
	responseBytes, ok := module.Memory().Read(outputPtr, uint32(written))
	if !ok {
		t.Fatal("读取WASM响应内存失败")
	}
	var response HTTPResponse
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		t.Fatalf("解析宿主响应失败: %v", err)
	}
	return response
}

func instantiateMemoryModule(t *testing.T, ctx context.Context, runtime *Runtime) api.Module {
	t.Helper()
	wasm := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x07, 0x0a, 0x01, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
	}
	compiled, err := runtime.runtime.CompileModule(ctx, wasm)
	if err != nil {
		t.Fatalf("编译测试WASM失败: %v", err)
	}
	defer compiled.Close(ctx)
	module, err := runtime.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		t.Fatalf("实例化测试WASM失败: %v", err)
	}
	return module
}
