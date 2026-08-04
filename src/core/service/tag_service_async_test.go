package service

import (
	"testing"
	"time"

	"myobj/src/pkg/tagging"
)

func TestNewTagServiceDefersTagRuntimeInitialization(t *testing.T) {
	service, err := NewTagService(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	if runtime := service.globalRuntime.Load(); runtime != nil {
		t.Fatal("标签服务构造期间不应同步加载分词词典")
	}
	if !service.autoEnabled.Load() {
		t.Fatal("异步配置加载完成前应保留自动标签默认开启状态")
	}
	if limit := service.autoLimit.Load(); limit != tagging.DefaultAutoTagLimit {
		t.Fatalf("异步配置加载完成前的标签数量默认值错误: %d", limit)
	}
	select {
	case <-service.runtimeReady:
		t.Fatal("标签服务构造完成时分词词典不应已标记为就绪")
	default:
	}
}

func TestTagWorkersCanBeCancelledWhileRuntimeLoads(t *testing.T) {
	service, err := NewTagService(nil)
	if err != nil {
		t.Fatal(err)
	}

	service.wg.Add(2)
	go service.runPendingWorker()
	go service.runRebuildWorker()
	service.cancel()

	done := make(chan struct{})
	go func() {
		service.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("标签工作线程在分词词典加载期间无法停止")
	}
}
