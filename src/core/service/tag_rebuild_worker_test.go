package service

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

// 本文件覆盖标签重建任务的失败处理：批提交失败熔断、终态写入守卫、重试范围。
// 这些路径此前都是静默 return，任务会永久停留在 running 且进度不推进。

// newRebuildWorkerTestService 构造一个跑得通单次重建的环境：
// 活动规则快照已加载、一个可生成标签的文件、一个处于 running 的任务。
func newRebuildWorkerTestService(t *testing.T, jobID string) (*TagService, *gorm.DB, *models.TagRebuildJob) {
	t.Helper()
	service, db := newTagCloudTestService(t)
	seedTagCloud(t, db)

	now := time.Now()
	global := &models.TagRuleSet{
		ID: "global-worker", Version: 1,
		Revision: 1, Status: models.TagRuleSetActive, CreatedAt: now, UpdatedAt: now,
		Rules: []models.TagRule{{
			ID: "rule-worker", RuleSetID: "global-worker", Type: models.TagRuleTypeRegex,
			TargetField: "basename", Pattern: `(?i)(batch)`, Replacement: "$1",
			CategoryID: "other", Weight: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
		}},
	}
	if err := db.Omit("Rules").Create(global).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&global.Rules).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := tagging.CompileSnapshot(*global, 20)
	if err != nil {
		t.Fatal(err)
	}
	service.globalRuntime.Store(&globalTagRuntime{ruleSet: global, snapshot: snapshot})
	service.autoEnabled.Store(true)
	service.autoLimit.Store(20)

	// seedTagCloud 已建好 one/two/other 三个文件，这里补齐实体文件信息和文件名，
	// 让文件名能命中规则并产出标签，批次内才有需要刷新统计的标签。
	total := int64(0)
	for _, file := range []struct {
		userID string
		ufID   string
		fileID string
	}{
		{userID: "user-1", ufID: "one", fileID: "physical-one"},
		{userID: "user-1", ufID: "two", fileID: "physical-two"},
		{userID: "user-2", ufID: "other", fileID: "physical-other"},
	} {
		name := "batch-" + file.ufID + ".mkv"
		fileInfo := &models.FileInfo{
			ID: file.fileID, Name: name, RandomName: file.fileID,
			Mime: "video/x-matroska", FileHash: "batch-hash-" + file.ufID,
			CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
		}
		if err := db.Create(fileInfo).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&tagCloudTestUserFile{}).Where("user_id = ? AND uf_id = ?", file.userID, file.ufID).
			Updates(map[string]interface{}{"file_id": file.fileID, "file_name": name}).Error; err != nil {
			t.Fatal(err)
		}
		total++
	}
	if err := db.AutoMigrate(&models.TagRebuildFailure{}); err != nil {
		t.Fatal(err)
	}

	lease := now.Add(tagWorkerLease)
	job := &models.TagRebuildJob{
		ID: jobID, TargetVersion: 1, Status: "running", RunToken: "job-token",
		LeaseExpires: &lease, StartedAt: &now, Total: total, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	return service, db, job
}

// TestRebuildJobFailsAfterRepeatedBatchCommitFailures 覆盖批提交连续失败的场景。
// 修复前：每次失败都静默 return，任务永远停留在 running 且 processed 不增长；
// 修复后：累计到阈值后强制置为 failed 并留下 last_error。
func TestRebuildJobFailsAfterRepeatedBatchCommitFailures(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db, job := newRebuildWorkerTestService(t, "job-commit-failure")

	// 让批次内的标签统计刷新失败：文件级标签生成仍能成功，但提交批次的事务会回滚。
	if err := db.Exec("DROP TABLE user_tag_stat").Error; err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= tagRebuildMaxBatchFailures; attempt++ {
		service.processRebuildJob(job)
		var stored models.TagRebuildJob
		if err := db.First(&stored, "id = ?", job.ID).Error; err != nil {
			t.Fatal(err)
		}
		if attempt < tagRebuildMaxBatchFailures {
			// 未达阈值时任务保持 running，等待租约到期后被重新捞起重试。
			if stored.Status != "running" {
				t.Fatalf("第%d次提交失败后任务应仍为running，实际: %s", attempt, stored.Status)
			}
			continue
		}
		if stored.Status != "failed" {
			t.Fatalf("连续%d次提交失败后任务应被熔断为failed，实际: %s", tagRebuildMaxBatchFailures, stored.Status)
		}
		if stored.LastError == "" {
			t.Fatal("熔断后必须记录 last_error，否则管理员在界面上看不到任何失败原因")
		}
		if stored.Processed != 0 {
			t.Fatalf("批次未提交成功时进度不应推进，实际: %d", stored.Processed)
		}
	}

	// 计数必须在熔断后清理，避免同一个任务再次被捞起时立刻触发熔断。
	if _, ok := service.rebuildBatchFailures.Load(job.ID); ok {
		t.Fatal("熔断后应清理连续失败计数")
	}
}

// TestRebuildJobRecoversAfterSuccessfulCommit 确认成功提交后计数归零：
// 一次偶发失败不应该让任务在后续批次里被误熔断。
func TestRebuildJobRecoversAfterSuccessfulCommit(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db, job := newRebuildWorkerTestService(t, "job-commit-recovery")

	service.bumpRebuildBatchFailure(job)
	service.bumpRebuildBatchFailure(job)
	service.processRebuildJob(job)

	var stored models.TagRebuildJob
	if err := db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "completed" {
		t.Fatalf("提交成功后任务应正常完成，实际: %s (%s)", stored.Status, stored.LastError)
	}
	if _, ok := service.rebuildBatchFailures.Load(job.ID); ok {
		t.Fatal("提交成功后应清零连续失败计数")
	}
}

// TestFinishRebuildJobGuardsRunningStatus 覆盖终态写入守卫。
// cancel 和 retry 都会把 run_token 置空，缺少 status='running' 守卫时，
// 持有空 token 的任务对象会把已取消的任务重新改成终态。
func TestFinishRebuildJobGuardsRunningStatus(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db, job := newRebuildWorkerTestService(t, "job-finish-guard")

	if result := db.Model(&models.TagRebuildJob{}).Where("id = ?", job.ID).
		Update("status", "cancelled"); result.Error != nil {
		t.Fatal(result.Error)
	}
	// 模拟 worker 仍持有一个 token 已被清空的旧任务对象。
	stale := *job
	stale.RunToken = ""

	if rows := service.finishRebuildJob(&stale, "completed", "完成"); rows != 0 {
		t.Fatalf("已取消的任务不应被写回终态，受影响行数: %d", rows)
	}
	var stored models.TagRebuildJob
	if err := db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "cancelled" {
		t.Fatalf("已取消的任务状态被改写为: %s", stored.Status)
	}
}

// TestFailRebuildJobByIDIgnoresStaleRunToken 覆盖熔断写入：
// 当租约已被其他实例接管时 run_token 早已变化，带 token 的写入会落空，
// 任务会重新回到"永远 running 但进度不涨"的死循环。
func TestFailRebuildJobByIDIgnoresStaleRunToken(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db, job := newRebuildWorkerTestService(t, "job-circuit-breaker")

	service.failRebuildJobByID(job.ID, "连续提交失败")

	var stored models.TagRebuildJob
	if err := db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" {
		t.Fatalf("熔断应把任务置为failed，实际: %s", stored.Status)
	}
	if stored.LastError != "连续提交失败" {
		t.Fatalf("熔断应记录失败原因，实际: %q", stored.LastError)
	}
	if stored.RunToken != "" {
		t.Fatalf("熔断后应释放run_token，实际: %q", stored.RunToken)
	}
}

// TestRetryRebuildJobAcceptsSuperseded 覆盖废弃任务可重试。
// 发布草稿或保存设置都会把存量任务批量置为 superseded，这个状态此前不在
// 白名单内，管理员点重试只会收到一个前端不展示的错误。
func TestRetryRebuildJobAcceptsSupersededAndAlignsVersion(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db, job := newRebuildWorkerTestService(t, "job-retry-superseded")

	if err := db.Model(&models.TagRebuildJob{}).Where("id = ?", job.ID).
		Updates(map[string]interface{}{"status": "superseded", "target_version": 0, "processed": 9}).Error; err != nil {
		t.Fatal(err)
	}

	if err := service.RetryRebuildJob(context.Background(), job.ID); err != nil {
		t.Fatalf("superseded 任务应可重试: %v", err)
	}
	var stored models.TagRebuildJob
	if err := db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "pending" {
		t.Fatalf("重试后任务应为pending，实际: %s", stored.Status)
	}
	// 目标版本必须对齐当前活动快照，否则任务一被捞起就会再次因版本不匹配被废弃。
	if stored.TargetVersion != 1 {
		t.Fatalf("重试后目标版本应刷新为当前活动版本1，实际: %d", stored.TargetVersion)
	}
	if stored.Processed != 0 || stored.Cursor != "" {
		t.Fatalf("重试后应重置进度，实际 processed=%d cursor=%q", stored.Processed, stored.Cursor)
	}
}

// TestInitializeRuntimeFallsBackWhenSettingsFail 覆盖初始化降级：
// 读取自动标签配置失败时不能中断，否则 runtimeReady 永不关闭，
// 重建与自动标签两个 worker 会永久阻塞且没有任何日志。
func TestInitializeRuntimeFallsBackWhenSettingsFail(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db := newTagCloudTestService(t)
	// 建一个结构不兼容的 sys_config 表，让读取配置返回非 ErrRecordNotFound 的错误，
	// 这正是修复前会让 runtimeReady 永不关闭、两个 worker 永久阻塞的分支。
	if err := db.Exec("CREATE TABLE sys_config (bogus integer)").Error; err != nil {
		t.Fatal(err)
	}
	service.runtimeReady = make(chan struct{})
	service.runtimeReadyOnce = sync.Once{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.ctx = ctx

	if err := service.reloadSettings(ctx); err == nil {
		t.Fatal("测试前提不成立：读取自动标签配置应当失败")
	}
	if err := service.initializeRuntime(ctx); err != nil {
		t.Fatalf("配置读取失败时初始化应降级而不是报错: %v", err)
	}
	if service.globalRuntime.Load() == nil {
		t.Fatal("配置读取失败时也应降级出可用的全局标签规则")
	}
	// waitForRuntime 必须立刻返回，否则 worker 会永久阻塞。
	done := make(chan bool, 1)
	go func() { done <- service.waitForRuntime() }()
	select {
	case ready := <-done:
		if !ready {
			t.Fatal("运行时已降级就绪，waitForRuntime 不应返回 false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitForRuntime 阻塞，worker 将永久挂起")
	}
	_ = db
}
