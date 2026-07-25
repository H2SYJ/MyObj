package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestValidateModuleKeepsCachedModuleCompiled(t *testing.T) {
	ctx := context.Background()
	runtime, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("创建插件运行时失败: %v", err)
	}
	defer runtime.Close(ctx)

	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	digest := sha256.Sum256(wasm)
	cacheKey := hex.EncodeToString(digest[:])

	if err := runtime.ValidateModule(ctx, wasm); err != nil {
		t.Fatalf("首次校验WASM失败: %v", err)
	}
	compiled, err := runtime.compiledModule(ctx, cacheKey, wasm)
	if err != nil {
		t.Fatalf("读取编译缓存失败: %v", err)
	}
	if err := runtime.ValidateModule(ctx, wasm); err != nil {
		t.Fatalf("重复校验WASM失败: %v", err)
	}

	module, err := runtime.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		t.Fatalf("重复校验后实例化WASM失败: %v", err)
	}
	if err := module.Close(ctx); err != nil {
		t.Fatalf("关闭WASM实例失败: %v", err)
	}
}

func TestCompiledModuleConcurrentFirstLoad(t *testing.T) {
	const (
		attempts = 20
		workers  = 32
	)
	ctx := context.Background()
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	digest := sha256.Sum256(wasm)
	cacheKey := hex.EncodeToString(digest[:])

	for attempt := 1; attempt <= attempts; attempt++ {
		runtime, err := NewRuntime(ctx)
		if err != nil {
			t.Fatalf("第%d轮创建插件运行时失败: %v", attempt, err)
		}

		start := make(chan struct{})
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				<-start
				_, err := runtime.compiledModule(ctx, cacheKey, wasm)
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				_ = runtime.Close(ctx)
				t.Fatalf("第%d轮并发编译WASM失败: %v", attempt, err)
			}
		}

		compiled, err := runtime.compiledModule(ctx, cacheKey, wasm)
		if err != nil {
			_ = runtime.Close(ctx)
			t.Fatalf("第%d轮读取编译缓存失败: %v", attempt, err)
		}
		module, err := runtime.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
		if err != nil {
			_ = runtime.Close(ctx)
			t.Fatalf("第%d轮并发首次加载后实例化WASM失败: %v", attempt, err)
		}
		if err := module.Close(ctx); err != nil {
			_ = runtime.Close(ctx)
			t.Fatalf("第%d轮关闭WASM实例失败: %v", attempt, err)
		}
		if err := runtime.Close(ctx); err != nil {
			t.Fatalf("第%d轮关闭插件运行时失败: %v", attempt, err)
		}
	}
}
