package service

import (
	"context"
	"errors"
	"os/exec"
	"strconv"

	"gorm.io/gorm"

	"myobj/src/pkg/models"
)

// 本文件负责自动标签开关、数量限制和运行状态。
func (s *TagService) TagSettings(ctx context.Context) (map[string]interface{}, error) {
	if err := s.reloadSettings(ctx); err != nil {
		return nil, err
	}
	runtime := s.globalRuntime.Load()
	version := int64(0)
	if runtime != nil && runtime.snapshot != nil {
		version = runtime.snapshot.GlobalVersion
	}
	type providerStatusRow struct {
		Status string
		Count  int64
	}
	var metadataStates []providerStatusRow
	if err := s.factory.DB().WithContext(ctx).Model(&models.FileMetadataState{}).
		Select("status, COUNT(*) AS count").Group("status").Scan(&metadataStates).Error; err != nil {
		return nil, err
	}
	statusCounts := make(map[string]int64, len(metadataStates))
	for _, row := range metadataStates {
		statusCounts[row.Status] = row.Count
	}
	_, ffprobeErr := exec.LookPath("ffprobe")
	return map[string]interface{}{
		"enabled": s.autoEnabled.Load(), "limit": s.autoLimit.Load(),
		"active_version": version, "degraded": s.degraded.Load(),
		"initializing":    runtime == nil,
		"degraded_reason": s.degradedReason.Load().(string),
		"providers": map[string]interface{}{
			"basic":   map[string]interface{}{"available": true},
			"image":   map[string]interface{}{"available": true},
			"ffprobe": map[string]interface{}{"available": ffprobeErr == nil},
			"states":  statusCounts,
		},
	}, nil
}

func (s *TagService) UpdateTagSettings(ctx context.Context, enabled bool, limit int) (map[string]interface{}, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("自动标签数量必须在1到100之间")
	}
	if err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for key, value := range map[string]string{"auto_tag_enabled": strconv.FormatBool(enabled), "auto_tag_limit": strconv.Itoa(limit)} {
			var config models.SysConfig
			err := tx.Where("`key` = ?", key).First(&config).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				config = models.SysConfig{Key: key, Value: value}
				if err := tx.Create(&config).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := tx.Model(&config).Update("value", value).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.autoEnabled.Store(enabled)
	s.autoLimit.Store(int64(limit))
	if err := s.reloadGlobalRules(ctx, true); err != nil {
		return nil, err
	}
	if enabled {
		runtime := s.globalRuntime.Load()
		if runtime == nil || runtime.snapshot == nil {
			return nil, errors.New("全局标签规则尚未加载")
		}
		if _, err := s.CreateRebuildJob(ctx, runtime.snapshot.GlobalVersion, "settings"); err != nil {
			return nil, err
		}
	}
	s.notifyRules()
	if enabled {
		s.notifyPending()
		s.notifyRebuild()
	}
	return s.TagSettings(ctx)
}
