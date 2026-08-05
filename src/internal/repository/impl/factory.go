package impl

import (
	"myobj/src/pkg/repository"
	"sync"

	"gorm.io/gorm"
)

type repositoryHooks struct {
	mu                 sync.RWMutex
	userFileQueuedHook func()
}

// RepositoryFactory 仓储工厂
type RepositoryFactory struct {
	db    *gorm.DB
	hooks *repositoryHooks

	userRepo         repository.UserRepository
	fileInfoRepo     repository.FileInfoRepository
	groupRepo        repository.GroupRepository
	shareRepo        repository.ShareRepository
	diskRepo         repository.DiskRepository
	apiKeyRepo       repository.ApiKeyRepository
	fileChunkRepo    repository.FileChunkRepository
	powerRepo        repository.PowerRepository
	groupPowerRepo   repository.GroupPowerRepository
	userFilesRepo    repository.UserFilesRepository
	directoryRepo    repository.DirectoryRepository
	recycledRepo     repository.RecycledRepository
	downloadTaskRepo repository.DownloadTaskRepository
	sysConfigRepo    repository.SysConfigRepository
	uploadChunkRepo  repository.UploadChunkRepository
	uploadTaskRepo   repository.UploadTaskRepository
}

// NewRepositoryFactory 创建仓储工厂实例
func NewRepositoryFactory(db *gorm.DB) *RepositoryFactory {
	return &RepositoryFactory{
		db: db, hooks: &repositoryHooks{},
	}
}

// User 获取用户仓储
func (f *RepositoryFactory) User() repository.UserRepository {
	if f.userRepo == nil {
		f.userRepo = NewUserRepository(f.db)
	}
	return f.userRepo
}

// FileInfo 获取文件信息仓储
func (f *RepositoryFactory) FileInfo() repository.FileInfoRepository {
	if f.fileInfoRepo == nil {
		f.fileInfoRepo = NewFileInfoRepository(f.db)
	}
	return f.fileInfoRepo
}

// Group 获取组仓储
func (f *RepositoryFactory) Group() repository.GroupRepository {
	if f.groupRepo == nil {
		f.groupRepo = NewGroupRepository(f.db)
	}
	return f.groupRepo
}

// Share 获取分享仓储
func (f *RepositoryFactory) Share() repository.ShareRepository {
	if f.shareRepo == nil {
		f.shareRepo = NewShareRepository(f.db)
	}
	return f.shareRepo
}

// Disk 获取磁盘仓储
func (f *RepositoryFactory) Disk() repository.DiskRepository {
	if f.diskRepo == nil {
		f.diskRepo = NewDiskRepository(f.db)
	}
	return f.diskRepo
}

// ApiKey 获取API密钥仓储
func (f *RepositoryFactory) ApiKey() repository.ApiKeyRepository {
	if f.apiKeyRepo == nil {
		f.apiKeyRepo = NewApiKeyRepository(f.db)
	}
	return f.apiKeyRepo
}

// FileChunk 获取文件分片仓储
func (f *RepositoryFactory) FileChunk() repository.FileChunkRepository {
	if f.fileChunkRepo == nil {
		f.fileChunkRepo = NewFileChunkRepository(f.db)
	}
	return f.fileChunkRepo
}

// Power 获取权限仓储
func (f *RepositoryFactory) Power() repository.PowerRepository {
	if f.powerRepo == nil {
		f.powerRepo = NewPowerRepository(f.db)
	}
	return f.powerRepo
}

// GroupPower 获取组权限关联仓储
func (f *RepositoryFactory) GroupPower() repository.GroupPowerRepository {
	if f.groupPowerRepo == nil {
		f.groupPowerRepo = NewGroupPowerRepository(f.db)
	}
	return f.groupPowerRepo
}

// UserFiles 获取用户文件关联仓储
func (f *RepositoryFactory) UserFiles() repository.UserFilesRepository {
	if f.userFilesRepo == nil {
		f.userFilesRepo = NewUserFilesRepository(f.db)
	}
	return f.userFilesRepo
}

// Directory 获取虚拟目录仓储。
func (f *RepositoryFactory) Directory() repository.DirectoryRepository {
	if f.directoryRepo == nil {
		f.directoryRepo = NewDirectoryRepository(f.db)
	}
	return f.directoryRepo
}

// Recycled 获取回收站仓储
func (f *RepositoryFactory) Recycled() repository.RecycledRepository {
	if f.recycledRepo == nil {
		f.recycledRepo = NewRecycledRepository(f.db)
	}
	return f.recycledRepo
}

// DownloadTask 获取下载任务仓储
func (f *RepositoryFactory) DownloadTask() repository.DownloadTaskRepository {
	if f.downloadTaskRepo == nil {
		f.downloadTaskRepo = NewDownloadTaskRepository(f.db)
	}
	return f.downloadTaskRepo
}

// SysConfig 获取系统配置仓储
func (f *RepositoryFactory) SysConfig() repository.SysConfigRepository {
	if f.sysConfigRepo == nil {
		f.sysConfigRepo = NewSysConfigRepository(f.db)
	}
	return f.sysConfigRepo
}

// UploadChunk 获取上传分片信息仓储
func (f *RepositoryFactory) UploadChunk() repository.UploadChunkRepository {
	if f.uploadChunkRepo == nil {
		f.uploadChunkRepo = NewUploadChunkRepository(f.db)
	}
	return f.uploadChunkRepo
}

// UploadTask 获取上传任务仓储
func (f *RepositoryFactory) UploadTask() repository.UploadTaskRepository {
	if f.uploadTaskRepo == nil {
		f.uploadTaskRepo = NewUploadTaskRepository(f.db)
	}
	return f.uploadTaskRepo
}

// DB 获取数据库实例（用于事务操作）
func (f *RepositoryFactory) DB() *gorm.DB {
	return f.db
}

// SetUserFileQueuedHook 设置用户文件标签任务提交后的应用内通知。
func (f *RepositoryFactory) SetUserFileQueuedHook(hook func()) {
	if f == nil || f.hooks == nil {
		return
	}
	f.hooks.mu.Lock()
	f.hooks.userFileQueuedHook = hook
	f.hooks.mu.Unlock()
}

// NotifyUserFileQueued 在包含标签任务的数据库事务提交成功后发送通知。
func (f *RepositoryFactory) NotifyUserFileQueued() {
	if f == nil || f.hooks == nil {
		return
	}
	f.hooks.mu.RLock()
	hook := f.hooks.userFileQueuedHook
	f.hooks.mu.RUnlock()
	if hook != nil {
		hook()
	}
}

// Clone 创建共享通知钩子但独立缓存仓储实例的工厂。
func (f *RepositoryFactory) Clone() *RepositoryFactory {
	if f == nil {
		return nil
	}
	return &RepositoryFactory{db: f.db, hooks: f.hooks}
}

// WithTx 创建基于事务的新工厂实例
func (f *RepositoryFactory) WithTx(tx *gorm.DB) *RepositoryFactory {
	return &RepositoryFactory{db: tx, hooks: f.hooks}
}
