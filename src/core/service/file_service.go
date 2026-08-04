package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/cache"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"myobj/src/pkg/tagging"
	"myobj/src/pkg/upload"
	"myobj/src/pkg/virtualpath"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrInvalidFileSearch 表示搜索条件本身无效，处理器应返回 HTTP 400。
var ErrInvalidFileSearch = errors.New("文件搜索参数无效")

const maxFileTagFilterCount = 100

// 全局上传锁，用于防止同一文件的并发处理
var uploadLocks sync.Map          // key: userID+fileName, value: *sync.Mutex
var processingFiles sync.Map      // key: userID+fileName, value: bool (标记文件是否正在处理)
var thumbnailUpdateLocks sync.Map // key: fileInfoID, value: *sync.Mutex

// FileService 文件服务
type FileService struct {
	factory         *impl.RepositoryFactory
	cacheLocal      cache.Cache
	finalizeManager *UploadFinalizeManager
	taskEvents      *TaskEventHub
	tagService      *TagService
}

func (f *FileService) SetTagService(service *TagService) { f.tagService = service }
func (f *FileService) TagService() *TagService           { return f.tagService }

func (f *FileService) createUserFileWithTagState(ctx context.Context, userFile *models.UserFiles) error {
	err := f.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := f.factory.WithTx(tx).UserFiles().Create(ctx, userFile); err != nil {
			return err
		}
		return tagging.QueueUserFile(ctx, tx, userFile.UserID, userFile.UfID)
	})
	if err == nil && f.tagService != nil {
		f.tagService.Notify()
	}
	return err
}

func (f *FileService) rollbackUserFileWithTagState(ctx context.Context, userID, ufID string) error {
	return f.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deleteUserFileTagRecords(tx, userID, ufID); err != nil {
			return err
		}
		return tx.Unscoped().Where("user_id = ? AND uf_id = ?", userID, ufID).Delete(&models.UserFiles{}).Error
	})
}

func (f *FileService) SetTaskEventHub(events *TaskEventHub) {
	f.taskEvents = events
}

func (f *FileService) publishUploadTask(task *models.UploadTask, action string, coalesce bool) {
	if f.taskEvents != nil && task != nil {
		f.taskEvents.Publish(uploadTaskEvent(task, action), coalesce)
	}
}

func NewFileService(factory *impl.RepositoryFactory, cacheLocal cache.Cache) *FileService {
	service := &FileService{
		factory:    factory,
		cacheLocal: cacheLocal,
	}
	service.finalizeManager = newUploadFinalizeManager(service)
	return service
}

func (f *FileService) StartFinalizeManager() {
	f.finalizeManager.Start()
}
func (f *FileService) GetRepository() *impl.RepositoryFactory {
	return f.factory
}

// UpdateThumbnail 校验所有权后替换文件缩略图。
func (f *FileService) UpdateThumbnail(
	ctx context.Context,
	fileID string,
	userID string,
	thumbnail multipart.File,
	header *multipart.FileHeader,
) (*models.JsonResponse, error) {
	if thumbnail == nil || header == nil {
		return models.NewJsonResponse(400, "缩略图不能为空", nil), nil
	}

	userFile, err := f.factory.UserFiles().GetByUserIDAndUfID(ctx, userID, fileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "文件不存在或无权访问", nil), nil
		}
		return nil, fmt.Errorf("查询用户文件失败: %w", err)
	}

	fileInfo, err := f.factory.FileInfo().GetByID(ctx, userFile.FileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "文件不存在", nil), nil
		}
		return nil, fmt.Errorf("查询文件信息失败: %w", err)
	}
	if fileInfo.IsEnc {
		return models.NewJsonResponse(403, "加密文件不支持修改缩略图", nil), nil
	}

	targetPath, err := thumbnailTargetPath(fileInfo)
	if err != nil {
		return nil, err
	}

	lockValue, _ := thumbnailUpdateLocks.LoadOrStore(fileInfo.ID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	tempDir, err := os.MkdirTemp(filepath.Dir(targetPath), ".thumbnail-update-*")
	if err != nil {
		return nil, fmt.Errorf("创建缩略图临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	stagedPath, err := upload.SaveVideoThumbnail(thumbnail, header.Size, tempDir)
	if err != nil {
		return models.NewJsonResponse(400, "缩略图无效", err.Error()), nil
	}

	oldThumbnailPath := fileInfo.ThumbnailImg
	if err := replaceThumbnailAndUpdate(
		stagedPath,
		targetPath,
		func() error {
			return f.factory.FileInfo().UpdateThumbnailPath(ctx, fileInfo.ID, targetPath)
		},
	); err != nil {
		return nil, err
	}

	if oldThumbnailPath != "" && filepath.Clean(oldThumbnailPath) != filepath.Clean(targetPath) {
		if err := os.Remove(oldThumbnailPath); err != nil && !os.IsNotExist(err) {
			logger.LOG.Warn("清理旧缩略图失败", "path", oldThumbnailPath, "error", err)
		}
	}

	logger.LOG.Info("修改缩略图成功", "fileID", fileID, "fileInfoID", fileInfo.ID, "userID", userID)
	return models.NewJsonResponse(200, "修改缩略图成功", map[string]interface{}{
		"file_id":       fileID,
		"has_thumbnail": true,
	}), nil
}

func thumbnailTargetPath(fileInfo *models.FileInfo) (string, error) {
	if fileInfo.Path == "" {
		return "", fmt.Errorf("文件存储路径为空")
	}
	if fileInfo.RandomName == "" {
		return "", fmt.Errorf("文件随机存储名为空")
	}
	return filepath.Join(filepath.Dir(fileInfo.Path), fileInfo.RandomName+".jpg"), nil
}

// replaceThumbnailAndUpdate 先替换磁盘文件，再更新数据库；数据库失败时恢复旧文件。
func replaceThumbnailAndUpdate(stagedPath, targetPath string, updatePath func() error) error {
	backupPath := stagedPath + ".previous"
	hadTarget := false
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return fmt.Errorf("备份旧缩略图失败: %w", err)
		}
		hadTarget = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查旧缩略图失败: %w", err)
	}

	if err := os.Rename(stagedPath, targetPath); err != nil {
		if hadTarget {
			_ = os.Rename(backupPath, targetPath)
		}
		return fmt.Errorf("保存新缩略图失败: %w", err)
	}

	if err := updatePath(); err != nil {
		removeErr := os.Remove(targetPath)
		var restoreErr error
		if hadTarget {
			restoreErr = os.Rename(backupPath, targetPath)
		}
		if removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("更新缩略图路径失败: %w；删除新缩略图失败: %v", err, removeErr)
		}
		if restoreErr != nil {
			return fmt.Errorf("更新缩略图路径失败: %w；恢复旧缩略图失败: %v", err, restoreErr)
		}
		return fmt.Errorf("更新缩略图路径失败: %w", err)
	}

	return nil
}

// Precheck 文件预检查
func (f *FileService) Precheck(req *request.UploadPrecheckRequest, c cache.Cache) (*models.JsonResponse, error) {
	ctx := context.Background()
	user, err := f.factory.User().GetByID(ctx, req.UserID)
	if err != nil {
		logger.LOG.Error("获取用户信息失败", "error", err, "userID", req.UserID)
		return nil, err
	}
	directory, err := f.factory.Directory().GetByID(ctx, req.DirectoryID)
	if err != nil || directory.UserID != req.UserID {
		return models.NewJsonResponse(400, "目录不存在或无权访问", nil), nil
	}
	// 检查用户可用空间 如果不是无限空间，且可用空间不足
	if user.Space > 0 && user.FreeSpace < req.FileSize {
		return models.NewJsonResponse(400, "用户可用空间不足", nil), nil
	}
	signature, err := f.factory.FileInfo().GetByChunkSignature(ctx, req.ChunkSignature, req.FileSize)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.LOG.Error("查询文件签名失败", "error", err, "chunkSignature", req.ChunkSignature)
		return nil, err
	}
	if len(req.FilesMd5) >= 3 {
		if signature.FirstChunkHash == req.FilesMd5[0] && signature.SecondChunkHash == req.FilesMd5[1] && signature.ThirdChunkHash == req.FilesMd5[2] && signature.IsEnc == false {
			userFile := &models.UserFiles{
				UserID:      user.ID,
				FileID:      signature.ID,
				FileName:    req.FileName,
				DirectoryID: req.DirectoryID,
				IsPublic:    false,
				CreatedAt:   custom_type.Now(),
				UfID:        uuid.NewString(),
			}
			err := f.createUserFileWithTagState(ctx, userFile)
			if err != nil {
				logger.LOG.Error("创建用户文件失败", "error", err, "userID", req.UserID, "fileID", signature.ID, "fileName", req.FileName)
				return nil, err
			}
			// 秒传成功后扣除用户空间（只对非无限空间用户）
			if user.Space > 0 {
				user.FreeSpace -= req.FileSize
				if err := f.factory.User().Update(ctx, user); err != nil {
					logger.LOG.Error("更新用户空间失败", "error", err, "userID", user.ID)
					// 回滚：删除刚创建的用户文件关联
					if delErr := f.rollbackUserFileWithTagState(ctx, user.ID, userFile.UfID); delErr != nil {
						logger.LOG.Error("回滚删除用户文件失败", "error", delErr)
					}
					return nil, err
				}
				logger.LOG.Debug("秒传扣除用户空间",
					"user_id", user.ID,
					"file_size", req.FileSize,
					"new_free_space", user.FreeSpace)
			}
			return models.NewJsonResponse(200, "秒传成功", nil), nil
		}
	} else {
		if signature.FileHash == req.FilesMd5[0] && signature.IsEnc == false {
			userFile := &models.UserFiles{
				UserID:      user.ID,
				FileID:      signature.ID,
				FileName:    req.FileName,
				DirectoryID: req.DirectoryID,
				IsPublic:    false,
				CreatedAt:   custom_type.Now(),
				UfID:        uuid.NewString(),
			}
			err := f.createUserFileWithTagState(ctx, userFile)
			if err != nil {
				logger.LOG.Error("创建用户文件失败", "error", err, "userID", req.UserID, "fileID", signature.ID, "fileName", req.FileName)
				return nil, err
			}
			// 秒传成功后扣除用户空间（只对非无限空间用户）
			if user.Space > 0 {
				user.FreeSpace -= req.FileSize
				if err := f.factory.User().Update(ctx, user); err != nil {
					logger.LOG.Error("更新用户空间失败", "error", err, "userID", user.ID)
					// 回滚：删除刚创建的用户文件关联
					if delErr := f.rollbackUserFileWithTagState(ctx, user.ID, userFile.UfID); delErr != nil {
						logger.LOG.Error("回滚删除用户文件失败", "error", delErr)
					}
					return nil, err
				}
				logger.LOG.Debug("秒传扣除用户空间",
					"user_id", user.ID,
					"file_size", req.FileSize,
					"new_free_space", user.FreeSpace)
			}
			return models.NewJsonResponse(200, "秒传成功", nil), nil
		}
	}
	bestDisk, err := upload.SelectBestDisk(ctx, f.factory, req.FileSize)
	if err != nil {
		return nil, err
	}
	uid := uuid.New().String()
	//无法触发秒传，但可上传，返回校验ID
	key := fmt.Sprintf("fileUpload:%s", uid)
	res := new(response.FilePrecheckResponse)
	res.PrecheckID = uid
	res.DiskID = bestDisk.ID
	chunks, err := f.factory.UploadChunk().GetByUserIDAndFileName(ctx, user.ID, req.FileName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.LOG.Error("获取文件分片失败", "error", err, "chunkSignature", req.ChunkSignature)
		return nil, err
	}
	// chunks 是数组，需要遍历
	for _, chunk := range chunks {
		res.Md5 = append(res.Md5, chunk.Md5)
	}

	// 计算分片大小和总分片数（默认5MB）
	chunkSize := int64(5 * 1024 * 1024)                            // 5MB
	totalChunks := int((req.FileSize + chunkSize - 1) / chunkSize) // 向上取整

	// 创建或更新上传任务记录（用于持久化和断点续传）
	uploadTask := &models.UploadTask{
		ID:             uid, // 使用 precheck_id 作为主键
		UserID:         user.ID,
		FileName:       req.FileName,
		FileSize:       req.FileSize,
		ChunkSize:      chunkSize,
		TotalChunks:    totalChunks,
		UploadedChunks: len(chunks), // 已上传的分片数
		ChunkSignature: req.ChunkSignature,
		DirectoryID:    req.DirectoryID,
		Status:         "pending",
		CreateTime:     custom_type.Now(),
		UpdateTime:     custom_type.Now(),
		ExpireTime:     custom_type.JsonTime(time.Now().Add(7 * 24 * time.Hour)), // 7天后过期
	}

	// 尝试获取已存在的任务（如果存在则更新，否则创建）
	existingTask, err := f.factory.UploadTask().GetByID(ctx, uid)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.LOG.Warn("查询上传任务失败", "error", err, "precheckID", uid)
		// 不阻塞主流程，继续执行
	} else if existingTask != nil {
		// 更新已存在的任务
		uploadTask.UploadedChunks = existingTask.UploadedChunks // 保留已上传的分片数
		if err := f.factory.UploadTask().Update(ctx, uploadTask); err != nil {
			logger.LOG.Warn("更新上传任务失败", "error", err, "precheckID", uid)
			// 不阻塞主流程，继续执行
		} else {
			f.publishUploadTask(uploadTask, "updated", false)
		}
	} else {
		// 创建新任务
		if err := f.factory.UploadTask().Create(ctx, uploadTask); err != nil {
			logger.LOG.Warn("创建上传任务失败", "error", err, "precheckID", uid)
			// 不阻塞主流程，继续执行
		} else {
			f.publishUploadTask(uploadTask, "created", false)
		}
	}

	// 存储预检请求信息到缓存（用于后续查询进度）
	reqCacheKey := fmt.Sprintf("fileUploadReq:%s", uid)
	reqJSON, err := json.Marshal(req)
	if err != nil {
		logger.LOG.Error("序列化预检请求失败", "error", err)
		return nil, err
	}
	// 存储预检请求信息到缓存（24小时过期，86400秒）
	if err := f.cacheLocal.Set(reqCacheKey, string(reqJSON), 86400); err != nil {
		logger.LOG.Warn("存储预检请求到缓存失败", "error", err)
		// 不阻塞主流程，继续执行
	}
	// 序列化为JSON字符串存储到Redis
	resJSON, err := json.Marshal(res)
	if err != nil {
		logger.LOG.Error("序列化预检响应失败", "error", err)
		return nil, err
	}
	err = c.Set(key, string(resJSON), 12*60*60) // 12小时内可用的校验
	if err != nil {
		logger.LOG.Error("缓存设置失败", "error", err, "key", key)
		return nil, err
	}
	//// 保存原始请求数据到缓存，供上传时使用
	//reqKey := fmt.Sprintf("fileUploadReq:%s", uid)
	//reqJSON, err = json.Marshal(req)
	//if err != nil {
	//	logger.LOG.Error("序列化预检请求失败", "error", err)
	//	return nil, err
	//}
	//if err := c.Set(reqKey, string(reqJSON), 12*60*60); err != nil {
	//	logger.LOG.Error("保存上传请求失败", "error", err, "key", reqKey)
	//	return nil, err
	//}
	return models.NewJsonResponse(201, "预检通过", uid), nil
}

// SearchUserFiles 搜索当前用户的文件
func (f *FileService) SearchUserFiles(req *request.FileSearchRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	page, pageSize := normalizeFilePage(req.Page, req.PageSize)
	tagIDs, tagMode, err := normalizeTagFilter(req.TagIDs, req.TagMode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Keyword) == "" && len(tagIDs) == 0 {
		return nil, fmt.Errorf("%w: 关键词或标签筛选至少填写一项", ErrInvalidFileSearch)
	}
	terms, err := f.searchTerms(ctx, userID, req.Keyword)
	if err != nil {
		return nil, err
	}
	sortBy, sortOrder := normalizeFileSort(req.SortBy, req.SortOrder)
	query := repository.UserFileQuery{
		UserID: userID, SearchTerms: terms, TagIDs: tagIDs, TagMode: tagMode,
		FileType: req.Type, SortBy: sortBy, SortOrder: sortOrder,
		Offset: (page - 1) * pageSize, Limit: pageSize,
	}
	if req.DirectoryID > 0 {
		query.DirectoryID = &req.DirectoryID
	}
	userFiles, err := f.factory.UserFiles().ListFiltered(ctx, query)
	if err != nil {
		logger.LOG.Error("搜索用户文件失败", "error", err, "userID", userID, "keyword", req.Keyword)
		return nil, err
	}
	query.Offset, query.Limit = 0, 0
	total, err := f.factory.UserFiles().CountFiltered(ctx, query)
	if err != nil {
		logger.LOG.Error("统计用户文件数量失败", "error", err, "userID", userID, "keyword", req.Keyword)
		return nil, err
	}
	items, err := f.buildSearchFileItems(ctx, userFiles, false)
	if err != nil {
		return nil, err
	}
	return models.NewJsonResponse(200, "搜索成功", response.FileSearchResponse{
		Files: items, Total: total, Page: page, PageSize: pageSize,
	}), nil
}

// SearchPublicFiles 搜索公开文件（广场）
func (f *FileService) SearchPublicFiles(req *request.FileSearchRequest) (*models.JsonResponse, error) {
	ctx := context.Background()
	page, pageSize := normalizeFilePage(req.Page, req.PageSize)
	tagIDs, tagMode, err := normalizeTagFilter(req.TagIDs, req.TagMode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Keyword) == "" && len(tagIDs) == 0 {
		return nil, fmt.Errorf("%w: 关键词或标签筛选至少填写一项", ErrInvalidFileSearch)
	}
	terms, err := f.searchTerms(ctx, "", req.Keyword)
	if err != nil {
		return nil, err
	}
	sortBy, sortOrder := normalizeFileSort(req.SortBy, req.SortOrder)
	query := repository.UserFileQuery{
		PublicOnly: true, SearchTerms: terms, TagIDs: tagIDs, TagMode: tagMode,
		FileType: req.Type, SortBy: sortBy, SortOrder: sortOrder,
		Offset: (page - 1) * pageSize, Limit: pageSize,
	}
	userFiles, err := f.factory.UserFiles().ListFiltered(ctx, query)
	if err != nil {
		return nil, err
	}
	query.Offset, query.Limit = 0, 0
	total, err := f.factory.UserFiles().CountFiltered(ctx, query)
	if err != nil {
		return nil, err
	}
	items, err := f.buildSearchFileItems(ctx, userFiles, true)
	if err != nil {
		return nil, err
	}
	return models.NewJsonResponse(200, "搜索成功", response.FileSearchResponse{
		Files: items, Total: total, Page: page, PageSize: pageSize,
	}), nil
}

// GetFileList 获取文件列表（我的文件页面）
func (f *FileService) GetFileList(req *request.FileListRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	sortBy, sortOrder := normalizeFileSort(req.SortBy, req.SortOrder)
	tagIDs, tagMode, err := normalizeTagFilter(req.TagIDs, req.TagMode)
	if err != nil {
		return nil, err
	}

	// 处理虚拟路径ID，空或为0时使用根目录
	var currentDirectoryID int
	var currentDirectory *models.VirtualDirectory

	if req.DirectoryID == 0 {
		// 查询用户根目录
		currentDirectory, err = f.factory.Directory().GetRoot(ctx, userID)
		if err != nil {
			logger.LOG.Error("获取根目录失败", "error", err, "userID", userID)
			return nil, fmt.Errorf("获取根目录失败: %w", err)
		}
		currentDirectoryID = currentDirectory.ID
	} else {
		currentDirectoryID = req.DirectoryID
		currentDirectory, err = f.factory.Directory().GetByID(ctx, currentDirectoryID)
		if err != nil {
			logger.LOG.Error("查询目录信息失败", "error", err, "directory_id", currentDirectoryID)
			return nil, fmt.Errorf("路径不存在: %w", err)
		}
		if currentDirectory.UserID != userID {
			return nil, errors.New("无权访问该路径")
		}
	}

	// 使用标签或文件类型筛选时只展示当前目录内的匹配文件，避免未匹配目录占用分页位置。
	filteringFiles := len(tagIDs) > 0 || (req.Type != "" && req.Type != "all")
	var folderCount int64
	if !filteringFiles {
		folderCount, err = f.factory.Directory().CountSubFoldersByParentID(ctx, userID, currentDirectoryID)
		if err != nil {
			logger.LOG.Error("统计子目录数量失败", "error", err, "userID", userID, "directory_id", currentDirectoryID)
			return nil, err
		}
	}
	fileQuery := repository.UserFileQuery{
		UserID: userID, DirectoryID: &currentDirectoryID, TagIDs: tagIDs, TagMode: tagMode,
		FileType: req.Type, SortBy: sortBy, SortOrder: sortOrder,
	}
	fileCount, err := f.factory.UserFiles().CountFiltered(ctx, fileQuery)
	if err != nil {
		logger.LOG.Error("统计文件数量失败", "error", err, "userID", userID, "directory_id", currentDirectoryID)
		return nil, err
	}
	totalCount := folderCount + fileCount

	// 计算分页偏移量
	offset := (req.Page - 1) * req.PageSize

	// 优先返回文件夹
	var folders []*models.VirtualDirectory
	var userFiles []*models.UserFiles

	if offset < int(folderCount) {
		// 当前页包含文件夹
		folderLimit := req.PageSize
		if offset+req.PageSize > int(folderCount) {
			folderLimit = int(folderCount) - offset
		}

		folderSortBy, folderSortOrder := sortBy, sortOrder
		if sortBy == "size" {
			folderSortBy, folderSortOrder = "name", "asc"
		}
		folders, err = f.factory.Directory().ListChildrenSorted(ctx, userID, currentDirectoryID, folderSortBy, folderSortOrder, offset, folderLimit)
		if err != nil {
			logger.LOG.Error("查询子目录列表失败", "error", err, "userID", userID, "directory_id", currentDirectoryID)
			return nil, err
		}

		// 如果还有剩余空间，查询文件（直接从user_files表查询，避免file_id重复问题）
		remaining := req.PageSize - len(folders)
		if remaining > 0 {
			fileQuery.Offset, fileQuery.Limit = 0, remaining
			userFiles, err = f.factory.UserFiles().ListFiltered(ctx, fileQuery)
			if err != nil {
				logger.LOG.Error("查询文件列表失败", "error", err, "userID", userID, "directory_id", currentDirectoryID)
				return nil, err
			}
		}
	} else {
		// 当前页只包含文件（直接从user_files表查询，避免file_id重复问题）
		fileOffset := offset - int(folderCount)
		fileQuery.Offset, fileQuery.Limit = fileOffset, req.PageSize
		userFiles, err = f.factory.UserFiles().ListFiltered(ctx, fileQuery)
		if err != nil {
			logger.LOG.Error("查询文件列表失败", "error", err, "userID", userID, "directory_id", currentDirectoryID)
			return nil, err
		}
	}

	// 获取面包屑导航（只展示当前、上级、上上级）
	breadcrumbs, err := f.buildBreadcrumbs(ctx, currentDirectory)
	if err != nil {
		logger.LOG.Error("构建面包屑导航失败", "error", err, "directory_id", currentDirectory.ID)
		return nil, err
	}

	// 构造响应
	resp := &response.FileListResponse{
		Breadcrumbs:        breadcrumbs,
		CurrentDirectoryID: currentDirectoryID,
		Folders:            make([]*response.FolderItem, 0, len(folders)),
		Files:              make([]*response.FileItem, 0, len(userFiles)),
		Total:              totalCount,
		Page:               req.Page,
		PageSize:           req.PageSize,
	}

	// 转换文件夹数据
	folderIDs := make([]int, 0, len(folders))
	for _, folder := range folders {
		folderIDs = append(folderIDs, folder.ID)
	}
	folderTags := make(map[int][]response.CompactTagView)
	if f.tagService != nil {
		folderTags, err = f.tagService.CompactDirectoryTags(ctx, userID, folderIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, folder := range folders {
		absolutePath, pathErr := virtualpath.ResolveAbsolutePath(ctx, userID, folder.ID, f.factory)
		if pathErr != nil {
			return nil, pathErr
		}
		tags := folderTags[folder.ID]
		cinemaMode := false
		for _, tag := range tags {
			if tag.SystemCode == models.TagSystemCodeCinemaMode {
				cinemaMode = true
				break
			}
		}
		resp.Folders = append(resp.Folders, &response.FolderItem{
			ID: folder.ID, Name: folder.Name, ParentID: folder.ParentID,
			AbsolutePath: absolutePath, CreatedAt: folder.CreatedAt, Tags: tags, CinemaMode: cinemaMode,
		})
	}

	fileIDs := make([]string, 0, len(userFiles))
	ufIDs := make([]string, 0, len(userFiles))
	for _, userFile := range userFiles {
		fileIDs = append(fileIDs, userFile.FileID)
		ufIDs = append(ufIDs, userFile.UfID)
	}
	var fileInfos []models.FileInfo
	if len(fileIDs) > 0 {
		if err := f.factory.DB().WithContext(ctx).Where("id IN ?", fileIDs).Find(&fileInfos).Error; err != nil {
			return nil, err
		}
	}
	fileInfoMap := make(map[string]models.FileInfo, len(fileInfos))
	for _, fileInfo := range fileInfos {
		fileInfoMap[fileInfo.ID] = fileInfo
	}
	tagMap := make(map[string][]response.CompactTagView)
	stateMap := make(map[string]string)
	if f.tagService != nil {
		tagMap, err = f.tagService.CompactTags(ctx, userID, ufIDs, false)
		if err != nil {
			return nil, err
		}
		stateMap, err = f.tagService.TagStates(ctx, ufIDs)
		if err != nil {
			return nil, err
		}
	}
	// 转换文件数据（直接使用user_files记录，避免file_id重复导致查询错误）
	for _, uf := range userFiles {
		fileInfo, exists := fileInfoMap[uf.FileID]
		if !exists {
			logger.LOG.Warn("获取文件信息失败", "fileID", uf.FileID, "ufID", uf.UfID)
			continue
		}

		resp.Files = append(resp.Files, &response.FileItem{
			FileID:       uf.UfID,
			FileName:     uf.FileName,
			FileSize:     fileInfo.Size,
			MimeType:     fileInfo.Mime,
			IsEnc:        fileInfo.IsEnc,
			HasThumbnail: fileInfo.ThumbnailImg != "",
			Public:       uf.IsPublic,
			CreatedAt:    fileInfo.CreatedAt,
			Tags:         tagMap[uf.UfID],
			TagState:     stateMap[uf.UfID],
		})
	}

	return models.NewJsonResponse(200, "获取成功", resp), nil
}

func normalizeFileSort(sortBy, sortOrder string) (string, string) {
	switch sortBy {
	case "name", "size", "time":
	default:
		sortBy = "time"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}
	return sortBy, sortOrder
}

func normalizeFilePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func normalizeTagFilter(raw, mode string) ([]string, string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "any"
	}
	if mode != "all" && mode != "any" {
		return nil, "", fmt.Errorf("%w: tag_mode 仅支持 all 或 any", ErrInvalidFileSearch)
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, tagID := range strings.Split(raw, ",") {
		tagID = strings.TrimSpace(tagID)
		if tagID == "" {
			continue
		}
		if _, exists := seen[tagID]; exists {
			continue
		}
		seen[tagID] = struct{}{}
		result = append(result, tagID)
		if len(result) > maxFileTagFilterCount {
			return nil, "", fmt.Errorf("%w: 标签筛选最多允许%d项", ErrInvalidFileSearch, maxFileTagFilterCount)
		}
	}
	return result, mode, nil
}

func (f *FileService) searchTerms(ctx context.Context, userID, keyword string) ([]string, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, nil
	}
	if f.tagService == nil {
		return []string{strings.TrimSpace(keyword)}, nil
	}
	return f.tagService.SearchTerms(ctx, userID, keyword)
}

func (f *FileService) buildSearchFileItems(ctx context.Context, userFiles []*models.UserFiles, publicOnly bool) ([]response.SearchFileItem, error) {
	if len(userFiles) == 0 {
		return []response.SearchFileItem{}, nil
	}
	fileIDs := make([]string, 0, len(userFiles))
	ufIDs := make([]string, 0, len(userFiles))
	userIDs := make([]string, 0, len(userFiles))
	for _, userFile := range userFiles {
		fileIDs = append(fileIDs, userFile.FileID)
		ufIDs = append(ufIDs, userFile.UfID)
		userIDs = append(userIDs, userFile.UserID)
	}
	var fileInfos []models.FileInfo
	if err := f.factory.DB().WithContext(ctx).Where("id IN ?", fileIDs).Find(&fileInfos).Error; err != nil {
		return nil, err
	}
	fileMap := make(map[string]models.FileInfo, len(fileInfos))
	for _, fileInfo := range fileInfos {
		fileMap[fileInfo.ID] = fileInfo
	}
	ownerMap := make(map[string]string)
	if publicOnly {
		var users []models.UserInfo
		if err := f.factory.DB().WithContext(ctx).Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			ownerMap[user.ID] = user.Name
		}
	}
	tags := make(map[string][]response.CompactTagView)
	states := make(map[string]string)
	if f.tagService != nil {
		var err error
		tags, err = f.tagService.CompactTags(ctx, "", ufIDs, publicOnly)
		if err != nil {
			return nil, err
		}
		states, err = f.tagService.TagStates(ctx, ufIDs)
		if err != nil {
			return nil, err
		}
	}
	items := make([]response.SearchFileItem, 0, len(userFiles))
	for _, userFile := range userFiles {
		fileInfo, exists := fileMap[userFile.FileID]
		if !exists {
			continue
		}
		items = append(items, response.SearchFileItem{
			ID: fileInfo.ID, Name: fileInfo.Name, Size: fileInfo.Size, Mime: fileInfo.Mime,
			ThumbnailImg: fileInfo.ThumbnailImg, CreatedAt: fileInfo.CreatedAt, UpdatedAt: fileInfo.UpdatedAt,
			IsEnc: fileInfo.IsEnc, UfID: userFile.UfID, FileName: userFile.FileName,
			Public: userFile.IsPublic, OwnerName: ownerMap[userFile.UserID], Tags: tags[userFile.UfID],
			TagState: states[userFile.UfID],
		})
	}
	return items, nil
}

// buildBreadcrumbs 构建面包屑导航（只展示当前、上级、上上级）
func (f *FileService) buildBreadcrumbs(ctx context.Context, currentPath *models.VirtualDirectory) ([]response.Breadcrumb, error) {
	breadcrumbs := []response.Breadcrumb{}

	// 添加当前目录
	currentAbsolutePath, err := virtualpath.ResolveAbsolutePath(ctx, currentPath.UserID, currentPath.ID, f.factory)
	if err != nil {
		return nil, err
	}
	breadcrumbs = append(breadcrumbs, response.Breadcrumb{
		ID: currentPath.ID, Name: currentPath.Name, AbsolutePath: currentAbsolutePath,
	})

	// 获取上级目录（如果存在）
	if currentPath.ParentID > 0 {
		parentID := currentPath.ParentID
		if parentID > 0 {
			parent, err := f.factory.Directory().GetByID(ctx, parentID)
			if err == nil {
				parentAbsolutePath, pathErr := virtualpath.ResolveAbsolutePath(ctx, currentPath.UserID, parent.ID, f.factory)
				if pathErr != nil {
					return nil, pathErr
				}
				// 在开头插入上级目录
				breadcrumbs = append([]response.Breadcrumb{{
					ID: parent.ID, Name: parent.Name, AbsolutePath: parentAbsolutePath,
				}}, breadcrumbs...)

				// 获取上上级目录（如果存在）
				if parent.ParentID > 0 {
					grandParentID := parent.ParentID
					if grandParentID > 0 {
						grandParent, err := f.factory.Directory().GetByID(ctx, grandParentID)
						if err == nil {
							grandParentAbsolutePath, pathErr := virtualpath.ResolveAbsolutePath(ctx, currentPath.UserID, grandParent.ID, f.factory)
							if pathErr != nil {
								return nil, pathErr
							}
							// 在开头插入上上级目录
							breadcrumbs = append([]response.Breadcrumb{{
								ID: grandParent.ID, Name: grandParent.Name, AbsolutePath: grandParentAbsolutePath,
							}}, breadcrumbs...)
						}
					}
				}
			}
		}
	}

	return breadcrumbs, nil
}

// MakeDir 创建目录
func (f *FileService) MakeDir(req *request.MakeDirRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	parent, err := f.factory.Directory().GetByID(ctx, req.ParentID)
	if err != nil || parent.UserID != userID {
		return models.NewJsonResponse(400, "父目录不存在或无权访问", nil), nil
	}
	name, err := virtualpath.NormalizeDirectoryName(req.Name)
	if err != nil {
		return models.NewJsonResponse(400, err.Error(), nil), nil
	}
	if _, err := f.factory.Directory().GetChild(ctx, userID, parent.ID, name); err == nil {
		return models.NewJsonResponse(400, "目录已存在", nil), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := custom_type.Now()
	directory := &models.VirtualDirectory{UserID: userID, Name: name, ParentID: parent.ID, CreatedAt: now, UpdatedAt: now}
	err = f.factory.Directory().Create(ctx, directory)
	if err != nil {
		logger.LOG.Error("创建目录失败", "error", err)
		return nil, err
	}
	absolutePath, err := virtualpath.ResolveAbsolutePath(ctx, userID, directory.ID, f.factory)
	if err != nil {
		return nil, err
	}
	return models.NewJsonResponse(200, "创建目录成功", response.DirectoryItem{ID: directory.ID, Name: directory.Name, ParentID: directory.ParentID, AbsolutePath: absolutePath, CreatedAt: directory.CreatedAt, UpdatedAt: directory.UpdatedAt}), nil
}

// MoveFile 移动文件
func (f *FileService) MoveFile(req *request.MoveFileRequest, userID string) (*models.JsonResponse, error) {
	return f.MoveItems(&request.MoveItemsRequest{
		FileIDs: []string{req.FileID}, TargetDirectoryID: req.TargetDirectoryID,
	}, userID)
}

// GetDirectories 获取用户虚拟目录树。
func (f *FileService) GetDirectories(userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	directories, err := f.factory.Directory().ListByUserID(ctx, userID, 0, -1)
	if err != nil {
		logger.LOG.Error("获取虚拟目录失败", "error", err)
		return nil, err
	}
	result := make([]response.DirectoryItem, 0, len(directories))
	for _, directory := range directories {
		absolutePath, pathErr := virtualpath.ResolveAbsolutePath(ctx, userID, directory.ID, f.factory)
		if pathErr != nil {
			return nil, pathErr
		}
		result = append(result, response.DirectoryItem{ID: directory.ID, Name: directory.Name, ParentID: directory.ParentID, AbsolutePath: absolutePath, CreatedAt: directory.CreatedAt, UpdatedAt: directory.UpdatedAt})
	}
	return models.NewJsonResponse(200, "获取虚拟目录成功", result), nil
}

// RenameFile 重命名文件
func (f *FileService) RenameFile(req *request.RenameFileRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 验证用户是否拥有该文件
	userFile, err := f.factory.UserFiles().GetByUserIDAndUfID(ctx, userID, req.FileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "文件不存在或无权访问", nil), nil
		}
		logger.LOG.Error("获取文件失败", "error", err, "fileID", req.FileID)
		return nil, err
	}

	// 2. 验证新文件名不能为空
	if strings.TrimSpace(req.NewFileName) == "" {
		return models.NewJsonResponse(400, "新文件名不能为空", nil), nil
	}

	// 3. 检查同一目录下是否已存在同名文件
	existingFiles, err := f.factory.UserFiles().ListByUserID(ctx, userID, 0, 10000)
	if err != nil {
		logger.LOG.Error("查询文件列表失败", "error", err)
		return nil, err
	}

	// 检查同一虚拟路径下是否有同名文件
	for _, file := range existingFiles {
		if file.DirectoryID == userFile.DirectoryID &&
			file.FileName == req.NewFileName &&
			file.UfID != req.FileID {
			return models.NewJsonResponse(400, "该目录下已存在同名文件", nil), nil
		}
	}

	// 4. 保存旧文件名用于日志
	oldFileName := userFile.FileName

	// 5. 更新文件名
	userFile.FileName = req.NewFileName
	err = f.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := f.factory.WithTx(tx).UserFiles().Update(ctx, userFile); err != nil {
			return err
		}
		if f.tagService != nil {
			return f.tagService.QueueUserFile(ctx, tx, userID, userFile.UfID)
		}
		return nil
	})
	if err != nil {
		logger.LOG.Error("重命名文件失败", "error", err, "fileID", req.FileID, "newFileName", req.NewFileName)
		return nil, fmt.Errorf("重命名文件失败: %w", err)
	}

	logger.LOG.Info("文件重命名成功", "fileID", req.FileID, "oldFileName", oldFileName, "newFileName", req.NewFileName)
	return models.NewJsonResponse(200, "文件重命名成功", map[string]interface{}{
		"file_id":   req.FileID,
		"file_name": req.NewFileName,
	}), nil
}

// RenameDir 重命名目录
func (f *FileService) RenameDir(req *request.RenameDirRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 获取目录信息
	directory, err := f.factory.Directory().GetByID(ctx, req.DirID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "目录不存在", nil), nil
		}
		logger.LOG.Error("获取目录失败", "error", err, "dirID", req.DirID)
		return nil, err
	}

	// 2. 验证目录是否属于当前用户
	if directory.UserID != userID {
		return models.NewJsonResponse(403, "无权访问该目录", nil), nil
	}

	// 2.1 检查是否是根目录
	rootDirectory, err := f.factory.Directory().GetRoot(ctx, userID)
	if err != nil {
		logger.LOG.Error("获取根目录失败", "error", err)
		return nil, err
	}

	isRootDir := rootDirectory.ID == req.DirID
	if isRootDir {
		// 根目录通常不应该被重命名，这里返回错误
		return models.NewJsonResponse(400, "根目录不能重命名", nil), nil
	}

	// 3. 验证新目录名不能为空
	newDirName, err := virtualpath.NormalizeDirectoryName(req.NewDirName)
	if err != nil {
		return models.NewJsonResponse(400, err.Error(), nil), nil
	}
	parentID := directory.ParentID

	// 查询同一父目录下的所有子目录
	subFolders, err := f.factory.Directory().ListChildren(ctx, userID, parentID, 0, 1000)
	if err != nil {
		logger.LOG.Error("查询子目录列表失败", "error", err)
		return nil, err
	}

	// 检查是否有同名目录（排除当前目录）
	for _, folder := range subFolders {
		if folder.Name == newDirName && folder.ID != req.DirID {
			return models.NewJsonResponse(400, "该目录下已存在同名目录", nil), nil
		}
	}

	// 6. 更新目录名称；完整路径由父子关系动态解析。
	oldName := directory.Name
	directory.Name = newDirName
	directory.UpdatedAt = custom_type.Now()

	err = f.factory.Directory().Update(ctx, directory)
	if err != nil {
		logger.LOG.Error("重命名目录失败", "error", err, "dirID", req.DirID, "newDirName", req.NewDirName)
		return nil, fmt.Errorf("重命名目录失败: %w", err)
	}

	logger.LOG.Info("目录重命名成功", "directory_id", req.DirID, "old_name", oldName, "new_name", newDirName)
	return models.NewJsonResponse(200, "目录重命名成功", map[string]interface{}{
		"directory_id": req.DirID,
		"name":         newDirName,
	}), nil
}

// SetFilePublic 设置文件公开状态
func (f *FileService) SetFilePublic(req *request.SetFilePublicRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 验证用户是否拥有该文件
	userFile, err := f.factory.UserFiles().GetByUserIDAndUfID(ctx, userID, req.FileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "文件不存在或无权访问", nil), nil
		}
		logger.LOG.Error("获取文件失败", "error", err, "fileID", req.FileID)
		return nil, err
	}

	// 2. 如果要设置为公开，检查文件是否加密
	if req.Public {
		// 获取文件信息
		fileInfo, err := f.factory.FileInfo().GetByID(ctx, userFile.FileID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return models.NewJsonResponse(404, "文件信息不存在", nil), nil
			}
			logger.LOG.Error("获取文件信息失败", "error", err, "fileID", userFile.FileID)
			return nil, err
		}

		// 如果文件是加密的，不允许设置为公开
		if fileInfo.IsEnc {
			return models.NewJsonResponse(400, "加密文件不能设置为公开", nil), nil
		}
	}

	// 3. 更新文件公开状态
	userFile.IsPublic = req.Public
	err = f.factory.UserFiles().Update(ctx, userFile)
	if err != nil {
		logger.LOG.Error("设置文件公开状态失败", "error", err, "fileID", req.FileID, "public", req.Public)
		return nil, fmt.Errorf("设置文件公开状态失败: %w", err)
	}

	logger.LOG.Info("文件公开状态已更新", "fileID", req.FileID, "public", req.Public)
	return models.NewJsonResponse(200, "文件公开状态已更新", map[string]interface{}{
		"file_id": req.FileID,
		"public":  req.Public,
	}), nil
}

// DeleteFiles 删除文件（移动到回收站）
func (f *FileService) DeleteFiles(req *request.DeleteFileRequest, userID string) (*models.JsonResponse, error) {
	return f.DeleteItems(&request.DeleteItemsRequest{FileIDs: req.FileIDs}, userID)
}

// UploadFile 文件上传处理
func (f *FileService) UploadFile(req *request.FileUploadRequest, file multipart.File, header *multipart.FileHeader, thumbnail multipart.File, thumbnailHeader *multipart.FileHeader, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 从缓存获取预检信息
	cacheKey := fmt.Sprintf("fileUpload:%s", req.PrecheckID)
	precheckData, err := f.cacheLocal.Get(cacheKey)
	if err != nil {
		logger.LOG.Error("获取预检信息失败", "error", err, "precheckID", req.PrecheckID)
		return nil, fmt.Errorf("预检信息已过期或不存在")
	}

	// 2. 反序列化预检响应数据
	var precheckResp response.FilePrecheckResponse
	// 统一处理：Redis和LocalCache都可能返回string或对象
	switch v := precheckData.(type) {
	case *response.FilePrecheckResponse:
		// LocalCache直接返回对象指针
		precheckResp = *v
	case string:
		// Redis返回JSON字符串，需要反序列化
		if err := json.Unmarshal([]byte(v), &precheckResp); err != nil {
			logger.LOG.Error("反序列化预检信息失败", "error", err, "data", v)
			return nil, fmt.Errorf("预检信息格式错误")
		}
	default:
		logger.LOG.Error("预检信息类型错误", "type", fmt.Sprintf("%T", v))
		return nil, fmt.Errorf("预检信息类型错误")
	}

	if precheckResp.PrecheckID != req.PrecheckID {
		return nil, fmt.Errorf("无效的预检ID")
	}

	// 3. 获取预检请求中的文件大小
	var fileSize int64
	reqCacheKey := fmt.Sprintf("fileUploadReq:%s", req.PrecheckID)
	reqData, err := f.cacheLocal.Get(reqCacheKey)
	if err != nil {
		logger.LOG.Error("获取预检请求失败", "error", err)
		return nil, fmt.Errorf("无法获取原始上传请求信息")
	}

	// 反序列化预检请求数据以获取文件大小
	var precheckReq request.UploadPrecheckRequest
	switch v := reqData.(type) {
	case *request.UploadPrecheckRequest:
		precheckReq = *v
	case string:
		if err := json.Unmarshal([]byte(v), &precheckReq); err != nil {
			logger.LOG.Error("反序列化预检请求失败", "error", err)
			return nil, fmt.Errorf("预检请求信息格式错误")
		}
	default:
		logger.LOG.Error("预检请求类型错误", "type", fmt.Sprintf("%T", v))
		return nil, fmt.Errorf("预检请求信息类型错误")
	}
	fileSize = precheckReq.FileSize

	// 新预检会固定存储磁盘；旧缓存缺少磁盘ID时执行一次兼容选择并回写缓存。
	var bestDisk *models.Disk
	if precheckResp.DiskID != "" {
		bestDisk, err = f.factory.Disk().GetByID(ctx, precheckResp.DiskID)
		if err != nil {
			return nil, fmt.Errorf("预检选择的存储磁盘不存在: %w", err)
		}
	} else {
		bestDisk, err = upload.SelectBestDisk(ctx, f.factory, fileSize)
		if err != nil {
			return nil, err
		}
		precheckResp.DiskID = bestDisk.ID
		updatedPrecheck, marshalErr := json.Marshal(&precheckResp)
		if marshalErr != nil {
			return nil, fmt.Errorf("更新预检信息失败: %w", marshalErr)
		}
		if setErr := f.cacheLocal.Set(cacheKey, string(updatedPrecheck), 12*60*60); setErr != nil {
			logger.LOG.Warn("回写预检磁盘信息失败", "error", setErr, "precheckID", req.PrecheckID)
		}
	}

	// 4. 在选中磁盘的temp目录下创建临时目录：{DiskPath}/temp/{fileName}_{sessionID}/
	// 参考下载时的临时目录管理方式
	sessionID := req.PrecheckID[:8] // 使用预检ID的前8位作为会话ID
	// 使用文件名（去除扩展名）+ sessionID作为子目录名
	fileNameWithoutExt := precheckReq.FileName
	if idx := strings.LastIndex(precheckReq.FileName, "."); idx != -1 {
		fileNameWithoutExt = precheckReq.FileName[:idx]
	}
	tempBaseDir := filepath.Join(bestDisk.DataPath, "temp", fmt.Sprintf("%s_%s", fileNameWithoutExt, sessionID))
	if err := os.MkdirAll(tempBaseDir, 0755); err != nil {
		logger.LOG.Error("创建临时目录失败", "error", err, "path", tempBaseDir)
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	logger.LOG.Info("创建临时目录", "path", tempBaseDir, "diskPath", bestDisk.DataPath)

	// 缩略图是可选数据，校验或保存失败不能阻断主文件上传。
	tempThumbnailPath := upload.TempVideoThumbnailPath(tempBaseDir)
	if thumbnail != nil && thumbnailHeader != nil && !req.IsEnc {
		savedPath, saveErr := upload.SaveVideoThumbnail(thumbnail, thumbnailHeader.Size, tempBaseDir)
		if saveErr != nil {
			logger.LOG.Warn("保存视频缩略图失败，继续上传原文件", "error", saveErr, "fileName", precheckReq.FileName)
		} else {
			tempThumbnailPath = savedPath
		}
	}
	if _, statErr := os.Stat(tempThumbnailPath); statErr != nil {
		tempThumbnailPath = ""
	}

	// 3. 判断是否为分片上传
	isChunkUpload := req.ChunkIndex != nil && req.TotalChunks != nil

	if isChunkUpload {
		// 分片上传处理
		return f.handleChunkUpload(ctx, req, file, header, userID, tempBaseDir, tempThumbnailPath, &precheckResp, &precheckReq)
	} else {
		// 小文件直传处理
		return f.handleSingleUpload(ctx, req, file, header, userID, tempBaseDir, tempThumbnailPath, &precheckResp)
	}
}

// handleChunkUpload 处理分片上传
func (f *FileService) handleChunkUpload(ctx context.Context, req *request.FileUploadRequest, file multipart.File, header *multipart.FileHeader, userID, tempBaseDir, tempThumbnailPath string, precheckResp *response.FilePrecheckResponse, precheckReq *request.UploadPrecheckRequest) (*models.JsonResponse, error) {
	chunkIndex := *req.ChunkIndex
	totalChunks := *req.TotalChunks

	// 1. 保存分片文件
	chunkPath := filepath.Join(tempBaseDir, fmt.Sprintf("%d.chunk.data", chunkIndex))
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("创建分片文件失败: %w", err)
	}
	defer chunkFile.Close()

	if _, err := io.Copy(chunkFile, file); err != nil {
		return nil, fmt.Errorf("保存分片文件失败: %w", err)
	}

	logger.LOG.Info("分片上传成功", "chunkIndex", chunkIndex, "totalChunks", totalChunks, "userID", userID)

	// 2. 使用锁保护分片计数和删除操作，防止并发竞争
	lockKey := userID + ":" + header.Filename
	mutexVal, _ := uploadLocks.LoadOrStore(lockKey, &sync.Mutex{})
	mutex := mutexVal.(*sync.Mutex)

	mutex.Lock()
	// 注意：不使用defer，因为我们需要在文件处理前手动释放锁

	// 3. 删除 UploadChunk 表中对应的 MD5 记录（在锁保护下）
	// 注意：这里删除只是为了清理数据，不用于统计进度
	if req.ChunkMD5 != "" {
		// 查找匹配的 UploadChunk 记录
		chunks, err := f.factory.UploadChunk().ListByUserID(ctx, userID, 0, 1000)
		if err == nil {
			for _, chunk := range chunks {
				if chunk.Md5 == req.ChunkMD5 && chunk.FileName == header.Filename {
					if err := f.factory.UploadChunk().Delete(ctx, chunk.ChunkID); err != nil {
						logger.LOG.Warn("删除UploadChunk记录失败", "error", err, "chunkID", chunk.ChunkID)
					} else {
						logger.LOG.Debug("删除UploadChunk记录成功", "chunkID", chunk.ChunkID, "md5", req.ChunkMD5)
					}
					break
				}
			}
		}
	}

	// 4. 检查是否所有分片都已上传完成（通过检查临时目录中的文件数量）
	// 重要：不依赖UploadChunk表，而是直接检查磁盘上的分片文件
	uploadedChunkCount := 0
	for i := 0; i < totalChunks; i++ {
		chunkPath := filepath.Join(tempBaseDir, fmt.Sprintf("%d.chunk.data", i))
		if _, err := os.Stat(chunkPath); err == nil {
			uploadedChunkCount++
		}
	}

	remaining := int64(totalChunks - uploadedChunkCount)
	logger.LOG.Debug("分片上传进度", "chunkIndex", chunkIndex, "uploadedChunkCount", uploadedChunkCount, "totalChunks", totalChunks, "remaining", remaining, "fileName", header.Filename)

	// 更新上传任务记录（更新已上传分片数和状态）
	if err := f.updateUploadTask(ctx, req.PrecheckID, userID, uploadedChunkCount, totalChunks, tempBaseDir, "uploading", ""); err != nil {
		logger.LOG.Warn("更新上传任务失败", "error", err, "precheckID", req.PrecheckID)
		// 不阻塞主流程，继续执行
	}

	// 持久化后台处理所需的非敏感数据。文件密码只保留在内存任务中。
	var metadataErr error
	if task, taskErr := f.factory.UploadTask().GetByID(ctx, req.PrecheckID); taskErr == nil {
		task.DiskID = precheckResp.DiskID
		task.IsEnc = req.IsEnc
		if len(precheckReq.FilesMd5) > 0 {
			task.FirstChunkHash = precheckReq.FilesMd5[0]
		}
		if len(precheckReq.FilesMd5) > 1 {
			task.SecondChunkHash = precheckReq.FilesMd5[1]
		}
		if len(precheckReq.FilesMd5) > 2 {
			task.ThirdChunkHash = precheckReq.FilesMd5[2]
		}
		if taskErr = f.factory.UploadTask().Update(ctx, task); taskErr != nil {
			metadataErr = taskErr
			logger.LOG.Warn("保存上传任务处理信息失败", "error", taskErr, "precheckID", req.PrecheckID)
		} else {
			f.publishUploadTask(task, "updated", true)
		}
	} else {
		metadataErr = taskErr
	}

	// 5. 如果还有分片未完成，释放锁并返回成功响应
	if remaining > 0 {
		mutex.Unlock() // 释放锁
		return models.NewJsonResponse(200, "分片上传成功", map[string]interface{}{
			"chunk_index": chunkIndex,
			"uploaded":    totalChunks - int(remaining),
			"total":       totalChunks,
			"is_complete": false,
		}), nil
	}

	if req.AsyncFinalize {
		if metadataErr != nil {
			mutex.Unlock()
			return nil, fmt.Errorf("保存后台处理任务信息失败: %w", metadataErr)
		}
		claimed, claimErr := f.factory.UploadTask().ClaimProcessing(ctx, req.PrecheckID, []string{"pending", "uploading"})
		mutex.Unlock()
		uploadLocks.Delete(lockKey)
		if claimErr != nil {
			return nil, fmt.Errorf("提交后台处理任务失败: %w", claimErr)
		}
		if claimed {
			if task, getErr := f.factory.UploadTask().GetByID(ctx, req.PrecheckID); getErr == nil {
				f.publishUploadTask(task, "updated", false)
			}
			f.finalizeManager.Enqueue(req.PrecheckID, req.FilePassword)
		}
		return models.NewJsonResponse(200, "分片上传完成，文件正在后台处理", map[string]interface{}{
			"task_id":     req.PrecheckID,
			"status":      "processing",
			"is_complete": false,
		}), nil
	}

	// 6. 所有分片上传完成，检查是否已经有其他请求在处理
	if _, isProcessing := processingFiles.LoadOrStore(lockKey, true); isProcessing {
		// 已经有其他请求在处理此文件
		mutex.Unlock()
		logger.LOG.Info("文件已被其他请求处理", "fileName", header.Filename)
		return models.NewJsonResponse(200, "文件处理中", map[string]interface{}{
			"is_complete": false,
			"message":     "文件正在处理中",
		}), nil
	}

	// 7. 标记为正在处理，现在可以释放锁了
	mutex.Unlock()
	uploadLocks.Delete(lockKey)

	// 确保处理完成后删除处理标记
	defer processingFiles.Delete(lockKey)

	logger.LOG.Info("所有分片上传完成，开始处理文件", "userID", userID, "fileName", header.Filename)

	// 构造上传数据
	// 安全地获取分片 MD5，避免数组越界
	var firstChunkHash, secondChunkHash, thirdChunkHash string
	if len(precheckReq.FilesMd5) > 0 {
		firstChunkHash = precheckReq.FilesMd5[0]
	}
	if len(precheckReq.FilesMd5) > 1 {
		secondChunkHash = precheckReq.FilesMd5[1]
	}
	if len(precheckReq.FilesMd5) > 2 {
		thirdChunkHash = precheckReq.FilesMd5[2]
	}

	uploadData := &upload.FileUploadData{
		TempFilePath:      filepath.Join(tempBaseDir, "0.chunk.data"), // 第一个分片路径作为基础
		TempThumbnailPath: tempThumbnailPath,
		FileName:          header.Filename,
		FileSize:          precheckReq.FileSize,
		ChunkSignature:    precheckReq.ChunkSignature,
		FirstChunkHash:    firstChunkHash,
		SecondChunkHash:   secondChunkHash,
		ThirdChunkHash:    thirdChunkHash,
		IsEnc:             req.IsEnc,
		IsChunk:           true,
		ChunkCount:        totalChunks,
		DirectoryID:       precheckReq.DirectoryID,
		UserID:            userID,
		DiskID:            precheckResp.DiskID,
		FilePassword:      req.FilePassword, // 添加加密密码
	}

	fileID, err := upload.ProcessUploadedFile(uploadData, f.factory)
	if err != nil {
		logger.LOG.Error("处理上传文件失败", "error", err)
		// 更新上传任务状态为失败
		if updateErr := f.updateUploadTask(ctx, req.PrecheckID, userID, uploadedChunkCount, totalChunks, tempBaseDir, "failed", err.Error()); updateErr != nil {
			logger.LOG.Warn("更新上传任务状态失败", "error", updateErr, "precheckID", req.PrecheckID)
		}
		return nil, fmt.Errorf("文件处理失败: %w", err)
	}

	// 更新上传任务状态为完成
	if err := f.updateUploadTask(ctx, req.PrecheckID, userID, totalChunks, totalChunks, tempBaseDir, "completed", ""); err != nil {
		logger.LOG.Warn("更新上传任务状态失败", "error", err, "precheckID", req.PrecheckID)
		// 不阻塞主流程，继续执行
	}

	// 6. 清除缓存
	f.cacheLocal.Delete(fmt.Sprintf("fileUpload:%s", req.PrecheckID))
	reqCacheKey := fmt.Sprintf("fileUploadReq:%s", req.PrecheckID)
	f.cacheLocal.Delete(reqCacheKey)

	logger.LOG.Info("文件上传完成", "fileID", fileID, "fileName", header.Filename)
	return models.NewJsonResponse(200, "上传成功", map[string]interface{}{
		"file_id":     fileID,
		"is_complete": true,
	}), nil
}

// handleSingleUpload 处理小文件直传
func (f *FileService) handleSingleUpload(ctx context.Context, req *request.FileUploadRequest, file multipart.File, header *multipart.FileHeader, userID, tempBaseDir, tempThumbnailPath string, precheckResp *response.FilePrecheckResponse) (*models.JsonResponse, error) {
	// 1. 保存临时文件
	tempFilePath := filepath.Join(tempBaseDir, "upload.tmp")
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	logger.LOG.Info("小文件上传成功", "fileName", header.Filename, "size", header.Size, "userID", userID)

	// 2. 获取预检请求中的原始数据
	var precheckReq request.UploadPrecheckRequest
	cacheKey := fmt.Sprintf("fileUploadReq:%s", req.PrecheckID)
	reqData, err := f.cacheLocal.Get(cacheKey)
	if err != nil {
		logger.LOG.Error("获取预检请求失败", "error", err)
		return nil, fmt.Errorf("无法获取原始上传请求信息")
	}

	// 反序列化预检请求数据
	switch v := reqData.(type) {
	case *request.UploadPrecheckRequest:
		// LocalCache直接返回对象指针
		precheckReq = *v
	case string:
		// Redis返回JSON字符串，需要反序列化
		if err := json.Unmarshal([]byte(v), &precheckReq); err != nil {
			logger.LOG.Error("反序列化预检请求失败", "error", err, "data", v)
			return nil, fmt.Errorf("预检请求信息格式错误")
		}
	default:
		logger.LOG.Error("预检请求类型错误", "type", fmt.Sprintf("%T", v))
		return nil, fmt.Errorf("预检请求信息类型错误")
	}

	// 3. 构造上传数据
	uploadData := &upload.FileUploadData{
		TempFilePath:      tempFilePath,
		TempThumbnailPath: tempThumbnailPath,
		FileName:          header.Filename,
		FileSize:          header.Size,
		ChunkSignature:    precheckReq.ChunkSignature,
		IsEnc:             req.IsEnc,
		IsChunk:           false,
		DirectoryID:       precheckReq.DirectoryID,
		UserID:            userID,
		DiskID:            precheckResp.DiskID,
		FilePassword:      req.FilePassword, // 添加加密密码
	}

	// 设置hash信息（如果有）
	if len(precheckReq.FilesMd5) > 0 {
		uploadData.FirstChunkHash = precheckReq.FilesMd5[0]
		if len(precheckReq.FilesMd5) > 1 {
			uploadData.SecondChunkHash = precheckReq.FilesMd5[1]
		}
		if len(precheckReq.FilesMd5) > 2 {
			uploadData.ThirdChunkHash = precheckReq.FilesMd5[2]
		}
	}

	// 4. 调用 ProcessUploadedFile
	fileID, err := upload.ProcessUploadedFile(uploadData, f.factory)
	if err != nil {
		logger.LOG.Error("处理上传文件失败", "error", err)
		// 更新上传任务状态为失败
		if updateErr := f.updateUploadTask(ctx, req.PrecheckID, userID, 0, 1, tempBaseDir, "failed", err.Error()); updateErr != nil {
			logger.LOG.Warn("更新上传任务状态失败", "error", updateErr, "precheckID", req.PrecheckID)
		}
		return nil, fmt.Errorf("文件处理失败: %w", err)
	}

	// 更新上传任务状态为完成
	if err := f.updateUploadTask(ctx, req.PrecheckID, userID, 1, 1, tempBaseDir, "completed", ""); err != nil {
		logger.LOG.Warn("更新上传任务状态失败", "error", err, "precheckID", req.PrecheckID)
		// 不阻塞主流程，继续执行
	}

	// 5. 清除缓存
	f.cacheLocal.Delete(fmt.Sprintf("fileUpload:%s", req.PrecheckID))
	f.cacheLocal.Delete(cacheKey)

	logger.LOG.Info("文件上传完成", "fileID", fileID, "fileName", header.Filename)
	return models.NewJsonResponse(200, "上传成功", map[string]interface{}{
		"file_id":     fileID,
		"is_complete": true,
	}), nil
}

// PublicFileList 获取公开文件列表
func (f *FileService) PublicFileList(req *request.PublicFileListRequest) (*models.JsonResponse, error) {
	ctx := context.Background()
	page, pageSize := normalizeFilePage(req.Page, req.PageSize)
	tagIDs, tagMode, err := normalizeTagFilter(req.TagIDs, req.TagMode)
	if err != nil {
		return nil, err
	}
	sortBy, sortOrder := normalizeFileSort(req.SortBy, "desc")
	query := repository.UserFileQuery{
		PublicOnly: true, TagIDs: tagIDs, TagMode: tagMode, FileType: req.Type,
		SortBy: sortBy, SortOrder: sortOrder, Offset: (page - 1) * pageSize, Limit: pageSize,
	}
	userFiles, err := f.factory.UserFiles().ListFiltered(ctx, query)
	if err != nil {
		return nil, err
	}
	query.Offset, query.Limit = 0, 0
	total, err := f.factory.UserFiles().CountFiltered(ctx, query)
	if err != nil {
		return nil, err
	}
	searchItems, err := f.buildSearchFileItems(ctx, userFiles, true)
	if err != nil {
		return nil, err
	}
	fileList := make([]response.PublicFileItem, 0, len(searchItems))
	for _, item := range searchItems {
		fileList = append(fileList, response.PublicFileItem{
			UfID: item.UfID, FileName: item.FileName, FileSize: item.Size, MimeType: item.Mime,
			OwnerName: item.OwnerName, HasThumbnail: item.ThumbnailImg != "", CreatedAt: item.CreatedAt,
			Tags: item.Tags,
		})
	}

	resp := response.PublicFileListResponse{
		Files:    fileList,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}

	return models.NewJsonResponse(200, "获取成功", resp), nil
}

// GetUploadProgress 查询上传和后台文件处理进度。
func (f *FileService) GetUploadProgress(req *request.UploadProgressRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	task, err := f.factory.UploadTask().GetByID(ctx, req.PrecheckID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "上传任务不存在", nil), nil
		}
		return nil, err
	}
	if task.UserID != userID {
		return models.NewJsonResponse(403, "无权查询该上传任务", nil), nil
	}

	progressResp := response.UploadProgressResponse{
		PrecheckID:   task.ID,
		FileName:     task.FileName,
		FileSize:     task.FileSize,
		Uploaded:     task.UploadedChunks,
		Total:        task.TotalChunks,
		Progress:     calculateUploadTaskProgress(task),
		Md5:          f.uploadedChunkMD5s(task),
		IsComplete:   task.Status == "completed",
		Status:       task.Status,
		Stage:        task.ProcessingStage,
		ErrorMessage: task.ErrorMessage,
		FileID:       task.ResultFileID,
		IsEnc:        task.IsEnc,
	}

	return models.NewJsonResponse(200, "查询成功", progressResp), nil
}

func (f *FileService) uploadedChunkMD5s(task *models.UploadTask) []string {
	value, err := f.cacheLocal.Get(fmt.Sprintf("fileUploadReq:%s", task.ID))
	if err != nil {
		return []string{}
	}
	var precheckReq request.UploadPrecheckRequest
	switch typed := value.(type) {
	case string:
		if err := json.Unmarshal([]byte(typed), &precheckReq); err != nil {
			return []string{}
		}
	case *request.UploadPrecheckRequest:
		precheckReq = *typed
	default:
		return []string{}
	}
	uploaded := make([]string, 0, task.UploadedChunks)
	for index, md5 := range precheckReq.FilesMd5 {
		if index >= task.TotalChunks {
			break
		}
		if _, err := os.Stat(filepath.Join(task.TempDir, fmt.Sprintf("%d.chunk.data", index))); err == nil {
			uploaded = append(uploaded, md5)
		}
	}
	return uploaded
}

// updateUploadTask 更新上传任务记录
func (f *FileService) updateUploadTask(ctx context.Context, precheckID, userID string, uploadedChunks, totalChunks int, tempDir, status, errorMsg string) error {
	task, err := f.factory.UploadTask().GetByID(ctx, precheckID)
	if err != nil {
		// 如果任务不存在，尝试创建（可能是从缓存恢复的场景）
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 从缓存获取预检请求信息
			reqCacheKey := fmt.Sprintf("fileUploadReq:%s", precheckID)
			reqData, err := f.cacheLocal.Get(reqCacheKey)
			if err != nil {
				return fmt.Errorf("无法获取预检请求信息: %w", err)
			}

			var precheckReq request.UploadPrecheckRequest
			switch v := reqData.(type) {
			case *request.UploadPrecheckRequest:
				precheckReq = *v
			case string:
				if err := json.Unmarshal([]byte(v), &precheckReq); err != nil {
					return fmt.Errorf("反序列化预检请求失败: %w", err)
				}
			default:
				return fmt.Errorf("预检请求类型错误: %T", v)
			}

			chunkSize := int64(5 * 1024 * 1024) // 5MB
			task = &models.UploadTask{
				ID:             precheckID,
				UserID:         userID,
				FileName:       precheckReq.FileName,
				FileSize:       precheckReq.FileSize,
				ChunkSize:      chunkSize,
				TotalChunks:    totalChunks,
				UploadedChunks: uploadedChunks,
				ChunkSignature: precheckReq.ChunkSignature,
				DirectoryID:    precheckReq.DirectoryID,
				TempDir:        tempDir,
				Status:         status,
				ErrorMessage:   errorMsg,
				CreateTime:     custom_type.Now(),
				UpdateTime:     custom_type.Now(),
				ExpireTime:     custom_type.JsonTime(time.Now().Add(7 * 24 * time.Hour)),
			}
			if createErr := f.factory.UploadTask().Create(ctx, task); createErr != nil {
				return createErr
			}
			f.publishUploadTask(task, "created", false)
			return nil
		}
		return err
	}

	// 更新任务信息
	task.UploadedChunks = uploadedChunks
	task.TotalChunks = totalChunks
	task.Status = status
	task.ErrorMessage = errorMsg
	if tempDir != "" {
		task.TempDir = tempDir
	}
	task.UpdateTime = custom_type.Now()

	err = f.factory.UploadTask().Update(ctx, task)
	if err != nil {
		logger.LOG.Error("更新上传任务失败", "error", err, "precheckID", precheckID, "status", status, "uploadedChunks", uploadedChunks, "totalChunks", totalChunks)
		return err
	}
	f.publishUploadTask(task, "updated", true)
	logger.LOG.Info("更新上传任务成功", "precheckID", precheckID, "status", status, "uploadedChunks", uploadedChunks, "totalChunks", totalChunks)
	return nil
}

// ListUncompletedUploads 查询未完成的上传任务列表
func (f *FileService) ListUncompletedUploads(userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	tasks, err := f.factory.UploadTask().GetUncompletedByUserID(ctx, userID)
	if err != nil {
		logger.LOG.Error("查询未完成上传任务失败", "error", err, "userID", userID)
		return nil, err
	}

	// 转换为响应格式
	var taskList []map[string]interface{}
	for _, task := range tasks {
		taskList = append(taskList, map[string]interface{}{
			"id":               task.ID,
			"file_name":        task.FileName,
			"file_size":        task.FileSize,
			"chunk_size":       task.ChunkSize,
			"total_chunks":     task.TotalChunks,
			"uploaded_chunks":  task.UploadedChunks,
			"progress":         calculateUploadTaskProgress(task),
			"status":           task.Status,
			"processing_stage": task.ProcessingStage,
			"is_enc":           task.IsEnc,
			"result_file_id":   task.ResultFileID,
			"error_message":    task.ErrorMessage,
			"directory_id":     task.DirectoryID,
			"create_time":      task.CreateTime,
			"update_time":      task.UpdateTime,
			"expire_time":      task.ExpireTime,
		})
	}

	return models.NewJsonResponse(200, "查询成功", taskList), nil
}

// DeleteUploadTask 删除上传任务
func (f *FileService) DeleteUploadTask(taskID string, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 先查询任务是否存在，并验证是否属于当前用户
	task, err := f.factory.UploadTask().GetByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "任务不存在", nil), nil
		}
		logger.LOG.Error("查询上传任务失败", "error", err, "taskID", taskID)
		return nil, err
	}

	// 验证任务是否属于当前用户
	if task.UserID != userID {
		return models.NewJsonResponse(403, "无权删除该任务", nil), nil
	}
	if task.Status == "processing" {
		return models.NewJsonResponse(409, "文件正在处理中，暂时不能删除任务", nil), nil
	}
	if task.Status != "completed" {
		if err := upload.CleanupTaskTempDir(task.TempDir); err != nil {
			return nil, fmt.Errorf("清理上传临时目录失败: %w", err)
		}
	}

	// 删除任务
	err = f.factory.UploadTask().Delete(ctx, taskID)
	if err != nil {
		logger.LOG.Error("删除上传任务失败", "error", err, "taskID", taskID, "userID", userID)
		return nil, err
	}
	f.publishUploadTask(task, "deleted", false)

	logger.LOG.Info("删除上传任务成功", "taskID", taskID, "userID", userID, "fileName", task.FileName)
	return models.NewJsonResponse(200, "删除成功", nil), nil
}

// CleanExpiredUploads 清理过期的上传任务
// userID: 如果提供，则只清理该用户的过期任务；如果为空，则清理所有用户的过期任务（系统自动清理）
func (f *FileService) CleanExpiredUploads(userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	var count int64
	var err error

	if userID != "" {
		// 用户清理自己的过期任务
		tasks, listErr := f.factory.UploadTask().GetExpiredByUserID(ctx, userID)
		if listErr != nil {
			return nil, listErr
		}
		for _, task := range tasks {
			if cleanupErr := upload.CleanupTaskTempDir(task.TempDir); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		count, err = f.factory.UploadTask().DeleteExpiredByUserID(ctx, userID)
		if err != nil {
			logger.LOG.Error("清理用户过期上传任务失败", "error", err, "userID", userID)
			return nil, err
		}
		for _, task := range tasks {
			f.publishUploadTask(task, "deleted", false)
		}
		logger.LOG.Info("清理用户过期上传任务完成", "count", count, "userID", userID)
	} else {
		// 系统自动清理所有过期任务
		tasks, listErr := f.factory.UploadTask().ListExpired(ctx)
		if listErr != nil {
			return nil, listErr
		}
		for _, task := range tasks {
			if cleanupErr := upload.CleanupTaskTempDir(task.TempDir); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		count, err = f.factory.UploadTask().DeleteExpired(ctx)
		if err != nil {
			logger.LOG.Error("清理过期上传任务失败", "error", err)
			return nil, err
		}
		for _, task := range tasks {
			f.publishUploadTask(task, "deleted", false)
		}
		logger.LOG.Info("清理过期上传任务完成", "count", count)
	}

	return models.NewJsonResponse(200, "清理完成", map[string]interface{}{
		"cleaned_count": count,
	}), nil
}

// ListExpiredUploads 查询过期的上传任务列表
func (f *FileService) ListExpiredUploads(userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	tasks, err := f.factory.UploadTask().GetExpiredByUserID(ctx, userID)
	if err != nil {
		logger.LOG.Error("查询过期上传任务失败", "error", err, "userID", userID)
		return nil, err
	}

	// 转换为响应格式
	var taskList []map[string]interface{}
	for _, task := range tasks {
		taskList = append(taskList, map[string]interface{}{
			"id":               task.ID,
			"file_name":        task.FileName,
			"file_size":        task.FileSize,
			"chunk_size":       task.ChunkSize,
			"total_chunks":     task.TotalChunks,
			"uploaded_chunks":  task.UploadedChunks,
			"progress":         calculateUploadTaskProgress(task),
			"status":           task.Status,
			"processing_stage": task.ProcessingStage,
			"is_enc":           task.IsEnc,
			"result_file_id":   task.ResultFileID,
			"error_message":    task.ErrorMessage,
			"directory_id":     task.DirectoryID,
			"create_time":      task.CreateTime,
			"update_time":      task.UpdateTime,
			"expire_time":      task.ExpireTime,
		})
	}

	return models.NewJsonResponse(200, "查询成功", taskList), nil
}

// RenewExpiredTask 延期过期任务（恢复任务，延长过期时间）
func (f *FileService) RenewExpiredTask(taskID string, userID string, days int) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 查询任务
	task, err := f.factory.UploadTask().GetByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "任务不存在", nil), nil
		}
		logger.LOG.Error("查询上传任务失败", "error", err, "taskID", taskID)
		return nil, err
	}

	// 验证任务是否属于当前用户
	if task.UserID != userID {
		return models.NewJsonResponse(403, "无权操作该任务", nil), nil
	}

	// 验证任务是否过期
	now := time.Now()
	if time.Time(task.ExpireTime).After(now) {
		return models.NewJsonResponse(400, "任务未过期，无需延期", nil), nil
	}

	// 延期任务（默认延长7天）
	if days <= 0 {
		days = 7
	}
	task.ExpireTime = custom_type.JsonTime(now.Add(time.Duration(days) * 24 * time.Hour))
	task.UpdateTime = custom_type.Now()

	err = f.factory.UploadTask().Update(ctx, task)
	if err != nil {
		logger.LOG.Error("延期上传任务失败", "error", err, "taskID", taskID, "userID", userID)
		return nil, err
	}
	f.publishUploadTask(task, "updated", false)

	logger.LOG.Info("延期上传任务成功", "taskID", taskID, "userID", userID, "fileName", task.FileName, "days", days)
	return models.NewJsonResponse(200, "延期成功", map[string]interface{}{
		"task_id":     taskID,
		"expire_time": task.ExpireTime,
	}), nil
}

// GetUploadTaskList 获取上传任务列表
func (f *FileService) GetUploadTaskList(req *request.UploadTaskListRequest, userID string) (*models.JsonResponse, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	ctx := context.Background()

	// 计算偏移量
	offset := (req.Page - 1) * req.PageSize

	// 获取总数
	total, err := f.factory.UploadTask().CountByUserID(ctx, userID)
	if err != nil {
		logger.LOG.Error("统计上传任务总数失败", "error", err, "userID", userID)
		return nil, err
	}

	// 获取任务列表
	tasks, err := f.factory.UploadTask().ListByUserID(ctx, userID, offset, req.PageSize)
	if err != nil {
		logger.LOG.Error("获取上传任务列表失败", "error", err, "userID", userID)
		return nil, err
	}

	// 转换为响应结构体（移除敏感信息）
	taskItems := make([]response.UploadTaskItem, 0, len(tasks))
	for _, task := range tasks {
		taskItems = append(taskItems, response.UploadTaskItem{
			ID:              task.ID,
			FileName:        task.FileName,
			FileSize:        task.FileSize,
			ChunkSize:       task.ChunkSize,
			TotalChunks:     task.TotalChunks,
			UploadedChunks:  task.UploadedChunks,
			ChunkSignature:  task.ChunkSignature,
			DirectoryID:     task.DirectoryID,
			Status:          task.Status,
			ProcessingStage: task.ProcessingStage,
			IsEnc:           task.IsEnc,
			ResultFileID:    task.ResultFileID,
			ErrorMessage:    task.ErrorMessage,
			Progress:        calculateUploadTaskProgress(task),
			CreateTime:      task.CreateTime,
			UpdateTime:      task.UpdateTime,
			ExpireTime:      task.ExpireTime,
		})
	}

	// 构建响应
	responseData := response.UploadTaskListResponse{
		Tasks:    taskItems,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	return models.NewJsonResponse(200, "获取上传任务列表成功", responseData), nil
}

func calculateUploadTaskProgress(task *models.UploadTask) float64 {
	if task.Status == "completed" {
		return 100
	}
	if task.Status == "processing" {
		stages := map[string]float64{
			"queued":     90,
			"validating": 92,
			"storing":    95,
			"encrypting": 96,
			"committing": 99,
			"completed":  100,
		}
		if progress, ok := stages[task.ProcessingStage]; ok {
			return progress
		}
		return 90
	}
	if task.TotalChunks <= 0 {
		return 0
	}

	progress := float64(task.UploadedChunks) / float64(task.TotalChunks) * 90
	if progress < 0 {
		return 0
	}
	if progress > 90 {
		return 90
	}
	return progress
}
