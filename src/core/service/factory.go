package service

import (
	"context"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/cache"
	pluginpkg "myobj/src/pkg/plugin"
)

type ServerFactoryInterface interface {
	GetRepository() *impl.RepositoryFactory
}

type ServerFactory struct {
	taskEvents          *TaskEventHub
	userService         *UserService
	fileService         *FileService
	shareService        *SharesService
	downloadService     *DownloadService
	recycledService     *RecycledService
	adminService        *AdminService
	tagService          *TagService
	pluginService       *PluginService
	subscriptionService *SubscriptionService
}

func NewServiceFactory(factory *impl.RepositoryFactory, cacheLocal cache.Cache) *ServerFactory {
	taskEvents := NewTaskEventHub()
	tagService, err := NewTagService(factory)
	if err != nil {
		panic(err)
	}
	networkPolicy := initializeDownloadNetworkPolicy(factory)
	downloadService := NewDownloadService(factory, networkPolicy)
	downloadService.SetTaskEventHub(taskEvents)
	fileService := NewFileService(factory, cacheLocal)
	fileService.SetTagService(tagService)
	fileService.SetTaskEventHub(taskEvents)
	runtime, err := pluginpkg.NewRuntime(context.Background())
	if err != nil {
		panic(err)
	}
	pluginService := NewPluginService(factory, runtime)
	subscriptionService := NewSubscriptionService(factory, pluginService, downloadService)
	downloadService.SetTaskFinishedHook(subscriptionService.NotifyThumbnailForDownloadTask)
	subscriptionService.Start()
	adminService := NewAdminService(factory, networkPolicy)
	adminService.SetTagService(tagService)
	recycledService := NewRecycledService(factory, cacheLocal)
	recycledService.SetTagService(tagService)
	tagService.Start()
	return &ServerFactory{
		taskEvents:          taskEvents,
		userService:         NewUserService(factory, cacheLocal),
		fileService:         fileService,
		shareService:        NewSharesService(factory, cacheLocal),
		downloadService:     downloadService,
		recycledService:     recycledService,
		adminService:        adminService,
		tagService:          tagService,
		pluginService:       pluginService,
		subscriptionService: subscriptionService,
	}
}

func (f *ServerFactory) TaskEvents() *TaskEventHub { return f.taskEvents }

func (f *ServerFactory) GetRepository() *impl.RepositoryFactory { return f.fileService.GetRepository() }

func (f *ServerFactory) PluginService() *PluginService { return f.pluginService }

func (f *ServerFactory) SubscriptionService() *SubscriptionService { return f.subscriptionService }

func (f *ServerFactory) Close(ctx context.Context) error {
	f.subscriptionService.Stop()
	f.downloadService.Stop()
	f.tagService.Close()
	return f.pluginService.Close(ctx)
}

func (f *ServerFactory) UserService() *UserService {
	return f.userService
}

func (f *ServerFactory) FileService() *FileService {
	return f.fileService
}

func (f *ServerFactory) ShareService() *SharesService {
	return f.shareService
}

func (f *ServerFactory) DownloadService() *DownloadService {
	return f.downloadService
}

func (f *ServerFactory) RecycledService() *RecycledService {
	return f.recycledService
}

func (f *ServerFactory) AdminService() *AdminService {
	return f.adminService
}

func (f *ServerFactory) TagService() *TagService { return f.tagService }
