package service

import (
	"myobj/src/config"
	"testing"
	"time"
)

func TestSignedFileCursorIsUserBound(t *testing.T) {
	previous := config.CONFIG
	config.CONFIG = &config.MyObjConfig{Auth: config.Auth{Secret: "cursor-test-secret"}}
	t.Cleanup(func() { config.CONFIG = previous })
	createdAt := time.Date(2026, 7, 21, 8, 0, 0, 123, time.UTC)
	cursor := encodeFileCursor("user-a", createdAt, "uf-1")
	decodedTime, ufID, err := decodeFileCursor("user-a", cursor)
	if err != nil || !decodedTime.Equal(createdAt) || ufID != "uf-1" {
		t.Fatalf("cursor往返失败: %v %s %v", decodedTime, ufID, err)
	}
	if _, _, err := decodeFileCursor("user-b", cursor); err == nil {
		t.Fatal("cursor不应跨用户复用")
	}
}

func TestNextScheduleInShanghai(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, location)
	next, err := nextScheduleInLocation("08:30", now, location)
	if err != nil || next.Day() != 22 || next.Hour() != 8 || next.Minute() != 30 {
		t.Fatalf("错过时间未补到下一执行点: %v %v", next, err)
	}
}
