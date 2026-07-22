package virtualpath

import (
	"strings"
	"testing"
)

func TestNormalizeVirtualPath(t *testing.T) {
	value, err := Normalize("/离线下载/电视剧/")
	if err != nil || value != "/离线下载/电视剧" {
		t.Fatalf("中文绝对目录规范化失败: %q %v", value, err)
	}
	invalid := []string{"relative/path", `C:\data`, `\\server\share`, "//server/share", "/C:/data", "https://example.com/path", "/a/../b", "/a/./b", `/a\b`, "/a\x00b"}
	for _, item := range invalid {
		if _, err := Normalize(item); err == nil {
			t.Fatalf("非法目录未被拒绝: %q", item)
		}
	}
}

func TestNormalizeVirtualPathLimits(t *testing.T) {
	path := ""
	for index := 0; index < MaxDepth+1; index++ {
		path += "/a"
	}
	if _, err := Normalize(path); err == nil {
		t.Fatal("超过20层的目录未被拒绝")
	}
}

func TestJoinSubscriptionPath(t *testing.T) {
	tests := []struct {
		name       string
		saveRoot   string
		pluginPath string
		want       string
	}{
		{name: "空插件目录", saveRoot: "/离线下载/订阅", want: "/离线下载/订阅"},
		{name: "插件根目录", saveRoot: "/离线下载/订阅/", pluginPath: "/", want: "/离线下载/订阅"},
		{name: "保存根目录", saveRoot: "/", pluginPath: "/频道/2026", want: "/频道/2026"},
		{name: "多级中文目录", saveRoot: "/离线下载/订阅", pluginPath: "/频道//2026/", want: "/离线下载/订阅/频道/2026"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := JoinSubscriptionPath(test.saveRoot, test.pluginPath)
			if err != nil || got != test.want {
				t.Fatalf("拼接结果错误: got=%q want=%q err=%v", got, test.want, err)
			}
		})
	}
}

func TestJoinSubscriptionPathRejectsInvalidOrOversizedResult(t *testing.T) {
	invalid := []string{"relative/path", "/频道/../私密", `/频道\私密`}
	for _, pluginPath := range invalid {
		if _, err := JoinSubscriptionPath("/离线下载/订阅", pluginPath); err == nil {
			t.Fatalf("非法插件目录未被拒绝: %q", pluginPath)
		}
	}
	rootSegment := strings.Repeat("根", MaxSegmentLength)
	childSegment := strings.Repeat("子", MaxSegmentLength)
	saveRoot := "/" + strings.Join([]string{rootSegment, rootSegment, rootSegment, rootSegment, rootSegment}, "/")
	pluginPath := "/" + strings.Join([]string{childSegment, childSegment, childSegment, childSegment, childSegment, childSegment}, "/")
	if _, err := JoinSubscriptionPath(saveRoot, pluginPath); err == nil {
		t.Fatal("超过最终长度限制的拼接目录未被拒绝")
	}
}
