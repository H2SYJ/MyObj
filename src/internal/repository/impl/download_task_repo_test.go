package impl

import (
	"context"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestListRunnableAppliesSchedulerConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:runnable-query?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	due := now.Add(-time.Second)
	future := now.Add(time.Minute)
	tasks := []*models.DownloadTask{
		{ID: "immediate", UserID: "user-a", Type: 0, State: 0, CreateTime: custom_type.JsonTime(now.Add(-6 * time.Minute))},
		{ID: "due", UserID: "user-b", Type: 9, State: 0, NextRetryAt: &due, CreateTime: custom_type.JsonTime(now.Add(-5 * time.Minute))},
		{ID: "future", UserID: "user-c", Type: 0, State: 0, NextRetryAt: &future, CreateTime: custom_type.JsonTime(now.Add(-4 * time.Minute))},
		{ID: "excluded-user", UserID: "busy-user", Type: 0, State: 0, CreateTime: custom_type.JsonTime(now.Add(-3 * time.Minute))},
		{ID: "excluded-batch", UserID: "user-d", Type: 0, State: 0, BatchID: "active-batch", CreateTime: custom_type.JsonTime(now.Add(-2 * time.Minute))},
		{ID: "torrent", UserID: "user-e", Type: 4, State: 0, CreateTime: custom_type.JsonTime(now.Add(-time.Minute))},
	}
	if err := db.Create(tasks).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewDownloadTaskRepository(db)
	options := repository.RunnableDownloadQueryOptions{
		ExcludedUserIDs:  []string{"busy-user"},
		ExcludedBatchIDs: []string{"active-batch"},
		AllowTorrent:     false,
	}
	runnable, err := repo.ListRunnable(context.Background(), now, 10, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(runnable) != 2 || runnable[0].ID != "immediate" || runnable[1].ID != "due" {
		t.Fatalf("可运行任务不符合预期: %#v", runnable)
	}
	next, err := repo.NextRunnableAt(context.Background(), now, options)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.Sub(future) > time.Millisecond || future.Sub(*next) > time.Millisecond {
		t.Fatalf("下一次唤醒时间不符合预期: got=%v want=%v", next, future)
	}
}

func TestListByFiltersPrioritizesDownloadingTasksBeforePagination(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:download-task-list-order?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tasks := []*models.DownloadTask{
		{ID: "finished-new", UserID: "user-a", Type: 0, State: 3, CreateTime: custom_type.JsonTime(now)},
		{ID: "downloading-old", UserID: "user-a", Type: 4, State: 1, CreateTime: custom_type.JsonTime(now.Add(-10 * time.Minute))},
		{ID: "queued-new", UserID: "user-a", Type: 5, State: 0, CreateTime: custom_type.JsonTime(now.Add(-time.Minute))},
		{ID: "downloading-new", UserID: "user-a", Type: 9, State: 1, CreateTime: custom_type.JsonTime(now.Add(-5 * time.Minute))},
	}
	if err := db.Create(tasks).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewDownloadTaskRepository(db)
	firstPage, err := repo.ListByFilters(context.Background(), "user-a", nil, []int{0, 4, 5, 9}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != "downloading-new" || firstPage[1].ID != "downloading-old" {
		t.Fatalf("第一页应仅包含按创建时间倒序排列的下载中任务: %#v", firstPage)
	}

	secondPage, err := repo.ListByFilters(context.Background(), "user-a", nil, []int{0, 4, 5, 9}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage) != 2 || secondPage[0].ID != "finished-new" || secondPage[1].ID != "queued-new" {
		t.Fatalf("非下载中任务应在后续页按创建时间倒序排列: %#v", secondPage)
	}
}
