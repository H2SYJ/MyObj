package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newResourceBatchTestFactory(t *testing.T) *impl.RepositoryFactory {
	t.Helper()
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Share{}, &models.Recycled{}); err != nil {
		t.Fatal(err)
	}
	return impl.NewRepositoryFactory(db)
}

func TestBatchDeleteSharesDeduplicatesAndKeepsOwnershipFailures(t *testing.T) {
	factory := newResourceBatchTestFactory(t)
	now := custom_type.Now()
	for _, share := range []*models.Share{
		{ID: 1, UserID: "user", FileID: "owned", Token: "owned", ExpiresAt: now, CreatedAt: now},
		{ID: 2, UserID: "other", FileID: "foreign", Token: "foreign", ExpiresAt: now, CreatedAt: now},
	} {
		if err := factory.Share().Create(context.Background(), share); err != nil {
			t.Fatal(err)
		}
	}

	service := NewSharesService(factory, nil)
	jsonResult := service.BatchDeleteShares(&request.BatchDeleteShareRequest{IDs: []int{1, 1, 2, 999}}, "user")
	result, ok := jsonResult.Data.(*response.BatchOperationResponse)
	if !ok {
		t.Fatalf("批量删除分享返回类型错误: %T", jsonResult.Data)
	}
	if result.TotalCount != 3 || result.SuccessCount != 1 || result.FailedCount != 2 || len(result.FailedItems) != 2 {
		t.Fatalf("批量删除分享统计错误: %#v", result)
	}
	if _, err := factory.Share().GetByID(context.Background(), 1); err == nil {
		t.Fatal("当前用户的分享应已删除")
	}
	if _, err := factory.Share().GetByID(context.Background(), 2); err != nil {
		t.Fatalf("其他用户的分享不应被删除: %v", err)
	}
	if result.FailedItems[0].ItemID != "2" || result.FailedItems[0].Reason != "无权限删除该分享" {
		t.Fatalf("归属校验失败信息错误: %#v", result.FailedItems[0])
	}
}

func TestRecycledBatchOperationDeduplicatesAndContinues(t *testing.T) {
	service := &RecycledService{}
	called := make([]string, 0, 2)
	jsonResult := service.batchOperate([]string{"ok", "ok", "failed"}, func(id string) error {
		called = append(called, id)
		if id == "failed" {
			return errors.New("无权操作此文件")
		}
		return nil
	}, "批量操作完成")
	result, ok := jsonResult.Data.(*response.BatchOperationResponse)
	if !ok {
		t.Fatalf("回收站批量操作返回类型错误: %T", jsonResult.Data)
	}
	if len(called) != 2 || result.TotalCount != 2 || result.SuccessCount != 1 || result.FailedCount != 1 {
		t.Fatalf("回收站批量操作统计错误: called=%v result=%#v", called, result)
	}
	if len(result.FailedItems) != 1 || result.FailedItems[0].ItemID != "failed" || result.FailedItems[0].Reason != "无权操作此文件" {
		t.Fatalf("回收站失败项错误: %#v", result.FailedItems)
	}
}
