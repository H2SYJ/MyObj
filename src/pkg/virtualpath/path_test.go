package virtualpath

import "testing"

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
