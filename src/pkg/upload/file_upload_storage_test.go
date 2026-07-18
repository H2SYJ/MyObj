package upload

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitAndStoreFileUsesBoundedBufferForLargeChunkSize(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.data")
	storageDir := filepath.Join(root, "storage")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("流式分片"), 128*1024)
	if err := os.WriteFile(sourcePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	chunks, mainPath, err := splitAndStoreFile(sourcePath, storageDir, "target", "file-id", 1)
	if err != nil {
		t.Fatalf("流式存储失败: %v", err)
	}
	if len(chunks) != 1 || mainPath == "" {
		t.Fatalf("存储分片结果错误: chunks=%d mainPath=%q", len(chunks), mainPath)
	}
	stored, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatal("流式存储后的内容不一致")
	}
}

func TestCleanupTaskTempDirRejectsPathsOutsideTempDirectory(t *testing.T) {
	root := t.TempDir()
	invalidDir := filepath.Join(root, "data", "task")
	if err := os.MkdirAll(invalidDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := CleanupTaskTempDir(invalidDir); err == nil {
		t.Fatal("应拒绝清理 temp 目录之外的路径")
	}
	if _, err := os.Stat(invalidDir); err != nil {
		t.Fatalf("非法路径不应被删除: %v", err)
	}

	validDir := filepath.Join(root, "data", "temp", "task")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := CleanupTaskTempDir(validDir); err != nil {
		t.Fatalf("清理合法临时目录失败: %v", err)
	}
	if _, err := os.Stat(validDir); !os.IsNotExist(err) {
		t.Fatalf("合法临时目录应被删除: %v", err)
	}
}
