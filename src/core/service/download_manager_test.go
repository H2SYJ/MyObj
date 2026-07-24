package service

import (
	"context"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newManagerTestFactory(t *testing.T) *impl.RepositoryFactory {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DownloadTask{}, &models.UserInfo{}); err != nil {
		t.Fatal(err)
	}
	return impl.NewRepositoryFactory(db)
}

func TestDownloadManagerSkipsHeartbeatAfterRecentProgress(t *testing.T) {
	now := time.Now()
	manager := &DownloadManager{
		active: map[string]*activeDownloadTask{
			"task-1": {lastLeaseRefresh: now},
		},
	}
	if manager.shouldHeartbeat("task-1", now.Add(14*time.Second)) {
		t.Fatal("最近已有进度更新时不应重复写入租约")
	}
	if !manager.shouldHeartbeat("task-1", now.Add(15*time.Second)) {
		t.Fatal("长时间无进度时应继续写入租约")
	}
}

func TestDownloadManagerIdleFallbackAndNotify(t *testing.T) {
	factory := newManagerTestFactory(t)
	var queryCount atomic.Int32
	if err := factory.DB().Callback().Query().After("gorm:query").Register("test:count-download-scheduler-query", func(db *gorm.DB) {
		if db.Statement.Table == "download_task" {
			queryCount.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewDownloadManager(factory, t.TempDir())
	manager.Start()
	defer manager.Stop()

	deadline := time.Now().Add(time.Second)
	for queryCount.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if queryCount.Load() < 3 {
		t.Fatalf("启动扫描未完成，查询次数=%d", queryCount.Load())
	}
	baseline := queryCount.Load()
	time.Sleep(1200 * time.Millisecond)
	if got := queryCount.Load(); got != baseline {
		t.Fatalf("空闲调度器仍在高频查询: before=%d after=%d", baseline, got)
	}

	manager.Notify("", "")
	deadline = time.Now().Add(time.Second)
	for queryCount.Load() == baseline && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := queryCount.Load(); got == baseline {
		t.Fatal("Notify未立即唤醒下载调度查询")
	}
	manager.Stop()
	manager.Stop()
}

func TestSchedulerWakeDelayUsesEarliestDeadlineAndFallback(t *testing.T) {
	now := time.Now()
	due := now.Add(250 * time.Millisecond)
	if got := schedulerWakeDelay(now, &due); got != 250*time.Millisecond {
		t.Fatalf("精确到期延迟=%v，期望250ms", got)
	}
	far := now.Add(time.Minute)
	if got := schedulerWakeDelay(now, &far); got != schedulerFallbackInterval {
		t.Fatalf("远期任务延迟=%v，期望兜底间隔%v", got, schedulerFallbackInterval)
	}
	past := now.Add(-time.Second)
	if got := schedulerWakeDelay(now, &past); got != 0 {
		t.Fatalf("已到期任务延迟=%v，期望立即唤醒", got)
	}
}

func TestQuotaReservationIsAtomic(t *testing.T) {
	factory := newManagerTestFactory(t)
	user := &models.UserInfo{
		ID: "u", Name: "用户", UserName: "user", Password: "password", Email: "user@example.com",
		Phone: "", GroupID: 1, CreatedAt: custom_type.Now(), Space: 100, FreeSpace: 100, State: 0,
	}
	if err := factory.User().Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	tasks := []*models.DownloadTask{
		{ID: "a", UserID: user.ID, Type: 0, State: 1, RunToken: "run-a", FileSize: 80},
		{ID: "b", UserID: user.ID, Type: 0, State: 1, RunToken: "run-b", FileSize: 80},
	}
	for _, task := range tasks {
		if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewDownloadManager(factory, t.TempDir())
	if _, err := manager.ensureReservation(tasks[0], 80); err != nil {
		t.Fatalf("首次预留失败: %v", err)
	}
	if _, err := manager.ensureReservation(tasks[1], 80); err == nil {
		t.Fatal("剩余空间不足时第二个任务不应预留成功")
	}
	latestUser, _ := factory.User().GetByID(context.Background(), user.ID)
	if latestUser.FreeSpace != 20 {
		t.Fatalf("预留后的剩余空间错误: %d", latestUser.FreeSpace)
	}
	manager.releaseReservation(tasks[0].ID)
	latestUser, _ = factory.User().GetByID(context.Background(), user.ID)
	if latestUser.FreeSpace != 100 {
		t.Fatalf("释放预留后空间未恢复: %d", latestUser.FreeSpace)
	}
}

func TestRecoverInterruptedTasks(t *testing.T) {
	factory := newManagerTestFactory(t)
	tasks := []*models.DownloadTask{
		{ID: "plain", UserID: "u", Type: 0, State: 1, RunToken: "old"},
		{ID: "hls", UserID: "u", Type: enum.DownloadTaskTypeHLS.Value(), State: 1, RunToken: "old-hls"},
		{ID: "encrypted", UserID: "u", Type: 0, State: 0, EnableEncryption: true},
		{ID: "canceled", UserID: "u", Type: 0, State: 5},
	}
	for _, task := range tasks {
		if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewDownloadManager(factory, t.TempDir())
	if err := manager.recoverInterruptedTasks(); err != nil {
		t.Fatal(err)
	}
	plain, _ := factory.DownloadTask().GetByID(context.Background(), "plain")
	hlsTask, _ := factory.DownloadTask().GetByID(context.Background(), "hls")
	encrypted, _ := factory.DownloadTask().GetByID(context.Background(), "encrypted")
	canceled, _ := factory.DownloadTask().GetByID(context.Background(), "canceled")
	if plain.State != enum.DownloadTaskStateInit.Value() || plain.RunToken != "" {
		t.Fatalf("普通任务未重新排队: %#v", plain)
	}
	if hlsTask.State != enum.DownloadTaskStateInit.Value() || hlsTask.RunToken != "" {
		t.Fatalf("HLS任务未重新排队: %#v", hlsTask)
	}
	if encrypted.State != enum.DownloadTaskStatePaused.Value() {
		t.Fatalf("加密任务未转为暂停: %#v", encrypted)
	}
	if canceled.State != enum.DownloadTaskStateCanceled.Value() {
		t.Fatalf("终态任务不应变化: %#v", canceled)
	}
}

func TestHLSCredentialsErrorPausesTask(t *testing.T) {
	factory := newManagerTestFactory(t)
	task := &models.DownloadTask{ID: "hls", UserID: "u", Type: enum.DownloadTaskTypeHLS.Value(), State: 1, RunToken: "run"}
	if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	manager := NewDownloadManager(factory, t.TempDir())
	manager.finishTask(task, "", &download.HLSCredentialsRequiredError{StatusCode: 401})
	latest, err := factory.DownloadTask().GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.State != enum.DownloadTaskStatePaused.Value() || !latest.RequiresHeaders || latest.RunToken != "" {
		t.Fatalf("HLS凭据失效状态错误: %#v", latest)
	}
}

func TestSuccessfulTaskPersistsFinalFileMetadata(t *testing.T) {
	factory := newManagerTestFactory(t)
	task := &models.DownloadTask{
		ID: "hls-finished", UserID: "u", Type: enum.DownloadTaskTypeHLS.Value(), State: 1,
		RunToken: "run", FileName: "video.mp4", FileSize: 12345,
	}
	if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	manager := NewDownloadManager(factory, t.TempDir())
	manager.finishTask(task, "file-id", nil)
	latest, err := factory.DownloadTask().GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.State != enum.DownloadTaskStateFinished.Value() || latest.FileID != "file-id" ||
		latest.FileName != task.FileName || latest.FileSize != task.FileSize || latest.DownloadedSize != task.FileSize {
		t.Fatalf("完成任务的文件元数据未正确回写: %#v", latest)
	}
}

func TestStaleRunTokenCannotOverwritePausedState(t *testing.T) {
	factory := newManagerTestFactory(t)
	task := &models.DownloadTask{ID: "task", UserID: "u", Type: 0, State: 1, RunToken: "old"}
	if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	transitioned, err := factory.DownloadTask().Transition(context.Background(), task.ID, []int{1}, 2, map[string]interface{}{"run_token": ""})
	if err != nil || !transitioned {
		t.Fatalf("暂停任务失败: %v", err)
	}
	updated, err := factory.DownloadTask().UpdateIfRunToken(context.Background(), task.ID, "old", map[string]interface{}{"state": 4})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("旧执行令牌不应更新任务")
	}
	latest, _ := factory.DownloadTask().GetByID(context.Background(), task.ID)
	if latest.State != enum.DownloadTaskStatePaused.Value() {
		t.Fatalf("暂停状态被覆盖: %#v", latest)
	}
}

func TestStaleRunTokenDoesNotPublishFinishedEvent(t *testing.T) {
	factory := newManagerTestFactory(t)
	task := &models.DownloadTask{ID: "task-event", UserID: "u", Type: 0, State: 2, RunToken: ""}
	if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	hub := NewTaskEventHub()
	events, cancel := hub.Subscribe(task.UserID)
	defer cancel()
	manager := NewDownloadManager(factory, t.TempDir())
	manager.SetTaskEventHub(hub)

	manager.finishTask(&models.DownloadTask{
		ID: task.ID, UserID: task.UserID, Type: task.Type, State: 1, RunToken: "old", FileSize: 10,
	}, "file-id", nil)

	select {
	case event := <-events:
		t.Fatalf("旧执行令牌不得发布完成事件: %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEncryptedResumeRequiresPassword(t *testing.T) {
	factory := newManagerTestFactory(t)
	task := &models.DownloadTask{ID: "task", UserID: "u", Type: 0, State: 2, EnableEncryption: true}
	if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	manager := NewDownloadManager(factory, t.TempDir())
	if err := manager.Resume(task, "", nil); err == nil {
		t.Fatal("加密任务缺少密码时不应恢复")
	}
	if err := manager.Resume(task, "secret", nil); err != nil {
		t.Fatalf("提供密码后恢复失败: %v", err)
	}
	latest, _ := factory.DownloadTask().GetByID(context.Background(), task.ID)
	if latest.State != enum.DownloadTaskStateInit.Value() {
		t.Fatalf("任务未重新排队: %#v", latest)
	}
}

func TestRetryTerminalTaskResetsExecutionState(t *testing.T) {
	factory := newManagerTestFactory(t)
	manager := NewDownloadManager(factory, t.TempDir())
	nextRetryAt := time.Now().Add(time.Minute)
	leaseExpiresAt := time.Now().Add(time.Minute)
	for _, state := range []int{enum.DownloadTaskStateFailed.Value(), enum.DownloadTaskStateCanceled.Value()} {
		taskID := fmt.Sprintf("task-%d", state)
		userID := fmt.Sprintf("user-%d", state)
		user := &models.UserInfo{
			ID: userID, Name: userID, UserName: userID, Password: "password", Email: userID + "@example.com",
			CreatedAt: custom_type.Now(), Space: 1024, FreeSpace: 960,
		}
		if err := factory.User().Create(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		task := &models.DownloadTask{
			ID: taskID, UserID: userID, Type: enum.DownloadTaskTypeHttp.Value(), State: state,
			FileID: "file-id", DownloadedSize: 64, Progress: 50, Speed: 12, Path: "old-path",
			ErrorMsg: "旧错误", RetryCount: 3, NextRetryAt: &nextRetryAt, RunToken: "old-run",
			WorkerID: "old-worker", LeaseExpiresAt: &leaseExpiresAt, ReservedSize: 64, FinishTime: custom_type.Now(),
		}
		if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		if err := manager.Retry(task, "", nil); err != nil {
			t.Fatalf("状态%d任务重试失败: %v", state, err)
		}
		latest, err := factory.DownloadTask().GetByID(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if latest.State != enum.DownloadTaskStateInit.Value() || latest.FileID != "" || latest.DownloadedSize != 0 ||
			latest.Progress != 0 || latest.Speed != 0 || latest.Path != "" || latest.ErrorMsg != "" ||
			latest.RetryCount != 0 || latest.NextRetryAt != nil || latest.RunToken != "" || latest.WorkerID != "" ||
			latest.LeaseExpiresAt != nil || latest.ReservedSize != 0 || !latest.FinishTime.IsZero() {
			t.Fatalf("状态%d任务未完整清零: %#v", state, latest)
		}
		latestUser, err := factory.User().GetByID(context.Background(), userID)
		if err != nil || latestUser.FreeSpace != 1024 {
			t.Fatalf("状态%d任务预留空间未释放: user=%#v err=%v", state, latestUser, err)
		}
		updated, err := factory.DownloadTask().UpdateIfRunToken(context.Background(), taskID, "old-run", map[string]interface{}{"state": enum.DownloadTaskStateFailed.Value()})
		if err != nil || updated {
			t.Fatalf("状态%d任务仍可被旧执行令牌覆盖: updated=%v err=%v", state, updated, err)
		}
	}
}

func TestRetryRejectsInvalidStateAndRequiresEncryptionPassword(t *testing.T) {
	factory := newManagerTestFactory(t)
	manager := NewDownloadManager(factory, t.TempDir())
	queued := &models.DownloadTask{ID: "queued", UserID: "u", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateInit.Value()}
	encrypted := &models.DownloadTask{ID: "encrypted-retry", UserID: "u", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateFailed.Value(), EnableEncryption: true}
	for _, task := range []*models.DownloadTask{queued, encrypted} {
		if err := factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Retry(queued, "", nil); err == nil {
		t.Fatal("排队任务不应允许重试")
	}
	if err := manager.Retry(encrypted, "", nil); err == nil {
		t.Fatal("加密任务缺少密码时不应允许重试")
	}
	unchanged, _ := factory.DownloadTask().GetByID(context.Background(), encrypted.ID)
	if unchanged.State != enum.DownloadTaskStateFailed.Value() {
		t.Fatalf("缺少密码时任务状态被修改: %#v", unchanged)
	}
	if err := manager.Retry(encrypted, "secret", nil); err != nil {
		t.Fatalf("提供密码后重试失败: %v", err)
	}
	manager.mu.Lock()
	secret := manager.secrets[encrypted.ID]
	manager.mu.Unlock()
	if secret != "secret" {
		t.Fatal("重试密码未保存在本次运行内存中")
	}
}
