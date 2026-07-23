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
	pluginService       *PluginService
	subscriptionService *SubscriptionService
}

func NewServiceFactory(factory *impl.RepositoryFactory, cacheLocal cache.Cache) *ServerFactory {
	taskEvents := NewTaskEventHub()
	networkPolicy := initializeDownloadNetworkPolicy(factory)
	downloadService := NewDownloadService(factory, networkPolicy)
	downloadService.SetTaskEventHub(taskEvents)
	fileService := NewFileService(factory, cacheLocal)
	fileService.SetTaskEventHub(taskEvents)
	runtime, err := pluginpkg.NewRuntime(context.Background())
	if err != nil {
		panic(err)
	}
	pluginService := NewPluginService(factory, runtime)
	subscriptionService := NewSubscriptionService(factory, pluginService, downloadService)
	subscriptionService.Start()
	return &ServerFactory{
		taskEvents:          taskEvents,
		userService:         NewUserService(factory, cacheLocal),
		fileService:         fileService,
		shareService:        NewSharesService(factory, cacheLocal),
		downloadService:     downloadService,
		recycledService:     NewRecycledService(factory, cacheLocal),
		adminService:        NewAdminService(factory, networkPolicy),
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
