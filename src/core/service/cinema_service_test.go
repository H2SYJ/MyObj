package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"myobj/src/core/domain/request"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

type cinemaTestUserFile struct {
	UserID      string     `gorm:"column:user_id"`
	FileID      string     `gorm:"column:file_id"`
	FileName    string     `gorm:"column:file_name"`
	DirectoryID int        `gorm:"column:directory_id"`
	IsPublic    bool       `gorm:"column:public"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	UFID        string     `gorm:"column:uf_id"`
}

func (cinemaTestUserFile) TableName() string { return "user_files" }

func newCinemaTestService(t *testing.T) (*CinemaService, *TagService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.VirtualDirectory{}, &cinemaTestUserFile{}, &models.FileInfo{},
		&models.TagCategory{}, &models.TagDefinition{}, &models.UserFileTag{},
		&models.UserFileTagExclusion{}, &models.UserTagPreference{}, &models.UserTagStat{},
		&models.UserDirectoryTag{}, &models.UserFileTagState{},
	); err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	tagService := &TagService{
		factory: factory, ctx: context.Background(),
		pendingWake: make(chan struct{}, 1), rebuildWake: make(chan struct{}, 1),
		metadataWake: make(chan struct{}, 1), ruleWake: make(chan struct{}, 1),
	}
	return NewCinemaService(factory, tagService), tagService, db
}

func createCinemaTestVideo(t *testing.T, db *gorm.DB, userID, ufID, name string, directoryID int, created time.Time) {
	t.Helper()
	fileID := "physical-" + ufID
	if err := db.Create(&models.FileInfo{
		ID: fileID, Name: name, RandomName: fileID, Size: 100, Mime: "video/mp4", Path: "/video/" + fileID,
		FileHash: fileID, CreatedAt: custom_type.JsonTime(created), UpdatedAt: custom_type.JsonTime(created),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserFiles{
		UserID: userID, FileID: fileID, FileName: name, DirectoryID: directoryID,
		UfID: ufID, CreatedAt: custom_type.JsonTime(created),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryTagsAreManualAndCinemaHomeIsRecursive(t *testing.T) {
	cinema, tags, db := newCinemaTestService(t)
	now := time.Now()
	for _, category := range []models.TagCategory{{ID: "other", Code: "other", Name: "其他", Enabled: true}, {ID: "title", Code: "title", Name: "标题", Enabled: true}} {
		category.CreatedAt, category.UpdatedAt = now, now
		if err := db.Create(&category).Error; err != nil {
			t.Fatal(err)
		}
	}
	root := models.VirtualDirectory{ID: 1, UserID: "user-1", Name: "影视库", ParentID: 0, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	child := models.VirtualDirectory{ID: 2, UserID: "user-1", Name: "剧集", ParentID: 1, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	if err := db.Create(&[]models.VirtualDirectory{root, child}).Error; err != nil {
		t.Fatal(err)
	}
	code := "cinema_mode"
	if err := db.Create(&models.TagDefinition{ID: "cinema", Name: "影视模式", NormalizedName: "影视模式", CategoryID: "other", SystemCode: &code, Builtin: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tags.UpdateDirectoryTags(context.Background(), "user-1", root.ID, []request.ManualTagInput{{Name: "影视模式", CategoryID: "other"}}, nil); err != nil {
		t.Fatal(err)
	}
	createCinemaTestVideo(t, db, "user-1", "root-video", "根目录.mp4", root.ID, now.Add(-time.Hour))
	for index := 0; index < 7; index++ {
		createCinemaTestVideo(t, db, "user-1", fmt.Sprintf("episode-%d", index), fmt.Sprintf("第%d集.mp4", index), child.ID, now.Add(time.Duration(index)*time.Minute))
	}

	home, err := cinema.Home(context.Background(), "user-1", root.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Sections) != 2 || home.Sections[0].Directory.ID != root.ID || home.Sections[1].Directory.ID != child.ID {
		t.Fatalf("递归分区或根目录顺序异常: %+v", home.Sections)
	}
	if len(home.Sections[1].Videos) != 6 || !home.Sections[1].HasMore || home.Sections[1].Videos[0].FileID != "episode-6" {
		t.Fatalf("分区最新六条结果异常: %+v", home.Sections[1])
	}
	firstPage, err := cinema.FolderVideos(context.Background(), "user-1", root.ID, child.ID, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := cinema.FolderVideos(context.Background(), "user-1", root.ID, child.ID, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.Total != 7 || !firstPage.HasMore || len(firstPage.Videos) != 3 || len(secondPage.Videos) != 3 || firstPage.Videos[0].FileID == secondPage.Videos[0].FileID {
		t.Fatalf("文件夹直属视频分页异常: first=%+v second=%+v", firstPage, secondPage)
	}
}

func TestDirectoryTagOwnershipLimitAndStableCinemaDefinition(t *testing.T) {
	_, tags, db := newCinemaTestService(t)
	now := time.Now()
	for _, category := range []models.TagCategory{
		{ID: "other", Code: "other", Name: "其他", Enabled: true},
		{ID: "title", Code: "title", Name: "标题", Enabled: true},
	} {
		category.CreatedAt, category.UpdatedAt = now, now
		if err := db.Create(&category).Error; err != nil {
			t.Fatal(err)
		}
	}
	code := models.TagSystemCodeCinemaMode
	if err := db.Create(&models.TagDefinition{
		ID: "cinema", Name: models.TagNameCinemaMode, NormalizedName: models.TagNameCinemaMode,
		CategoryID: "other", SystemCode: &code, Builtin: true, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	directories := []models.VirtualDirectory{
		{ID: 1, UserID: "user-1", Name: "影视库", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
		{ID: 2, UserID: "user-1", Name: "标签上限", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
		{ID: 3, UserID: "user-2", Name: "他人目录", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
	}
	if err := db.Create(&directories).Error; err != nil {
		t.Fatal(err)
	}
	if err := tags.UpdateDirectoryTags(context.Background(), "user-1", 1, []request.ManualTagInput{{
		Name: models.TagNameCinemaMode, CategoryID: "title",
	}}, nil); err != nil {
		t.Fatal(err)
	}
	enabled, err := tags.IsCinemaDirectory(context.Background(), "user-1", 1)
	if err != nil || !enabled {
		t.Fatalf("选择非默认分类时未复用内置影视标签: enabled=%v err=%v", enabled, err)
	}
	if err := tags.UpdateDirectoryTags(context.Background(), "user-1", 3, []request.ManualTagInput{{Name: "越权"}}, nil); err == nil {
		t.Fatal("他人目录标签更新应被拒绝")
	}
	inputs := make([]request.ManualTagInput, 0, 101)
	for index := 0; index < 101; index++ {
		inputs = append(inputs, request.ManualTagInput{Name: fmt.Sprintf("标签%03d", index)})
	}
	if err := tags.UpdateDirectoryTags(context.Background(), "user-1", 2, inputs, nil); err == nil {
		t.Fatal("超过100个目录标签应被拒绝")
	}
	var count int64
	if err := db.Model(&models.UserDirectoryTag{}).Where("directory_id = ?", 2).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("标签上限事务未回滚: count=%d", count)
	}
}

func TestCinemaHomeSortsAllDescendantsAndUsesUserFileCreatedAt(t *testing.T) {
	cinema, _, db := newCinemaTestService(t)
	now := time.Now()
	if err := db.Create(&models.TagCategory{ID: "other", Code: "other", Name: "其他", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	root := models.VirtualDirectory{ID: 1, UserID: "user-1", Name: "影视库", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	directoryB := models.VirtualDirectory{ID: 2, UserID: "user-1", Name: "B目录", ParentID: 1, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	directoryA := models.VirtualDirectory{ID: 3, UserID: "user-1", Name: "A目录", ParentID: 1, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	if err := db.Create(&[]models.VirtualDirectory{root, directoryB, directoryA}).Error; err != nil {
		t.Fatal(err)
	}
	code := models.TagSystemCodeCinemaMode
	if err := db.Create(&models.TagDefinition{ID: "cinema", Name: models.TagNameCinemaMode, NormalizedName: models.TagNameCinemaMode, CategoryID: "other", SystemCode: &code, Builtin: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserDirectoryTag{ID: uuid.NewString(), UserID: "user-1", DirectoryID: root.ID, TagID: "cinema", CreatedBy: "user-1", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	createCinemaTestVideo(t, db, "user-1", "b-old", "B旧片.mp4", directoryB.ID, now.Add(-time.Hour))
	createCinemaTestVideo(t, db, "user-1", "b-new", "B新片.mp4", directoryB.ID, now)
	createCinemaTestVideo(t, db, "user-1", "a-video", "A片.mp4", directoryA.ID, now)
	for _, file := range []struct {
		ufID string
		mime string
		path string
	}{
		{ufID: "root-text", mime: "text/plain", path: "/file/root-text"},
		{ufID: "root-missing", mime: "video/mp4", path: ""},
	} {
		fileID := "physical-" + file.ufID
		if err := db.Create(&models.FileInfo{
			ID: fileID, Name: file.ufID, RandomName: fileID, Size: 100, Mime: file.mime, Path: file.path,
			FileHash: fileID, CreatedAt: custom_type.JsonTime(now), UpdatedAt: custom_type.JsonTime(now),
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.UserFiles{
			UserID: "user-1", FileID: fileID, FileName: file.ufID, DirectoryID: root.ID,
			UfID: file.ufID, CreatedAt: custom_type.JsonTime(now),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&models.FileInfo{}).Where("id = ?", "physical-b-old").Update("created_at", now.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.FileInfo{}).Where("id = ?", "physical-b-new").Update("created_at", now.Add(-2*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	home, err := cinema.Home(context.Background(), "user-1", root.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(home.Sections) != 2 || home.Sections[0].Directory.ID != directoryA.ID || home.Sections[1].Directory.ID != directoryB.ID {
		t.Fatalf("根目录为空时后代目录路径排序异常: %+v", home.Sections)
	}
	if got := home.Sections[1].Videos[0].FileID; got != "b-new" {
		t.Fatalf("视频未按用户文件加入时间排序: got=%s", got)
	}
}

func TestCinemaLatestIncludesDescendantsFiltersAndPaginates(t *testing.T) {
	cinema, _, db := newCinemaTestService(t)
	now := time.Now().Truncate(time.Second)
	if err := db.Create(&models.TagCategory{ID: "other", Code: "other", Name: "其他", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	root := models.VirtualDirectory{ID: 1, UserID: "user-1", Name: "影视库", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	child := models.VirtualDirectory{ID: 2, UserID: "user-1", Name: "子目录", ParentID: 1, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	outside := models.VirtualDirectory{ID: 3, UserID: "user-1", Name: "其他目录", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	if err := db.Create(&[]models.VirtualDirectory{root, child, outside}).Error; err != nil {
		t.Fatal(err)
	}
	code := models.TagSystemCodeCinemaMode
	if err := db.Create(&models.TagDefinition{ID: "cinema", Name: models.TagNameCinemaMode, NormalizedName: models.TagNameCinemaMode, CategoryID: "other", SystemCode: &code, Builtin: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserDirectoryTag{ID: uuid.NewString(), UserID: "user-1", DirectoryID: root.ID, TagID: "cinema", CreatedBy: "user-1", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	createCinemaTestVideo(t, db, "user-1", "same-a", "同秒A.mp4", root.ID, now)
	createCinemaTestVideo(t, db, "user-1", "same-b", "同秒B.mp4", child.ID, now)
	if err := db.Table("user_files").Where("uf_id = ?", "same-b").Update("public", true).Error; err != nil {
		t.Fatal(err)
	}
	createCinemaTestVideo(t, db, "user-1", "older", "旧片.mp4", child.ID, now.Add(-time.Hour))
	createCinemaTestVideo(t, db, "user-1", "outside", "库外视频.mp4", outside.ID, now.Add(time.Hour))
	createCinemaTestVideo(t, db, "user-2", "other-user", "其他用户.mp4", child.ID, now.Add(time.Hour))

	createCinemaTestVideo(t, db, "user-1", "deleted", "已删除.mp4", child.ID, now.Add(2*time.Hour))
	deletedAt := now
	if err := db.Table("user_files").Where("uf_id = ?", "deleted").Update("deleted_at", deletedAt).Error; err != nil {
		t.Fatal(err)
	}
	fileID := "physical-unplayable"
	if err := db.Create(&models.FileInfo{ID: fileID, Name: "不可播放.mp4", RandomName: fileID, Size: 100, Mime: "video/mp4", FileHash: fileID, CreatedAt: custom_type.JsonTime(now), UpdatedAt: custom_type.JsonTime(now)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserFiles{UserID: "user-1", FileID: fileID, FileName: "不可播放.mp4", DirectoryID: child.ID, UfID: "unplayable", CreatedAt: custom_type.JsonTime(now.Add(2 * time.Hour))}).Error; err != nil {
		t.Fatal(err)
	}

	first, err := cinema.Latest(context.Background(), "user-1", root.ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || !first.HasMore || len(first.Videos) != 2 {
		t.Fatalf("最新视频首分页异常: %+v", first)
	}
	if first.Videos[0].FileID != "same-b" || first.Videos[1].FileID != "same-a" {
		t.Fatalf("最新视频同时间稳定排序异常: %+v", first.Videos)
	}
	if first.Videos[0].Directory.ID != child.ID {
		t.Fatalf("最新视频目录信息异常: %+v", first.Videos[0].Directory)
	}
	if !first.Videos[0].Public {
		t.Fatal("影视视频响应未返回用户文件公开状态")
	}
	second, err := cinema.Latest(context.Background(), "user-1", root.ID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || len(second.Videos) != 1 || second.Videos[0].FileID != "older" {
		t.Fatalf("最新视频第二页异常: %+v", second)
	}
	if _, err := cinema.Latest(context.Background(), "user-2", root.ID, 1, 2); err == nil {
		t.Fatal("其他用户不应访问当前影视库最新视频")
	}
}

func TestCinemaRejectsUnauthorizedAndMovedOutTargets(t *testing.T) {
	cinema, _, db := newCinemaTestService(t)
	now := time.Now()
	if err := db.Create(&models.TagCategory{ID: "other", Code: "other", Name: "其他", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	directories := []models.VirtualDirectory{
		{ID: 1, UserID: "user-1", Name: "影视库", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
		{ID: 2, UserID: "user-1", Name: "剧集", ParentID: 1, CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
		{ID: 3, UserID: "user-1", Name: "普通目录", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
		{ID: 4, UserID: "user-2", Name: "他人影视库", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()},
	}
	if err := db.Create(&directories).Error; err != nil {
		t.Fatal(err)
	}
	code := models.TagSystemCodeCinemaMode
	if err := db.Create(&models.TagDefinition{ID: "cinema", Name: models.TagNameCinemaMode, NormalizedName: models.TagNameCinemaMode, CategoryID: "other", SystemCode: &code, Builtin: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for _, binding := range []models.UserDirectoryTag{
		{ID: uuid.NewString(), UserID: "user-1", DirectoryID: 1, TagID: "cinema", CreatedBy: "user-1", CreatedAt: now},
		{ID: uuid.NewString(), UserID: "user-2", DirectoryID: 4, TagID: "cinema", CreatedBy: "user-2", CreatedAt: now},
	} {
		if err := db.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	createCinemaTestVideo(t, db, "user-1", "episode", "剧集.mp4", 2, now)
	if _, err := cinema.Home(context.Background(), "user-2", 1, 1, 20); err == nil {
		t.Fatal("访问他人影视根目录应被拒绝")
	}
	if _, err := cinema.VideoDetail(context.Background(), "user-1", 1, "episode"); err != nil {
		t.Fatalf("移动前视频应可访问: %v", err)
	}
	if err := db.Model(&models.VirtualDirectory{}).Where("id = ?", 2).Update("parent_id", 3).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := cinema.FolderVideos(context.Background(), "user-1", 1, 2, 1, 24); err == nil {
		t.Fatal("移出影视子树的目录应被拒绝")
	}
	if _, err := cinema.VideoDetail(context.Background(), "user-1", 1, "episode"); err == nil {
		t.Fatal("移出影视子树的视频应被拒绝")
	}
	if err := db.Where("user_id = ? AND directory_id = ?", "user-1", 1).Delete(&models.UserDirectoryTag{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := cinema.Home(context.Background(), "user-1", 1, 1, 20); err == nil {
		t.Fatal("移除影视模式标签后根目录路由应失效")
	}
}

func TestCinemaRelatedRanksSharedTagsBeforeFilenameFallback(t *testing.T) {
	cinema, _, db := newCinemaTestService(t)
	now := time.Now()
	if err := db.Create(&models.TagCategory{ID: "other", Code: "other", Name: "其他", Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	root := models.VirtualDirectory{ID: 1, UserID: "user-1", Name: "影视库", CreatedAt: custom_type.Now(), UpdatedAt: custom_type.Now()}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	code := "cinema_mode"
	if err := db.Create(&models.TagDefinition{ID: "cinema", Name: "影视模式", NormalizedName: "影视模式", CategoryID: "other", SystemCode: &code, Builtin: true, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserDirectoryTag{ID: uuid.NewString(), UserID: "user-1", DirectoryID: 1, TagID: "cinema", CreatedBy: "user-1", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TagDefinition{ID: "shared", Name: "科幻", NormalizedName: "科幻", CategoryID: "other", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	createCinemaTestVideo(t, db, "user-1", "current", "银河冒险.mp4", 1, now.Add(-time.Hour))
	createCinemaTestVideo(t, db, "user-1", "shared-old", "其他影片.mp4", 1, now.Add(-2*time.Hour))
	createCinemaTestVideo(t, db, "user-1", "fallback-new", "银河冒险续集.mp4", 1, now)
	createCinemaTestVideo(t, db, "user-1", "suppressed", "无关影片.mp4", 1, now.Add(2*time.Hour))
	createCinemaTestVideo(t, db, "user-1", "latest", "最新影片.mp4", 1, now.Add(time.Hour))
	for _, ufID := range []string{"current", "shared-old"} {
		if err := db.Create(&models.UserFileTag{
			ID: uuid.NewString(), UserID: "user-1", UFID: ufID, TagID: "shared", SourceType: models.TagSourceManual,
			SourceKey: "user", Visibility: models.TagVisibilityPrivate, CreatedBy: "user-1", CreatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.UserFileTag{
		ID: uuid.NewString(), UserID: "user-1", UFID: "suppressed", TagID: "shared", SourceType: models.TagSourceRule,
		SourceKey: "rule", Visibility: models.TagVisibilityInherit, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserFileTagExclusion{UserID: "user-1", UFID: "suppressed", TagID: "shared", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	firstPage, err := cinema.Related(context.Background(), "user-1", root.ID, "current", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := cinema.Related(context.Background(), "user-1", root.ID, "current", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	ordered := append(firstPage.Videos, secondPage.Videos...)
	if len(ordered) != 4 || ordered[0].FileID != "shared-old" || ordered[1].FileID != "fallback-new" || ordered[2].FileID != "suppressed" || ordered[3].FileID != "latest" {
		t.Fatalf("相关推荐排序或稳定分页异常: %+v", ordered)
	}
}

func TestFilenameTokensMatchContinuousChineseTitles(t *testing.T) {
	if overlapCount(filenameTokens("银河冒险.mp4"), filenameTokens("银河冒险续集.mkv")) == 0 {
		t.Fatal("连续中文片名应产生可重合的规范化词元")
	}
}
