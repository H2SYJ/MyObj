package virtualpath

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

func TestNormalizeAbsolutePath(t *testing.T) {
	value, err := NormalizeAbsolutePath("/离线下载/电视剧/")
	if err != nil || value != "/离线下载/电视剧" {
		t.Fatalf("中文绝对目录规范化失败: %q %v", value, err)
	}
	if value, err = NormalizeAbsolutePath("/频道//2026/"); err != nil || value != "/频道/2026" {
		t.Fatalf("重复分隔符规范化失败: %q %v", value, err)
	}
	invalid := []string{"relative/path", `C:\data`, `\\server\share`, "//server/share", "/C:/data", "https://example.com/path", "/a/../b", "/a/./b", `/a\b`, "/a\x00b"}
	for _, item := range invalid {
		if _, err := NormalizeAbsolutePath(item); err == nil {
			t.Fatalf("非法绝对目录未被拒绝: %q", item)
		}
	}
}

func TestNormalizeRelativePathAndDirectoryName(t *testing.T) {
	if value, err := NormalizeRelativePath("频道//2026/"); err != nil || value != "频道/2026" {
		t.Fatalf("相对目录规范化失败: %q %v", value, err)
	}
	if value, err := NormalizeRelativePath(""); err != nil || value != "" {
		t.Fatalf("空相对目录失败: %q %v", value, err)
	}
	for _, item := range []string{"/频道", "../频道", `频道\私密`} {
		if _, err := NormalizeRelativePath(item); err == nil {
			t.Fatalf("非法相对目录未被拒绝: %q", item)
		}
	}
	if name, err := NormalizeDirectoryName(" 目录 "); err != nil || name != "目录" {
		t.Fatalf("目录名称规范化失败: %q %v", name, err)
	}
}

func TestNormalizePathLimits(t *testing.T) {
	path := ""
	for index := 0; index < MaxDepth+1; index++ {
		path += "/a"
	}
	if _, err := NormalizeAbsolutePath(path); err == nil {
		t.Fatal("超过20层的目录未被拒绝")
	}
	if _, err := NormalizeDirectoryName(strings.Repeat("名", MaxSegmentLength+1)); err == nil {
		t.Fatal("超长目录名称未被拒绝")
	}
	if name, err := NormalizeDirectoryName(strings.Repeat("名", MaxSegmentLength)); err != nil || len([]rune(name)) != MaxSegmentLength {
		t.Fatalf("目录名称上限应有效: len=%d err=%v", len([]rune(name)), err)
	}
	segments := make([]string, MaxDepth)
	for index := range segments {
		segments[index] = strings.Repeat("a", 50)
	}
	if _, err := NormalizeAbsolutePath("/" + strings.Join(segments, "/")); err == nil {
		t.Fatal("超过1000字符的目录未被拒绝")
	}
}

func TestJoinSavePath(t *testing.T) {
	tests := []struct{ root, relative, want string }{
		{root: "/离线下载/订阅", want: "/离线下载/订阅"},
		{root: "/", relative: "频道/2026", want: "/频道/2026"},
		{root: "/离线下载/订阅/", relative: "频道//2026/", want: "/离线下载/订阅/频道/2026"},
	}
	for _, test := range tests {
		got, err := JoinSavePath(test.root, test.relative)
		if err != nil || got != test.want {
			t.Fatalf("拼接结果错误: got=%q want=%q err=%v", got, test.want, err)
		}
	}
}

func TestEnsureDirectoryAndResolveAbsolutePath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.VirtualDirectory{}); err != nil {
		t.Fatal(err)
	}
	now := custom_type.Now()
	root := models.VirtualDirectory{ID: 1, UserID: "user-a", Name: "", ParentID: 0, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	directoryID, err := EnsureDirectory(context.Background(), "user-a", "/保存/频道", factory)
	if err != nil {
		t.Fatal(err)
	}
	absolutePath, err := ResolveAbsolutePath(context.Background(), "user-a", directoryID, factory)
	if err != nil || absolutePath != "/保存/频道" {
		t.Fatalf("目录解析失败: id=%d path=%q err=%v", directoryID, absolutePath, err)
	}
	resolvedID, err := ResolveDirectoryID(context.Background(), "user-a", "/保存/频道", factory)
	if err != nil || resolvedID != directoryID {
		t.Fatalf("路径反向解析失败: id=%d err=%v", resolvedID, err)
	}
}
