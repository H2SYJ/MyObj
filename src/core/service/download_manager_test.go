package service

import (
	"context"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"testing"

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
