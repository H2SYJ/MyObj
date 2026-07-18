package middleware

import (
	"context"
	"errors"
	"math"
	"myobj/src/config"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/auth"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"myobj/src/pkg/util"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthMiddleware 认证中间件配置
type AuthMiddleware struct {
	cache          cache.Cache
	sessions       *auth.SessionStore
	apiKeyRepo     repository.ApiKeyRepository
	userRepo       repository.UserRepository
	groupPowerRepo repository.GroupPowerRepository
	powerRepo      repository.PowerRepository
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(
	cache cache.Cache,
	apiKeyRepo repository.ApiKeyRepository,
	userRepo repository.UserRepository,
	groupPowerRepo repository.GroupPowerRepository,
	powerRepo repository.PowerRepository,
) *AuthMiddleware {
	return &AuthMiddleware{
		cache:          cache,
		sessions:       auth.NewSessionStore(cache),
		apiKeyRepo:     apiKeyRepo,
		userRepo:       userRepo,
		groupPowerRepo: groupPowerRepo,
		powerRepo:      powerRepo,
	}
}

// Verify 认证验证中间件
func (m *AuthMiddleware) Verify() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 尝试从Authorization头获取JWT Token
		authorization := c.Request.Header.Get("Authorization")
		if authorization != "" {
			// JWT认证流程
			if err := m.handleJWTAuth(c, authorization); err != nil {
				writeAuthenticationError(c, err)
				c.Abort()
				return
			}
			c.Next()
			return
		}
		// 2. 尝试从Cookie中获取JWT
		cookie, err := c.Request.Cookie("Authorization")
		if err == nil && cookie.Value != "" {
			// JWT认证流程
			if err := m.handleJWTAuth(c, "Bearer "+cookie.Value); err != nil {
				writeAuthenticationError(c, err)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 3. 如果没有JWT,检查是否启用了API Key
		if config.CONFIG.Auth.ApiKey {
			// API Key认证流程
			if err := m.handleAPIKeyAuth(c); err != nil {
				writeAuthenticationError(c, err)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 4. 没有任何认证信息
		c.JSON(401, models.NewJsonResponse(401, "缺少登录凭证", map[string]string{"reason": "session_missing"}))
		c.Abort()
	}
}

// VerifyOptional 可选认证验证中间件（允许未登录用户访问，但会尝试认证）
func (m *AuthMiddleware) VerifyOptional() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 尝试从Authorization头获取JWT Token
		authorization := c.Request.Header.Get("Authorization")

		if authorization != "" {
			// JWT认证流程（如果失败，不阻止请求，只是不设置用户信息）
			if err := m.handleJWTAuth(c, authorization); err == nil {
				c.Next()
				return
			}
		}
		// 2. 尝试从Cookie中获取JWT
		cookie, err := c.Request.Cookie("Authorization")
		if err == nil && cookie.Value != "" {
			// JWT认证流程
			if err := m.handleJWTAuth(c, "Bearer "+cookie.Value); err != nil {
				writeAuthenticationError(c, err)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 3. 如果没有JWT,检查是否启用了API Key
		if config.CONFIG.Auth.ApiKey {
			// API Key认证流程（如果失败，不阻止请求，只是不设置用户信息）
			if err := m.handleAPIKeyAuth(c); err == nil {
				c.Next()
				return
			}
		}

		// 4. 没有任何认证信息，继续执行（允许未登录访问）
		c.Next()
	}
}

type authenticationError struct {
	status  int
	reason  string
	message string
	cause   error
}

func (e *authenticationError) Error() string {
	return e.message
}

func (e *authenticationError) Unwrap() error {
	return e.cause
}

func newAuthenticationError(status int, reason, message string, cause error) error {
	return &authenticationError{status: status, reason: reason, message: message, cause: cause}
}

func writeAuthenticationError(c *gin.Context, err error) {
	status := 503
	reason := "authentication_service_unavailable"
	message := "认证服务暂时不可用，请稍后重试"
	var authErr *authenticationError
	if errors.As(err, &authErr) {
		status = authErr.status
		reason = authErr.reason
		message = authErr.message
	}
	c.JSON(status, models.NewJsonResponse(status, message, map[string]string{"reason": reason}))
}

// handleJWTAuth 处理JWT认证
func (m *AuthMiddleware) handleJWTAuth(c *gin.Context, authorization string) error {
	// 解析Authorization头,支持 "Bearer {token}" 格式
	token := strings.TrimSpace(authorization)
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}
	token = strings.TrimSpace(token)

	if token == "" {
		return newAuthenticationError(401, "session_missing", "登录凭证为空", nil)
	}
	record, err := m.sessions.Get(token)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return newAuthenticationError(401, "session_invalid", "登录会话不存在或已过期", err)
		}
		return newAuthenticationError(503, "authentication_service_unavailable", "认证服务暂时不可用，请稍后重试", err)
	}
	// 解析JWT
	claims, err := auth.ParseToken(record.JWT)
	if err != nil {
		_ = m.sessions.Delete(token)
		return newAuthenticationError(401, "session_invalid", "登录会话无效，请重新登录", err)
	}
	if claims.SessionID != token || claims.UserID != record.UserID {
		_ = m.sessions.Delete(token)
		return newAuthenticationError(401, "session_invalid", "登录会话校验失败，请重新登录", nil)
	}

	// 检查JWT是否过期
	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		_ = m.sessions.Delete(token)
		return newAuthenticationError(401, "session_expired", "登录会话已过期，请重新登录", nil)
	}

	// 检查JWT剩余时间,如果不足5分钟则刷新
	if claims.ExpiresAt != nil {
		timeRemaining := time.Until(claims.ExpiresAt.Time)
		if timeRemaining > 0 && timeRemaining < 5*time.Minute {
			// 重新生成JWT
			newToken, generateErr := auth.GenerateJWT(claims.UserID, claims.SessionID, claims.UserLogin)
			if generateErr != nil {
				return newAuthenticationError(503, "authentication_service_unavailable", "刷新登录会话失败，请稍后重试", generateErr)
			}
			ttlSeconds := config.CONFIG.Auth.JwtExpire * 60 * 60
			record.JWT = newToken
			if refreshErr := m.sessions.Refresh(token, record, ttlSeconds); refreshErr != nil {
				return newAuthenticationError(503, "authentication_service_unavailable", "刷新登录会话失败，请稍后重试", refreshErr)
			}
			c.SetCookie("Authorization", token, ttlSeconds, "/", auth.GetCookieDomain(c.Request.Host), false, true)
		}
	}
	id, err := m.powerRepo.GetByGroupID(context.Background(), claims.UserLogin.User.GroupID)
	if err != nil {
		return newAuthenticationError(503, "authorization_service_unavailable", "权限服务暂时不可用，请稍后重试", err)
	}
	claims.UserLogin.Power = id
	// 将用户信息放入gin context
	c.Set("userLogin", claims.UserLogin)
	c.Set("userID", claims.UserID)
	return nil
}

// handleAPIKeyAuth 处理API Key认证
func (m *AuthMiddleware) handleAPIKeyAuth(c *gin.Context) error {
	// 获取API Key相关请求头
	apiKey := c.Request.Header.Get("X-API-Key")
	signature := c.Request.Header.Get("X-Signature")
	timestampStr := c.Request.Header.Get("X-Timestamp")
	nonce := c.Request.Header.Get("X-Nonce")

	// 检查必要参数
	if apiKey == "" || signature == "" || timestampStr == "" || nonce == "" {
		return newAuthenticationError(401, "api_key_invalid", "API Key认证参数不完整", nil)
	}

	// 验证nonce不为空
	if strings.TrimSpace(nonce) == "" {
		return newAuthenticationError(401, "api_key_invalid", "nonce不能为空", nil)
	}

	// 解析时间戳
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return newAuthenticationError(401, "api_key_invalid", "时间戳格式错误", err)
	}

	// 验证时间戳(不能超过5分钟)
	requestTime := time.UnixMilli(timestamp)
	timeDiff := math.Abs(time.Since(requestTime).Minutes())
	if timeDiff > 5 {
		return newAuthenticationError(401, "api_key_expired", "请求时间戳已过期", nil)
	}

	// 查询API Key记录
	ctx := context.Background()
	apiKeyRecord, err := m.apiKeyRepo.GetByKey(ctx, apiKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newAuthenticationError(401, "api_key_invalid", "API Key不存在", err)
		}
		return newAuthenticationError(503, "authentication_service_unavailable", "认证服务暂时不可用，请稍后重试", err)
	}

	// 检查API Key是否过期
	if !apiKeyRecord.ExpiresAt.IsZero() && time.Time(apiKeyRecord.ExpiresAt).Before(time.Now()) {
		return newAuthenticationError(401, "api_key_expired", "API Key已过期", nil)
	}

	// 使用私钥解密签名
	decryptedData, err := util.DecryptToString(apiKeyRecord.PrivateKey, signature)
	if err != nil {
		return newAuthenticationError(401, "api_key_invalid", "签名验证失败", err)
	}

	// 解析签名内容: apikey=""&timestamp=毫秒时间戳&nonce="随机字符串"
	parsedValues, err := url.ParseQuery(decryptedData)
	if err != nil {
		return newAuthenticationError(401, "api_key_invalid", "签名内容格式错误", err)
	}

	// 验证签名中的apikey
	signApiKey := parsedValues.Get("apikey")
	if signApiKey != apiKey {
		return newAuthenticationError(401, "api_key_invalid", "签名中的API Key不匹配", nil)
	}

	// 验证签名中的时间戳
	signTimestamp := parsedValues.Get("timestamp")
	if signTimestamp != timestampStr {
		return newAuthenticationError(401, "api_key_invalid", "签名中的时间戳不匹配", nil)
	}

	// 验证签名中的nonce
	signNonce := parsedValues.Get("nonce")
	if signNonce == "" || signNonce != nonce {
		return newAuthenticationError(401, "api_key_invalid", "签名中的nonce不匹配", nil)
	}

	// 查询用户信息
	user, err := m.userRepo.GetByID(ctx, apiKeyRecord.UserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newAuthenticationError(401, "api_key_invalid", "API Key所属用户不存在", err)
		}
		return newAuthenticationError(503, "authentication_service_unavailable", "认证服务暂时不可用，请稍后重试", err)
	}

	// 查询用户权限
	groupPowers, err := m.groupPowerRepo.GetByGroupID(ctx, user.GroupID)
	if err != nil {
		return newAuthenticationError(503, "authorization_service_unavailable", "权限服务暂时不可用，请稍后重试", err)
	}

	// 获取权限详情
	var powers []*models.Power
	for _, gp := range groupPowers {
		power, err := m.powerRepo.GetByID(ctx, gp.PowerID)
		if err != nil {
			return newAuthenticationError(503, "authorization_service_unavailable", "权限服务暂时不可用，请稍后重试", err)
		}
		if power != nil {
			powers = append(powers, power)
		}
	}

	// 构造UserLoginResponse
	userLoginResp := response.UserLoginResponse{
		Token: "", // API Key认证不使用JWT Token
		User:  user,
		Power: powers,
	}

	// 将用户信息放入gin context
	c.Set("userLogin", userLoginResp)
	c.Set("userID", user.ID)

	return nil
}
