package service

import (
	"context"
	"errors"
	"fmt"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/download"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

func loadDownloadNetworkSettings(ctx context.Context, factory *impl.RepositoryFactory, proxyOverride *string, downloadLimitOverride, uploadLimitOverride *float64) (download.NetworkSettings, error) {
	proxyValue, err := getSystemConfigValue(ctx, factory, download.OfflineDownloadProxyConfigKey)
	if err != nil {
		return download.NetworkSettings{}, err
	}
	downloadLimitValue, err := getSystemConfigValue(ctx, factory, download.OfflineDownloadSpeedLimitConfigKey)
	if err != nil {
		return download.NetworkSettings{}, err
	}
	uploadLimitValue, err := getSystemConfigValue(ctx, factory, download.OfflineDownloadBTUploadSpeedLimitConfigKey)
	if err != nil {
		return download.NetworkSettings{}, err
	}

	if proxyOverride != nil {
		proxyValue = *proxyOverride
	}
	proxyValue, err = download.NormalizeProxyURL(proxyValue)
	if err != nil {
		return download.NetworkSettings{}, err
	}

	var downloadLimit float64
	if downloadLimitOverride != nil {
		downloadLimit = *downloadLimitOverride
	} else {
		downloadLimit, err = parseSpeedLimit(downloadLimitValue)
		if err != nil {
			return download.NetworkSettings{}, fmt.Errorf("全局下载限速配置错误: %w", err)
		}
	}
	if err := download.ValidateSpeedLimitMBPerSec(downloadLimit); err != nil {
		return download.NetworkSettings{}, err
	}

	var uploadLimit float64
	if uploadLimitOverride != nil {
		uploadLimit = *uploadLimitOverride
	} else {
		uploadLimit, err = parseSpeedLimit(uploadLimitValue)
		if err != nil {
			return download.NetworkSettings{}, fmt.Errorf("BT上传限速配置错误: %w", err)
		}
	}
	if err := download.ValidateSpeedLimitMBPerSec(uploadLimit); err != nil {
		return download.NetworkSettings{}, err
	}

	return download.NetworkSettings{
		ProxyURL:                   proxyValue,
		DownloadSpeedLimitMBPerSec: downloadLimit,
		BTUploadSpeedLimitMBPerSec: uploadLimit,
	}, nil
}

func getSystemConfigValue(ctx context.Context, factory *impl.RepositoryFactory, key string) (string, error) {
	config, err := factory.SysConfig().GetByKey(ctx, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取系统配置%s失败: %w", key, err)
	}
	return config.Value, nil
}

func parseSpeedLimit(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("必须是有效数值")
	}
	return value, nil
}

func initializeDownloadNetworkPolicy(factory *impl.RepositoryFactory) *download.NetworkPolicy {
	policy := download.NewNetworkPolicy()
	settings, err := loadDownloadNetworkSettings(context.Background(), factory, nil, nil, nil)
	if err != nil {
		if logger.LOG != nil {
			logger.LOG.Warn("加载离线下载网络配置失败，使用默认配置", "error", err)
		}
		return policy
	}
	if err := policy.Apply(settings); err != nil {
		if logger.LOG != nil {
			logger.LOG.Warn("应用离线下载网络配置失败，使用默认配置", "error", err)
		}
		return download.NewNetworkPolicy()
	}
	return policy
}

func systemConfigForValue(ctx context.Context, factory *impl.RepositoryFactory, key, value string) (*models.SysConfig, error) {
	config, err := factory.SysConfig().GetByKey(ctx, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.SysConfig{Key: key, Value: value}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取系统配置%s失败: %w", key, err)
	}
	config.Value = value
	return config, nil
}
