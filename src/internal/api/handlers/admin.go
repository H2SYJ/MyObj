package handlers

import (
	"errors"
	"fmt"
	"io"
	"myobj/src/core/domain/request"
	"myobj/src/core/service"
	"myobj/src/internal/api/middleware"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/util"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AdminHandler struct {
	service *service.AdminService
	cache   cache.Cache
}

func NewAdminHandler(service *service.AdminService, cacheLocal cache.Cache) *AdminHandler {
	return &AdminHandler{
		service: service,
		cache:   cacheLocal,
	}
}

func (a *AdminHandler) Router(c *gin.RouterGroup) {
	verify := middleware.NewAuthMiddleware(a.cache,
		a.service.GetRepository().ApiKey(),
		a.service.GetRepository().User(),
		a.service.GetRepository().GroupPower(),
		a.service.GetRepository().Power())

	admin := c.Group("/admin")
	admin.Use(verify.Verify())
	admin.Use(middleware.AdminVerify()) // 管理员权限验证
	{
		// 用户管理
		admin.GET("/user/list", a.UserList)
		admin.POST("/user/create", a.CreateUser)
		admin.POST("/user/update", a.UpdateUser)
		admin.POST("/user/delete", a.DeleteUser)
		admin.POST("/user/toggle-state", a.ToggleUserState)

		// 组管理
		admin.GET("/group/list", a.GroupList)
		admin.POST("/group/create", a.CreateGroup)
		admin.POST("/group/update", a.UpdateGroup)
		admin.POST("/group/delete", a.DeleteGroup)

		// 权限管理
		admin.GET("/power/list", a.PowerList)
		admin.POST("/power/create", a.CreatePower)
		admin.POST("/power/update", a.UpdatePower)
		admin.POST("/power/delete", a.DeletePower)
		admin.POST("/power/batch-delete", a.BatchDeletePower)
		admin.POST("/power/assign", a.AssignPower)
		admin.GET("/power/group-powers", a.GetGroupPowers)

		// 磁盘管理
		admin.GET("/disk/list", a.DiskList)
		admin.POST("/disk/create", a.CreateDisk)
		admin.POST("/disk/update", a.UpdateDisk)
		admin.POST("/disk/delete", a.DeleteDisk)
		admin.GET("/disk/scan", a.GetDisk)

		// 系统配置
		admin.GET("/system/config", a.GetSystemConfig)
		admin.POST("/system/update-config", a.UpdateSystemConfig)

		admin.GET("/tag/settings", a.GetTagSettings)
		admin.PUT("/tag/settings", a.UpdateTagSettings)
		admin.GET("/tag/categories", a.ListTagCategories)
		admin.POST("/tag/categories", a.SaveTagCategory)
		admin.DELETE("/tag/categories/:id", a.DeleteTagCategory)
		admin.GET("/tag/rule-sets", a.ListTagRuleSets)
		admin.GET("/tag/rule-sets/:id", a.GetTagRuleSet)
		admin.POST("/tag/drafts", a.CreateTagDraft)
		admin.PUT("/tag/drafts/:id", a.SaveTagDraft)
		admin.POST("/tag/drafts/:id/import", a.ImportTagDraft)
		admin.GET("/tag/rule-sets/:id/export", a.ExportTagRuleSet)
		admin.GET("/tag/rule-sets/:id/diff", a.DiffTagRuleSet)
		admin.POST("/tag/drafts/:id/preview", a.PreviewTagDraft)
		admin.POST("/tag/drafts/:id/publish", a.PublishTagDraft)
		admin.POST("/tag/rule-sets/:id/rollback", a.RollbackTagRuleSet)
		admin.GET("/tag/rebuild-jobs", a.ListTagRebuildJobs)
		admin.GET("/tag/rebuild-jobs/:id", a.GetTagRebuildJob)
		admin.GET("/tag/rebuild-jobs/:id/failures", a.ListTagRebuildFailures)
		admin.POST("/tag/rebuild-jobs/:id/failures/:uf_id/retry", a.RetryTagRebuildFailure)
		admin.POST("/tag/rebuild-jobs/:id/cancel", a.CancelTagRebuildJob)
		admin.POST("/tag/rebuild-jobs/:id/retry", a.RetryTagRebuildJob)
	}

	logger.LOG.Info("[路由] 管理路由注册完成✔️")
}

// GetTagSettings godoc
// @Summary 获取自动标签设置与 Provider 状态
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse
// @Router /admin/tag/settings [get]
func (a *AdminHandler) GetTagSettings(c *gin.Context) {
	result, err := a.service.TagService().TagSettings(c.Request.Context())
	adminTagResult(c, result, err)
}

// UpdateTagSettings godoc
// @Summary 更新自动标签开关和数量上限
// @Tags 标签与词典管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.AdminTagSettingsRequest true "自动标签设置"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/settings [put]
func (a *AdminHandler) UpdateTagSettings(c *gin.Context) {
	var req request.AdminTagSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	result, err := a.service.TagService().UpdateTagSettings(c.Request.Context(), req.Enabled, req.Limit)
	adminTagResult(c, result, err)
}

// ListTagCategories godoc
// @Summary 获取全部标签分类
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=[]models.TagCategory}
// @Router /admin/tag/categories [get]
func (a *AdminHandler) ListTagCategories(c *gin.Context) {
	result, err := a.service.TagService().ListCategories(c.Request.Context(), false)
	adminTagResult(c, result, err)
}

// SaveTagCategory godoc
// @Summary 新增或更新标签分类
// @Tags 标签与词典管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.AdminTagCategoryRequest true "标签分类"
// @Success 200 {object} models.JsonResponse{data=models.TagCategory}
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/categories [post]
func (a *AdminHandler) SaveTagCategory(c *gin.Context) {
	var req request.AdminTagCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	result, err := a.service.TagService().SaveCategory(c.Request.Context(), req)
	adminTagResult(c, result, err)
}

// DeleteTagCategory godoc
// @Summary 删除自定义标签分类
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "分类ID"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/categories/{id} [delete]
func (a *AdminHandler) DeleteTagCategory(c *gin.Context) {
	err := a.service.TagService().DeleteCategory(c.Request.Context(), c.Param("id"))
	adminTagResult(c, nil, err)
}

// ListTagRuleSets godoc
// @Summary 获取全局标签规则版本
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=[]models.TagRuleSet}
// @Router /admin/tag/rule-sets [get]
func (a *AdminHandler) ListTagRuleSets(c *gin.Context) {
	result, err := a.service.TagService().GlobalRuleSets(c.Request.Context())
	adminTagResult(c, result, err)
}

// GetTagRuleSet godoc
// @Summary 获取标签规则集详情
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则集ID"
// @Success 200 {object} models.JsonResponse{data=models.TagRuleSet}
// @Failure 404 {object} models.JsonResponse
// @Router /admin/tag/rule-sets/{id} [get]
func (a *AdminHandler) GetTagRuleSet(c *gin.Context) {
	result, err := a.service.TagService().RuleSet(c.Request.Context(), c.Param("id"))
	adminTagResult(c, result, err)
}

// CreateTagDraft godoc
// @Summary 创建或获取全局标签规则草稿
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=models.TagRuleSet}
// @Router /admin/tag/drafts [post]
func (a *AdminHandler) CreateTagDraft(c *gin.Context) {
	result, err := a.service.TagService().CreateGlobalDraft(c.Request.Context(), c.GetString("userID"))
	adminTagResult(c, result, err)
}

// SaveTagDraft godoc
// @Summary 整体保存全局标签规则草稿
// @Description revision 不一致时返回 HTTP 409
// @Tags 标签与词典管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "草稿ID"
// @Param request body request.AdminSaveTagDraftRequest true "草稿规则与revision"
// @Success 200 {object} models.JsonResponse{data=models.TagRuleSet}
// @Failure 409 {object} models.JsonResponse
// @Router /admin/tag/drafts/{id} [put]
func (a *AdminHandler) SaveTagDraft(c *gin.Context) {
	var req request.AdminSaveTagDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	result, err := a.service.TagService().SaveGlobalDraft(c.Request.Context(), c.Param("id"), req.Revision, req.Rules)
	if err != nil && strings.Contains(err.Error(), "冲突") {
		c.JSON(409, models.NewJsonResponse(409, err.Error(), nil))
		return
	}
	adminTagResult(c, result, err)
}

// ImportTagDraft godoc
// @Summary 导入全局标签规则到草稿
// @Description 仅接受 UTF-8 无 BOM 的 JSON 或 CSV，不会自动发布
// @Tags 标签与词典管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "草稿ID"
// @Param revision query int true "草稿revision"
// @Param format query string false "导入格式" Enums(json,csv) default(json)
// @Param file body string true "JSON或CSV内容"
// @Success 200 {object} models.JsonResponse{data=models.TagRuleSet}
// @Failure 400 {object} models.JsonResponse
// @Failure 409 {object} models.JsonResponse
// @Router /admin/tag/drafts/{id}/import [post]
func (a *AdminHandler) ImportTagDraft(c *gin.Context) {
	revision, err := strconv.Atoi(c.Query("revision"))
	if err != nil || revision < 1 {
		c.JSON(400, models.NewJsonResponse(400, "revision 无效", nil))
		return
	}
	reader := io.LimitReader(c.Request.Body, 1024*1024+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		adminTagResult(c, nil, err)
		return
	}
	result, err := a.service.TagService().ImportGlobalDraft(c.Request.Context(), c.Param("id"), revision, c.DefaultQuery("format", "json"), data)
	if err != nil && strings.Contains(err.Error(), "冲突") {
		c.JSON(409, models.NewJsonResponse(409, err.Error(), nil))
		return
	}
	adminTagResult(c, result, err)
}

// ExportTagRuleSet godoc
// @Summary 导出标签规则集
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则集ID"
// @Param format query string false "导出格式" Enums(json,csv) default(json)
// @Success 200 {string} string "UTF-8 无 BOM 的 JSON 或 CSV"
// @Router /admin/tag/rule-sets/{id}/export [get]
func (a *AdminHandler) ExportTagRuleSet(c *gin.Context) {
	format := c.DefaultQuery("format", "json")
	data, contentType, err := a.service.TagService().ExportRuleSet(c.Request.Context(), c.Param("id"), format)
	if err != nil {
		adminTagResult(c, nil, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=tag-rules-%s.%s", c.Param("id"), format))
	c.Data(200, contentType, data)
}

// DiffTagRuleSet godoc
// @Summary 对比标签规则版本
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "规则集ID"
// @Success 200 {object} models.JsonResponse
// @Router /admin/tag/rule-sets/{id}/diff [get]
func (a *AdminHandler) DiffTagRuleSet(c *gin.Context) {
	result, err := a.service.TagService().RuleSetDiff(c.Request.Context(), c.Param("id"))
	adminTagResult(c, result, err)
}

// PreviewTagDraft godoc
// @Summary 预览全局标签规则草稿
// @Tags 标签与词典管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "草稿ID"
// @Param request body request.TagPreviewRequest true "文件名与元数据样例"
// @Success 200 {object} models.JsonResponse{data=[]response.TagPreviewItem}
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/drafts/{id}/preview [post]
func (a *AdminHandler) PreviewTagDraft(c *gin.Context) {
	var req request.TagPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if len(req.Rules) == 0 {
		ruleSet, err := a.service.TagService().RuleSet(c.Request.Context(), c.Param("id"))
		if err != nil {
			adminTagResult(c, nil, err)
			return
		}
		for _, rule := range ruleSet.Rules {
			req.Rules = append(req.Rules, request.TagRuleInput{ID: rule.ID, Type: rule.Type, TargetField: rule.TargetField, Pattern: rule.Pattern, Replacement: rule.Replacement, CategoryID: rule.CategoryID, Priority: rule.Priority, Weight: rule.Weight, Enabled: rule.Enabled})
		}
	}
	result, err := a.service.TagService().PreviewRules(c.Request.Context(), req.Samples, req.Rules)
	adminTagResult(c, result, err)
}

// PublishTagDraft godoc
// @Summary 发布全局标签规则草稿
// @Description 完整编译后原子热替换，并创建全量重建任务
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "草稿ID"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/drafts/{id}/publish [post]
func (a *AdminHandler) PublishTagDraft(c *gin.Context) {
	ruleSet, job, err := a.service.TagService().PublishGlobalDraft(c.Request.Context(), c.Param("id"), c.GetString("userID"))
	rebuildJobID := ""
	if job != nil {
		rebuildJobID = job.ID
	}
	adminTagResult(c, gin.H{"rule_set": ruleSet, "active_version": func() int64 {
		if ruleSet != nil {
			return ruleSet.Version
		}
		return 0
	}(), "rebuild_job_id": rebuildJobID, "rebuild_job": job}, err)
}

// RollbackTagRuleSet godoc
// @Summary 回滚到指定标签规则内容
// @Description 复制旧内容生成单调递增的新版本并发布
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "来源规则集ID"
// @Success 200 {object} models.JsonResponse
// @Router /admin/tag/rule-sets/{id}/rollback [post]
func (a *AdminHandler) RollbackTagRuleSet(c *gin.Context) {
	ruleSet, job, err := a.service.TagService().RollbackGlobalRules(c.Request.Context(), c.Param("id"), c.GetString("userID"))
	adminTagResult(c, gin.H{"rule_set": ruleSet, "rebuild_job": job}, err)
}

// ListTagRebuildJobs godoc
// @Summary 获取标签重建任务列表
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param limit query int false "返回数量，1到100" default(20)
// @Success 200 {object} models.JsonResponse{data=[]models.TagRebuildJob}
// @Router /admin/tag/rebuild-jobs [get]
func (a *AdminHandler) ListTagRebuildJobs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := a.service.TagService().RebuildJobs(c.Request.Context(), limit)
	adminTagResult(c, result, err)
}

// GetTagRebuildJob godoc
// @Summary 获取标签重建任务详情
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "任务ID"
// @Success 200 {object} models.JsonResponse{data=models.TagRebuildJob}
// @Failure 404 {object} models.JsonResponse
// @Router /admin/tag/rebuild-jobs/{id} [get]
func (a *AdminHandler) GetTagRebuildJob(c *gin.Context) {
	result, err := a.service.TagService().RebuildJob(c.Request.Context(), c.Param("id"))
	adminTagResult(c, result, err)
}

// ListTagRebuildFailures godoc
// @Summary 获取标签重建逐文件失败明细
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "任务ID"
// @Param status query string false "明细状态" Enums(failed,retrying,resolved)
// @Param limit query int false "返回数量，1到100" default(50)
// @Success 200 {object} models.JsonResponse{data=[]models.TagRebuildFailure}
// @Router /admin/tag/rebuild-jobs/{id}/failures [get]
func (a *AdminHandler) ListTagRebuildFailures(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := a.service.TagService().RebuildFailures(c.Request.Context(), c.Param("id"), c.Query("status"), limit)
	adminTagResult(c, result, err)
}

// RetryTagRebuildFailure godoc
// @Summary 重试单个标签重建失败文件
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "任务ID"
// @Param uf_id path string true "用户文件ID"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/rebuild-jobs/{id}/failures/{uf_id}/retry [post]
func (a *AdminHandler) RetryTagRebuildFailure(c *gin.Context) {
	err := a.service.TagService().RetryRebuildFailure(c.Request.Context(), c.Param("id"), c.Param("uf_id"))
	adminTagResult(c, nil, err)
}

// CancelTagRebuildJob godoc
// @Summary 取消标签重建任务
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "任务ID"
// @Success 200 {object} models.JsonResponse
// @Router /admin/tag/rebuild-jobs/{id}/cancel [post]
func (a *AdminHandler) CancelTagRebuildJob(c *gin.Context) {
	err := a.service.TagService().CancelRebuildJob(c.Request.Context(), c.Param("id"))
	adminTagResult(c, nil, err)
}

// RetryTagRebuildJob godoc
// @Summary 重试整个标签重建任务
// @Tags 标签与词典管理
// @Produce json
// @Security BearerAuth
// @Param id path string true "任务ID"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /admin/tag/rebuild-jobs/{id}/retry [post]
func (a *AdminHandler) RetryTagRebuildJob(c *gin.Context) {
	err := a.service.TagService().RetryRebuildJob(c.Request.Context(), c.Param("id"))
	adminTagResult(c, nil, err)
}

func adminTagResult(c *gin.Context, data interface{}, err error) {
	if err != nil {
		status := 400
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = 404
		}
		c.JSON(status, models.NewJsonResponse(status, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "操作成功", data))
}

// ========== 用户管理 ==========

// UserList 获取用户列表
func (a *AdminHandler) UserList(c *gin.Context) {
	req := new(request.AdminUserListRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminUserList(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// CreateUser 创建用户
func (a *AdminHandler) CreateUser(c *gin.Context) {
	req := new(request.AdminCreateUserRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminCreateUser(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// UpdateUser 更新用户
func (a *AdminHandler) UpdateUser(c *gin.Context) {
	req := new(request.AdminUpdateUserRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminUpdateUser(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// DeleteUser 删除用户
func (a *AdminHandler) DeleteUser(c *gin.Context) {
	req := new(request.AdminDeleteUserRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminDeleteUser(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// ToggleUserState 启用/禁用用户
func (a *AdminHandler) ToggleUserState(c *gin.Context) {
	req := new(request.AdminToggleUserStateRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminToggleUserState(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// ========== 组管理 ==========

// GroupList 获取组列表
func (a *AdminHandler) GroupList(c *gin.Context) {
	res, err := a.service.AdminGroupList()
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// CreateGroup 创建组
func (a *AdminHandler) CreateGroup(c *gin.Context) {
	req := new(request.AdminCreateGroupRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminCreateGroup(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// UpdateGroup 更新组
func (a *AdminHandler) UpdateGroup(c *gin.Context) {
	req := new(request.AdminUpdateGroupRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminUpdateGroup(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// DeleteGroup 删除组
func (a *AdminHandler) DeleteGroup(c *gin.Context) {
	req := new(request.AdminDeleteGroupRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminDeleteGroup(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// ========== 权限管理 ==========

// PowerList 获取权限列表
func (a *AdminHandler) PowerList(c *gin.Context) {
	req := new(request.AdminPowerListRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminPowerList(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// AssignPower 分配权限
func (a *AdminHandler) AssignPower(c *gin.Context) {
	req := new(request.AdminAssignPowerRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminAssignPower(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// GetGroupPowers 获取组的权限列表
func (a *AdminHandler) GetGroupPowers(c *gin.Context) {
	req := new(request.AdminGetGroupPowersRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminGetGroupPowers(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// CreatePower 创建权限
func (a *AdminHandler) CreatePower(c *gin.Context) {
	req := new(request.AdminCreatePowerRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminCreatePower(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// UpdatePower 更新权限
func (a *AdminHandler) UpdatePower(c *gin.Context) {
	req := new(request.AdminUpdatePowerRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminUpdatePower(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// DeletePower 删除权限
func (a *AdminHandler) DeletePower(c *gin.Context) {
	req := new(request.AdminDeletePowerRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminDeletePower(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// BatchDeletePower 批量删除权限
func (a *AdminHandler) BatchDeletePower(c *gin.Context) {
	req := new(request.AdminBatchDeletePowerRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminBatchDeletePower(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// ========== 磁盘管理 ==========

// DiskList 获取磁盘列表
func (a *AdminHandler) DiskList(c *gin.Context) {
	res, err := a.service.AdminDiskList()
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// CreateDisk 创建磁盘
func (a *AdminHandler) CreateDisk(c *gin.Context) {
	req := new(request.AdminCreateDiskRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminCreateDisk(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// UpdateDisk 更新磁盘
func (a *AdminHandler) UpdateDisk(c *gin.Context) {
	req := new(request.AdminUpdateDiskRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminUpdateDisk(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// DeleteDisk 删除磁盘
func (a *AdminHandler) DeleteDisk(c *gin.Context) {
	req := new(request.AdminDeleteDiskRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminDeleteDisk(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// GetDisk 获取扫描到的磁盘信息
func (a *AdminHandler) GetDisk(c *gin.Context) {
	info, err := util.GetDiskInfo()
	if err != nil {
		logger.LOG.Error("获取磁盘信息失败", "err", err)
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "成功", info))
}

// ========== 系统配置 ==========

// GetSystemConfig 获取系统配置
func (a *AdminHandler) GetSystemConfig(c *gin.Context) {
	res, err := a.service.AdminGetSystemConfig()
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}

// UpdateSystemConfig 更新系统配置
func (a *AdminHandler) UpdateSystemConfig(c *gin.Context) {
	req := new(request.AdminUpdateSystemConfigRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	res, err := a.service.AdminUpdateSystemConfig(req)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, res)
}
