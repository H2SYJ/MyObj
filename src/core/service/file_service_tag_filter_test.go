package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeTagFilter(t *testing.T) {
	tags, mode, err := normalizeTagFilter(" tag-1,tag-2,tag-1, ", "ANY")
	if err != nil {
		t.Fatal(err)
	}
	if mode != "any" || len(tags) != 2 || tags[0] != "tag-1" || tags[1] != "tag-2" {
		t.Fatalf("标签筛选归一化结果不正确: tags=%v mode=%s", tags, mode)
	}
	if _, _, err := normalizeTagFilter("tag-1", "invalid"); !errors.Is(err, ErrInvalidFileSearch) {
		t.Fatalf("非法匹配模式应返回ErrInvalidFileSearch: %v", err)
	}
}

func TestNormalizeTagFilterDefaultsToAny(t *testing.T) {
	tags, mode, err := normalizeTagFilter("tag-1,tag-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || mode != "any" {
		t.Fatalf("未指定匹配模式时应默认任一匹配: tags=%v mode=%s", tags, mode)
	}
}

func TestNormalizeTagFilterRejectsTooManyTags(t *testing.T) {
	values := make([]string, maxFileTagFilterCount+1)
	for index := range values {
		values[index] = fmt.Sprintf("tag-%d", index)
	}
	if _, _, err := normalizeTagFilter(strings.Join(values, ","), "all"); !errors.Is(err, ErrInvalidFileSearch) {
		t.Fatalf("超过标签筛选上限应返回ErrInvalidFileSearch: %v", err)
	}
}
