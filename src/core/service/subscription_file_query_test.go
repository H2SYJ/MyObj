package service

import (
	"context"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionFileQueriesAreLimitedToSaveRoot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.FileInfo{}, &models.VirtualDirectory{}, &models.TagDefinition{}, &models.UserFileTag{}, &models.UserFileTagExclusion{}, &models.UserTagPreference{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE user_files (
		user_id TEXT NOT NULL, file_id TEXT NOT NULL, file_name TEXT NOT NULL,
		directory_id INTEGER NOT NULL, public BOOLEAN NOT NULL, created_at DATETIME NOT NULL,
		deleted_at DATETIME NULL, uf_id TEXT NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}

	now := custom_type.Now()
	paths := []models.VirtualDirectory{
		{ID: 1, UserID: "user-a", Name: "", ParentID: 0, CreatedAt: now, UpdatedAt: now},
		{ID: 2, UserID: "user-a", Name: "保存", ParentID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 3, UserID: "user-a", Name: "频道", ParentID: 2, CreatedAt: now, UpdatedAt: now},
		{ID: 4, UserID: "user-a", Name: "深层", ParentID: 3, CreatedAt: now, UpdatedAt: now},
		{ID: 5, UserID: "user-a", Name: "其他", ParentID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 6, UserID: "user-a", Name: "旧频道", ParentID: 2, CreatedAt: now, UpdatedAt: now},
		{ID: 10, UserID: "user-b", Name: "", ParentID: 0, CreatedAt: now, UpdatedAt: now},
		{ID: 11, UserID: "user-b", Name: "保存", ParentID: 10, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&paths).Error; err != nil {
		t.Fatal(err)
	}

	createdAt := time.Now()
	files := []struct {
		userID string
		pathID int
		ufID   string
	}{
		{userID: "user-a", pathID: 2, ufID: "uf-direct"},
		{userID: "user-a", pathID: 3, ufID: "uf-channel"},
		{userID: "user-a", pathID: 4, ufID: "uf-deep"},
		{userID: "user-a", pathID: 5, ufID: "uf-outside"},
		{userID: "user-a", pathID: 6, ufID: "uf-legacy"},
		{userID: "user-b", pathID: 11, ufID: "uf-user-b"},
	}
	for index, entry := range files {
		fileID := fmt.Sprintf("file-%d", index)
		file := models.FileInfo{ID: fileID, Name: fileID, RandomName: fileID, Size: 12, Mime: "text/plain", FileHash: "hash-" + fileID, IsChunk: false, EncPath: "", CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO user_files(user_id,file_id,file_name,directory_id,public,created_at,deleted_at,uf_id) VALUES(?,?,?,?,?,?,NULL,?)", entry.userID, fileID, entry.ufID+".txt", entry.pathID, false, createdAt.Add(-time.Duration(index)*time.Minute), entry.ufID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.TagDefinition{ID: "tag-video", Name: "电影", NormalizedName: "电影", CategoryID: "title", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.UserFileTag{ID: "binding-video", UserID: "user-a", UFID: "uf-channel", TagID: "tag-video", SourceType: models.TagSourceManual, SourceKey: "user", Visibility: models.TagVisibilityPrivate, CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}

	service := &SubscriptionService{factory: impl.NewRepositoryFactory(db)}
	ctx := context.Background()

	direct, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query"})
	if err != nil || len(direct.Files) != 1 || direct.Files[0].UFID != "uf-direct" {
		t.Fatalf("空路径应只查询保存目录直属文件: response=%+v err=%v", direct, err)
	}
	recursive, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "", Recursive: true})
	if err != nil || len(recursive.Files) != 4 {
		t.Fatalf("保存目录递归查询范围错误: response=%+v err=%v", recursive, err)
	}
	channel, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "频道"})
	if err != nil || len(channel.Files) != 1 || channel.Files[0].UFID != "uf-channel" || channel.Files[0].AbsolutePath != "/保存/频道" || len(channel.Files[0].Tags) != 1 {
		t.Fatalf("相对保存目录的子目录查询失败: response=%+v err=%v", channel, err)
	}
	byTag, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", Recursive: true, TagsAll: []string{"电影"}})
	if err != nil || len(byTag.Files) != 1 || byTag.Files[0].UFID != "uf-channel" {
		t.Fatalf("标签查询失败: response=%+v err=%v", byTag, err)
	}
	channelRecursive, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "频道", Recursive: true})
	if err != nil || len(channelRecursive.Files) != 2 {
		t.Fatalf("子目录递归查询失败: response=%+v err=%v", channelRecursive, err)
	}
	exact, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "频道", NameEquals: "uf-channel.txt"})
	if err != nil || len(exact.Files) != 1 || exact.Files[0].UFID != "uf-channel" {
		t.Fatalf("精确文件名查询失败: response=%+v err=%v", exact, err)
	}
	exactMissing, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "频道", NameEquals: "uf-channel"})
	if err != nil || len(exactMissing.Files) != 0 {
		t.Fatalf("精确文件名查询不应返回部分匹配: response=%+v err=%v", exactMissing, err)
	}
	legacy, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "旧频道", NameEquals: "uf-legacy.txt"})
	if err != nil || len(legacy.Files) != 1 || legacy.Files[0].UFID != "uf-legacy" {
		t.Fatalf("历史目录名称查询失败: response=%+v err=%v", legacy, err)
	}
	missing, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "不存在", Recursive: true})
	if err != nil || len(missing.Files) != 0 {
		t.Fatalf("不存在的子目录应返回空结果: response=%+v err=%v", missing, err)
	}
	if _, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", RelativePath: "../其他"}); err == nil {
		t.Fatal("包含..的查询目录未被拒绝")
	}
	tooManyTags := make([]string, maxFileTagFilterCount+1)
	for index := range tooManyTags {
		tooManyTags[index] = fmt.Sprintf("标签%d", index)
	}
	if _, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "query", TagsAll: tooManyTags}); err == nil || !strings.Contains(err.Error(), "invalid_request") {
		t.Fatalf("插件标签查询超过上限时应被拒绝: %v", err)
	}

	inside, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "get", UFID: "uf-channel"})
	if err != nil || len(inside.Files) != 1 {
		t.Fatalf("读取保存目录内文件失败: response=%+v err=%v", inside, err)
	}
	for _, ufID := range []string{"uf-outside", "uf-user-b"} {
		_, err := service.queryFilesInternal(ctx, "user-a", "/保存", pluginpkg.FileQueryRequest{Operation: "get", UFID: ufID})
		if err == nil || !strings.Contains(err.Error(), "not_found") {
			t.Fatalf("越界FileGet必须统一返回not_found: uf_id=%s err=%v", ufID, err)
		}
	}

	noRoot, err := service.queryFilesInternal(ctx, "user-a", "/尚未创建", pluginpkg.FileQueryRequest{Operation: "query", Recursive: true})
	if err != nil || len(noRoot.Files) != 0 {
		t.Fatalf("尚未创建的保存目录应返回空结果: response=%+v err=%v", noRoot, err)
	}
	_, err = service.queryFilesInternal(ctx, "user-a", "/尚未创建", pluginpkg.FileQueryRequest{Operation: "get", UFID: "uf-direct"})
	if err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("保存目录不存在时FileGet必须返回not_found: %v", err)
	}
}
