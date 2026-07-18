package auth

import (
	"errors"
	"myobj/src/pkg/cache"
	"testing"
)

func TestSessionStoreUsesNamespacedKeysAndRevokesOnlyTargetUser(t *testing.T) {
	cacheStore := cache.NewLocalCache()
	defer cacheStore.Stop()
	sessions := NewSessionStore(cacheStore)

	if err := sessions.Create("session-a", "user-a", "jwt-a", 3600); err != nil {
		t.Fatalf("创建用户A会话失败: %v", err)
	}
	if err := sessions.Create("session-b", "user-b", "jwt-b", 3600); err != nil {
		t.Fatalf("创建用户B会话失败: %v", err)
	}
	if _, err := cacheStore.Get("session-a"); !errors.Is(err, cache.ErrKeyNotFound) {
		t.Fatalf("不应写入旧格式裸会话键: %v", err)
	}

	record, err := sessions.Get("session-a")
	if err != nil {
		t.Fatalf("读取用户A会话失败: %v", err)
	}
	if record.JWT != "jwt-a" || record.UserID != "user-a" {
		t.Fatalf("会话内容错误: %#v", record)
	}

	if err := sessions.RevokeUser("user-a"); err != nil {
		t.Fatalf("撤销用户A会话失败: %v", err)
	}
	if _, err := sessions.Get("session-a"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("用户A会话应已失效: %v", err)
	}
	if _, err := sessions.Get("session-b"); err != nil {
		t.Fatalf("撤销用户A不应影响用户B: %v", err)
	}
}

func TestSessionStoreRefreshKeepsStableSessionID(t *testing.T) {
	cacheStore := cache.NewLocalCache()
	defer cacheStore.Stop()
	sessions := NewSessionStore(cacheStore)

	if err := sessions.Create("stable-id", "user-a", "jwt-old", 3600); err != nil {
		t.Fatal(err)
	}
	record := &SessionRecord{JWT: "jwt-new", UserID: "user-a"}
	if err := sessions.Refresh("stable-id", record, 3600); err != nil {
		t.Fatal(err)
	}
	refreshed, err := sessions.Get("stable-id")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.JWT != "jwt-new" {
		t.Fatalf("JWT未刷新: %#v", refreshed)
	}
}
