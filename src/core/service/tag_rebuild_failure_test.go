package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/models"
)

type tagFailureTestUserFile struct {
	UserID      string     `gorm:"column:user_id"`
	FileID      string     `gorm:"column:file_id"`
	FileName    string     `gorm:"column:file_name"`
	DirectoryID int        `gorm:"column:directory_id"`
	IsPublic    bool       `gorm:"column:public"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	UFID        string     `gorm:"column:uf_id"`
}

func (tagFailureTestUserFile) TableName() string { return "user_files" }

func newTagFailureTestService(t *testing.T, modelsToMigrate ...interface{}) (*TagService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(modelsToMigrate...); err != nil {
		t.Fatal(err)
	}
	return &TagService{factory: impl.NewRepositoryFactory(db), wake: make(chan struct{}, 1)}, db
}

type tagWorkerTraceRecorder struct {
	errors []error
}

func (r *tagWorkerTraceRecorder) LogMode(gormlogger.LogLevel) gormlogger.Interface { return r }
func (r *tagWorkerTraceRecorder) Info(context.Context, string, ...interface{})     {}
func (r *tagWorkerTraceRecorder) Warn(context.Context, string, ...interface{})     {}
func (r *tagWorkerTraceRecorder) Error(context.Context, string, ...interface{})    {}
func (r *tagWorkerTraceRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), err error) {
	if err != nil {
		r.errors = append(r.errors, err)
	}
	_, _ = fc()
}

func TestEmptyTagWorkerQueuesDoNotEmitRecordNotFound(t *testing.T) {
	recorder := &tagWorkerTraceRecorder{}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: recorder})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UserFileTagState{}, &models.TagRebuildJob{}, &models.FileMetadataState{}); err != nil {
		t.Fatal(err)
	}
	recorder.errors = nil
	service := &TagService{factory: impl.NewRepositoryFactory(db), ctx: context.Background(), wake: make(chan struct{}, 1)}

	if state, ok := service.claimPendingState(); ok || state != nil {
		t.Fatalf("空自动标签队列不应领取到任务: state=%+v ok=%v", state, ok)
	}
	if job, ok := service.claimRebuildJob(); ok || job != nil {
		t.Fatalf("空重建队列不应领取到任务: job=%+v ok=%v", job, ok)
	}
	if state, ok := service.claimMetadataState(); ok || state != nil {
		t.Fatalf("空元数据队列不应领取到任务: state=%+v ok=%v", state, ok)
	}
	if err := db.Create(&models.FileMetadataState{
		FileID: "file-1", Status: models.TagStatePending, UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	state, ok := service.claimMetadataState()
	if !ok || state.FileID != "file-1" || state.Status != models.TagStateRunning || state.RunToken == "" {
		t.Fatalf("元数据队列没有正确领取待处理任务: state=%+v ok=%v", state, ok)
	}
	if len(recorder.errors) != 0 {
		t.Fatalf("空 Worker 队列不应产生数据库错误日志: %v", recorder.errors)
	}
}

func TestRetryRebuildFailureQueuesSingleFile(t *testing.T) {
	service, db := newTagFailureTestService(t,
		&tagFailureTestUserFile{}, &models.UserFileTagState{}, &models.TagRebuildFailure{},
	)
	now := time.Now()
	if err := db.Create(&tagFailureTestUserFile{
		UserID: "user-1", FileID: "file-1", UFID: "uf-1", FileName: "测试.mp4",
		DirectoryID: 1, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TagRebuildFailure{
		JobID: "job-1", UFID: "uf-1", UserID: "user-1", Status: models.TagRebuildFailureFailed,
		Error: "生成失败", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := service.RetryRebuildFailure(context.Background(), "job-1", "uf-1"); err != nil {
		t.Fatal(err)
	}

	var state models.UserFileTagState
	if err := db.First(&state, "uf_id = ?", "uf-1").Error; err != nil {
		t.Fatal(err)
	}
	if state.Status != models.TagStatePending || state.UserID != "user-1" {
		t.Fatalf("失败文件没有重新入队: %+v", state)
	}
	var failure models.TagRebuildFailure
	if err := db.First(&failure, "job_id = ? AND uf_id = ?", "job-1", "uf-1").Error; err != nil {
		t.Fatal(err)
	}
	if failure.Status != models.TagRebuildFailureRetrying || failure.RetryCount != 1 || failure.Error != "" {
		t.Fatalf("失败明细状态不正确: %+v", failure)
	}

	service.resolveQueuedRebuildFailures("uf-1")
	if err := db.First(&failure, "job_id = ? AND uf_id = ?", "job-1", "uf-1").Error; err != nil {
		t.Fatal(err)
	}
	if failure.Status != models.TagRebuildFailureResolved {
		t.Fatalf("重试成功后失败明细未解决: %+v", failure)
	}
}

func TestRetryRebuildJobClearsOldFailureDetails(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.TagRebuildJob{}, &models.TagRebuildFailure{})
	now := time.Now()
	job := models.TagRebuildJob{
		ID: "job-1", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 2,
		Status: "completed_with_errors", Total: 1, Processed: 1, Failed: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TagRebuildFailure{
		JobID: job.ID, UFID: "uf-1", UserID: "user-1", Status: models.TagRebuildFailureFailed,
		Error: "生成失败", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := service.RetryRebuildJob(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&job, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "pending" || job.Processed != 0 || job.Failed != 0 {
		t.Fatalf("重建任务没有正确重置: %+v", job)
	}
	var count int64
	if err := db.Model(&models.TagRebuildFailure{}).Where("job_id = ?", job.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("整任务重试后仍保留旧失败明细: %d", count)
	}
}

func TestCancelRebuildJobRejectsMissingOrFinishedJob(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.TagRebuildJob{})
	now := time.Now()
	job := models.TagRebuildJob{
		ID: "job-pending", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 2,
		Status: "pending", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.CancelRebuildJob(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&job, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "cancelled" || job.FinishedAt == nil {
		t.Fatalf("重建任务没有正确取消: %+v", job)
	}
	if err := service.CancelRebuildJob(context.Background(), job.ID); err == nil {
		t.Fatal("重复取消已结束任务应返回错误")
	}
	if err := service.CancelRebuildJob(context.Background(), "missing"); err == nil {
		t.Fatal("取消不存在任务应返回错误")
	}
}

func TestRebuildFailuresRequiresExistingJob(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.TagRebuildJob{}, &models.TagRebuildFailure{})
	now := time.Now()
	job := models.TagRebuildJob{
		ID: "job-1", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 2,
		Status: "completed_with_errors", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	failures, err := service.RebuildFailures(context.Background(), job.ID, "", 50)
	if err != nil || len(failures) != 0 {
		t.Fatalf("存在任务且没有失败明细时应返回空列表: failures=%+v err=%v", failures, err)
	}
	if _, err := service.RebuildFailures(context.Background(), "missing", "", 50); err != gorm.ErrRecordNotFound {
		t.Fatalf("不存在任务应返回gorm.ErrRecordNotFound，实际为%v", err)
	}
}

func TestDisabledAutoTagWorkCanResumeWithoutLosingProgress(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.UserFileTagState{}, &models.TagRebuildJob{})
	service.ctx = context.Background()
	now := time.Now()
	lease := now.Add(time.Minute)
	state := models.UserFileTagState{
		UFID: "uf-1", UserID: "user-1", Status: models.TagStateRunning,
		RunToken: "state-token", LeaseExpires: &lease, UpdatedAt: now,
	}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	service.releasePendingState(&state)
	var pausedState models.UserFileTagState
	if err := db.First(&pausedState, "uf_id = ?", state.UFID).Error; err != nil {
		t.Fatal(err)
	}
	if pausedState.Status != models.TagStatePending || pausedState.RunToken != "" || pausedState.LeaseExpires != nil {
		t.Fatalf("文件级任务暂停状态不正确: %+v", pausedState)
	}
	if claimed, ok := service.claimPendingState(); !ok || claimed.UFID != state.UFID {
		t.Fatalf("重新开启后文件级任务不可继续: claimed=%+v ok=%v", claimed, ok)
	}

	job := models.TagRebuildJob{
		ID: "job-1", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 2,
		Status: "running", Cursor: "uf-100", Processed: 100, Succeeded: 99, Failed: 1,
		RunToken: "job-token", LeaseExpires: &lease, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	service.pauseRebuildJob(&job)
	var pausedJob models.TagRebuildJob
	if err := db.First(&pausedJob, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pausedJob.Status != "pending" || pausedJob.Cursor != "uf-100" || pausedJob.Processed != 100 || pausedJob.RunToken != "" || pausedJob.LeaseExpires != nil {
		t.Fatalf("重建任务暂停时丢失进度: %+v", pausedJob)
	}
	claimedJob, ok := service.claimRebuildJob()
	if !ok || claimedJob.ID != job.ID || claimedJob.Cursor != "uf-100" || claimedJob.Processed != 100 {
		t.Fatalf("重新开启后重建任务不可续跑: claimed=%+v ok=%v", claimedJob, ok)
	}
}

func TestRebuildGuardRejectsCancelledOrSupersededWorker(t *testing.T) {
	_, db := newTagFailureTestService(t, &models.TagRebuildJob{})
	now := time.Now()
	job := models.TagRebuildJob{
		ID: "job-1", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 2,
		Status: "running", RunToken: "current-token", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	guard := &tagRebuildGuard{jobID: job.ID, runToken: job.RunToken}
	if err := validateTagRebuildGuard(db, guard); err != nil {
		t.Fatalf("有效重建运行令牌被拒绝: %v", err)
	}
	if err := db.Model(&models.TagRebuildJob{}).Where("id = ?", job.ID).
		Updates(map[string]interface{}{"status": "cancelled", "run_token": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := validateTagRebuildGuard(db, guard); !errors.Is(err, errStaleTagGeneration) {
		t.Fatalf("已取消任务仍可通过写入校验: %v", err)
	}
}

func TestClaimRebuildStateInvalidatesOlderFileWorker(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.UserFileTagState{})
	service.ctx = context.Background()
	now := time.Now()
	oldLease := now.Add(time.Minute)
	old := models.UserFileTagState{
		UFID: "uf-1", UserID: "user-1", Status: models.TagStateRunning,
		RunToken: "old-token", LeaseExpires: &oldLease, UpdatedAt: now,
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := service.claimRebuildState("user-1", "uf-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.RunToken == "" || claimed.RunToken == old.RunToken {
		t.Fatalf("重建没有取得新的文件运行令牌: %+v", claimed)
	}
	var oldCount int64
	if err := db.Model(&models.UserFileTagState{}).
		Where("uf_id = ? AND run_token = ?", old.UFID, old.RunToken).Count(&oldCount).Error; err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 {
		t.Fatalf("旧文件Worker令牌仍然有效: %d", oldCount)
	}
}

func TestTagSuggestionsIncludeOnlyOwnedOrPubliclyAllowedTags(t *testing.T) {
	service, db := newTagFailureTestService(t,
		&models.TagCategory{}, &models.TagDefinition{}, &tagFailureTestUserFile{},
		&models.UserFileTag{}, &models.UserFileTagExclusion{},
	)
	now := time.Now()
	if err := db.Create(&models.TagCategory{ID: "other", Code: "other", Name: "其他", Color: "#999999", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	tags := []models.TagDefinition{
		{ID: "own", Name: "我的私有", NormalizedName: "我的私有", CategoryID: "other", CreatedAt: now},
		{ID: "auto", Name: "公开自动", NormalizedName: "公开自动", CategoryID: "other", CreatedAt: now},
		{ID: "public", Name: "公开手工", NormalizedName: "公开手工", CategoryID: "other", CreatedAt: now},
		{ID: "private", Name: "他人私有", NormalizedName: "他人私有", CategoryID: "other", CreatedAt: now},
	}
	if err := db.Create(&tags).Error; err != nil {
		t.Fatal(err)
	}
	files := []tagFailureTestUserFile{
		{UserID: "user-1", FileID: "file-1", UFID: "uf-1", FileName: "一", DirectoryID: 1, CreatedAt: now},
		{UserID: "user-2", FileID: "file-2", UFID: "uf-2", FileName: "二", DirectoryID: 1, IsPublic: true, CreatedAt: now},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	bindings := []models.UserFileTag{
		{ID: "b1", UserID: "user-1", UFID: "uf-1", TagID: "own", SourceType: models.TagSourceManual, SourceKey: "user", Visibility: models.TagVisibilityPrivate, CreatedAt: now},
		{ID: "b2", UserID: "user-2", UFID: "uf-2", TagID: "auto", SourceType: models.TagSourceFilename, SourceKey: "name", Visibility: models.TagVisibilityInherit, CreatedAt: now},
		{ID: "b3", UserID: "user-2", UFID: "uf-2", TagID: "public", SourceType: models.TagSourceManual, SourceKey: "user", Visibility: models.TagVisibilityPublic, CreatedAt: now},
		{ID: "b4", UserID: "user-2", UFID: "uf-2", TagID: "private", SourceType: models.TagSourceManual, SourceKey: "user", Visibility: models.TagVisibilityPrivate, CreatedAt: now},
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatal(err)
	}

	suggestions, err := service.Suggestions(context.Background(), "user-1", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool, len(suggestions))
	for _, item := range suggestions {
		found[item.ID] = true
	}
	if !found["own"] || !found["auto"] || !found["public"] || found["private"] {
		t.Fatalf("标签建议公开范围错误: %+v", suggestions)
	}
}
