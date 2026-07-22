package service

import "testing"

func TestNormalizeFileSortUsesWhitelist(t *testing.T) {
	tests := []struct {
		name      string
		sortBy    string
		sortOrder string
		wantBy    string
		wantOrder string
	}{
		{name: "名称升序", sortBy: "name", sortOrder: "asc", wantBy: "name", wantOrder: "asc"},
		{name: "大小降序", sortBy: "size", sortOrder: "desc", wantBy: "size", wantOrder: "desc"},
		{name: "非法字段回退", sortBy: "name desc; drop table", sortOrder: "sideways", wantBy: "time", wantOrder: "desc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotBy, gotOrder := normalizeFileSort(test.sortBy, test.sortOrder)
			if gotBy != test.wantBy || gotOrder != test.wantOrder {
				t.Fatalf("排序归一化结果为 %s %s，期望 %s %s", gotBy, gotOrder, test.wantBy, test.wantOrder)
			}
		})
	}
}

func TestArchivePathValidation(t *testing.T) {
	valid := []string{"目录/文件.txt", "文件.txt", "一级/二级/文件.bin"}
	for _, value := range valid {
		if !validArchivePath(value) {
			t.Fatalf("安全路径被拒绝: %s", value)
		}
	}
	invalid := []string{"", ".", "..", "../文件", "/绝对路径", "目录/../文件", "目录\\文件"}
	for _, value := range invalid {
		if validArchivePath(value) {
			t.Fatalf("不安全路径被接受: %s", value)
		}
	}
}

func TestSafeArchiveSegmentRemovesPathSyntaxAndControls(t *testing.T) {
	got := safeArchiveSegment("../目录:\x00名称")
	if got == "" || got == "." || got == ".." {
		t.Fatalf("压缩包名称无效: %q", got)
	}
	for _, forbidden := range []rune{'/', '\\', ':', 0} {
		for _, current := range got {
			if current == forbidden {
				t.Fatalf("压缩包名称仍包含非法字符 %q: %q", forbidden, got)
			}
		}
	}
}

func TestUniqueSelections(t *testing.T) {
	strings := uniqueStrings([]string{"a", "", "a", "b"})
	if len(strings) != 2 || strings[0] != "a" || strings[1] != "b" {
		t.Fatalf("文件选择去重失败: %#v", strings)
	}
	ints := uniqueInts([]int{2, 0, -1, 2, 3})
	if len(ints) != 2 || ints[0] != 2 || ints[1] != 3 {
		t.Fatalf("目录选择去重失败: %#v", ints)
	}
}
