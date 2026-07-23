package handlers

import (
	"myobj/src/config"
	"myobj/src/core/domain/response"
	"myobj/src/core/service"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/auth"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type eventHandlerTestFactory struct {
	repository *impl.RepositoryFactory
}

func (f *eventHandlerTestFactory) GetRepository() *impl.RepositoryFactory {
	return f.repository
}

func TestEventStreamWritesHeadersSyncAndHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("userID", "user")
	context.Request = httptest.NewRequest("GET", "/api/events", nil)
	handler := &EventHandler{
		events:            service.NewTaskEventHub(),
		heartbeatInterval: 5 * time.Millisecond,
		maxConnectionAge:  20 * time.Millisecond,
	}

	handler.Stream(context)

	if recorder.Code != 200 {
		t.Fatalf("SSE状态码错误: %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("SSE Content-Type错误: %q", contentType)
	}
	if recorder.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("未禁用Nginx响应缓冲: %#v", recorder.Header())
	}
	body := recorder.Body.String()
	for _, expected := range []string{"retry: 1000", "event:sync", "event:heartbeat", `"version":1`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("SSE响应缺少%q: %s", expected, body)
		}
	}
}

func TestEventStreamUsesCookieAndIgnoresURLToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousConfig := config.CONFIG
	config.CONFIG = &config.MyObjConfig{Auth: config.Auth{Secret: "event-test-secret", JwtExpire: 1}}
	t.Cleanup(func() { config.CONFIG = previousConfig })

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Group{}, &models.GroupPower{}, &models.Power{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Group{ID: 1, Name: "用户组", CreatedAt: custom_type.Now()}).Error; err != nil {
		t.Fatal(err)
	}

	cacheStore := cache.NewLocalCache(time.Hour)
	t.Cleanup(cacheStore.Stop)
	user := &models.UserInfo{ID: "user", GroupID: 1, CreatedAt: custom_type.Now()}
	login := response.UserLoginResponse{User: user}
	sessionID := "cookie-session"
	jwtToken, err := auth.GenerateJWT(user.ID, sessionID, login)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseToken(jwtToken); err != nil {
		t.Fatalf("测试JWT无法解析: %v", err)
	}
	if err := auth.NewSessionStore(cacheStore).Create(sessionID, user.ID, jwtToken, 3600); err != nil {
		t.Fatal(err)
	}

	handler := NewEventHandler(service.NewTaskEventHub(), cacheStore, &eventHandlerTestFactory{
		repository: impl.NewRepositoryFactory(db),
	})
	handler.heartbeatInterval = 5 * time.Millisecond
	handler.maxConnectionAge = 15 * time.Millisecond
	router := gin.New()
	handler.Router(router.Group("/api"))

	queryRecorder := httptest.NewRecorder()
	queryRequest := httptest.NewRequest(http.MethodGet, "/api/events?token="+sessionID, nil)
	router.ServeHTTP(queryRecorder, queryRequest)
	if queryRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("URL Token不应通过认证，实际状态码%d", queryRecorder.Code)
	}

	cookieRecorder := httptest.NewRecorder()
	cookieRequest := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	cookieRequest.AddCookie(&http.Cookie{Name: "Authorization", Value: sessionID})
	router.ServeHTTP(cookieRecorder, cookieRequest)
	if cookieRecorder.Code != http.StatusOK || !strings.Contains(cookieRecorder.Body.String(), "event:sync") {
		t.Fatalf("HttpOnly登录Cookie未建立SSE连接: code=%d body=%s", cookieRecorder.Code, cookieRecorder.Body.String())
	}
}
