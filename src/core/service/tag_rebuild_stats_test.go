package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

type tagStatRefreshRecorder struct {
	queryCount int
}

func (r *tagStatRefreshRecorder) LogMode(gormlogger.LogLevel) gormlogger.Interface { return r }
func (r *tagStatRefreshRecorder) Info(context.Context, string, ...interface{})     {}
func (r *tagStatRefreshRecorder) Warn(context.Context, string, ...interface{})     {}
func (r *tagStatRefreshRecorder) Error(context.Context, string, ...interface{})    {}
func (r *tagStatRefreshRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.Contains(sql, "COUNT(DISTINCT uft.uf_id)") {
		r.queryCount++
	}
}

func TestRebuildRefreshesTagStatsOncePerBatch(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db := newTagCloudTestService(t)
	seedTagCloud(t, db)
	if err := db.AutoMigrate(&models.TagRebuildFailure{}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	global := &models.TagRuleSet{
		ID: "global-batch", ScopeType: models.TagRuleScopeGlobal, Version: 1,
		Revision: 1, Status: models.TagRuleSetActive, CreatedAt: now, UpdatedAt: now,
		Rules: []models.TagRule{{
			ID: "rule-batch", RuleSetID: "global-batch", Type: models.TagRuleTypeRegex,
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
	snapshot, err := tagging.CompileSnapshot([]models.TagRuleSet{*global}, 20)
	if err != nil {
		t.Fatal(err)
	}
	service.globalRuntime.Store(&globalTagRuntime{ruleSet: global, snapshot: snapshot})
	service.autoEnabled.Store(true)
	service.autoLimit.Store(20)

	for _, file := range []struct {
		userID string
		ufID   string
		fileID string
		name   string
		hash   string
	}{
		{userID: "user-1", ufID: "one", fileID: "physical-one", name: "batch-one.mkv", hash: "batch-hash-1"},
		{userID: "user-1", ufID: "two", fileID: "physical-two", name: "batch-two.mkv", hash: "batch-hash-2"},
		{userID: "user-2", ufID: "other", fileID: "physical-other", name: "batch-other.mkv", hash: "batch-hash-3"},
	} {
		fileInfo := &models.FileInfo{
			ID: file.fileID, Name: file.name, RandomName: file.fileID, Mime: "video/x-matroska",
			FileHash: file.hash, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now(),
		}
		if err := db.Create(fileInfo).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&tagCloudTestUserFile{}).Where("user_id = ? AND uf_id = ?", file.userID, file.ufID).
			Updates(map[string]interface{}{"file_id": file.fileID, "file_name": file.name}).Error; err != nil {
			t.Fatal(err)
		}
	}

	lease := now.Add(tagWorkerLease)
	job := &models.TagRebuildJob{
		ID: "job-batch-stats", ScopeType: models.TagRuleScopeGlobal, TargetVersion: 1,
		Status: "running", RunToken: "job-token", LeaseExpires: &lease, StartedAt: &now,
		Total: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}

	recorder := &tagStatRefreshRecorder{}
	recordedDB := db.Session(&gorm.Session{Logger: recorder})
	service.factory = impl.NewRepositoryFactory(recordedDB)
	service.processRebuildJob(job)

	if recorder.queryCount != 1 {
		t.Fatalf("同一重建批次应只刷新一次标签统计，实际查询次数: %d", recorder.queryCount)
	}
	if err := db.First(job, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.Processed != 3 || job.Succeeded != 3 || job.Failed != 0 {
		t.Fatalf("重建任务状态异常: %+v", job)
	}
	var generated models.TagDefinition
	if err := db.Where("normalized_name = ? AND category_id = ?", "batch", "other").First(&generated).Error; err != nil {
		t.Fatal(err)
	}
	var stat models.UserTagStat
	if err := db.Where("user_id = ? AND tag_id = ?", "user-1", generated.ID).First(&stat).Error; err != nil {
		t.Fatal(err)
	}
	if stat.FileCount != 2 {
		t.Fatalf("批次标签统计异常: %+v", stat)
	}
	stat = models.UserTagStat{}
	if err := db.Where("user_id = ? AND tag_id = ?", "user-2", generated.ID).First(&stat).Error; err != nil {
		t.Fatal(err)
	}
	if stat.FileCount != 1 {
		t.Fatalf("跨用户批次标签统计异常: %+v", stat)
	}
}
