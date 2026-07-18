package middleware

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWriteAuthenticationErrorKeepsInfrastructureFailureAs503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeAuthenticationError(context, errors.New("redis连接失败"))

	if recorder.Code != 503 {
		t.Fatalf("基础设施故障应返回503，实际为%d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "authentication_service_unavailable") {
		t.Fatalf("响应缺少稳定故障原因: %s", recorder.Body.String())
	}
}

func TestWriteAuthenticationErrorReturnsReasonForInvalidSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeAuthenticationError(context, newAuthenticationError(401, "session_expired", "登录会话已过期", nil))

	if recorder.Code != 401 {
		t.Fatalf("过期会话应返回401，实际为%d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "session_expired") {
		t.Fatalf("响应缺少会话失效原因: %s", recorder.Body.String())
	}
}
