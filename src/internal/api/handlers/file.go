package handlers

import (
	"errors"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/core/service"
	"myobj/src/internal/api/middleware"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FileHandler struct {
	service *service.FileService
	cache   cache.Cache
}

func NewFileHandler(service *service.FileService, cacheLocal cache.Cache) *FileHandler {
	return &FileHandler{
		service: service,
		cache:   cacheLocal,
	}
}

func (f *FileHandler) Router(c *gin.RouterGroup) {
	verify := middleware.NewAuthMiddleware(f.cache,
		f.service.GetRepository().ApiKey(),
		f.service.GetRepository().User(),
		f.service.GetRepository().GroupPower(),
		f.service.GetRepository().Power())

	// 公开路由（不需要验证）
	publicGroup := c.Group("/file")
	{
		// 公开文件列表
		publicGroup.GET("/public/list", f.PublicFileList)
	}

	// 需要验证的路由
	fileGroup := c.Group("/file")
	{
		fileGroup.Use(verify.Verify())
		// 预检接口
		fileGroup.POST("/upload/precheck", middleware.PowerVerify("file:upload"), f.Precheck)
		// 上传进度查询接口
		fileGroup.GET("/upload/progress", middleware.PowerVerify("file:upload"), f.GetUploadProgress)
		// 查询未完成的上传任务列表
		fileGroup.GET("/upload/uncompleted", middleware.PowerVerify("file:upload"), f.ListUncompletedUploads)
		// 查询过期的上传任务列表
		fileGroup.GET("/upload/expired", middleware.PowerVerify("file:upload"), f.ListExpiredUploads)
		// 查询上传任务列表
		fileGroup.GET("/upload/taskList", middleware.PowerVerify("file:upload"), f.GetUploadTaskList)
		// 删除上传任务
		fileGroup.POST("/upload/delete", middleware.PowerVerify("file:upload"), f.DeleteUploadTask)
		// 延期过期任务（恢复任务）
		fileGroup.POST("/upload/renew", middleware.PowerVerify("file:upload"), f.RenewExpiredTask)
		// 清理过期的上传任务（用户可清理自己的，系统自动清理所有）
		fileGroup.POST("/upload/clean-expired", middleware.PowerVerify("file:upload"), f.CleanExpiredUploads)
		// 文件上传接口
		fileGroup.POST("/upload", middleware.PowerVerify("file:upload"), f.UploadFile)
		fileGroup.POST("/upload/finalize/retry", middleware.PowerVerify("file:upload"), f.RetryUploadFinalize)
		// 获取文件列表
		fileGroup.GET("/list", middleware.PowerVerify("file:preview"), f.GetFileList)
		// 获取缩略图
		fileGroup.GET("/thumbnail/:fileId", middleware.PowerVerify("file:preview"), f.GetThumbnail)
		// 修改缩略图（业务逻辑会验证文件所有权）
		fileGroup.PUT("/thumbnail/:fileId", f.UpdateThumbnail)
		// 搜索当前用户文件
		fileGroup.GET("/search/user", middleware.PowerVerify("file:preview"), f.SearchUserFiles)
		// 搜索公开文件
		fileGroup.GET("/search/public", middleware.PowerVerify("file:preview"), f.SearchPublicFiles)
		// 创建目录
		fileGroup.POST("/makeDir", middleware.PowerVerify("dir:create"), f.MakeDir)
		// 移动文件
		fileGroup.POST("/move", middleware.PowerVerify("file:move"), f.MoveFile)
		fileGroup.POST("/moveBatch", middleware.PowerVerify("file:move"), f.MoveItems)
		// 删除文件
		fileGroup.POST("/delete", middleware.PowerVerify("file:delete"), f.DeleteFile)
		fileGroup.POST("/deleteBatch", f.DeleteItems)
		// 重命名文件（业务逻辑已验证文件所有权，无需额外权限验证）
		fileGroup.POST("/rename", f.RenameFile)
		// 在线编辑文本文件
		fileGroup.POST("/edit/save", middleware.PowerVerify("file:edit"), f.SaveFileContent)
		// 加载可编辑文本内容（UTF-8 解码 + 编码/base_hash 元数据）
		fileGroup.GET("/edit/load", middleware.PowerVerify("file:edit"), f.LoadFileContent)
		// 重命名目录（业务逻辑已验证目录所有权，无需额外权限验证）
		fileGroup.POST("/renameDir", f.RenameDir)
		// 删除目录（业务逻辑已验证目录所有权，无需额外权限验证）
		fileGroup.POST("/deleteDir", f.DeleteDir)
		// 设置文件公开状态（业务逻辑已验证文件所有权和加密状态，无需额外权限验证）
		fileGroup.POST("/setPublic", f.SetFilePublic)
		// 获取虚拟目录
		fileGroup.GET("/directories", middleware.PowerVerify("file:preview"), f.GetDirectories)
		fileGroup.GET("/directories/:directory_id/tags", middleware.PowerVerify("file:preview"), f.GetDirectoryTags)
		fileGroup.PUT("/directories/:directory_id/tags/manual", middleware.PowerVerify("file:tag"), f.UpdateDirectoryTags)
		fileGroup.GET("/tag-categories", middleware.PowerVerify("file:tag"), f.ListTagCategories)
		fileGroup.GET("/tag-cloud", middleware.PowerVerify("file:tag"), f.GetTagCloud)
		fileGroup.GET("/tag-cloud/:tag_id", middleware.PowerVerify("file:tag"), f.GetTagCloudItem)
		fileGroup.PUT("/tag-cloud/:tag_id", middleware.PowerVerify("file:tag"), f.UpdateTagCloudItem)
		fileGroup.DELETE("/tag-cloud/:tag_id", middleware.PowerVerify("file:tag"), f.HideTagCloudItem)
		fileGroup.POST("/tag-cloud/:tag_id/restore", middleware.PowerVerify("file:tag"), f.RestoreTagCloudItem)
		fileGroup.GET("/tags/suggestions", middleware.PowerVerify("file:tag"), f.TagSuggestions)
		fileGroup.GET("/tags/:uf_id", middleware.PowerVerify("file:preview"), f.GetFileTags)
		fileGroup.PUT("/tags/:uf_id/manual", middleware.PowerVerify("file:tag"), f.UpdateManualTags)
		fileGroup.PUT("/tags/:uf_id/exclusions", middleware.PowerVerify("file:tag"), f.UpdateTagExclusions)
		fileGroup.POST("/tags/:uf_id/retry", middleware.PowerVerify("file:tag"), f.RetryFileTags)
		fileGroup.POST("/tags/batch", middleware.PowerVerify("file:tag"), f.BatchUpdateTags)
		fileGroup.GET("/tag-dictionary", middleware.PowerVerify("file:tag"), f.GetPersonalTagDictionary)
		fileGroup.PUT("/tag-dictionary", middleware.PowerVerify("file:tag"), f.UpdatePersonalTagDictionary)
		fileGroup.POST("/tag-dictionary/preview", middleware.PowerVerify("file:tag"), f.PreviewPersonalTagDictionary)
		// 打包下载
		fileGroup.POST("/package/create", middleware.PowerVerify("file:download"), f.CreatePackage)
		fileGroup.GET("/package/progress", middleware.PowerVerify("file:download"), f.GetPackageProgress)
		fileGroup.GET("/package/download", middleware.PowerVerify("file:download"), f.DownloadPackage)
	}

	logger.LOG.Info("[路由] 文件路由注册完成✔️")
}

func (f *FileHandler) GetDirectoryTags(c *gin.Context) {
	directoryID, err := strconv.Atoi(c.Param("directory_id"))
	if err != nil || directoryID <= 0 {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, "文件夹ID无效", nil))
		return
	}
	result, err := f.service.TagService().GetDirectoryTags(c.Request.Context(), c.GetString("userID"), directoryID)
	if err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "查询成功", result))
}

func (f *FileHandler) UpdateDirectoryTags(c *gin.Context) {
	directoryID, err := strconv.Atoi(c.Param("directory_id"))
	if err != nil || directoryID <= 0 {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, "文件夹ID无效", nil))
		return
	}
	var req request.UpdateDirectoryTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := f.service.TagService().UpdateDirectoryTags(c.Request.Context(), c.GetString("userID"), directoryID, req.Add, req.RemoveTagIDs); err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	result, err := f.service.TagService().GetDirectoryTags(c.Request.Context(), c.GetString("userID"), directoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.NewJsonResponse(500, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "文件夹标签已更新", result))
}

// GetFileTags godoc
// @Summary 获取文件标签
// @Description 获取当前用户文件的有效标签、已屏蔽自动标签及生成状态
// @Tags 文件标签
// @Produce json
// @Security BearerAuth
// @Param uf_id path string true "用户文件ID"
// @Success 200 {object} models.JsonResponse{data=response.FileTagsResponse}
// @Failure 400 {object} models.JsonResponse
// @Router /file/tags/{uf_id} [get]
func (f *FileHandler) GetFileTags(c *gin.Context) {
	result, err := f.service.TagService().GetFileTags(c.Request.Context(), c.GetString("userID"), c.Param("uf_id"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", result))
}

// ListTagCategories godoc
// @Summary 获取可用标签分类
// @Tags 文件标签
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=[]models.TagCategory}
// @Router /file/tag-categories [get]
func (f *FileHandler) ListTagCategories(c *gin.Context) {
	result, err := f.service.TagService().ListCategories(c.Request.Context(), true)
	if err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "查询成功", result))
}

func (f *FileHandler) GetTagCloud(c *gin.Context) {
	result, err := f.service.TagService().TagCloud(c.Request.Context(), c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "查询成功", result))
}

func (f *FileHandler) GetTagCloudItem(c *gin.Context) {
	result, err := f.service.TagService().TagCloudEditor(c.Request.Context(), c.GetString("userID"), c.Param("tag_id"))
	if err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "查询成功", result))
}

func (f *FileHandler) UpdateTagCloudItem(c *gin.Context) {
	var req request.UpdateTagCloudItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	result, job, err := f.service.TagService().UpdateTagCloudItem(c.Request.Context(), c.GetString("userID"), c.Param("tag_id"), req)
	if err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "标签设置已更新", gin.H{"editor": result, "rebuild_job": job}))
}

func (f *FileHandler) HideTagCloudItem(c *gin.Context) {
	if err := f.service.TagService().HideTagCloudItem(c.Request.Context(), c.GetString("userID"), c.Param("tag_id")); err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "标签已隐藏", nil))
}

func (f *FileHandler) RestoreTagCloudItem(c *gin.Context) {
	job, err := f.service.TagService().RestoreTagCloudItem(c.Request.Context(), c.GetString("userID"), c.Param("tag_id"))
	if err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "标签已恢复", gin.H{"rebuild_job": job}))
}

// RetryFileTags godoc
// @Summary 重试单文件自动标签
// @Tags 文件标签
// @Produce json
// @Security BearerAuth
// @Param uf_id path string true "用户文件ID"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /file/tags/{uf_id}/retry [post]
func (f *FileHandler) RetryFileTags(c *gin.Context) {
	if err := f.service.TagService().RetryUserFile(c.Request.Context(), c.GetString("userID"), c.Param("uf_id")); err != nil {
		c.JSON(http.StatusOK, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(http.StatusOK, models.NewJsonResponse(200, "已重新排队", nil))
}

// UpdateManualTags godoc
// @Summary 更新文件手工标签
// @Description 原子增加或删除手工标签，并设置公开性
// @Tags 文件标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param uf_id path string true "用户文件ID"
// @Param request body request.UpdateManualTagsRequest true "手工标签变更"
// @Success 200 {object} models.JsonResponse{data=response.FileTagsResponse}
// @Failure 400 {object} models.JsonResponse
// @Router /file/tags/{uf_id}/manual [put]
func (f *FileHandler) UpdateManualTags(c *gin.Context) {
	var req request.UpdateManualTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	err := f.service.TagService().UpdateManualTags(c.Request.Context(), c.GetString("userID"), []string{c.Param("uf_id")}, req.Add, req.RemoveTagIDs)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	result, err := f.service.TagService().GetFileTags(c.Request.Context(), c.GetString("userID"), c.Param("uf_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.NewJsonResponse(500, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "标签已更新", result))
}

// UpdateTagExclusions godoc
// @Summary 屏蔽或恢复自动标签
// @Tags 文件标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param uf_id path string true "用户文件ID"
// @Param request body request.UpdateTagExclusionsRequest true "屏蔽项变更"
// @Success 200 {object} models.JsonResponse{data=response.FileTagsResponse}
// @Failure 400 {object} models.JsonResponse
// @Router /file/tags/{uf_id}/exclusions [put]
func (f *FileHandler) UpdateTagExclusions(c *gin.Context) {
	var req request.UpdateTagExclusionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	err := f.service.TagService().UpdateExclusions(c.Request.Context(), c.GetString("userID"), c.Param("uf_id"), req.SuppressTagIDs, req.RestoreTagIDs)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	result, err := f.service.TagService().GetFileTags(c.Request.Context(), c.GetString("userID"), c.Param("uf_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.NewJsonResponse(500, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "自动标签设置已更新", result))
}

// BatchUpdateTags godoc
// @Summary 批量更新手工标签
// @Description 先校验全部文件归属，再对最多100个文件原子更新
// @Tags 文件标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.BatchUpdateTagsRequest true "批量标签变更"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /file/tags/batch [post]
func (f *FileHandler) BatchUpdateTags(c *gin.Context) {
	var req request.BatchUpdateTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	if err := f.service.TagService().UpdateManualTags(c.Request.Context(), c.GetString("userID"), req.FileIDs, req.Add, req.RemoveTagIDs); err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "批量标签已更新", nil))
}

// TagSuggestions godoc
// @Summary 获取标签建议
// @Description 仅返回当前用户使用过的标签和允许公开的全局建议
// @Tags 文件标签
// @Produce json
// @Security BearerAuth
// @Param keyword query string false "标签关键词"
// @Param tag_ids query string false "逗号分隔的标签ID，用于回填已选标签"
// @Param scope query string false "建议范围" Enums(user,public) default(user)
// @Param limit query int false "返回数量，1到50" default(20)
// @Success 200 {object} models.JsonResponse{data=[]response.CompactTagView}
// @Router /file/tags/suggestions [get]
func (f *FileHandler) TagSuggestions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := f.service.TagService().SuggestionsForTarget(
		c.Request.Context(),
		c.GetString("userID"),
		c.Query("keyword"),
		strings.Split(c.Query("tag_ids"), ","),
		c.Query("scope"),
		c.Query("target"),
		limit,
	)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", result))
}

// GetPersonalTagDictionary godoc
// @Summary 获取个人分词词典
// @Tags 文件标签
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=models.TagRuleSet}
// @Router /file/tag-dictionary [get]
func (f *FileHandler) GetPersonalTagDictionary(c *gin.Context) {
	result, err := f.service.TagService().PersonalDictionary(c.Request.Context(), c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "查询成功", result))
}

// UpdatePersonalTagDictionary godoc
// @Summary 热更新个人分词词典
// @Description 保存个人词语、停用词和别名，并创建用户范围历史重建任务
// @Tags 文件标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.UpdatePersonalDictionaryRequest true "个人词典"
// @Success 200 {object} models.JsonResponse
// @Failure 400 {object} models.JsonResponse
// @Router /file/tag-dictionary [put]
func (f *FileHandler) UpdatePersonalTagDictionary(c *gin.Context) {
	var req request.UpdatePersonalDictionaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	ruleSet, job, err := f.service.TagService().SavePersonalDictionary(c.Request.Context(), c.GetString("userID"), req.Rules)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "个人词典已热更新", gin.H{"rule_set": ruleSet, "rebuild_job": job}))
}

// PreviewPersonalTagDictionary godoc
// @Summary 预览个人分词词典
// @Tags 文件标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.TagPreviewRequest true "文件名样例和候选规则"
// @Success 200 {object} models.JsonResponse{data=[]response.TagPreviewItem}
// @Failure 400 {object} models.JsonResponse
// @Router /file/tag-dictionary/preview [post]
func (f *FileHandler) PreviewPersonalTagDictionary(c *gin.Context) {
	var req request.TagPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", nil))
		return
	}
	result, err := f.service.TagService().PreviewRules(c.Request.Context(), c.GetString("userID"), req.Samples, req.Rules, true)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "预览成功", result))
}

// Precheck godoc
// @Summary 文件上传预检
// @Description 上传前的预检查，检查空间、秒传可能性，返回预检ID
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.UploadPrecheckRequest true "预检请求"
// @Success 200 {object} models.JsonResponse{data=string} "预检ID"
// @Success 200 {object} models.JsonResponse{message=string} "秒传成功"
// @Failure 400 {object} models.JsonResponse "预检失败"
// @Router /file/upload/precheck [post]
func (f *FileHandler) Precheck(c *gin.Context) {
	req := new(request.UploadPrecheckRequest)
	if err := c.ShouldBind(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	req.UserID = c.GetString("userID")
	precheck, err := f.service.Precheck(req, f.cache)
	if err != nil {
		c.JSON(400, models.NewJsonResponse(400, "预检查失败", err.Error()))
		return
	}
	c.JSON(200, precheck)
}

// SearchUserFiles godoc
// @Summary 搜索当前用户文件
// @Description 关键词和标签筛选至少提供一项；关键词仅匹配文件名
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param keyword query string false "搜索关键词"
// @Param directory_id query int false "限制在当前目录"
// @Param tag_ids query string false "逗号分隔的标签ID"
// @Param tag_mode query string false "标签匹配模式，省略时按任一标签匹配" Enums(all,any) default(any)
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页数量" default(20)
// @Success 200 {object} models.JsonResponse{data=object} "搜索结果"
// @Failure 500 {object} models.JsonResponse "搜索失败"
// @Router /file/search/user [get]
func (f *FileHandler) SearchUserFiles(c *gin.Context) {
	req := new(request.FileSearchRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	userID := c.GetString("userID")
	result, err := f.service.SearchUserFiles(req, userID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidFileSearch) {
			c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, err.Error(), nil))
			return
		}
		c.JSON(200, models.NewJsonResponse(500, "搜索失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// SearchPublicFiles godoc
// @Summary 搜索公开文件
// @Description 关键词和标签筛选至少提供一项；关键词仅匹配文件名，私有手工标签不参与显式标签筛选
// @Tags 文件管理
// @Produce json
// @Security BearerAuth
// @Param keyword query string false "搜索关键词"
// @Param tag_ids query string false "逗号分隔的标签ID"
// @Param tag_mode query string false "标签匹配模式，省略时按任一标签匹配" Enums(all,any) default(any)
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页数量" default(20)
// @Success 200 {object} models.JsonResponse{data=object}
// @Failure 400 {object} models.JsonResponse
// @Router /file/search/public [get]
func (f *FileHandler) SearchPublicFiles(c *gin.Context) {
	req := new(request.FileSearchRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.SearchPublicFiles(req, c.GetString("userID"))
	if err != nil {
		if errors.Is(err, service.ErrInvalidFileSearch) {
			c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, err.Error(), nil))
			return
		}
		c.JSON(200, models.NewJsonResponse(500, "搜索失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// GetFileList godoc
// @Summary 获取文件列表
// @Description 获取当前用户指定目录下的文件列表
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param directory_id query int false "目录ID，0表示用户根目录"
// @Param tag_ids query string false "逗号分隔的标签ID"
// @Param tag_mode query string false "标签匹配模式，省略时按任一标签匹配" Enums(all,any) default(any)
// @Param page query int true "页码" minimum(1)
// @Param pageSize query int true "每页数量" minimum(1) maximum(100)
// @Success 200 {object} models.JsonResponse{data=response.FileListResponse} "文件列表"
// @Failure 500 {object} models.JsonResponse "获取失败"
// @Router /file/list [get]
func (f *FileHandler) GetFileList(c *gin.Context) {
	req := new(request.FileListRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	userID := c.GetString("userID")
	result, err := f.service.GetFileList(req, userID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidFileSearch) {
			c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
			return
		}
		c.JSON(200, models.NewJsonResponse(500, "获取失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// sendThumbnailResponse 发送缩略图响应（提取的公共逻辑）
func (f *FileHandler) sendThumbnailResponse(c *gin.Context, thumbnailPath string) {
	// 检查是否有缩略图
	if thumbnailPath == "" {
		c.JSON(404, models.NewJsonResponse(404, "缩略图不存在", nil))
		return
	}

	// 设置响应头
	ext := filepath.Ext(thumbnailPath)
	contentType := "image/jpeg"
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=86400") // 缓存1天
	c.File(thumbnailPath)
}

// GetThumbnail 获取文件缩略图
func (f *FileHandler) GetThumbnail(c *gin.Context) {
	fileID := c.Param("fileId")
	if fileID == "" {
		c.JSON(200, models.NewJsonResponse(400, "文件ID不能为空", nil))
		return
	}

	userID := c.GetString("userID")
	ctx := c.Request.Context()

	// 先通过 uf_id 查询 user_files 表，获取真实的 file_id
	// 因为前端传递的是 uf_id（用户文件关联表的ID），而不是 file_info 表的 id
	userFile, err := f.service.GetRepository().UserFiles().GetByUfID(ctx, fileID)
	if err != nil {
		// 如果通过 uf_id 查询失败，尝试直接作为 file_id 查询（兼容旧版本）
		fileInfo, err2 := f.service.GetRepository().FileInfo().GetByID(ctx, fileID)
		if err2 != nil {
			c.JSON(200, models.NewJsonResponse(404, "文件不存在", err.Error()))
			return
		}
		// 发送缩略图响应
		f.sendThumbnailResponse(c, fileInfo.ThumbnailImg)
		return
	}
	if !userFile.IsPublic && userFile.UserID != userID {
		c.JSON(200, models.NewJsonResponse(403, "文件不存在", nil))
		return
	}
	// 通过 user_files 获取到的 file_id 查询 file_info
	fileInfo, err := f.service.GetRepository().FileInfo().GetByID(ctx, userFile.FileID)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(404, "文件不存在", err.Error()))
		return
	}
	if fileInfo.IsEnc {
		c.JSON(200, models.NewJsonResponse(400, "加密文件无法获取缩略图", nil))
		return
	}
	// 发送缩略图响应
	f.sendThumbnailResponse(c, fileInfo.ThumbnailImg)
}

// UpdateThumbnail godoc
// @Summary 修改文件缩略图
// @Description 修改当前用户文件的缩略图，仅支持 JPEG，最大1MB，宽高不超过1000像素
// @Tags 文件管理
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param fileId path string true "用户文件ID"
// @Param thumbnail formData file true "JPEG缩略图（最大1MB，宽高不超过1000像素）"
// @Success 200 {object} models.JsonResponse{data=object} "修改成功"
// @Failure 400 {object} models.JsonResponse "参数错误或缩略图无效"
// @Failure 403 {object} models.JsonResponse "加密文件不支持缩略图"
// @Failure 404 {object} models.JsonResponse "文件不存在或无权访问"
// @Failure 500 {object} models.JsonResponse "修改失败"
// @Router /file/thumbnail/{fileId} [put]
func (f *FileHandler) UpdateThumbnail(c *gin.Context) {
	fileID := c.Param("fileId")
	if fileID == "" {
		c.JSON(200, models.NewJsonResponse(400, "文件ID不能为空", nil))
		return
	}

	thumbnail, thumbnailHeader, err := c.Request.FormFile("thumbnail")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			c.JSON(200, models.NewJsonResponse(400, "缩略图不能为空", nil))
			return
		}
		c.JSON(200, models.NewJsonResponse(400, "读取缩略图失败", err.Error()))
		return
	}
	defer thumbnail.Close()

	result, err := f.service.UpdateThumbnail(
		c.Request.Context(),
		fileID,
		c.GetString("userID"),
		thumbnail,
		thumbnailHeader,
	)
	if err != nil {
		logger.LOG.Error("修改缩略图失败", "error", err, "fileID", fileID)
		c.JSON(200, models.NewJsonResponse(500, "修改缩略图失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// MakeDir 创建目录
// MakeDir godoc
// @Summary 创建虚拟目录
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.MakeDirRequest true "目录名称和父目录ID"
// @Success 200 {object} models.JsonResponse{data=response.DirectoryItem}
// @Router /file/makeDir [post]
func (f *FileHandler) MakeDir(c *gin.Context) {
	req := new(request.MakeDirRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	userID := c.GetString("userID")
	makeDir, err := f.service.MakeDir(req, userID)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "创建目录失败", err.Error()))
		return
	}
	c.JSON(200, makeDir)
}

// MoveFile godoc
// @Summary 移动单个文件
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.MoveFileRequest true "文件ID和目标目录ID"
// @Success 200 {object} models.JsonResponse
// @Router /file/move [post]
func (f *FileHandler) MoveFile(c *gin.Context) {
	req := new(request.MoveFileRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	moveFile, err := f.service.MoveFile(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "移动文件失败", err.Error()))
		return
	}
	c.JSON(200, moveFile)
}

// MoveItems godoc
// @Summary 批量移动文件和目录
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.MoveItemsRequest true "文件ID、目录ID和目标目录ID"
// @Success 200 {object} models.JsonResponse
// @Router /file/moveBatch [post]
func (f *FileHandler) MoveItems(c *gin.Context) {
	req := new(request.MoveItemsRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.MoveItems(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "移动失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// GetDirectories godoc
// @Summary 获取虚拟目录
// @Tags 文件管理
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=[]response.DirectoryItem}
// @Router /file/directories [get]
func (f *FileHandler) GetDirectories(c *gin.Context) {
	userID := c.GetString("userID")
	result, err := f.service.GetDirectories(userID)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "获取虚拟目录失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// DeleteFile godoc
// @Summary 删除文件
// @Description 将文件移动到回收站（软删除）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.DeleteFileRequest true "删除请求"
// @Success 200 {object} models.JsonResponse{data=object} "删除结果"
// @Failure 500 {object} models.JsonResponse "删除失败"
// @Router /file/delete [post]
func (f *FileHandler) DeleteFile(c *gin.Context) {
	req := new(request.DeleteFileRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.DeleteFiles(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "删除文件失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// SaveFileContent godoc
// @Summary 在线编辑文本文件
// @Description 保存文本文件的新内容（按原编码写回；加密文件需提供密码；base_hash 不匹配返回 409）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.EditFileContentRequest true "编辑请求"
// @Success 200 {object} models.JsonResponse{data=response.EditFileContentResponse} "保存结果"
// @Failure 400 {object} models.JsonResponse "参数错误"
// @Failure 409 {object} models.JsonResponse "文件内容已被他人修改"
// @Failure 500 {object} models.JsonResponse "保存失败"
// @Router /file/edit/save [post]
func (f *FileHandler) SaveFileContent(c *gin.Context) {
	req := new(request.EditFileContentRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.EditFileContent(c.Request.Context(), c.GetString("userID"), req)
	if err != nil {
		if errors.Is(err, service.ErrFileContentConflict) {
			c.JSON(http.StatusConflict, models.NewJsonResponse(409, "保存冲突", err.Error()))
			return
		}
		c.JSON(http.StatusInternalServerError, models.NewJsonResponse(500, "保存文件失败", err.Error()))
		return
	}
	c.JSON(200, models.NewJsonResponse(200, "保存成功", result))
}

// LoadFileContent godoc
// @Summary 加载可编辑文本内容
// @Description 加载文本文件并解码为 UTF-8，响应头携带 X-File-Encoding（原编码）与 X-File-Hash（明文 blake3，即保存时的 base_hash）
// @Tags 文件管理
// @Produce text/plain
// @Security BearerAuth
// @Param file_id query string true "用户文件ID（UserFiles的UfID）"
// @Param file_password query string false "文件解密密码（加密文件必需）"
// @Success 200 {string} string "UTF-8 解码后的文本内容"
// @Header 200 {string} X-File-Encoding "原文件编码"
// @Header 200 {string} X-File-Hash "明文文件 blake3 哈希（base_hash）"
// @Failure 400 {object} models.JsonResponse "参数错误或文件不可编辑"
// @Failure 403 {object} models.JsonResponse "无权限"
// @Failure 500 {object} models.JsonResponse "加载失败"
// @Router /file/edit/load [get]
func (f *FileHandler) LoadFileContent(c *gin.Context) {
	fileID := c.Query("file_id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, "参数错误：file_id不能为空", nil))
		return
	}
	result, err := f.service.LoadFileContent(c.Request.Context(), c.GetString("userID"), fileID, c.Query("file_password"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, models.NewJsonResponse(404, "文件不存在", err.Error()))
			return
		}
		c.JSON(http.StatusBadRequest, models.NewJsonResponse(400, "加载文件失败", err.Error()))
		return
	}
	c.Header("X-File-Encoding", result.Encoding)
	c.Header("X-File-Hash", result.FileHash)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(result.Content))
}

// DeleteItems 批量删除文件和目录，并按项目类型校验权限。
func (f *FileHandler) DeleteItems(c *gin.Context) {
	req := new(request.DeleteItemsRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	if len(req.FileIDs) > 0 && !requestHasPower(c, "file:delete") {
		c.JSON(403, models.NewJsonResponse(403, "无文件删除权限", nil))
		return
	}
	if len(req.DirIDs) > 0 && !requestHasPower(c, "dir:delete") {
		c.JSON(403, models.NewJsonResponse(403, "无目录删除权限", nil))
		return
	}
	result, err := f.service.DeleteItems(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "删除失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

func requestHasPower(c *gin.Context, characteristic string) bool {
	value, ok := c.Get("userLogin")
	if !ok {
		return false
	}
	login, ok := value.(response.UserLoginResponse)
	if !ok {
		return false
	}
	for _, power := range login.Power {
		if power.Characteristic == characteristic {
			return true
		}
	}
	return false
}

// RenameFile godoc
// @Summary 重命名文件
// @Description 重命名用户文件
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.RenameFileRequest true "重命名请求"
// @Success 200 {object} models.JsonResponse{data=object} "重命名成功"
// @Failure 400 {object} models.JsonResponse "参数错误或重命名失败"
// @Failure 404 {object} models.JsonResponse "文件不存在"
// @Router /file/rename [post]
func (f *FileHandler) RenameFile(c *gin.Context) {
	req := new(request.RenameFileRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.RenameFile(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "重命名文件失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// RenameDir godoc
// @Summary 重命名目录
// @Description 重命名用户目录，并自动更新子目录和文件的路径
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.RenameDirRequest true "重命名请求"
// @Success 200 {object} models.JsonResponse{data=object} "重命名成功"
// @Failure 400 {object} models.JsonResponse "参数错误或重命名失败"
// @Failure 404 {object} models.JsonResponse "目录不存在"
// @Failure 403 {object} models.JsonResponse "无权访问"
// @Router /file/renameDir [post]
func (f *FileHandler) RenameDir(c *gin.Context) {
	req := new(request.RenameDirRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.RenameDir(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "重命名目录失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// DeleteDir godoc
// @Summary 删除目录
// @Description 删除目录及其下的所有文件和子目录（文件会移动到回收站）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.DeleteDirRequest true "删除目录请求"
// @Success 200 {object} models.JsonResponse{data=object} "删除成功"
// @Failure 400 {object} models.JsonResponse "参数错误或根目录不能删除"
// @Failure 404 {object} models.JsonResponse "目录不存在"
// @Failure 403 {object} models.JsonResponse "无权访问"
// @Router /file/deleteDir [post]
func (f *FileHandler) DeleteDir(c *gin.Context) {
	req := new(request.DeleteDirRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.DeleteDir(req, c.GetString("userID"))
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "删除目录失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// SetFilePublic godoc
// @Summary 设置文件公开状态
// @Description 设置文件是否公开（加密文件不能设置为公开）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.SetFilePublicRequest true "设置公开状态请求"
// @Success 200 {object} models.JsonResponse{data=object} "设置成功"
// @Failure 400 {object} models.JsonResponse "参数错误或加密文件不能公开"
// @Failure 404 {object} models.JsonResponse "文件不存在"
// @Router /file/setPublic [post]
func (f *FileHandler) SetFilePublic(c *gin.Context) {
	req := new(request.SetFilePublicRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		logger.LOG.Error("参数错误", "err", err)
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.SetFilePublic(req, c.GetString("userID"))
	if err != nil {
		logger.LOG.Error("设置文件公开状态失败", "err", err)
		c.JSON(200, models.NewJsonResponse(500, "设置文件公开状态失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// UploadFile godoc
// @Summary 文件上传
// @Description 支持小文件直传和大文件分片上传
// @Tags 文件管理
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param precheck_id formData string true "预检ID"
// @Param file formData file true "文件数据"
// @Param thumbnail formData file false "视频缩略图（JPEG，最大1MB，宽高不超过1000像素）"
// @Param chunk_index formData int false "分片索引"
// @Param total_chunks formData int false "总分片数"
// @Param chunk_md5 formData string false "分片MD5"
// @Param is_enc formData boolean false "是否加密"
// @Param file_password formData string false "文件加密密码(加密文件必须)"
// @Success 200 {object} models.JsonResponse{data=object} "上传成功"
// @Failure 400 {object} models.JsonResponse "上传失败"
// @Router /file/upload [post]
func (f *FileHandler) UploadFile(c *gin.Context) {
	// 1. 解析请求参数
	req := new(request.FileUploadRequest)
	if err := c.ShouldBind(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}

	// 2. 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(200, models.NewJsonResponse(400, "获取上传文件失败", err.Error()))
		return
	}
	defer file.Close()

	// 3. 获取可选的视频缩略图
	thumbnail, thumbnailHeader, thumbnailErr := c.Request.FormFile("thumbnail")
	if thumbnailErr == nil {
		defer thumbnail.Close()
	} else if !errors.Is(thumbnailErr, http.ErrMissingFile) {
		logger.LOG.Warn("读取视频缩略图失败，继续上传原文件", "error", thumbnailErr)
		thumbnail = nil
		thumbnailHeader = nil
	}

	// 4. 调用 Service 处理上传
	userID := c.GetString("userID")
	result, err := f.service.UploadFile(req, file, header, thumbnail, thumbnailHeader, userID)
	if err != nil {
		c.JSON(200, models.NewJsonResponse(500, "上传失败", err.Error()))
		return
	}

	c.JSON(200, result)
}

// RetryUploadFinalize 重新提交失败的后台文件处理任务。
func (f *FileHandler) RetryUploadFinalize(c *gin.Context) {
	req := new(request.RetryUploadFinalizeRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.RetryUploadFinalize(req, c.GetString("userID"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(200, models.NewJsonResponse(404, "上传任务不存在", nil))
			return
		}
		c.JSON(200, models.NewJsonResponse(500, "重新处理失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// GetUploadProgress godoc
// @Summary 查询上传进度
// @Description 根据预检ID查询文件上传进度
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param precheck_id query string true "预检ID"
// @Success 200 {object} models.JsonResponse{data=response.UploadProgressResponse} "进度信息"
// @Failure 400 {object} models.JsonResponse "参数错误"
// @Failure 404 {object} models.JsonResponse "预检信息不存在"
// @Router /file/upload/progress [get]
func (f *FileHandler) GetUploadProgress(c *gin.Context) {
	req := new(request.UploadProgressRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}

	userID := c.GetString("userID")
	result, err := f.service.GetUploadProgress(req, userID)
	if err != nil {
		c.JSON(500, models.NewJsonResponse(500, "查询失败", err.Error()))
		return
	}

	c.JSON(200, result)
}

// GetUploadTaskList godoc
// @Summary 获取上传任务列表
// @Description 分页获取用户上传任务列表，不返回敏感信息（如临时目录路径）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param page query int true "页码" minimum(1)
// @Param pageSize query int true "每页数量" minimum(1) maximum(100)
// @Success 200 {object} models.JsonResponse{data=response.UploadTaskListResponse} "成功"
// @Failure 400 {object} models.JsonResponse "参数错误"
// @Failure 500 {object} models.JsonResponse "失败"
// @Router /file/upload/taskList [get]
func (f *FileHandler) GetUploadTaskList(c *gin.Context) {
	req := new(request.UploadTaskListRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.GetUploadTaskList(req, c.GetString("userID"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(200, models.NewJsonResponse(200, "获取上传任务列表成功", new([]string)))
			return
		}
		c.JSON(200, models.NewJsonResponse(500, "获取上传任务列表失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// PublicFileList 广场公开文件列表
// @Summary 获取广场公开文件列表
// @Description 获取广场公开文件列表
// @Tags 文件管理
// @Produce json
// @Param type query string false "文件类型"
// @Param tag_ids query string false "逗号分隔的标签ID"
// @Param tag_mode query string false "标签匹配模式，省略时按任一标签匹配" Enums(all,any) default(any)
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页数量" default(20)
// @Success 200 {object} models.JsonResponse{data=object} "成功"
// @Failure 500 {object} models.JsonResponse "失败"
// @Router /file/public/list [get]
func (f *FileHandler) PublicFileList(c *gin.Context) {
	req := new(request.PublicFileListRequest)
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(200, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}
	result, err := f.service.PublicFileList(req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidFileSearch) {
			c.JSON(200, models.NewJsonResponse(400, err.Error(), nil))
			return
		}
		c.JSON(200, models.NewJsonResponse(500, "获取文件列表失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// ListUncompletedUploads godoc
// @Summary 查询未完成的上传任务列表
// @Description 查询当前用户所有未完成的上传任务（用于断点续传）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=[]object} "未完成的上传任务列表"
// @Failure 500 {object} models.JsonResponse "查询失败"
// @Router /file/upload/uncompleted [get]
func (f *FileHandler) ListUncompletedUploads(c *gin.Context) {
	userID := c.GetString("userID")
	result, err := f.service.ListUncompletedUploads(userID)
	if err != nil {
		c.JSON(500, models.NewJsonResponse(500, "查询失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// DeleteUploadTask godoc
// @Summary 删除上传任务
// @Description 删除指定的上传任务（从数据库中删除记录）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.DeleteUploadTaskRequest true "删除请求"
// @Success 200 {object} models.JsonResponse "删除成功"
// @Failure 400 {object} models.JsonResponse "参数错误"
// @Failure 500 {object} models.JsonResponse "删除失败"
// @Router /file/upload/delete [post]
func (f *FileHandler) DeleteUploadTask(c *gin.Context) {
	req := new(request.DeleteUploadTaskRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}

	userID := c.GetString("userID")
	result, err := f.service.DeleteUploadTask(req.TaskID, userID)
	if err != nil {
		c.JSON(500, models.NewJsonResponse(500, "删除失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// CleanExpiredUploads godoc
// @Summary 清理过期的上传任务
// @Description 清理过期的未完成上传任务。如果提供 userID 参数，则只清理该用户的过期任务；如果不提供，则清理所有用户的过期任务（系统自动清理）
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param user_id query string false "用户ID（可选，不提供则清理所有用户的过期任务）"
// @Success 200 {object} models.JsonResponse{data=object} "清理结果"
// @Failure 500 {object} models.JsonResponse "清理失败"
// @Router /file/upload/clean-expired [post]
func (f *FileHandler) CleanExpiredUploads(c *gin.Context) {
	// 获取当前用户ID（用户清理自己的任务）
	userID := c.GetString("userID")

	// 如果提供了 user_id 查询参数，使用该参数（用于系统自动清理）
	if queryUserID := c.Query("user_id"); queryUserID != "" {
		userID = queryUserID
	}

	result, err := f.service.CleanExpiredUploads(userID)
	if err != nil {
		c.JSON(500, models.NewJsonResponse(500, "清理失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// ListExpiredUploads godoc
// @Summary 查询过期的上传任务列表
// @Description 查询当前用户所有过期的上传任务
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.JsonResponse{data=[]object} "过期的上传任务列表"
// @Failure 500 {object} models.JsonResponse "查询失败"
// @Router /file/upload/expired [get]
func (f *FileHandler) ListExpiredUploads(c *gin.Context) {
	userID := c.GetString("userID")
	result, err := f.service.ListExpiredUploads(userID)
	if err != nil {
		c.JSON(500, models.NewJsonResponse(500, "查询失败", err.Error()))
		return
	}
	c.JSON(200, result)
}

// RenewExpiredTask godoc
// @Summary 延期过期任务（恢复任务）
// @Description 延期过期的上传任务，延长过期时间使其可以继续上传
// @Tags 文件管理
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.RenewExpiredTaskRequest true "延期请求"
// @Success 200 {object} models.JsonResponse{data=object} "延期成功"
// @Failure 400 {object} models.JsonResponse "参数错误"
// @Failure 403 {object} models.JsonResponse "无权操作"
// @Failure 404 {object} models.JsonResponse "任务不存在"
// @Failure 500 {object} models.JsonResponse "延期失败"
// @Router /file/upload/renew [post]
func (f *FileHandler) RenewExpiredTask(c *gin.Context) {
	req := new(request.RenewExpiredTaskRequest)
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(400, models.NewJsonResponse(400, "参数错误", err.Error()))
		return
	}

	userID := c.GetString("userID")
	result, err := f.service.RenewExpiredTask(req.TaskID, userID, req.Days)
	if err != nil {
		c.JSON(500, models.NewJsonResponse(500, "延期失败", err.Error()))
		return
	}
	c.JSON(200, result)
}
