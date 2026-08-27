package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

// 本文件负责标签规则快照、运行时缓存和搜索分词。
// SearchTerms 使用与自动标签相同的全局词典拆分普通搜索词。
// 调用方在一次查询中固定使用返回结果，避免热切换时观察到两个规则版本。
func (s *TagService) SearchTerms(_ context.Context, _ string, keyword string) ([]string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	runtime := s.globalRuntime.Load()
	if runtime == nil || runtime.snapshot == nil {
		return nil, errors.New("全局标签规则尚未加载")
	}
	terms := runtime.snapshot.TokenizeQuery(keyword)
	if len(terms) == 0 {
		return []string{tagging.Normalize(keyword)}, nil
	}
	return terms, nil
}

func (s *TagService) reloadSettings(ctx context.Context) error {
	enabled := true
	limit := tagging.DefaultAutoTagLimit
	if config, err := s.factory.SysConfig().GetByKey(ctx, "auto_tag_enabled"); err == nil {
		enabled = config.Value == "true"
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if config, err := s.factory.SysConfig().GetByKey(ctx, "auto_tag_limit"); err == nil {
		if parsed, parseErr := strconv.Atoi(config.Value); parseErr == nil && parsed >= 1 && parsed <= 100 {
			limit = parsed
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	s.autoEnabled.Store(enabled)
	s.autoLimit.Store(int64(limit))
	return nil
}

func (s *TagService) loadActiveRuleSet(ctx context.Context) (*models.TagRuleSet, error) {
	var ruleSet models.TagRuleSet
	err := s.factory.DB().WithContext(ctx).Preload("Rules", func(db *gorm.DB) *gorm.DB {
		return db.Order("priority DESC, id ASC")
	}).Where("status = ?", models.TagRuleSetActive).
		Order("version DESC").First(&ruleSet).Error
	if err != nil {
		return nil, err
	}
	return &ruleSet, nil
}

func (s *TagService) reloadGlobalRules(ctx context.Context, force bool) error {
	ruleSet, err := s.loadActiveRuleSet(ctx)
	if err != nil {
		return fmt.Errorf("加载活动全局标签规则失败: %w", err)
	}
	current := s.globalRuntime.Load()
	if !force && !s.degraded.Load() && current != nil && current.snapshot.GlobalVersion == ruleSet.Version && current.snapshot.Limit == int(s.autoLimit.Load()) {
		return nil
	}
	started := time.Now()
	snapshot, err := tagging.CompileSnapshot(*ruleSet, int(s.autoLimit.Load()))
	if err != nil {
		return err
	}
	s.runtimeMu.Lock()
	current = s.globalRuntime.Load()
	if current != nil && current.snapshot != nil && current.snapshot.GlobalVersion > snapshot.GlobalVersion {
		s.runtimeMu.Unlock()
		return nil
	}
	s.globalRuntime.Store(&globalTagRuntime{ruleSet: ruleSet, snapshot: snapshot})
	s.degraded.Store(false)
	s.degradedReason.Store("")
	s.runtimeMu.Unlock()
	s.markRuntimeReady()
	logger.LOG.Info("标签规则快照已加载", "version", snapshot.GlobalVersion, "duration", time.Since(started))
	return nil
}

func (s *TagService) snapshot() (*tagging.Snapshot, error) {
	runtime := s.globalRuntime.Load()
	if runtime == nil || runtime.snapshot == nil {
		return nil, errors.New("全局标签规则尚未加载")
	}
	return runtime.snapshot, nil
}
