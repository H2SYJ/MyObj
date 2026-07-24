package service

import "testing"

func TestUploadFinalizeManagerCoalescesPendingRetry(t *testing.T) {
	manager := &UploadFinalizeManager{
		queue:   make(chan uploadFinalizeJob, 4),
		running: make(map[string]bool),
		pending: make(map[string]uploadFinalizeJob),
	}
	if !manager.Enqueue("task-1", "旧密码") {
		t.Fatal("首次任务应直接入队")
	}
	if manager.Enqueue("task-1", "新密码") {
		t.Fatal("运行中的任务应合并到待执行槽位")
	}
	first := <-manager.queue
	if first.filePassword != "旧密码" {
		t.Fatalf("首次任务密码不符合预期: %q", first.filePassword)
	}
	manager.finishJob("task-1")
	second := <-manager.queue
	if second.filePassword != "新密码" {
		t.Fatalf("待执行任务未保留最新密码: %q", second.filePassword)
	}
	manager.finishJob("task-1")
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.running["task-1"] || len(manager.pending) != 0 {
		t.Fatalf("任务状态未清理: running=%v pending=%v", manager.running, manager.pending)
	}
}
