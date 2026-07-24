package service

import (
	"context"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/models"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newSubscriptionSchedulerTestService(t *testing.T) *SubscriptionService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Subscription{}, &models.SubscriptionRun{}, &models.SubscriptionItem{}, &models.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &SubscriptionService{
		factory:       impl.NewRepositoryFactory(db),
		ctx:           ctx,
		cancel:        cancel,
		stop:          make(chan struct{}),
		wake:          make(chan struct{}, 1),
		thumbnailWake: make(chan struct{}, 1),
		sem:           make(chan struct{}, 2),
		thumbnailSem:  make(chan struct{}, 4),
		active:        map[string]context.CancelFunc{},
		pending:       map[string]bool{},
		location:      subscriptionLocation(),
	}
	// 阻止测试中的订阅运行进入插件执行，只验证调度入队行为。
	service.sem <- struct{}{}
	service.sem <- struct{}{}
	return service
}

func createScheduledSubscription(id string, next time.Time) models.Subscription {
	now := time.Now()
	return models.Subscription{
		ID: id, UserID: "user-1", Name: id, PluginID: "plugin-1", PluginVersion: "1.0.0",
		ScheduleTime: "23:59", SavePath: "/离线下载", InitialLimit: 10, MaxItemsPerRun: 100,
		SourceGeneration: 1, Enabled: true, Status: "ready", NextRunAt: &next, CreatedAt: now, UpdatedAt: now,
	}
}

func TestSubscriptionDispatchLimitsBatchAndReturnsImmediateWake(t *testing.T) {
	service := newSubscriptionSchedulerTestService(t)
	defer service.Stop()
	due := time.Now().Add(-time.Minute)
	for index := 0; index < 21; index++ {
		subscription := createScheduledSubscription(fmt.Sprintf("subscription-%02d", index), due.Add(time.Duration(index)*time.Millisecond))
		if err := service.factory.DB().Create(&subscription).Error; err != nil {
			t.Fatal(err)
		}
	}
	next, err := service.dispatchDue()
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || time.Until(*next) > time.Second {
		t.Fatalf("剩余到期订阅未请求立即唤醒: %v", next)
	}
	var runCount int64
	if err := service.factory.DB().Model(&models.SubscriptionRun{}).Count(&runCount).Error; err != nil {
		t.Fatal(err)
	}
	if runCount != 20 {
		t.Fatalf("单批订阅入队数量=%d，期望20", runCount)
	}
}

func TestSubscriptionNotifyWakesTimer(t *testing.T) {
	service := newSubscriptionSchedulerTestService(t)
	future := time.Now().Add(time.Hour)
	subscription := createScheduledSubscription("subscription-notify", future)
	if err := service.factory.DB().Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	service.Start()
	defer service.Stop()

	time.Sleep(100 * time.Millisecond)
	due := time.Now().Add(-time.Second)
	if err := service.factory.DB().Model(&models.Subscription{}).Where("id = ?", subscription.ID).Update("next_run_at", due).Error; err != nil {
		t.Fatal(err)
	}
	service.Notify()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := service.factory.DB().Model(&models.SubscriptionRun{}).Where("subscription_id = ?", subscription.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Notify未立即唤醒订阅调度")
}
