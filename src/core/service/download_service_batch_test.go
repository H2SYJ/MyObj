package service

import (
	"context"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/enum"
	"myobj/src/pkg/models"
	"testing"
)

func TestBatchCancelTasksContinuesAfterIndividualFailures(t *testing.T) {
	service := newRetryTestService(t)
	tasks := []*models.DownloadTask{
		{ID: "cancelable", UserID: "user", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateInit.Value()},
		{ID: "foreign", UserID: "other", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateInit.Value()},
		{ID: "finished", UserID: "user", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateFinished.Value()},
		{ID: "unsupported", UserID: "user", Type: enum.DownloadTaskTypeLocalFile.Value(), State: enum.DownloadTaskStateInit.Value()},
	}
	for _, task := range tasks {
		if err := service.factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}

	jsonResult := service.BatchCancelTasks(&request.BatchTaskOperationRequest{
		TaskIDs: []string{"cancelable", "foreign", "finished", "unsupported"},
	}, "user")
	result, ok := jsonResult.Data.(*response.BatchTaskOperationResponse)
	if !ok {
		t.Fatalf("批量取消返回类型错误: %T", jsonResult.Data)
	}
	if result.TotalCount != 4 || result.SuccessCount != 1 || result.FailedCount != 3 || len(result.FailedItems) != 3 {
		t.Fatalf("批量取消统计错误: %#v", result)
	}
	latest, err := service.factory.DownloadTask().GetByID(context.Background(), "cancelable")
	if err != nil {
		t.Fatal(err)
	}
	if latest.State != enum.DownloadTaskStateCanceled.Value() {
		t.Fatalf("可取消任务状态错误: %d", latest.State)
	}
}

func TestBatchDeleteTasksContinuesAfterIndividualFailures(t *testing.T) {
	service := newRetryTestService(t)
	tasks := []*models.DownloadTask{
		{ID: "finished", UserID: "user", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateFinished.Value()},
		{ID: "failed", UserID: "user", Type: enum.DownloadTaskTypeHLS.Value(), State: enum.DownloadTaskStateFailed.Value()},
		{ID: "active", UserID: "user", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateDownloading.Value()},
		{ID: "foreign", UserID: "other", Type: enum.DownloadTaskTypeHttp.Value(), State: enum.DownloadTaskStateCanceled.Value()},
	}
	for _, task := range tasks {
		if err := service.factory.DownloadTask().Create(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}

	jsonResult := service.BatchDeleteTasks(&request.BatchTaskOperationRequest{
		TaskIDs: []string{"finished", "active", "failed", "foreign"},
	}, "user")
	result, ok := jsonResult.Data.(*response.BatchTaskOperationResponse)
	if !ok {
		t.Fatalf("批量删除返回类型错误: %T", jsonResult.Data)
	}
	if result.TotalCount != 4 || result.SuccessCount != 2 || result.FailedCount != 2 || len(result.FailedItems) != 2 {
		t.Fatalf("批量删除统计错误: %#v", result)
	}
	for _, taskID := range []string{"finished", "failed"} {
		if _, err := service.factory.DownloadTask().GetByID(context.Background(), taskID); err == nil {
			t.Fatalf("任务%s应已删除", taskID)
		}
	}
	for _, taskID := range []string{"active", "foreign"} {
		if _, err := service.factory.DownloadTask().GetByID(context.Background(), taskID); err != nil {
			t.Fatalf("任务%s不应被删除: %v", taskID, err)
		}
	}
}
