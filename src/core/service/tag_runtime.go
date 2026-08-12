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
// SearchTerms 使用与自动标签相同的全局和个人词典拆分普通搜索词。
// 调用方在一次查询中固定使用返回结果，避免热切换时观察到两个规则版本。
func (s *TagService) SearchTerms(ctx context.Context, userID, keyword string) ([]string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	snapshot, err := s.snapshotForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	terms := snapshot.TokenizeQuery(keyword)
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

func (s *TagService) loadActiveRuleSet(ctx context.Context, scopeType, scopeID string) (*models.TagRuleSet, error) {
	var ruleSet models.TagRuleSet
	err := s.factory.DB().WithContext(ctx).Preload("Rules", func(db *gorm.DB) *gorm.DB {
		return db.Order("priority DESC, id ASC")
	}).Where("scope_type = ? AND scope_id = ? AND status = ?", scopeType, scopeID, models.TagRuleSetActive).
		Order("version DESC").First(&ruleSet).Error
	if err != nil {
		return nil, err
	}
	return &ruleSet, nil
}

func (s *TagService) reloadGlobalRules(ctx context.Context, force bool) error {
	ruleSet, err := s.loadActiveRuleSet(ctx, models.TagRuleScopeGlobal, "")
	if err != nil {
		return fmt.Errorf("加载活动全局标签规则失败: %w", err)
	}
	current := s.globalRuntime.Load()
	if !force && !s.degraded.Load() && current != nil && current.snapshot.GlobalVersion == ruleSet.Version && current.snapshot.Limit == int(s.autoLimit.Load()) {
		return nil
	}
	started := time.Now()
	snapshot, err := tagging.CompileSnapshot([]models.TagRuleSet{*ruleSet}, int(s.autoLimit.Load()))
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
	s.clearUserCache()
	s.markRuntimeReady()
	logger.LOG.Info("标签规则快照已加载", "version", snapshot.GlobalVersion, "duration", time.Since(started))
	return nil
}

func (s *TagService) clearUserCache() {
	s.userCacheMu.Lock()
	s.userCache = make(map[string]cachedUserSnapshot)
	s.userCacheMu.Unlock()
}

func (s *TagService) invalidateUserCache(userID string) {
	s.userCacheMu.Lock()
	delete(s.userCache, userID)
	s.userCacheMu.Unlock()
}

func (s *TagService) snapshotForUser(ctx context.Context, userID string) (*tagging.Snapshot, error) {
	runtime := s.globalRuntime.Load()
	if runtime == nil || runtime.snapshot == nil || runtime.ruleSet == nil {
		return nil, errors.New("全局标签规则尚未加载")
	}
	global := runtime.snapshot
	globalRules := runtime.ruleSet
	personal, err := s.loadActiveRuleSet(ctx, models.TagRuleScopeUser, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return global, nil
	}
	if err != nil {
		return nil, err
	}
	s.userCacheMu.RLock()
	cached, exists := s.userCache[userID]
	s.userCacheMu.RUnlock()
	if exists && cached.globalVersion == global.GlobalVersion && cached.userVersion == personal.Version {
		return cached.snapshot, nil
	}
	snapshot, err := tagging.CompileSnapshot([]models.TagRuleSet{*globalRules, *personal}, int(s.autoLimit.Load()))
	if err != nil {
		return nil, err
	}
	s.userCacheMu.Lock()
	s.userCache[userID] = cachedUserSnapshot{
		globalVersion: global.GlobalVersion, userVersion: personal.Version, snapshot: snapshot,
	}
	s.userCacheMu.Unlock()
	return snapshot, nil
}
