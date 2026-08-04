package repository

import (
	"context"
	"myobj/src/pkg/models"
	"time"
)

// UserRepository 用户仓储接口
type UserRepository interface {
	Create(ctx context.Context, user *models.UserInfo) error
	GetByID(ctx context.Context, id string) (*models.UserInfo, error)
	GetByUserName(ctx context.Context, userName string) (*models.UserInfo, error)
	Update(ctx context.Context, user *models.UserInfo) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, offset, limit int) ([]*models.UserInfo, error)
	Count(ctx context.Context) (int64, error)
}

// FileInfoRepository 文件信息仓储接口
type FileInfoRepository interface {
	Create(ctx context.Context, file *models.FileInfo) error
	GetByID(ctx context.Context, id string) (*models.FileInfo, error)
	GetByHash(ctx context.Context, hash string) (*models.FileInfo, error)
	GetByChunkSignature(ctx context.Context, signature string, fileSize int64) (*models.FileInfo, error)
	Update(ctx context.Context, file *models.FileInfo) error
	// ListUnencryptedVideosAfter 按文件ID游标查询未加密视频。
	ListUnencryptedVideosAfter(ctx context.Context, afterID string, limit int) ([]*models.FileInfo, error)
	// UpdateThumbnailPath 只更新文件的缩略图路径和更新时间。
	UpdateThumbnailPath(ctx context.Context, id, thumbnailPath string) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, offset, limit int) ([]*models.FileInfo, error)
	Count(ctx context.Context) (int64, error)
	BatchCreate(ctx context.Context, files []*models.FileInfo) error
	SearchByName(ctx context.Context, keyword string, offset, limit int) ([]*models.FileInfo, error)
	CountByName(ctx context.Context, keyword string) (int64, error)
	ListByDirectoryID(ctx context.Context, userID string, directoryID int, offset, limit int) ([]*models.FileInfo, error)
	CountByDirectoryID(ctx context.Context, userID string, directoryID int) (int64, error)
}

// GroupRepository 组仓储接口
type GroupRepository interface {
	Create(ctx context.Context, group *models.Group) error
	GetByID(ctx context.Context, id int) (*models.Group, error)
	Update(ctx context.Context, group *models.Group) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context, offset, limit int) ([]*models.Group, error)
	Count(ctx context.Context) (int64, error)
	GetDefaultGroup(ctx context.Context) (*models.Group, error)
}

// ShareRepository 分享仓储接口
type ShareRepository interface {
	Create(ctx context.Context, share *models.Share) error
	GetByID(ctx context.Context, id int) (*models.Share, error)
	GetByToken(ctx context.Context, token string) (*models.Share, error)
	Update(ctx context.Context, share *models.Share) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context, userID string, offset, limit int) ([]*models.Share, error)
	Count(ctx context.Context, userID string) (int64, error)
	IncrementDownloadCount(ctx context.Context, id int) error
}

// DiskRepository 磁盘仓储接口
type DiskRepository interface {
	Create(ctx context.Context, disk *models.Disk) error
	GetByID(ctx context.Context, id string) (*models.Disk, error)
	GetBigDisk(ctx context.Context) (*models.Disk, error)
	GetByPath(ctx context.Context, path string) (*models.Disk, error)
	Update(ctx context.Context, disk *models.Disk) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, offset, limit int) ([]*models.Disk, error)
	Count(ctx context.Context) (int64, error)
}

// ApiKeyRepository API密钥仓储接口
type ApiKeyRepository interface {
	Create(ctx context.Context, apiKey *models.ApiKey) error
	GetByID(ctx context.Context, id int) (*models.ApiKey, error)
	GetByKey(ctx context.Context, key string) (*models.ApiKey, error)
	Update(ctx context.Context, apiKey *models.ApiKey) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context, userID string, offset, limit int) ([]*models.ApiKey, error)
	Count(ctx context.Context, userID string) (int64, error)
}

// FileChunkRepository 文件分片仓储接口
type FileChunkRepository interface {
	Create(ctx context.Context, chunk *models.FileChunk) error
	GetByID(ctx context.Context, id string) (*models.FileChunk, error)
	GetByFileID(ctx context.Context, fileID string) ([]*models.FileChunk, error)
	Update(ctx context.Context, chunk *models.FileChunk) error
	Delete(ctx context.Context, id string) error
	DeleteByFileID(ctx context.Context, fileID string) error
	BatchCreate(ctx context.Context, chunks []*models.FileChunk) error
}

// PowerRepository 权限仓储接口
type PowerRepository interface {
	Create(ctx context.Context, power *models.Power) error
	GetByID(ctx context.Context, id int) (*models.Power, error)
	Update(ctx context.Context, power *models.Power) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context, offset, limit int) ([]*models.Power, error)
	Count(ctx context.Context) (int64, error)
	GetByGroupID(ctx context.Context, groupID int) ([]*models.Power, error)
}

// GroupPowerRepository 组权限关联仓储接口
type GroupPowerRepository interface {
	Create(ctx context.Context, groupPower *models.GroupPower) error
	GetByGroupID(ctx context.Context, groupID int) ([]*models.GroupPower, error)
	GetByPowerID(ctx context.Context, powerID int) ([]*models.GroupPower, error)
	Delete(ctx context.Context, groupID, powerID int) error
	DeleteByGroupID(ctx context.Context, groupID int) error
	BatchCreate(ctx context.Context, groupPowers []*models.GroupPower) error
}

// UserFilesRepository 用户文件关联仓储接口
type UserFilesRepository interface {
	Create(ctx context.Context, userFile *models.UserFiles) error
	GetByUserIDAndFileID(ctx context.Context, userID, fileID string) (*models.UserFiles, error)
	Update(ctx context.Context, userFile *models.UserFiles) error
	Delete(ctx context.Context, userID, fileID string) error
	ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.UserFiles, error)
	Count(ctx context.Context, userID string) (int64, error)
	ListPublicFiles(ctx context.Context, offset, limit int) ([]*models.UserFiles, error)
	CountPublicFiles(ctx context.Context) (int64, error)
	SearchPublicFiles(ctx context.Context, keyword string, offset, limit int) ([]*models.UserFiles, error)
	CountPublicFilesByKeyword(ctx context.Context, keyword string) (int64, error)
	SearchUserFiles(ctx context.Context, userID, keyword string, offset, limit int) ([]*models.UserFiles, error)
	SearchUserFilesSorted(ctx context.Context, userID, keyword, sortBy, sortOrder string, offset, limit int) ([]*models.UserFiles, error)
	CountUserFilesByKeyword(ctx context.Context, userID, keyword string) (int64, error)
	GetByUserIDAndUfID(ctx context.Context, userID, ufID string) (*models.UserFiles, error)
	// GetByUfID 通过 uf_id 查询文件（用于公开文件访问，不要求 user_id）
	GetByUfID(ctx context.Context, ufID string) (*models.UserFiles, error)
	ListByDirectoryID(ctx context.Context, userID string, directoryID int, offset, limit int) ([]*models.UserFiles, error)
	ListByDirectoryIDSorted(ctx context.Context, userID string, directoryID int, sortBy, sortOrder string, offset, limit int) ([]*models.UserFiles, error)
	ListFiltered(ctx context.Context, query UserFileQuery) ([]*models.UserFiles, error)
	CountFiltered(ctx context.Context, query UserFileQuery) (int64, error)
}

// UserFileQuery 描述文件列表、搜索和文件广场共用的可组合查询条件。
// SearchTerms 中每个词都必须由文件名或可见标签命中；TagMode 控制标签筛选的 all/any 语义。
type UserFileQuery struct {
	UserID      string
	PublicOnly  bool
	DirectoryID *int
	SearchTerms []string
	TagIDs      []string
	TagMode     string
	FileType    string
	SortBy      string
	SortOrder   string
	Offset      int
	Limit       int
}

// DirectoryRepository 虚拟目录仓储接口。
type DirectoryRepository interface {
	Create(ctx context.Context, directory *models.VirtualDirectory) error
	GetByID(ctx context.Context, id int) (*models.VirtualDirectory, error)
	GetChild(ctx context.Context, userID string, parentID int, name string) (*models.VirtualDirectory, error)
	Update(ctx context.Context, directory *models.VirtualDirectory) error
	Delete(ctx context.Context, id int) error
	ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.VirtualDirectory, error)
	Count(ctx context.Context, userID string) (int64, error)
	ListChildren(ctx context.Context, userID string, parentID int, offset, limit int) ([]*models.VirtualDirectory, error)
	ListChildrenSorted(ctx context.Context, userID string, parentID int, sortBy, sortOrder string, offset, limit int) ([]*models.VirtualDirectory, error)
	CountSubFoldersByParentID(ctx context.Context, userID string, parentID int) (int64, error)
	GetRoot(ctx context.Context, userID string) (*models.VirtualDirectory, error)
}

// RecycledRepository 回收站仓储接口
type RecycledRepository interface {
	Create(ctx context.Context, recycled *models.Recycled) error
	GetByID(ctx context.Context, id string) (*models.Recycled, error)
	GetByUserIDAndFileID(ctx context.Context, userID, fileID string) (*models.Recycled, error)
	Delete(ctx context.Context, id string) error
	ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.Recycled, error)
	Count(ctx context.Context, userID string) (int64, error)
	// GetExpiredRecords 获取超过指定天数的回收站记录
	GetExpiredRecords(ctx context.Context, days int) ([]*models.Recycled, error)
	// CountFileReferences 统计指定文件被多少个用户持有
	CountFileReferences(ctx context.Context, fileID string) (int64, error)
}

// RunnableDownloadQueryOptions 描述调度器当前无法接收的任务范围。
type RunnableDownloadQueryOptions struct {
	ExcludedUserIDs  []string
	ExcludedBatchIDs []string
	AllowTorrent     bool
}

// DownloadTaskRepository 下载任务仓储接口
type DownloadTaskRepository interface {
	Create(ctx context.Context, task *models.DownloadTask) error
	GetByID(ctx context.Context, id string) (*models.DownloadTask, error)
	Update(ctx context.Context, task *models.DownloadTask) error
	Delete(ctx context.Context, id string) error
	ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.DownloadTask, error)
	Count(ctx context.Context, userID string) (int64, error)
	// ListByState 查询指定状态的任务
	ListByState(ctx context.Context, userID string, state int, offset, limit int) ([]*models.DownloadTask, error)
	// CountByState 统计指定状态的任务数量
	CountByState(ctx context.Context, userID string, state int) (int64, error)
	// ListByType 查询指定类型的任务
	ListByType(ctx context.Context, userID string, taskType int, offset, limit int) ([]*models.DownloadTask, error)
	// CountByType 统计指定类型的任务数量
	CountByType(ctx context.Context, userID string, taskType int) (int64, error)
	// ListByStateAndType 查询指定状态和类型的任务
	ListByStateAndType(ctx context.Context, userID string, state int, taskType int, offset, limit int) ([]*models.DownloadTask, error)
	// CountByStateAndType 统计指定状态和类型的任务数量
	CountByStateAndType(ctx context.Context, userID string, state int, taskType int) (int64, error)
	// ListByFilters 按状态和多个类型查询任务。
	ListByFilters(ctx context.Context, userID string, state *int, taskTypes []int, offset, limit int) ([]*models.DownloadTask, error)
	// CountByFilters 统计按状态和多个类型过滤后的任务数量。
	CountByFilters(ctx context.Context, userID string, state *int, taskTypes []int) (int64, error)
	// ListRunnable 查询当前可认领的离线下载任务。
	ListRunnable(ctx context.Context, now time.Time, limit int, options RunnableDownloadQueryOptions) ([]*models.DownloadTask, error)
	// NextRunnableAt 查询下一条延迟重试任务的可运行时间。
	NextRunnableAt(ctx context.Context, now time.Time, options RunnableDownloadQueryOptions) (*time.Time, error)
	// Claim 将排队任务原子认领为下载中。
	Claim(ctx context.Context, id, workerID, runToken string, leaseExpiresAt time.Time) (bool, error)
	// Transition 按允许的原状态原子切换任务状态。
	Transition(ctx context.Context, id string, allowedStates []int, newState int, updates map[string]interface{}) (bool, error)
	// UpdateIfRunToken 仅允许当前执行令牌更新任务。
	UpdateIfRunToken(ctx context.Context, id, runToken string, updates map[string]interface{}) (bool, error)
	// Heartbeat 延长当前任务租约。
	Heartbeat(ctx context.Context, id, runToken string, leaseExpiresAt time.Time) (bool, error)
}

// SysConfigRepository 系统配置仓储接口
type SysConfigRepository interface {
	Create(ctx context.Context, config *models.SysConfig) error
	GetByID(ctx context.Context, id int) (*models.SysConfig, error)
	GetByKey(ctx context.Context, key string) (*models.SysConfig, error)
	Update(ctx context.Context, config *models.SysConfig) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context, offset, limit int) ([]*models.SysConfig, error)
	Count(ctx context.Context) (int64, error)
	// BatchUpdate 批量更新配置
	BatchUpdate(ctx context.Context, configs []*models.SysConfig) error
	// GetAllAsMap 获取所有配置并以 key-value 格式返回
	GetAllAsMap(ctx context.Context) (map[string]string, error)
}

// UploadTaskRepository 上传任务仓储接口
type UploadTaskRepository interface {
	Create(ctx context.Context, task *models.UploadTask) error
	GetByID(ctx context.Context, id string) (*models.UploadTask, error)
	GetByUserID(ctx context.Context, userID string) ([]*models.UploadTask, error)
	GetUncompletedByUserID(ctx context.Context, userID string) ([]*models.UploadTask, error)
	GetExpiredByUserID(ctx context.Context, userID string) ([]*models.UploadTask, error) // 获取过期任务
	ListExpired(ctx context.Context) ([]*models.UploadTask, error)
	Update(ctx context.Context, task *models.UploadTask) error
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) (int64, error)
	DeleteExpiredByUserID(ctx context.Context, userID string) (int64, error)
	ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.UploadTask, error)
	CountByUserID(ctx context.Context, userID string) (int64, error) // 统计用户上传任务总数
	ListByStatus(ctx context.Context, status string) ([]*models.UploadTask, error)
	ClaimProcessing(ctx context.Context, id string, allowedStatuses []string) (bool, error)
}

// UploadChunkRepository 上传分片信息仓储接口
type UploadChunkRepository interface {
	Create(ctx context.Context, chunk *models.UploadChunk) error
	GetByID(ctx context.Context, chunkID int) (*models.UploadChunk, error)
	Update(ctx context.Context, chunk *models.UploadChunk) error
	Delete(ctx context.Context, chunkID int) error
	ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.UploadChunk, error)
	Count(ctx context.Context, userID string) (int64, error)
	// GetByUserIDAndFileName 根据用户ID和文件名获取分片信息
	GetByUserIDAndFileName(ctx context.Context, userID, fileName string) ([]models.UploadChunk, error)
	// DeleteByUserID 删除用户的所有上传分片记录
	DeleteByUserID(ctx context.Context, userID string) error
	// ListByDirectoryID 根据目录ID获取分片列表
	ListByDirectoryID(ctx context.Context, directoryID int, offset, limit int) ([]*models.UploadChunk, error)
}
