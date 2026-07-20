package service

import (
	"context"
	"math"
	"myobj/src/core/domain/request"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/download"
	"myobj/src/pkg/models"
	"myobj/src/pkg/util"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDiskSizeGBToBytes(t *testing.T) {
	size, err := diskSizeGBToBytes(447)
	if err != nil {
		t.Fatalf("转换磁盘容量失败: %v", err)
	}
	if size != 447*util.DiskByte {
		t.Fatalf("磁盘容量转换错误: got=%d want=%d", size, 447*util.DiskByte)
	}
}

func TestDiskSizeGBToBytesRejectsInvalidValue(t *testing.T) {
	if _, err := diskSizeGBToBytes(0); err == nil {
		t.Fatal("0GB应返回错误")
	}
	if _, err := diskSizeGBToBytes(math.MaxInt64); err == nil {
		t.Fatal("溢出的磁盘容量应返回错误")
	}
}

func newAdminConfigTestService(t *testing.T) (*AdminService, *impl.RepositoryFactory, *download.NetworkPolicy) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SysConfig{}); err != nil {
		t.Fatal(err)
	}
	factory := impl.NewRepositoryFactory(db)
	policy := download.NewNetworkPolicy()
	return NewAdminService(factory, policy), factory, policy
}

func TestAdminUpdateSystemConfigPersistsAndAppliesNetworkPolicy(t *testing.T) {
	service, factory, policy := newAdminConfigTestService(t)
	proxy := " http://127.0.0.1:7890/ "
	downloadLimit := 2.5
	uploadLimit := 1.25
	_, err := service.AdminUpdateSystemConfig(&request.AdminUpdateSystemConfigRequest{
		AllowRegister:                             true,
		WebdavEnabled:                             true,
		OfflineDownloadProxy:                      &proxy,
		OfflineDownloadSpeedLimitMBPerSec:         &downloadLimit,
		OfflineDownloadBTUploadSpeedLimitMBPerSec: &uploadLimit,
	})
	if err != nil {
		t.Fatal(err)
	}

	settings := policy.Settings()
	if settings.ProxyURL != "http://127.0.0.1:7890" || settings.DownloadSpeedLimitMBPerSec != downloadLimit || settings.BTUploadSpeedLimitMBPerSec != uploadLimit {
		t.Fatalf("运行时配置未正确生效: %#v", settings)
	}
	storedProxy, err := factory.SysConfig().GetByKey(context.Background(), download.OfflineDownloadProxyConfigKey)
	if err != nil || storedProxy.Value != settings.ProxyURL {
		t.Fatalf("代理配置未正确保存: %#v, %v", storedProxy, err)
	}

	_, err = service.AdminUpdateSystemConfig(&request.AdminUpdateSystemConfigRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Settings().ProxyURL != settings.ProxyURL {
		t.Fatal("省略网络字段时不应清空原配置")
	}
}

func TestAdminUpdateSystemConfigRejectsInvalidValuesBeforeWriting(t *testing.T) {
	service, factory, _ := newAdminConfigTestService(t)
	if err := factory.SysConfig().Create(context.Background(), &models.SysConfig{Key: "allow_register", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	invalidProxy := "ftp://127.0.0.1:21"
	_, err := service.AdminUpdateSystemConfig(&request.AdminUpdateSystemConfigRequest{
		OfflineDownloadProxy: &invalidProxy,
	})
	if err == nil {
		t.Fatal("非法代理地址应返回错误")
	}
	allowRegister, _ := factory.SysConfig().GetByKey(context.Background(), "allow_register")
	if allowRegister.Value != "true" {
		t.Fatal("校验失败时不应写入其他配置")
	}
}

func TestAdminUpdateSystemConfigRollsBackTransaction(t *testing.T) {
	service, factory, _ := newAdminConfigTestService(t)
	ctx := context.Background()
	if err := factory.SysConfig().Create(ctx, &models.SysConfig{Key: "allow_register", Value: "true"}); err != nil {
		t.Fatal(err)
	}
	if err := factory.DB().Exec(`CREATE TRIGGER reject_proxy BEFORE INSERT ON sys_config
		WHEN NEW.key = 'offline_download_proxy'
		BEGIN SELECT RAISE(ABORT, '拒绝代理配置'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	proxy := "http://127.0.0.1:7890"
	_, err := service.AdminUpdateSystemConfig(&request.AdminUpdateSystemConfigRequest{
		AllowRegister:        false,
		OfflineDownloadProxy: &proxy,
	})
	if err == nil {
		t.Fatal("数据库失败应返回错误")
	}
	allowRegister, _ := factory.SysConfig().GetByKey(ctx, "allow_register")
	if allowRegister.Value != "true" {
		t.Fatal("事务失败后其他配置必须回滚")
	}
}
