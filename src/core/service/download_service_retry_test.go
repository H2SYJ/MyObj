package service

import (
	"context"
	"io"
	"log/slog"
	"myobj/src/config"
	"myobj/src/core/domain/request"
	"myobj/src/pkg/download"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"testing"
)

func newRetryTestService(t *testing.T) *DownloadService {
	t.Helper()
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	factory := newManagerTestFactory(t)
	tempDir := t.TempDir()
	policy := download.NewNetworkPolicy()
	return &DownloadService{
		factory:       factory,
		tempDir:       tempDir,
		manager:       NewDownloadManager(factory, tempDir, policy),
		networkPolicy: policy,
	}
}

func TestRetryTaskValidatesOwnerTypeAndState(t *testing.T) {
	service := newRetryTestService(t)
	tasks := []*models.DownloadTask{
		{ID: "foreign", UserID: "other", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateFailed.Value()},
		{ID: "unsupported", UserID: "user", Type: enum.DownloadTaskTypeLocalFile.Value(), State: enum.DownloadTaskStateFailed.Value()},
		{ID: "running", UserID: "user", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateDownloading.Value()},
	}
	for _, task := range tasks {
		if err := service.factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	for _, taskID := range []string{"foreign", "unsupported", "running"} {
		if _, err := service.RetryTask(&request.TaskOperationRequest{TaskID: taskID}, "user"); err == nil {
			t.Fatalf("任务%s不应允许当前用户重试", taskID)
		}
	}
}

func TestGetLocalDownloadTaskScopesOwnerAndType(t *testing.T) {
	service := newRetryTestService(t)
	tasks := []*models.DownloadTask{
		{ID: "local", UserID: "user", Type: enum.DownloadTaskTypeLocalFile.Value(), State: enum.DownloadTaskStateFinished.Value()},
		{ID: "foreign", UserID: "other", Type: enum.DownloadTaskTypeLocalFile.Value(), State: enum.DownloadTaskStateFinished.Value()},
		{ID: "offline", UserID: "user", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateFinished.Value()},
	}
	for _, task := range tasks {
		if err := service.factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}

	result, err := service.GetLocalDownloadTask("local", "user")
	if err != nil || result.Code != 200 {
		t.Fatalf("当前用户应能查询自己的网盘下载任务: result=%#v err=%v", result, err)
	}
	for _, taskID := range []string{"foreign", "offline", "missing"} {
		result, err = service.GetLocalDownloadTask(taskID, "user")
		if err != nil || result.Code != 404 {
			t.Fatalf("任务%s不应对当前用户暴露: result=%#v err=%v", taskID, result, err)
		}
	}
}

func TestRetryTaskRequiresAndUpdatesExpiredHeaders(t *testing.T) {
	previousConfig := config.CONFIG
	config.CONFIG = &config.MyObjConfig{Auth: config.Auth{Secret: "retry-header-test-secret"}}
	t.Cleanup(func() { config.CONFIG = previousConfig })
	service := newRetryTestService(t)
	task := &models.DownloadTask{
		ID: "headers", UserID: "user", Type: enum.DownloadTaskTypeHLS.Value(), State: enum.DownloadTaskStateFailed.Value(),
		URL: "https://example.com/video.m3u8", RequiresHeaders: true,
	}
	if err := service.factory.DownloadTask().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RetryTask(&request.TaskOperationRequest{TaskID: task.ID}, task.UserID); err == nil {
		t.Fatal("凭据失效任务缺少新请求头时不应允许重试")
	}
	unchanged, _ := service.factory.DownloadTask().GetByID(context.Background(), task.ID)
	if unchanged.State != enum.DownloadTaskStateFailed.Value() || !unchanged.RequiresHeaders {
		t.Fatalf("请求头校验失败后任务状态被修改: %#v", unchanged)
	}
	headers := request.UniqueHTTPHeaders{"Authorization": "Bearer refreshed"}
	if _, err := service.RetryTask(&request.TaskOperationRequest{TaskID: task.ID, RequestHeaders: &headers}, task.UserID); err != nil {
		t.Fatalf("更新请求头后重试失败: %v", err)
	}
	latest, _ := service.factory.DownloadTask().GetByID(context.Background(), task.ID)
	if latest.State != enum.DownloadTaskStateInit.Value() || latest.RequiresHeaders || latest.RequestHeadersEncrypted == "" {
		t.Fatalf("更新请求头后的任务状态错误: %#v", latest)
	}
}

func TestRetryableEncryptedTaskResponseRequiresPassword(t *testing.T) {
	service := newRetryTestService(t)
	for _, state := range []int{
		enum.DownloadTaskStatePaused.Value(),
		enum.DownloadTaskStateFailed.Value(),
		enum.DownloadTaskStateCanceled.Value(),
	} {
		response := service.convertTaskToResponse(&models.DownloadTask{EnableEncryption: true, State: state})
		if !response.RequiresPassword {
			t.Fatalf("状态%d的加密任务应提示输入密码", state)
		}
	}
	response := service.convertTaskToResponse(&models.DownloadTask{EnableEncryption: true, State: enum.DownloadTaskStateInit.Value()})
	if response.RequiresPassword {
		t.Fatal("排队中的加密任务不应提示输入密码")
	}
}
