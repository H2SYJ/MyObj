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
	"myobj/src/pkg/upload"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 全局上传锁，用于防止同一文件的并发处理
var uploadLocks sync.Map          // key: userID+fileName, value: *sync.Mutex
var processingFiles sync.Map      // key: userID+fileName, value: bool (标记文件是否正在处理)
var thumbnailUpdateLocks sync.Map // key: fileInfoID, value: *sync.Mutex

// FileService 文件服务
type FileService struct {
	factory         *impl.RepositoryFactory
	cacheLocal      cache.Cache
	finalizeManager *UploadFinalizeManager
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
				VirtualPath: req.PathID,
				IsPublic:    false,
				CreatedAt:   custom_type.Now(),
				UfID:        uuid.NewString(),
			}
			err := f.factory.UserFiles().Create(context.Background(), userFile)
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
					if delErr := f.factory.UserFiles().Delete(ctx, user.ID, userFile.FileID); delErr != nil {
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
				VirtualPath: req.PathID,
				IsPublic:    false,
				CreatedAt:   custom_type.Now(),
				UfID:        uuid.NewString(),
			}
			err := f.factory.UserFiles().Create(context.Background(), userFile)
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
					if delErr := f.factory.UserFiles().Delete(ctx, user.ID, userFile.FileID); delErr != nil {
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
		PathID:         req.PathID,
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
		}
	} else {
		// 创建新任务
		if err := f.factory.UploadTask().Create(ctx, uploadTask); err != nil {
			logger.LOG.Warn("创建上传任务失败", "error", err, "precheckID", uid)
			// 不阻塞主流程，继续执行
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

	// 默认分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// 搜索用户文件
	sortBy, sortOrder := normalizeFileSort(req.SortBy, req.SortOrder)
	userFiles, err := f.factory.UserFiles().SearchUserFilesSorted(ctx, userID, req.Keyword, sortBy, sortOrder, offset, pageSize)
	if err != nil {
		logger.LOG.Error("搜索用户文件失败", "error", err, "userID", userID, "keyword", req.Keyword)
		return nil, err
	}
	// 获取文件详情和用户文件信息
	type FileWithUserInfo struct {
		*models.FileInfo
		UfID     string `json:"uf_id"`
		FileName string `json:"file_name"`
		IsPublic bool   `json:"public"`
	}

	resultFiles := make([]*FileWithUserInfo, 0, len(userFiles))
	for _, uf := range userFiles {
		file, err := f.factory.FileInfo().GetByID(ctx, uf.FileID)
		if err != nil {
			continue
		}

		resultFiles = append(resultFiles, &FileWithUserInfo{
			FileInfo: file,
			UfID:     uf.UfID,
			FileName: uf.FileName,
			IsPublic: uf.IsPublic,
		})
	}

	// 统计总数
	total, err := f.factory.UserFiles().CountUserFilesByKeyword(ctx, userID, req.Keyword)
	if err != nil {
		logger.LOG.Error("统计用户文件数量失败", "error", err, "userID", userID, "keyword", req.Keyword)
		return nil, err
	}

	result := map[string]interface{}{
		"files": resultFiles,
		"total": total,
	}
	return models.NewJsonResponse(200, "搜索成功", result), nil
}

// SearchPublicFiles 搜索公开文件（广场）
func (f *FileService) SearchPublicFiles(req *request.FileSearchRequest) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 默认分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var userFiles []*models.UserFiles
	var total int64
	var err error

	if req.Keyword != "" {
		// 根据关键词搜索
		userFiles, err = f.factory.UserFiles().SearchPublicFiles(ctx, req.Keyword, offset, pageSize)
		if err != nil {
			logger.LOG.Error("搜索公开文件失败", "error", err, "keyword", req.Keyword)
			return nil, err
		}
		total, err = f.factory.UserFiles().CountPublicFilesByKeyword(ctx, req.Keyword)
	} else {
		// 获取所有公开文件
		userFiles, err = f.factory.UserFiles().ListPublicFiles(ctx, offset, pageSize)
		if err != nil {
			logger.LOG.Error("获取公开文件列表失败", "error", err)
			return nil, err
		}
		total, err = f.factory.UserFiles().CountPublicFiles(ctx)
	}

	if err != nil {
		logger.LOG.Error("统计公开文件数量失败", "error", err)
		return nil, err
	}

	// 获取文件详情和用户信息
	type FileWithOwner struct {
		*models.FileInfo
		OwnerName string `json:"owner_name"`
	}

	resultFiles := make([]*FileWithOwner, 0, len(userFiles))
	for _, uf := range userFiles {
		file, err := f.factory.FileInfo().GetByID(ctx, uf.FileID)
		if err != nil {
			continue
		}

		// 获取用户名
		user, err := f.factory.User().GetByID(ctx, uf.UserID)
		ownerName := "Unknown"
		if err == nil && user != nil {
			ownerName = user.UserName
		}

		resultFiles = append(resultFiles, &FileWithOwner{
			FileInfo:  file,
			OwnerName: ownerName,
		})
	}

	result := map[string]interface{}{
		"files": resultFiles,
		"total": total,
	}
	return models.NewJsonResponse(200, "搜索成功", result), nil
}

// GetFileList 获取文件列表（我的文件页面）
func (f *FileService) GetFileList(req *request.FileListRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()
	sortBy, sortOrder := normalizeFileSort(req.SortBy, req.SortOrder)

	// 处理虚拟路径ID，空或为0时使用根目录
	var currentPathID int
	var currentPath *models.VirtualPath
	var err error

	if req.VirtualPath == "" || req.VirtualPath == "0" {
		// 查询用户根目录
		currentPath, err = f.factory.VirtualPath().GetRootPath(ctx, userID)
		if err != nil {
			logger.LOG.Error("获取根目录失败", "error", err, "userID", userID)
			return nil, fmt.Errorf("获取根目录失败: %w", err)
		}
		currentPathID = currentPath.ID
	} else {
		// 解析虚拟路径ID
		pathID := 0
		_, err := fmt.Sscanf(req.VirtualPath, "%d", &pathID)
		if err != nil {
			logger.LOG.Error("解析虚拟路径ID失败", "error", err, "virtualPath", req.VirtualPath)
			return nil, fmt.Errorf("无效的路径ID: %w", err)
		}
		currentPathID = pathID
		// 查询当前路径信息
		currentPath, err = f.factory.VirtualPath().GetByID(ctx, currentPathID)
		if err != nil {
			logger.LOG.Error("查询路径信息失败", "error", err, "pathID", currentPathID)
			return nil, fmt.Errorf("路径不存在: %w", err)
		}
		if currentPath.UserID != userID {
			return nil, errors.New("无权访问该路径")
		}
	}

	// 查询总数（子目录 + 文件）
	folderCount, err := f.factory.VirtualPath().CountSubFoldersByParentID(ctx, userID, currentPathID)
	if err != nil {
		logger.LOG.Error("统计子目录数量失败", "error", err, "userID", userID, "pathID", currentPathID)
		return nil, err
	}
	// 文件表中virtual_path字段存的是路径ID（字符串格式）
	virtualPathIDStr := fmt.Sprintf("%d", currentPathID)
	fileCount, err := f.factory.FileInfo().CountByVirtualPath(ctx, userID, virtualPathIDStr)
	if err != nil {
		logger.LOG.Error("统计文件数量失败", "error", err, "userID", userID, "virtualPath", virtualPathIDStr)
		return nil, err
	}
	totalCount := folderCount + fileCount

	// 计算分页偏移量
	offset := (req.Page - 1) * req.PageSize

	// 优先返回文件夹
	var folders []*models.VirtualPath
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
		folders, err = f.factory.VirtualPath().ListSubFoldersByParentIDSorted(ctx, userID, currentPathID, folderSortBy, folderSortOrder, offset, folderLimit)
		if err != nil {
			logger.LOG.Error("查询子目录列表失败", "error", err, "userID", userID, "pathID", currentPathID)
			return nil, err
		}

		// 如果还有剩余空间，查询文件（直接从user_files表查询，避免file_id重复问题）
		remaining := req.PageSize - len(folders)
		if remaining > 0 {
			userFiles, err = f.factory.UserFiles().ListByVirtualPathSorted(ctx, userID, virtualPathIDStr, sortBy, sortOrder, 0, remaining)
			if err != nil {
				logger.LOG.Error("查询文件列表失败", "error", err, "userID", userID, "virtualPath", virtualPathIDStr)
				return nil, err
			}
		}
	} else {
		// 当前页只包含文件（直接从user_files表查询，避免file_id重复问题）
		fileOffset := offset - int(folderCount)
		userFiles, err = f.factory.UserFiles().ListByVirtualPathSorted(ctx, userID, virtualPathIDStr, sortBy, sortOrder, fileOffset, req.PageSize)
		if err != nil {
			logger.LOG.Error("查询文件列表失败", "error", err, "userID", userID, "virtualPath", virtualPathIDStr)
			return nil, err
		}
	}

	// 获取面包屑导航（只展示当前、上级、上上级）
	breadcrumbs, err := f.buildBreadcrumbs(ctx, currentPath)
	if err != nil {
		logger.LOG.Error("构建面包屑导航失败", "error", err, "pathID", currentPath.ID)
		return nil, err
	}

	// 构造响应
	resp := &response.FileListResponse{
		Breadcrumbs: breadcrumbs,
		CurrentPath: fmt.Sprintf("%d", currentPathID),
		Folders:     make([]*response.FolderItem, 0, len(folders)),
		Files:       make([]*response.FileItem, 0, len(userFiles)),
		Total:       totalCount,
		Page:        req.Page,
		PageSize:    req.PageSize,
	}

	// 转换文件夹数据
	for _, folder := range folders {
		resp.Folders = append(resp.Folders, &response.FolderItem{
			ID:          folder.ID,
			Name:        folder.Path,
			Path:        fmt.Sprintf("%d", folder.ID),
			CreatedTime: folder.CreatedTime,
		})
	}

	// 转换文件数据（直接使用user_files记录，避免file_id重复导致查询错误）
	for _, uf := range userFiles {
		// 获取file_info详情
		fileInfo, err := f.factory.FileInfo().GetByID(ctx, uf.FileID)
		if err != nil {
			logger.LOG.Warn("获取文件信息失败", "error", err, "fileID", uf.FileID, "ufID", uf.UfID)
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

// buildBreadcrumbs 构建面包屑导航（只展示当前、上级、上上级）
func (f *FileService) buildBreadcrumbs(ctx context.Context, currentPath *models.VirtualPath) ([]response.Breadcrumb, error) {
	breadcrumbs := []response.Breadcrumb{}

	// 添加当前目录
	breadcrumbs = append(breadcrumbs, response.Breadcrumb{
		ID:   currentPath.ID,
		Name: currentPath.Path,
		Path: fmt.Sprintf("%d", currentPath.ID),
	})

	// 获取上级目录（如果存在）
	if currentPath.ParentLevel != "" {
		parentID := 0
		_, err := fmt.Sscanf(currentPath.ParentLevel, "%d", &parentID)
		if err == nil && parentID > 0 {
			parent, err := f.factory.VirtualPath().GetByID(ctx, parentID)
			if err == nil {
				// 在开头插入上级目录
				breadcrumbs = append([]response.Breadcrumb{{
					ID:   parent.ID,
					Name: parent.Path,
					Path: fmt.Sprintf("%d", parent.ID),
				}}, breadcrumbs...)

				// 获取上上级目录（如果存在）
				if parent.ParentLevel != "" {
					grandParentID := 0
					_, err := fmt.Sscanf(parent.ParentLevel, "%d", &grandParentID)
					if err == nil && grandParentID > 0 {
						grandParent, err := f.factory.VirtualPath().GetByID(ctx, grandParentID)
						if err == nil {
							// 在开头插入上上级目录
							breadcrumbs = append([]response.Breadcrumb{{
								ID:   grandParent.ID,
								Name: grandParent.Path,
								Path: fmt.Sprintf("%d", grandParent.ID),
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
	path, err := f.factory.VirtualPath().GetByPath(ctx, userID, req.DirPath)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.LOG.Error("获取目录失败", "error", err)
		return nil, err
	}
	if path != nil {
		logger.LOG.Error("目录已存在", "path", req.DirPath)
		return models.NewJsonResponse(400, "目录已存在", nil), nil
	}
	//转换为int
	parentLevel, err := strconv.Atoi(req.ParentLevel)
	if err != nil {
		logger.LOG.Error("参数错误", "error", err)
		return nil, err
	}
	virtualPath := &models.VirtualPath{
		UserID:      userID,
		Path:        req.DirPath,
		CreatedTime: custom_type.Now(),
		UpdateTime:  custom_type.Now(),
	}
	if parentLevel > 0 {
		vp, err := f.factory.VirtualPath().GetByID(ctx, parentLevel)
		if err != nil {
			logger.LOG.Error("获取上级目录失败", "error", err)
			return nil, err
		}
		virtualPath.ParentLevel = fmt.Sprintf("%d", vp.ID)
	}
	err = f.factory.VirtualPath().Create(ctx, virtualPath)
	if err != nil {
		logger.LOG.Error("创建目录失败", "error", err)
		return nil, err
	}
	return models.NewJsonResponse(200, "创建目录成功", nil), nil
}

// MoveFile 移动文件
func (f *FileService) MoveFile(req *request.MoveFileRequest, userID string) (*models.JsonResponse, error) {
	return f.MoveItems(&request.MoveItemsRequest{
		FileIDs: []string{req.FileID}, TargetPath: req.TargetPath,
	}, userID)
}

// GetVirtualPath 获取虚拟路径
func (f *FileService) GetVirtualPath(userID string) (*models.JsonResponse, error) {
	user, err := f.factory.VirtualPath().GetPathByUser(context.Background(), userID)
	if err != nil {
		logger.LOG.Error("获取虚拟路径失败", "error", err)
		return nil, err
	}
	return models.NewJsonResponse(200, "获取虚拟路径成功", user), nil
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
	// 注意：UserFiles.VirtualPath 存储的是路径ID（字符串格式）
	existingFiles, err := f.factory.UserFiles().ListByUserID(ctx, userID, 0, 10000)
	if err != nil {
		logger.LOG.Error("查询文件列表失败", "error", err)
		return nil, err
	}

	// 检查同一虚拟路径下是否有同名文件
	for _, file := range existingFiles {
		if file.VirtualPath == userFile.VirtualPath &&
			file.FileName == req.NewFileName &&
			file.UfID != req.FileID {
			return models.NewJsonResponse(400, "该目录下已存在同名文件", nil), nil
		}
	}

	// 4. 保存旧文件名用于日志
	oldFileName := userFile.FileName

	// 5. 更新文件名
	userFile.FileName = req.NewFileName
	err = f.factory.UserFiles().Update(ctx, userFile)
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
	virtualPath, err := f.factory.VirtualPath().GetByID(ctx, req.DirID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "目录不存在", nil), nil
		}
		logger.LOG.Error("获取目录失败", "error", err, "dirID", req.DirID)
		return nil, err
	}

	// 2. 验证目录是否属于当前用户
	if virtualPath.UserID != userID {
		return models.NewJsonResponse(403, "无权访问该目录", nil), nil
	}

	// 2.1 检查是否是根目录（根目录的 ParentLevel 为空或 NULL）
	rootPath, err := f.factory.VirtualPath().GetRootPath(ctx, userID)
	if err != nil {
		logger.LOG.Error("获取根目录失败", "error", err)
		return nil, err
	}

	isRootDir := rootPath.ID == req.DirID
	if isRootDir {
		// 根目录通常不应该被重命名，这里返回错误
		return models.NewJsonResponse(400, "根目录不能重命名", nil), nil
	}

	// 3. 验证新目录名不能为空
	newDirName := strings.TrimSpace(req.NewDirName)
	if newDirName == "" {
		return models.NewJsonResponse(400, "新目录名不能为空", nil), nil
	}

	// 4. 构建新路径（VirtualPath.Path 存储的是目录名，如 "/folder1"）
	newPath := "/" + newDirName

	// 5. 检查同级目录下是否已存在同名目录
	// 获取父目录ID（用于查询同级目录）
	var parentID int
	if virtualPath.ParentLevel != "" {
		// 有父目录，解析父目录ID
		var err error
		parentID, err = strconv.Atoi(virtualPath.ParentLevel)
		if err != nil {
			logger.LOG.Error("解析父目录ID失败", "error", err, "parentLevel", virtualPath.ParentLevel)
			return nil, fmt.Errorf("无效的父目录ID: %w", err)
		}
	} else {
		// ParentLevel 为空，应该是根目录的子目录
		// 根据代码逻辑，根目录的子目录的 ParentLevel 应该是根目录的ID
		// 但如果 ParentLevel 为空，说明可能是数据不一致，使用根目录ID作为父目录ID
		parentID = rootPath.ID
		logger.LOG.Warn("目录的 ParentLevel 为空，使用根目录ID作为父目录", "dirID", req.DirID)
	}

	// 查询同一父目录下的所有子目录
	subFolders, err := f.factory.VirtualPath().ListSubFoldersByParentID(ctx, userID, parentID, 0, 1000)
	if err != nil {
		logger.LOG.Error("查询子目录列表失败", "error", err)
		return nil, err
	}

	// 检查是否有同名目录（排除当前目录）
	for _, folder := range subFolders {
		if folder.Path == newPath && folder.ID != req.DirID {
			return models.NewJsonResponse(400, "该目录下已存在同名目录", nil), nil
		}
	}

	// 6. 更新目录路径
	oldPath := virtualPath.Path
	virtualPath.Path = newPath
	virtualPath.UpdateTime = custom_type.Now()

	err = f.factory.VirtualPath().Update(ctx, virtualPath)
	if err != nil {
		logger.LOG.Error("重命名目录失败", "error", err, "dirID", req.DirID, "newDirName", req.NewDirName)
		return nil, fmt.Errorf("重命名目录失败: %w", err)
	}

	// 7. 注意：由于 VirtualPath.Path 只存储目录名（如 "/folder1"），
	// 而 UserFiles.VirtualPath 存储的是路径ID（字符串格式），
	// 所以重命名目录时，子目录和文件的路径不需要更新
	// 只需要更新当前目录的 Path 即可

	logger.LOG.Info("目录重命名成功", "dirID", req.DirID, "oldPath", oldPath, "newPath", newPath)
	return models.NewJsonResponse(200, "目录重命名成功", map[string]interface{}{
		"dir_id":   req.DirID,
		"dir_path": newPath,
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

// DeleteDir 删除目录（递归删除目录下的所有文件和子目录）
func (f *FileService) deleteDirLegacy(req *request.DeleteDirRequest, userID string) (*models.JsonResponse, error) {
	ctx := context.Background()

	// 1. 获取目录信息
	virtualPath, err := f.factory.VirtualPath().GetByID(ctx, req.DirID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NewJsonResponse(404, "目录不存在", nil), nil
		}
		logger.LOG.Error("获取目录失败", "error", err, "dirID", req.DirID)
		return nil, err
	}

	// 2. 验证目录是否属于当前用户
	if virtualPath.UserID != userID {
		return models.NewJsonResponse(403, "无权访问该目录", nil), nil
	}

	// 3. 检查是否是根目录（根目录不能删除）
	rootPath, err := f.factory.VirtualPath().GetRootPath(ctx, userID)
	if err != nil {
		logger.LOG.Error("获取根目录失败", "error", err)
		return nil, err
	}

	isRootDir := rootPath.ID == req.DirID
	if isRootDir {
		return models.NewJsonResponse(400, "根目录不能删除", nil), nil
	}

	// 4. 递归获取目录下的所有文件和子目录
	dirPathID := strconv.Itoa(req.DirID)

	// 4.1 获取目录下的所有文件（直接查询该目录下的文件，避免获取所有文件）
	// 注意：由于 UserFilesRepository 没有 ListByVirtualPath 方法，我们使用 ListByUserID 然后过滤
	// 对于大多数用户，文件数量不会太多，这个实现是可以接受的
	allFiles, err := f.factory.UserFiles().ListByUserID(ctx, userID, 0, 100000)
	if err != nil {
		logger.LOG.Error("获取文件列表失败", "error", err)
		return nil, err
	}

	// 过滤出该目录下的文件（VirtualPath 存储的是路径ID的字符串形式）
	var filesToDelete []string
	for _, file := range allFiles {
		// 检查文件是否在该目录下（VirtualPath 存储的是目录ID的字符串形式）
		if file.VirtualPath == dirPathID {
			filesToDelete = append(filesToDelete, file.UfID)
		}
	}

	// 4.2 递归获取所有子目录
	// 注意：需要处理根目录的子目录（ParentLevel 可能为空）的情况
	var dirsToDelete []int
	err = f.collectSubDirs(ctx, userID, req.DirID, &dirsToDelete, rootPath.ID)
	if err != nil {
		logger.LOG.Error("收集子目录失败", "error", err)
		return nil, err
	}

	// 5. 删除所有文件（移动到回收站）
	fileSuccessCount := 0
	fileFailedCount := 0
	if len(filesToDelete) > 0 {
		deleteFileReq := &request.DeleteFileRequest{
			FileIDs: filesToDelete,
		}
		result, err := f.DeleteFiles(deleteFileReq, userID)
		if err != nil {
			logger.LOG.Error("删除目录下文件失败", "error", err)
			return nil, err
		}
		// 解析删除结果
		if result.Data != nil {
			if data, ok := result.Data.(map[string]interface{}); ok {
				if success, ok := data["success"].(float64); ok {
					fileSuccessCount = int(success)
				}
				if failed, ok := data["failed"].(float64); ok {
					fileFailedCount = int(failed)
				}
			}
		}
	}

	// 6. 递归删除所有子目录（从最深层开始）
	// 注意：如果子目录删除失败，我们仍然会尝试删除父目录
	// 这是合理的，因为用户已经确认要删除整个目录，且返回了详细的失败信息
	dirSuccessCount := 0
	dirFailedCount := 0
	// 反转数组，从最深层开始删除（确保先删除子目录，再删除父目录）
	for i := len(dirsToDelete) - 1; i >= 0; i-- {
		dirID := dirsToDelete[i]
		err := f.factory.VirtualPath().Delete(ctx, dirID)
		if err != nil {
			logger.LOG.Error("删除子目录失败", "error", err, "dirID", dirID)
			dirFailedCount++
			// 继续删除其他目录，不中断流程
		} else {
			dirSuccessCount++
		}
	}

	// 7. 删除目录本身
	// 即使部分子目录删除失败，仍然删除父目录（用户已确认删除）
	err = f.factory.VirtualPath().Delete(ctx, req.DirID)
	if err != nil {
		logger.LOG.Error("删除目录失败", "error", err, "dirID", req.DirID)
		return nil, fmt.Errorf("删除目录失败: %w", err)
	}

	logger.LOG.Info("目录删除成功", "dirID", req.DirID,
		"filesDeleted", fileSuccessCount, "filesFailed", fileFailedCount,
		"dirsDeleted", dirSuccessCount, "dirsFailed", dirFailedCount)

	message := fmt.Sprintf("目录删除成功，已删除 %d 个文件", fileSuccessCount)
	if fileFailedCount > 0 {
		message = fmt.Sprintf("%s，%d 个文件删除失败", message, fileFailedCount)
	}
	if dirSuccessCount > 0 {
		message = fmt.Sprintf("%s，已删除 %d 个子目录", message, dirSuccessCount)
	}
	if dirFailedCount > 0 {
		message = fmt.Sprintf("%s，%d 个子目录删除失败", message, dirFailedCount)
	}

	return models.NewJsonResponse(200, message, map[string]interface{}{
		"dir_id":        req.DirID,
		"files_deleted": fileSuccessCount,
		"files_failed":  fileFailedCount,
		"dirs_deleted":  dirSuccessCount,
		"dirs_failed":   dirFailedCount,
	}), nil
}

// collectSubDirs 递归收集目录下的所有子目录
func (f *FileService) collectSubDirs(ctx context.Context, userID string, parentDirID int, result *[]int, rootDirID int) error {
	// 获取直接子目录
	// 注意：ListSubFoldersByParentID 使用整数 parentID 查询 TEXT 类型的 parent_level 字段
	// GORM 会自动进行类型转换，这在其他代码（如 RenameDir）中已经验证可行
	subDirs, err := f.factory.VirtualPath().ListSubFoldersByParentID(ctx, userID, parentDirID, 0, 10000)
	if err != nil {
		return err
	}

	// 验证 parent_level 是否匹配（作为额外的安全检查）
	parentLevelStr := strconv.Itoa(parentDirID)
	for _, subDir := range subDirs {
		// 验证确实是子目录
		// 情况1：ParentLevel 等于父目录ID的字符串形式（正常情况）
		// 情况2：ParentLevel 为空且父目录是根目录（根目录的直接子目录）
		isValidChild := false
		if subDir.ParentLevel == parentLevelStr {
			// 正常情况：ParentLevel 匹配
			isValidChild = true
		} else if subDir.ParentLevel == "" && parentDirID == rootDirID {
			// 特殊情况：根目录的直接子目录，ParentLevel 可能为空
			isValidChild = true
		}

		if isValidChild && subDir.IsDir {
			*result = append(*result, subDir.ID)
			// 递归收集子目录的子目录
			if err := f.collectSubDirs(ctx, userID, subDir.ID, result, rootDirID); err != nil {
				return err
			}
		}
	}

	return nil
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
		VirtualPath:       precheckReq.PathID,
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
		VirtualPath:       precheckReq.PathID,
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

	// 默认分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// 获取所有公开文件
	userFiles, err := f.factory.UserFiles().ListPublicFiles(ctx, offset, pageSize)
	if err != nil {
		logger.LOG.Error("获取公开文件列表失败", "error", err)
		return nil, err
	}

	// 统计公开文件数量
	total, err := f.factory.UserFiles().CountPublicFiles(ctx)
	if err != nil {
		logger.LOG.Error("统计公开文件数量失败", "error", err)
		return nil, err
	}

	// 构建响应数据
	fileList := make([]response.PublicFileItem, 0, len(userFiles))
	for _, uf := range userFiles {
		// 获取文件详情
		fileInfo, err := f.factory.FileInfo().GetByID(ctx, uf.FileID)
		if err != nil {
			logger.LOG.Warn("获取文件信息失败", "fileID", uf.FileID, "error", err)
			continue
		}

		// 获取文件所属用户信息
		user, err := f.factory.User().GetByID(ctx, uf.UserID)
		if err != nil {
			logger.LOG.Warn("获取用户信息失败", "userID", uf.UserID, "error", err)
			continue
		}

		// 根据文件类型过滤
		if req.Type != "" && req.Type != "all" {
			// 获取文件主类型（如 image、video、audio 等）
			mainType := ""
			if len(fileInfo.Mime) > 0 {
				parts := strings.Split(fileInfo.Mime, "/")
				if len(parts) > 0 {
					mainType = parts[0]
				}
			}

			// 特殊处理压缩文件
			if req.Type == "archive" {
				if !strings.Contains(fileInfo.Mime, "zip") && !strings.Contains(fileInfo.Mime, "rar") &&
					!strings.Contains(fileInfo.Mime, "7z") && !strings.Contains(fileInfo.Mime, "tar") &&
					!strings.Contains(fileInfo.Mime, "gzip") {
					continue
				}
			} else if req.Type == "doc" {
				// 文档类型：pdf、word、excel、ppt等
				// 检查 PDF
				isPDF := strings.Contains(fileInfo.Mime, "pdf")
				// 检查 Word 文档（包括旧格式 .doc 和新格式 .docx）
				isWord := strings.Contains(fileInfo.Mime, "word") || strings.Contains(fileInfo.Mime, "wordprocessingml")
				// 检查 Excel 表格（包括旧格式 .xls 和新格式 .xlsx）
				isExcel := strings.Contains(fileInfo.Mime, "excel") || strings.Contains(fileInfo.Mime, "spreadsheetml")
				// 检查 PowerPoint 演示（包括旧格式 .ppt 和新格式 .pptx）
				isPPT := strings.Contains(fileInfo.Mime, "powerpoint") || strings.Contains(fileInfo.Mime, "presentationml")
				// 检查通用文档类型
				isDocument := strings.Contains(fileInfo.Mime, "document") || strings.Contains(fileInfo.Mime, "presentation")

				if !isPDF && !isWord && !isDocument && !isExcel && !isPPT {
					continue
				}
			} else if req.Type == "other" {
				// 其他类型：匹配所有不属于 image、video、audio、doc、archive 的文件
				if mainType == "image" || mainType == "video" || mainType == "audio" {
					continue
				}
				// 检查是否是文档类型
				isPDF := strings.Contains(fileInfo.Mime, "pdf")
				isWord := strings.Contains(fileInfo.Mime, "word") || strings.Contains(fileInfo.Mime, "wordprocessingml")
				isExcel := strings.Contains(fileInfo.Mime, "excel") || strings.Contains(fileInfo.Mime, "spreadsheetml")
				isPPT := strings.Contains(fileInfo.Mime, "powerpoint") || strings.Contains(fileInfo.Mime, "presentationml")
				isDocument := strings.Contains(fileInfo.Mime, "document") || strings.Contains(fileInfo.Mime, "presentation")

				if isPDF || isWord || isDocument || isExcel || isPPT {
					continue
				}
				// 检查是否是压缩文件
				if strings.Contains(fileInfo.Mime, "zip") || strings.Contains(fileInfo.Mime, "rar") ||
					strings.Contains(fileInfo.Mime, "7z") || strings.Contains(fileInfo.Mime, "tar") ||
					strings.Contains(fileInfo.Mime, "gzip") {
					continue
				}
				// 其他所有类型都匹配（包括 text、application 等）
			} else if mainType != req.Type {
				// 其他类型直接匹配主类型（image、video、audio）
				continue
			}
		}

		fileList = append(fileList, response.PublicFileItem{
			UfID:         uf.UfID,
			FileName:     uf.FileName,
			FileSize:     fileInfo.Size,
			MimeType:     fileInfo.Mime,
			OwnerName:    user.Name,
			HasThumbnail: fileInfo.ThumbnailImg != "",
			CreatedAt:    uf.CreatedAt,
		})
	}

	// 排序
	if req.SortBy != "" {
		switch req.SortBy {
		case "name":
			sort.Slice(fileList, func(i, j int) bool {
				return fileList[i].FileName < fileList[j].FileName
			})
		case "size":
			sort.Slice(fileList, func(i, j int) bool {
				return fileList[i].FileSize > fileList[j].FileSize
			})
		case "time":
			sort.Slice(fileList, func(i, j int) bool {
				return fileList[i].CreatedAt.After(fileList[j].CreatedAt)
			})
		}
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
				PathID:         precheckReq.PathID,
				TempDir:        tempDir,
				Status:         status,
				ErrorMessage:   errorMsg,
				CreateTime:     custom_type.Now(),
				UpdateTime:     custom_type.Now(),
				ExpireTime:     custom_type.JsonTime(time.Now().Add(7 * 24 * time.Hour)),
			}
			return f.factory.UploadTask().Create(ctx, task)
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
			"path_id":          task.PathID,
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
			"path_id":          task.PathID,
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
			PathID:          task.PathID,
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
