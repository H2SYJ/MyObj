package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

func newTagSchedulerTestService(factory *impl.RepositoryFactory) *TagService {
	return &TagService{
		factory: factory, ctx: context.Background(),
		pendingWake: make(chan struct{}, 1), rebuildWake: make(chan struct{}, 1),
		metadataWake: make(chan struct{}, 1), ruleWake: make(chan struct{}, 1),
	}
}

func TestTagWorkerNotificationsAreTypedAndCoalesced(t *testing.T) {
	service := newTagSchedulerTestService(nil)
	for index := 0; index < 100; index++ {
		service.notifyPending()
	}
	if len(service.pendingWake) != 1 {
		t.Fatalf("自动标签通知没有合并: %d", len(service.pendingWake))
	}
	if len(service.rebuildWake) != 0 || len(service.metadataWake) != 0 || len(service.ruleWake) != 0 {
		t.Fatal("自动标签通知错误唤醒了其他工作线程")
	}

	service.notifyRebuild()
	service.notifyMetadata()
	service.notifyRules()
	if len(service.rebuildWake) != 1 || len(service.metadataWake) != 1 || len(service.ruleWake) != 1 {
		t.Fatalf("分类通知状态不正确: rebuild=%d metadata=%d rules=%d", len(service.rebuildWake), len(service.metadataWake), len(service.ruleWake))
	}
}

func TestQueueUserFileDoesNotNotifyBeforeTransactionCommit(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.UserFileTagState{})
	rollbackErr := errors.New("强制回滚")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := service.QueueUserFile(context.Background(), tx, "user-1", "uf-rollback"); err != nil {
			return err
		}
		if len(service.pendingWake) != 0 {
			return errors.New("事务提交前错误发送了标签任务通知")
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("事务没有按预期回滚: %v", err)
	}
	var count int64
	if err := db.Model(&models.UserFileTagState{}).Where("uf_id = ?", "uf-rollback").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 || len(service.pendingWake) != 0 {
		t.Fatalf("回滚后遗留任务或通知: state=%d wake=%d", count, len(service.pendingWake))
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		return service.QueueUserFile(context.Background(), tx, "user-1", "uf-committed")
	}); err != nil {
		t.Fatal(err)
	}
	service.notifyPending()
	if len(service.pendingWake) != 1 {
		t.Fatal("事务提交成功后没有发送自动标签通知")
	}
}

func TestTagSchedulersUsePersistedRetryAndLeaseTimes(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.UserFileTagState{}, &models.TagRebuildJob{}, &models.FileMetadataState{})
	now := time.Now()
	retryAt := now.Add(2 * time.Minute)
	leaseAt := now.Add(time.Minute)
	if err := db.Create(&[]models.UserFileTagState{
		{UFID: "retry", UserID: "user-1", Status: models.TagStateFailed, NextRetryAt: &retryAt, UpdatedAt: now},
		{UFID: "lease", UserID: "user-1", Status: models.TagStateRunning, RunToken: "old", LeaseExpires: &leaseAt, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if state, ok, err := service.claimPendingState(); err != nil || ok || state != nil {
		t.Fatalf("未到期任务不应被认领: state=%+v ok=%v err=%v", state, ok, err)
	}
	next, err := service.nextPendingWakeAt(now)
	if err != nil {
		t.Fatal(err)
	}
	assertTimeClose(t, next, leaseAt)

	expired := now.Add(-time.Second)
	if err := db.Model(&models.UserFileTagState{}).Where("uf_id = ?", "lease").Update("lease_expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := service.claimPendingState()
	if err != nil || !ok || claimed.UFID != "lease" {
		t.Fatalf("过期租约没有被恢复: state=%+v ok=%v err=%v", claimed, ok, err)
	}

	if err := db.Create(&models.TagRebuildJob{
		ID: "rebuild", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 1,
		Status: "running", RunToken: "old", LeaseExpires: &retryAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if job, ok, err := service.claimRebuildJob(); err != nil || ok || job != nil {
		t.Fatalf("租约未到期的重建任务不应被认领: job=%+v ok=%v err=%v", job, ok, err)
	}
	rebuildNext, err := service.nextRebuildWakeAt(now)
	if err != nil {
		t.Fatal(err)
	}
	assertTimeClose(t, rebuildNext, retryAt)

	if err := db.Create(&models.FileMetadataState{
		FileID: "metadata", Status: models.TagStateFailed, NextRetryAt: &retryAt, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if state, ok, err := service.claimMetadataState(); err != nil || ok || state != nil {
		t.Fatalf("未到期的元数据任务不应被认领: state=%+v ok=%v err=%v", state, ok, err)
	}
	metadataNext, err := service.nextMetadataWakeAt(now)
	if err != nil {
		t.Fatal(err)
	}
	assertTimeClose(t, metadataNext, retryAt)
}

func TestRuleReloadWakesPausedWorkersWhenAutoTagIsEnabled(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.SysConfig{}, &models.TagRuleSet{}, &models.TagRule{})
	now := time.Now()
	if err := db.Create(&[]models.SysConfig{
		{Key: "auto_tag_enabled", Value: "true"},
		{Key: "auto_tag_limit", Value: "20"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TagRuleSet{
		ID: "global-v1", ScopeType: models.TagRuleScopeGlobal, ScopeID: "", Version: 1,
		Revision: 1, Status: models.TagRuleSetActive, CreatedBy: "system", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	service.autoEnabled.Store(false)
	service.autoLimit.Store(20)
	if err := service.reloadRuleRuntime(); err != nil {
		t.Fatal(err)
	}
	if !service.autoEnabled.Load() {
		t.Fatal("规则校验没有加载已开启的自动标签配置")
	}
	if len(service.pendingWake) != 1 || len(service.rebuildWake) != 1 {
		t.Fatalf("自动标签重新开启后没有唤醒暂停任务: pending=%d rebuild=%d", len(service.pendingWake), len(service.rebuildWake))
	}
	if len(service.metadataWake) != 0 || len(service.ruleWake) != 0 {
		t.Fatal("规则校验错误唤醒了无关工作线程")
	}
}

func TestMetadataBackfillIsBatchedAndNormalDispatchDoesNotScanFiles(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.FileInfo{}, &models.FileMetadataState{})
	files := make([]models.FileInfo, 0, 150)
	for index := 0; index < 150; index++ {
		id := fmt.Sprintf("file-%03d", index)
		files = append(files, models.FileInfo{
			ID: id, Name: id, RandomName: id, Size: 1, Mime: "text/plain", FileHash: id,
			CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
		})
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	seeded, err := service.seedMissingMetadataStates()
	if err != nil || seeded != metadataBackfillBatch {
		t.Fatalf("首批元数据回填数量错误: count=%d err=%v", seeded, err)
	}
	seeded, err = service.seedMissingMetadataStates()
	if err != nil || seeded != 50 {
		t.Fatalf("第二批元数据回填数量错误: count=%d err=%v", seeded, err)
	}
	seeded, err = service.seedMissingMetadataStates()
	if err != nil || seeded != 0 {
		t.Fatalf("元数据回填完成后仍返回任务: count=%d err=%v", seeded, err)
	}

	otherDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"-dispatch?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := otherDB.AutoMigrate(&models.FileInfo{}, &models.FileMetadataState{}); err != nil {
		t.Fatal(err)
	}
	otherService := newTagSchedulerTestService(impl.NewRepositoryFactory(otherDB))
	missing := models.FileInfo{
		ID: "missing", Name: "missing", RandomName: "missing", Size: 1,
		Mime: "text/plain", FileHash: "missing", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
	}
	if err := otherDB.Create(&missing).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := otherService.dispatchMetadata(); err != nil {
		t.Fatal(err)
	}
	var stateCount int64
	if err := otherDB.Model(&models.FileMetadataState{}).Count(&stateCount).Error; err != nil {
		t.Fatal(err)
	}
	if stateCount != 0 {
		t.Fatalf("普通元数据调度不应执行历史缺失扫描: %d", stateCount)
	}
}

type tagQueryCountRecorder struct {
	mu    sync.Mutex
	count int
}

func (r *tagQueryCountRecorder) LogMode(gormlogger.LogLevel) gormlogger.Interface { return r }
func (r *tagQueryCountRecorder) Info(context.Context, string, ...interface{})     {}
func (r *tagQueryCountRecorder) Warn(context.Context, string, ...interface{})     {}
func (r *tagQueryCountRecorder) Error(context.Context, string, ...interface{})    {}
func (r *tagQueryCountRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	_, _ = fc()
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
}
func (r *tagQueryCountRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}
func (r *tagQueryCountRecorder) Reset() {
	r.mu.Lock()
	r.count = 0
	r.mu.Unlock()
}

func TestPendingWorkerDoesNotUseShortIntervalPolling(t *testing.T) {
	recorder := &tagQueryCountRecorder{}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: recorder})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UserFileTagState{}); err != nil {
		t.Fatal(err)
	}
	recorder.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	close(ready)
	service := newTagSchedulerTestService(impl.NewRepositoryFactory(db))
	service.ctx = ctx
	service.runtimeReady = ready
	service.autoEnabled.Store(true)
	service.wg.Add(1)
	go service.runPendingWorker()

	deadline := time.Now().Add(time.Second)
	for recorder.Count() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if recorder.Count() < 3 {
		cancel()
		service.wg.Wait()
		t.Fatalf("自动标签工作线程没有完成首次恢复查询: %d", recorder.Count())
	}
	time.Sleep(20 * time.Millisecond)
	baseline := recorder.Count()
	time.Sleep(150 * time.Millisecond)
	if current := recorder.Count(); current != baseline {
		t.Fatalf("空闲工作线程仍在短周期查询数据库: before=%d after=%d", baseline, current)
	}
	cancel()
	service.wg.Wait()
}

func TestTagSchedulerDefaultIntervals(t *testing.T) {
	if schedulerFallbackInterval != 30*time.Second || schedulerErrorRetry != 5*time.Second {
		t.Fatalf("任务补偿或错误重试间隔异常: fallback=%v error=%v", schedulerFallbackInterval, schedulerErrorRetry)
	}
	if tagRuleReconcileInterval != 60*time.Second || metadataReconcileInterval != 10*time.Minute || metadataBackfillBatchDelay != time.Second {
		t.Fatalf("规则或元数据校验间隔异常: rules=%v metadata=%v backfill=%v", tagRuleReconcileInterval, metadataReconcileInterval, metadataBackfillBatchDelay)
	}
}

func assertTimeClose(t *testing.T, actual *time.Time, expected time.Time) {
	t.Helper()
	if actual == nil || actual.Sub(expected) > time.Second || expected.Sub(*actual) > time.Second {
		t.Fatalf("下次唤醒时间不正确: actual=%v expected=%v", actual, expected)
	}
}
