package util

import (
	"path/filepath"
	"testing"
)

func TestGetPathDiskSpaceUsesExistingParent(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "尚未创建", "data")

	info, err := GetPathDiskSpace(targetPath)
	if err != nil {
		t.Fatalf("获取路径磁盘空间失败: %v", err)
	}
	if info.Total == 0 {
		t.Fatal("磁盘总空间不应为0")
	}
	if info.Avail > info.Total {
		t.Fatalf("磁盘可用空间不能大于总空间: avail=%d total=%d", info.Avail, info.Total)
	}
}

func TestGetPathDiskSpaceRejectsEmptyPath(t *testing.T) {
	if _, err := GetPathDiskSpace(" "); err == nil {
		t.Fatal("空路径应返回错误")
	}
}
