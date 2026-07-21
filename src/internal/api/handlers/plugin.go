package handlers

import (
	"encoding/json"
	"myobj/src/core/service"
	"myobj/src/internal/api/middleware"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/models"
	pluginpkg "myobj/src/pkg/plugin"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type PluginHandler struct {
	service *service.PluginService
	cache   cache.Cache
}

func NewPluginHandler(service *service.PluginService, cacheLocal cache.Cache) *PluginHandler {
	return &PluginHandler{service: service, cache: cacheLocal}
}

func (h *PluginHandler) Router(c *gin.RouterGroup) {
	repository := h.service.GetRepository()
	verify := middleware.NewAuthMiddleware(h.cache, repository.ApiKey(), repository.User(), repository.GroupPower(), repository.Power())
	group := c.Group("/admin/plugin")
	group.Use(verify.Verify(), middleware.AdminVerify())
	group.GET("/list", h.List)
	group.POST("/install", h.Install)
	group.POST("/toggle", h.Toggle)
	group.POST("/uninstall", h.Uninstall)
	group.GET("/audit", h.Audit)
}

func (h *PluginHandler) Audit(c *gin.Context) {
	page, pageSize := 1, 20
	if value, err := strconv.Atoi(c.Query("page")); err == nil {
		page = value
	}
	if value, err := strconv.Atoi(c.Query("pageSize")); err == nil {
		pageSize = value
	}
	rows, total, err := h.service.Audit(c.Request.Context(), page, pageSize)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "查询审计记录失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", map[string]interface{}{"items": rows, "total": total}))
}

func (h *PluginHandler) List(c *gin.Context) {
	rows, err := h.service.List(c.Request.Context(), false)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "查询插件失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", rows))
}

func (h *PluginHandler) Install(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, pluginpkg.MaxPackageSize+1024*1024)
	header, err := c.FormFile("plugin")
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "请选择插件包", nil))
		return
	}
	file, err := header.Open()
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "读取插件包失败", err.Error()))
		return
	}
	defer file.Close()
	if c.PostForm("review_only") == "true" {
		result, inspectErr := h.service.Inspect(c.Request.Context(), file, header.Size)
		if inspectErr != nil {
			c.JSON(200, models.NewJsonResponse(400, "校验插件失败", inspectErr.Error()))
			return
		}
		c.JSON(200, models.NewJsonResponse(200, "请审核插件权限", result))
		return
	}
	var approvedPermissions []string
	if err := json.Unmarshal([]byte(c.PostForm("approved_permissions")), &approvedPermissions); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "请确认插件声明的全部权限", nil))
		return
	}
	record, err := h.service.Install(c.Request.Context(), c.GetString("userID"), file, header.Size, approvedPermissions, c.PostForm("trust_unsigned") == "true")
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "安装插件失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "插件安装成功", record))
}

func (h *PluginHandler) Toggle(c *gin.Context) {
	var req struct {
		ID      string `json:"id" binding:"required"`
		Enabled bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := h.service.Toggle(c.Request.Context(), req.ID, req.Enabled); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "操作失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "操作成功", nil))
}

func (h *PluginHandler) Uninstall(c *gin.Context) {
	var req struct {
		ID string `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := h.service.Uninstall(c.Request.Context(), req.ID); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "卸载失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "卸载成功", nil))
}
