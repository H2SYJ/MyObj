package handlers

import (
	"myobj/src/core/domain/request"
	"myobj/src/core/service"
	"myobj/src/internal/api/middleware"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/models"

	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct {
	service *service.SubscriptionService
	cache   cache.Cache
}

func NewSubscriptionHandler(service *service.SubscriptionService, cacheLocal cache.Cache) *SubscriptionHandler {
	return &SubscriptionHandler{service: service, cache: cacheLocal}
}

func (h *SubscriptionHandler) Router(c *gin.RouterGroup) {
	repository := h.service.GetRepository()
	verify := middleware.NewAuthMiddleware(h.cache, repository.ApiKey(), repository.User(), repository.GroupPower(), repository.Power())
	group := c.Group("/subscription")
	group.Use(verify.Verify(), middleware.PowerVerify("file:offLine"))
	group.GET("/plugins", h.Plugins)
	group.GET("/list", h.List)
	group.POST("/create", h.Create)
	group.POST("/update", h.Update)
	group.POST("/delete", h.Delete)
	group.POST("/toggle", h.Toggle)
	group.POST("/run", h.Run)
	group.POST("/permissions", h.Update)
	group.GET("/runs", h.Runs)
	group.GET("/items", h.Items)
	group.POST("/item/thumbnail/retry", h.RetryThumbnail)
}

func (h *SubscriptionHandler) Plugins(c *gin.Context) {
	rows, err := h.service.AvailablePlugins(c.Request.Context())
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "查询失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", rows))
}

func (h *SubscriptionHandler) List(c *gin.Context) {
	var req request.SubscriptionListRequest
	_ = c.ShouldBindQuery(&req)
	rows, total, err := h.service.List(c.Request.Context(), c.GetString("userID"), req.Page, req.PageSize)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "查询失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", map[string]interface{}{"subscriptions": rows, "total": total}))
}

func (h *SubscriptionHandler) Create(c *gin.Context) {
	var req request.SubscriptionCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	row, runID, err := h.service.Create(c.Request.Context(), c.GetString("userID"), &req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "创建失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "创建成功", map[string]interface{}{"subscription": row, "run_id": runID}))
}

func (h *SubscriptionHandler) Update(c *gin.Context) {
	var req request.SubscriptionUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	if err := h.service.Update(c.Request.Context(), c.GetString("userID"), &req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "更新失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "更新成功", nil))
}

func (h *SubscriptionHandler) Delete(c *gin.Context) {
	var req request.SubscriptionIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := h.service.Delete(c.Request.Context(), c.GetString("userID"), req.ID); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "删除失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "删除成功", nil))
}
func (h *SubscriptionHandler) Toggle(c *gin.Context) {
	var req request.SubscriptionToggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := h.service.Toggle(c.Request.Context(), c.GetString("userID"), req.ID, req.Enabled); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "操作失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "操作成功", nil))
}
func (h *SubscriptionHandler) Run(c *gin.Context) {
	var req request.SubscriptionIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	runID, err := h.service.RunNow(c.Request.Context(), c.GetString("userID"), req.ID)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "运行失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "已开始运行", map[string]string{"run_id": runID}))
}
func (h *SubscriptionHandler) Runs(c *gin.Context)  { h.history(c, "runs") }
func (h *SubscriptionHandler) Items(c *gin.Context) { h.history(c, "items") }
func (h *SubscriptionHandler) RetryThumbnail(c *gin.Context) {
	var req request.SubscriptionThumbnailRetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := h.service.RetryThumbnail(c.Request.Context(), c.GetString("userID"), req.ItemID); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "重试失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "已重新提交缩略图", nil))
}
func (h *SubscriptionHandler) history(c *gin.Context, kind string) {
	var req request.SubscriptionHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	rows, total, err := h.service.History(c.Request.Context(), c.GetString("userID"), req.SubscriptionID, kind, req.Page, req.PageSize)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "查询失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", map[string]interface{}{"items": rows, "total": total}))
}
