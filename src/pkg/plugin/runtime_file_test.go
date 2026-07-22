package plugin

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHostFileQueryHasNoCallCountLimit(t *testing.T) {
	ctx := context.Background()
	runtime, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("创建插件运行时失败: %v", err)
	}
	defer runtime.Close(ctx)

	module := instantiateMemoryModule(t, ctx, runtime)
	defer module.Close(ctx)

	queryCount := 0
	host := &InvocationHost{
		Permissions: map[string]bool{PermissionReadMetadata: true},
		FileQuery: func(_ context.Context, request FileQueryRequest) (FileQueryResponse, error) {
			queryCount++
			if request.Operation != "query" {
				t.Fatalf("文件操作 = %q，期望 query", request.Operation)
			}
			return FileQueryResponse{}, nil
		},
	}
	requestBytes, err := json.Marshal(FileQueryRequest{Limit: 1})
	if err != nil {
		t.Fatalf("编码文件查询请求失败: %v", err)
	}

	const requestPtr = uint32(0)
	const outputPtr = uint32(4096)
	const outputCapacity = uint32(4096)
	if !module.Memory().Write(requestPtr, requestBytes) {
		t.Fatal("写入WASM请求内存失败")
	}
	for call := 1; call <= 11; call++ {
		written := runtime.hostFileQuery(
			context.WithValue(ctx, hostContextKey{}, host),
			module,
			requestPtr,
			uint32(len(requestBytes)),
			outputPtr,
			outputCapacity,
		)
		if written <= 0 {
			t.Fatalf("第 %d 次文件查询返回 %d", call, written)
		}
		responseBytes, ok := module.Memory().Read(outputPtr, uint32(written))
		if !ok {
			t.Fatalf("读取第 %d 次文件查询响应失败", call)
		}
		var response FileQueryResponse
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			t.Fatalf("解析第 %d 次文件查询响应失败: %v", call, err)
		}
		if response.Error != "" {
			t.Fatalf("第 %d 次文件查询返回错误 %q", call, response.Error)
		}
	}
	if queryCount != 11 {
		t.Fatalf("文件查询实际执行 %d 次，期望 11 次", queryCount)
	}
}
