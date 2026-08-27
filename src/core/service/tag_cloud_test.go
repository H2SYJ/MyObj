package service

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

type tagCloudTestUserFile struct {
	UserID    string         `gorm:"column:user_id;primaryKey"`
	FileID    string         `gorm:"column:file_id"`
	FileName  string         `gorm:"column:file_name"`
	UFID      string         `gorm:"column:uf_id;primaryKey"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (tagCloudTestUserFile) TableName() string { return "user_files" }

func newTagCloudTestService(t *testing.T) (*TagService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&tagCloudTestUserFile{}, &models.FileInfo{}, &models.TagCategory{}, &models.TagDefinition{},
		&models.UserFileTag{}, &models.UserDirectoryTag{}, &models.UserFileTagExclusion{},
		&models.UserTagStat{}, &models.UserFileTagState{},
		&models.FileMetadata{}, &models.FileMetadataState{}, &models.TagRuleSet{}, &models.TagRule{}, &models.TagRebuildJob{},
	); err != nil {
		t.Fatal(err)
	}
	service := &TagService{factory: impl.NewRepositoryFactory(db), ctx: context.Background(), rebuildWake: make(chan struct{}, 1), ruleWake: make(chan struct{}, 1)}
	return service, db
}

func seedTagCloud(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	for _, category := range []models.TagCategory{
		{ID: "other", Code: "other", Name: "其他", Color: "#64748b", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "title", Code: "title", Name: "标题", Color: "#409eff", Enabled: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&category).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tag := range []models.TagDefinition{
		{ID: "normal", Name: "统一名称", NormalizedName: "统一名称", CategoryID: "other", CreatedAt: now},
		{ID: "system", Name: "系统", NormalizedName: "系统", CategoryID: "other", Builtin: true, CreatedAt: now},
	} {
		if err := db.Create(&tag).Error; err != nil {
			t.Fatal(err)
		}
	}
	files := []tagCloudTestUserFile{{UserID: "user-1", UFID: "one"}, {UserID: "user-1", UFID: "two"}, {UserID: "user-2", UFID: "other"}}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	bindings := []models.UserFileTag{
		{ID: "a", UserID: "user-1", UFID: "one", TagID: "normal", SourceType: models.TagSourceManual, SourceKey: "user", CreatedAt: now},
		{ID: "b", UserID: "user-1", UFID: "one", TagID: "normal", SourceType: models.TagSourceFilename, SourceKey: "auto", CreatedAt: now},
		{ID: "c", UserID: "user-1", UFID: "two", TagID: "normal", SourceType: models.TagSourceManual, SourceKey: "user", CreatedAt: now},
		{ID: "d", UserID: "user-1", UFID: "two", TagID: "system", SourceType: models.TagSourceManual, SourceKey: "user", CreatedAt: now},
		{ID: "e", UserID: "user-2", UFID: "other", TagID: "normal", SourceType: models.TagSourceManual, SourceKey: "user", CreatedAt: now},
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserDirectoryTag{
		ID: "directory-normal", UserID: "user-1", DirectoryID: 7, TagID: "normal", CreatedBy: "user-1", CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func refreshSeededTagCloudStats(t *testing.T, service *TagService, db *gorm.DB, userID string, tagIDs ...string) {
	t.Helper()
	if err := service.refreshUserTagStats(context.Background(), db, userID, tagIDs); err != nil {
		t.Fatal(err)
	}
}

func TestTagCloudCountsDistinctFilesPerUser(t *testing.T) {
	service, db := newTagCloudTestService(t)
	seedTagCloud(t, db)
	refreshSeededTagCloudStats(t, service, db, "user-1", "normal", "system")
	refreshSeededTagCloudStats(t, service, db, "user-2", "normal")

	cloud, err := service.TagCloud(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.Tags) != 2 || cloud.Tags[0].ID != "normal" || cloud.Tags[0].FileCount != 2 {
		t.Fatalf("标签云统计错误: %+v", cloud.Tags)
	}
	other, err := service.TagCloud(context.Background(), "user-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Tags) != 1 || other.Tags[0].ID != "normal" || other.Tags[0].FileCount != 1 {
		t.Fatalf("其他用户标签云统计错误: %+v", other.Tags)
	}
}

func TestSharedTagNameAndCategoryAreUsedAcrossViews(t *testing.T) {
	service, db := newTagCloudTestService(t)
	seedTagCloud(t, db)
	refreshSeededTagCloudStats(t, service, db, "user-1", "normal", "system")

	compact, err := service.CompactTags(context.Background(), "user-1", "user-1", []string{"one"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(compact["one"]) != 1 || compact["one"][0].Name != "统一名称" || compact["one"][0].CategoryCode != "other" {
		t.Fatalf("文件标签摘要未使用共享定义: %+v", compact)
	}
	directoryTags, err := service.CompactDirectoryTags(context.Background(), "user-1", []int{7})
	if err != nil {
		t.Fatal(err)
	}
	if len(directoryTags[7]) != 1 || directoryTags[7][0].Name != "统一名称" {
		t.Fatalf("目录标签摘要未使用共享定义: %+v", directoryTags)
	}
	fileTags, err := service.GetFileTags(context.Background(), "user-1", "one")
	if err != nil {
		t.Fatal(err)
	}
	if len(fileTags.Tags) != 1 || fileTags.Tags[0].Name != "统一名称" {
		t.Fatalf("文件标签详情未使用共享定义: %+v", fileTags.Tags)
	}
	suggestions, err := service.SuggestionsForTarget(context.Background(), "user-1", "统一", nil, "user", "directory", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].ID != "normal" || suggestions[0].Name != "统一名称" {
		t.Fatalf("标签建议未使用共享定义: %+v", suggestions)
	}
}

func TestTagCloudUsesStatsAndRefreshesManualChanges(t *testing.T) {
	service, db := newTagCloudTestService(t)
	seedTagCloud(t, db)
	refreshSeededTagCloudStats(t, service, db, "user-1", "normal", "system")

	if err := service.UpdateManualTags(context.Background(), "user-1", []string{"one"}, nil, []string{"normal"}); err != nil {
		t.Fatal(err)
	}
	cloud, err := service.TagCloud(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range cloud.Tags {
		if tag.ID == "normal" && tag.FileCount != 2 {
			t.Fatalf("删除单一来源不应影响去重文件数: %+v", cloud.Tags)
		}
	}
	if err := service.UpdateManualTags(context.Background(), "user-1", []string{"two"}, nil, []string{"normal"}); err != nil {
		t.Fatal(err)
	}
	cloud, err = service.TagCloud(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range cloud.Tags {
		if tag.ID == "normal" && tag.FileCount != 1 {
			t.Fatalf("删除最后一个文件来源后统计未刷新: %+v", cloud.Tags)
		}
	}
}

func TestGenerateUserFileRefreshesTagStatsForOldAndNewAutomaticTags(t *testing.T) {
	if logger.LOG == nil {
		logger.LOG = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service, db := newTagCloudTestService(t)
	seedTagCloud(t, db)
	now := time.Now()
	for _, category := range []models.TagCategory{
		{ID: "codec", Code: "codec", Name: "编码", Color: "#7b61ff", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "resolution", Code: "resolution", Name: "分辨率", Color: "#909399", Enabled: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&category).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.TagDefinition{ID: "hevc", Name: "hevc", NormalizedName: "hevc", CategoryID: "codec", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserFileTag{ID: "old-auto", UserID: "user-1", UFID: "one", TagID: "hevc", SourceType: models.TagSourceFilename, SourceKey: "old", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	refreshSeededTagCloudStats(t, service, db, "user-1", "hevc")
	global := &models.TagRuleSet{
		ID: "global-auto", Version: 1, Revision: 1, Status: models.TagRuleSetActive, CreatedAt: now, UpdatedAt: now,
		Rules: []models.TagRule{{
			ID: "rule-1080p", RuleSetID: "global-auto", Type: models.TagRuleTypeRegex,
			TargetField: "basename", Pattern: `(?i)(1080p)`, Replacement: "$1",
			CategoryID: "resolution", Weight: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
		}},
	}
	if err := db.Create(&models.TagRuleSet{
		ID: global.ID, Version: global.Version, Revision: global.Revision, Status: global.Status,
		CreatedAt: global.CreatedAt, UpdatedAt: global.UpdatedAt,
	}).Error; err != nil {
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
	if err := db.Create(&models.FileInfo{ID: "physical-one", Name: "电影.1080p.mkv", RandomName: "physical-one", Mime: "video/x-matroska", FileHash: "hash-one", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&tagCloudTestUserFile{}).Where("user_id = ? AND uf_id = ?", "user-1", "one").Updates(map[string]interface{}{
		"file_id": "physical-one", "file_name": "电影.1080p.mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserFileTagState{UFID: "one", UserID: "user-1", Status: models.TagStateRunning, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	if err := service.GenerateUserFile(context.Background(), "user-1", "one", "", 0); err != nil {
		t.Fatal(err)
	}

	var oldStatCount int64
	if err := db.Model(&models.UserTagStat{}).Where("user_id = ? AND tag_id = ?", "user-1", "hevc").Count(&oldStatCount).Error; err != nil {
		t.Fatal(err)
	}
	if oldStatCount != 0 {
		t.Fatalf("旧自动标签统计未清理: %d", oldStatCount)
	}
	var generated models.TagDefinition
	if err := db.Where("normalized_name = ? AND category_id = ?", "1080p", "resolution").First(&generated).Error; err != nil {
		t.Fatal(err)
	}
	var stat models.UserTagStat
	if err := db.Where("user_id = ? AND tag_id = ?", "user-1", generated.ID).First(&stat).Error; err != nil {
		t.Fatal(err)
	}
	if stat.FileCount != 1 {
		t.Fatalf("新自动标签统计异常: %+v", stat)
	}
}
