package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/internal/repository/impl"
	"myobj/src/pkg/logger"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

var (
	errStaleTagGeneration = errors.New("标签生成任务已过期")
	errAutoTagDisabled    = errors.New("自动标签已关闭")
)

type cachedUserSnapshot struct {
	globalVersion int64
	userVersion   int64
	snapshot      *tagging.Snapshot
}

type globalTagRuntime struct {
	ruleSet  *models.TagRuleSet
	snapshot *tagging.Snapshot
}

type tagRebuildGuard struct {
	jobID    string
	runToken string
}

// TagService 管理规则热更新、文件标签以及持久化重建任务。
type TagService struct {
	factory          *impl.RepositoryFactory
	globalRuntime    atomic.Pointer[globalTagRuntime]
	runtimeMu        sync.Mutex
	autoEnabled      atomic.Bool
	autoLimit        atomic.Int64
	runtimeReady     chan struct{}
	runtimeReadyOnce sync.Once
	userCacheMu      sync.RWMutex
	userCache        map[string]cachedUserSnapshot
	wake             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	started          atomic.Bool
	degraded         atomic.Bool
	degradedReason   atomic.Value
}

func NewTagService(factory *impl.RepositoryFactory) (*TagService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &TagService{
		factory: factory, userCache: make(map[string]cachedUserSnapshot),
		wake: make(chan struct{}, 1), runtimeReady: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
	service.autoEnabled.Store(true)
	service.autoLimit.Store(tagging.DefaultAutoTagLimit)
	service.degradedReason.Store("")
	return service, nil
}

func (s *TagService) initializeRuntime(ctx context.Context) error {
	if err := s.reloadSettings(ctx); err != nil {
		return err
	}
	if err := s.reloadGlobalRules(ctx, true); err != nil {
		if fallbackErr := s.installFallbackSnapshot(err); fallbackErr != nil {
			return fallbackErr
		}
	}
	return nil
}

func (s *TagService) markRuntimeReady() {
	if s == nil || s.runtimeReady == nil {
		return
	}
	s.runtimeReadyOnce.Do(func() { close(s.runtimeReady) })
}

func (s *TagService) waitForRuntime() bool {
	if s == nil {
		return false
	}
	if s.runtimeReady == nil {
		return s.globalRuntime.Load() != nil
	}
	select {
	case <-s.runtimeReady:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *TagService) installFallbackSnapshot(cause error) error {
	if s.globalRuntime.Load() != nil {
		return nil
	}
	now := time.Now()
	ruleSet := &models.TagRuleSet{
		ID: "builtin-fallback", ScopeType: models.TagRuleScopeGlobal, ScopeID: "",
		Version: 0, Revision: 1, Status: models.TagRuleSetActive,
		CreatedBy: "system", CreatedAt: now, UpdatedAt: now,
	}
	snapshot, err := tagging.CompileSnapshot([]models.TagRuleSet{*ruleSet}, int(s.autoLimit.Load()))
	if err != nil {
		return fmt.Errorf("编译内置基础标签规则失败: %w", err)
	}
	s.runtimeMu.Lock()
	if s.globalRuntime.Load() != nil {
		s.runtimeMu.Unlock()
		return nil
	}
	s.globalRuntime.Store(&globalTagRuntime{ruleSet: ruleSet, snapshot: snapshot})
	s.degraded.Store(true)
	s.degradedReason.Store(cause.Error())
	s.runtimeMu.Unlock()
	s.markRuntimeReady()
	logger.LOG.Error("活动标签规则损坏，已启用内置基础规则", "error", cause)
	return nil
}

func (s *TagService) Start() {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	s.wg.Add(4)
	go s.runPendingWorker()
	go s.runRebuildWorker()
	go s.runRulePoller()
	go s.runMetadataWorker()
	s.Notify()
}

func (s *TagService) Close() {
	if s == nil {
		return
	}
	s.cancel()
	if s.started.Load() {
		s.wg.Wait()
	}
}

func (s *TagService) Notify() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *TagService) QueueUserFile(ctx context.Context, db *gorm.DB, userID, ufID string) error {
	if err := tagging.QueueUserFile(ctx, db, userID, ufID); err != nil {
		return err
	}
	s.Notify()
	return nil
}

// RetryUserFile 将单个用户文件重新放入自动标签队列，不改变手工标签和屏蔽记录。
func (s *TagService) RetryUserFile(ctx context.Context, userID, ufID string) error {
	if err := s.ensureOwnership(ctx, userID, []string{ufID}); err != nil {
		return err
	}
	if err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.QueueUserFile(ctx, tx, userID, ufID)
	}); err != nil {
		return err
	}
	s.Notify()
	return nil
}

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
	}
	if config, err := s.factory.SysConfig().GetByKey(ctx, "auto_tag_limit"); err == nil {
		if parsed, parseErr := strconv.Atoi(config.Value); parseErr == nil && parsed >= 1 && parsed <= 100 {
			limit = parsed
		}
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

func (s *TagService) GenerateUserFile(ctx context.Context, userID, ufID, runToken string, targetVersion int64) error {
	return s.generateUserFile(ctx, userID, ufID, runToken, targetVersion, nil)
}

func (s *TagService) generateUserFile(ctx context.Context, userID, ufID, runToken string, targetVersion int64, rebuildGuard *tagRebuildGuard) error {
	if !s.autoEnabled.Load() {
		return errAutoTagDisabled
	}
	snapshot, err := s.snapshotForUser(ctx, userID)
	if err != nil {
		return err
	}
	if targetVersion > 0 && snapshot.GlobalVersion != targetVersion {
		return errStaleTagGeneration
	}

	var userFile models.UserFiles
	if err := s.factory.DB().WithContext(ctx).Where("user_id = ? AND uf_id = ? AND deleted_at IS NULL", userID, ufID).First(&userFile).Error; err != nil {
		return err
	}
	var fileInfo models.FileInfo
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", userFile.FileID).First(&fileInfo).Error; err != nil {
		return err
	}
	metadata, metadataVersion, metadataPartial := s.loadMetadata(ctx, userFile.FileID, fileInfo.IsEnc)
	candidates := snapshot.Generate(tagging.Input{
		Filename: userFile.FileName, MIME: fileInfo.Mime, Size: int64(fileInfo.Size),
		IsEnc: fileInfo.IsEnc, Metadata: metadata,
	})

	status := models.TagStateReady
	if metadataPartial {
		status = models.TagStatePartial
	}
	now := time.Now()
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if runToken != "" {
			var count int64
			if err := tx.Model(&models.UserFileTagState{}).
				Where("uf_id = ? AND run_token = ?", ufID, runToken).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return errStaleTagGeneration
			}
		}
		if err := validateTagRebuildGuard(tx, rebuildGuard); err != nil {
			return err
		}
		if current := s.globalRuntime.Load(); current == nil || current.snapshot.GlobalVersion != snapshot.GlobalVersion {
			return errStaleTagGeneration
		}
		var activeGlobalVersion int64
		if err := tx.Model(&models.TagRuleSet{}).
			Where("scope_type = ? AND scope_id = '' AND status = ?", models.TagRuleScopeGlobal, models.TagRuleSetActive).
			Select("COALESCE(MAX(version), 0)").Scan(&activeGlobalVersion).Error; err != nil {
			return err
		}
		if activeGlobalVersion != snapshot.GlobalVersion {
			return errStaleTagGeneration
		}
		var activeUserVersion int64
		if err := tx.Model(&models.TagRuleSet{}).
			Where("scope_type = ? AND scope_id = ? AND status = ?", models.TagRuleScopeUser, userID, models.TagRuleSetActive).
			Select("COALESCE(MAX(version), 0)").Scan(&activeUserVersion).Error; err != nil {
			return err
		}
		if activeUserVersion != snapshot.UserVersion {
			return errStaleTagGeneration
		}
		if err := tx.Where("user_id = ? AND uf_id = ? AND source_type <> ?", userID, ufID, models.TagSourceManual).
			Delete(&models.UserFileTag{}).Error; err != nil {
			return err
		}
		var excluded []string
		if err := tx.Model(&models.UserFileTagExclusion{}).Where("user_id = ? AND uf_id = ?", userID, ufID).
			Pluck("tag_id", &excluded).Error; err != nil {
			return err
		}
		excludedSet := make(map[string]struct{}, len(excluded))
		for _, id := range excluded {
			excludedSet[id] = struct{}{}
		}
		for _, candidate := range candidates {
			tag, tagErr := ensureTagDefinition(tx, candidate.Name, candidate.CategoryID)
			if tagErr != nil {
				return tagErr
			}
			if _, suppressed := excludedSet[tag.ID]; suppressed {
				continue
			}
			binding := &models.UserFileTag{
				ID: uuid.NewString(), UserID: userID, UFID: ufID, TagID: tag.ID,
				SourceType: candidate.SourceType, SourceKey: candidate.SourceKey,
				RuleVersion: snapshot.GlobalVersion, Visibility: models.TagVisibilityInherit,
				CreatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(binding).Error; err != nil {
				return err
			}
		}
		updates := map[string]interface{}{
			"global_version": snapshot.GlobalVersion, "user_version": snapshot.UserVersion,
			"metadata_version": metadataVersion, "status": status, "last_error": "",
			"retry_count": 0, "next_retry_at": nil, "run_token": "", "lease_expires_at": nil,
			"generated_at": now, "updated_at": now,
		}
		query := tx.Model(&models.UserFileTagState{}).Where("uf_id = ?", ufID)
		if runToken != "" {
			query = query.Where("run_token = ?", runToken)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errStaleTagGeneration
		}
		return nil
	})
	if err == nil {
		logger.LOG.Info("自动标签生成完成", "uf_id", ufID, "global_version", snapshot.GlobalVersion,
			"user_version", snapshot.UserVersion, "generated", len(candidates), "status", status)
	}
	return err
}

func validateTagRebuildGuard(tx *gorm.DB, guard *tagRebuildGuard) error {
	if guard == nil {
		return nil
	}
	var count int64
	if err := tx.Model(&models.TagRebuildJob{}).
		Where("id = ? AND run_token = ? AND status = ?", guard.jobID, guard.runToken, "running").
		Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errStaleTagGeneration
	}
	return nil
}

func (s *TagService) loadMetadata(ctx context.Context, fileID string, encrypted bool) (map[string]string, int64, bool) {
	var entries []models.FileMetadata
	_ = s.factory.DB().WithContext(ctx).Where("file_id = ?", fileID).Find(&entries).Error
	metadata := make(map[string]string, len(entries))
	var version int64
	for _, entry := range entries {
		metadata[entry.Key] = entry.Value
		version = max(version, entry.Version)
	}
	var state models.FileMetadataState
	err := s.factory.DB().WithContext(ctx).Where("file_id = ?", fileID).First(&state).Error
	partial := encrypted && len(entries) == 0
	if err == nil {
		version = max(version, state.Version)
		partial = state.Status == models.TagStatePartial || state.Status == models.TagStateFailed
	}
	return metadata, version, partial
}

func (s *TagService) markTagState(ctx context.Context, ufID, runToken, status, message string, globalVersion, userVersion, metadataVersion int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status": status, "last_error": message, "global_version": globalVersion,
		"user_version": userVersion, "metadata_version": metadataVersion,
		"run_token": "", "lease_expires_at": nil, "updated_at": now,
	}
	query := s.factory.DB().WithContext(ctx).Model(&models.UserFileTagState{}).Where("uf_id = ?", ufID)
	if runToken != "" {
		query = query.Where("run_token = ?", runToken)
	}
	return query.Updates(updates).Error
}

func ensureTagDefinition(tx *gorm.DB, name, categoryID string) (*models.TagDefinition, error) {
	name = tagging.DisplayName(name)
	if !tagging.ValidTagName(name) {
		return nil, fmt.Errorf("标签名称无效")
	}
	if categoryID == "" {
		categoryID = "other"
	}
	var categoryCount int64
	if err := tx.Model(&models.TagCategory{}).Where("id = ? AND enabled = ?", categoryID, true).Count(&categoryCount).Error; err != nil {
		return nil, err
	}
	if categoryCount == 0 {
		categoryID = "other"
	}
	normalized := tagging.Normalize(name)
	var existing models.TagDefinition
	err := tx.Where("normalized_name = ? AND category_id = ?", normalized, categoryID).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	created := &models.TagDefinition{
		ID: uuid.NewString(), Name: name, NormalizedName: normalized,
		CategoryID: categoryID, CreatedAt: time.Now(),
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(created).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("normalized_name = ? AND category_id = ?", normalized, categoryID).First(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func ensureDirectoryTagDefinition(tx *gorm.DB, name, categoryID string) (*models.TagDefinition, error) {
	if tagging.Normalize(name) != tagging.Normalize(models.TagNameCinemaMode) {
		return ensureTagDefinition(tx, name, categoryID)
	}
	var existing models.TagDefinition
	err := tx.Where("system_code = ?", models.TagSystemCodeCinemaMode).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = tx.Where("normalized_name = ?", tagging.Normalize(models.TagNameCinemaMode)).
			Order("CASE WHEN category_id = 'other' THEN 0 ELSE 1 END, created_at ASC, id ASC").First(&existing).Error
	}
	if err == nil {
		if updateErr := tx.Model(&models.TagDefinition{}).Where("id = ?", existing.ID).
			Updates(map[string]interface{}{"system_code": models.TagSystemCodeCinemaMode, "builtin": true}).Error; updateErr != nil {
			return nil, updateErr
		}
		code := models.TagSystemCodeCinemaMode
		existing.SystemCode = &code
		existing.Builtin = true
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	created, err := ensureTagDefinition(tx, models.TagNameCinemaMode, "other")
	if err != nil {
		return nil, err
	}
	if err := tx.Model(&models.TagDefinition{}).Where("id = ?", created.ID).
		Updates(map[string]interface{}{"system_code": models.TagSystemCodeCinemaMode, "builtin": true}).Error; err != nil {
		return nil, err
	}
	code := models.TagSystemCodeCinemaMode
	created.SystemCode = &code
	created.Builtin = true
	return created, nil
}

type compactTagRow struct {
	UFID         string
	ID           string
	Name         string
	CategoryCode string
	Color        string
	Visibility   string
	SourceType   string
	SystemCode   string
}

func (s *TagService) CompactTags(ctx context.Context, userID string, ufIDs []string, publicOnly bool) (map[string][]response.CompactTagView, error) {
	result := make(map[string][]response.CompactTagView, len(ufIDs))
	if len(ufIDs) == 0 {
		return result, nil
	}
	query := s.factory.DB().WithContext(ctx).Table("user_file_tag AS uft").
		Select("uft.uf_id, td.id, td.name, tc.code AS category_code, tc.color, uft.visibility, uft.source_type, td.system_code").
		Joins("JOIN tag_definition td ON td.id = uft.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("uft.uf_id IN ?", ufIDs).
		Where("NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)")
	if userID != "" {
		query = query.Where("uft.user_id = ?", userID)
	}
	if publicOnly {
		query = query.Where("uft.source_type <> ? OR uft.visibility = ?", models.TagSourceManual, models.TagVisibilityPublic)
	}
	var rows []compactTagRow
	if err := query.Order("tc.sort_order ASC, td.name ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, row := range rows {
		key := row.UFID + "\x00" + row.ID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result[row.UFID] = append(result[row.UFID], response.CompactTagView{
			ID: row.ID, Name: row.Name, CategoryCode: row.CategoryCode,
			Color: row.Color, Visibility: row.Visibility, SystemCode: row.SystemCode,
		})
	}
	return result, nil
}

type compactDirectoryTagRow struct {
	DirectoryID  int
	ID           string
	Name         string
	CategoryCode string
	Color        string
	SystemCode   string
}

func (s *TagService) CompactDirectoryTags(ctx context.Context, userID string, directoryIDs []int) (map[int][]response.CompactTagView, error) {
	result := make(map[int][]response.CompactTagView, len(directoryIDs))
	if len(directoryIDs) == 0 {
		return result, nil
	}
	var rows []compactDirectoryTagRow
	err := s.factory.DB().WithContext(ctx).Table("user_directory_tag AS udt").
		Select("udt.directory_id, td.id, td.name, tc.code AS category_code, tc.color, td.system_code").
		Joins("JOIN tag_definition td ON td.id = udt.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("udt.user_id = ? AND udt.directory_id IN ?", userID, directoryIDs).
		Order("tc.sort_order ASC, td.name ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.DirectoryID] = append(result[row.DirectoryID], response.CompactTagView{
			ID: row.ID, Name: row.Name, CategoryCode: row.CategoryCode,
			Color: row.Color, Visibility: models.TagVisibilityPrivate, SystemCode: row.SystemCode,
		})
	}
	return result, nil
}

func (s *TagService) GetDirectoryTags(ctx context.Context, userID string, directoryID int) (*response.DirectoryTagsResponse, error) {
	if err := s.ensureDirectoryOwnership(ctx, userID, directoryID); err != nil {
		return nil, err
	}
	var rows []detailedTagRow
	err := s.factory.DB().WithContext(ctx).Table("user_directory_tag AS udt").
		Select("td.id, td.name, tc.id AS category_id, tc.code AS category_code, tc.name AS category_name, tc.color").
		Joins("JOIN tag_definition td ON td.id = udt.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("udt.user_id = ? AND udt.directory_id = ?", userID, directoryID).
		Order("tc.sort_order ASC, td.name ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := &response.DirectoryTagsResponse{DirectoryID: directoryID, Tags: make([]response.TagView, 0, len(rows))}
	for _, row := range rows {
		result.Tags = append(result.Tags, response.TagView{
			ID: row.ID, Name: row.Name, Sources: []string{models.TagSourceManual}, Visibility: models.TagVisibilityPrivate,
			Category: response.TagCategoryView{ID: row.CategoryID, Code: row.CategoryCode, Name: row.CategoryName, Color: row.Color},
		})
	}
	return result, nil
}

func (s *TagService) UpdateDirectoryTags(ctx context.Context, userID string, directoryID int, add []request.ManualTagInput, removeTagIDs []string) error {
	if err := s.ensureDirectoryOwnership(ctx, userID, directoryID); err != nil {
		return err
	}
	return s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(removeTagIDs) > 0 {
			if err := tx.Where("user_id = ? AND directory_id = ? AND tag_id IN ?", userID, directoryID, uniqueTagStrings(removeTagIDs)).
				Delete(&models.UserDirectoryTag{}).Error; err != nil {
				return err
			}
		}
		for _, input := range add {
			tag, err := ensureDirectoryTagDefinition(tx, input.Name, input.CategoryID)
			if err != nil {
				return err
			}
			binding := models.UserDirectoryTag{
				ID: uuid.NewString(), UserID: userID, DirectoryID: directoryID, TagID: tag.ID,
				CreatedBy: userID, CreatedAt: time.Now(),
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&binding).Error; err != nil {
				return err
			}
		}
		var count int64
		if err := tx.Model(&models.UserDirectoryTag{}).Where("user_id = ? AND directory_id = ?", userID, directoryID).Count(&count).Error; err != nil {
			return err
		}
		if count > 100 {
			return errors.New("每个文件夹最多允许100个手工标签")
		}
		return nil
	})
}

func (s *TagService) ensureDirectoryOwnership(ctx context.Context, userID string, directoryID int) error {
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.VirtualDirectory{}).
		Where("id = ? AND user_id = ?", directoryID, userID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errors.New("文件夹不存在或无权访问")
	}
	return nil
}

func (s *TagService) IsCinemaDirectory(ctx context.Context, userID string, directoryID int) (bool, error) {
	var count int64
	err := s.factory.DB().WithContext(ctx).Table("user_directory_tag AS udt").
		Joins("JOIN tag_definition td ON td.id = udt.tag_id").
		Where("udt.user_id = ? AND udt.directory_id = ? AND td.system_code = ?", userID, directoryID, models.TagSystemCodeCinemaMode).
		Count(&count).Error
	return count > 0, err
}

func (s *TagService) TagStates(ctx context.Context, ufIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(ufIDs))
	if len(ufIDs) == 0 {
		return result, nil
	}
	var states []models.UserFileTagState
	if err := s.factory.DB().WithContext(ctx).Where("uf_id IN ?", ufIDs).Find(&states).Error; err != nil {
		return nil, err
	}
	for _, state := range states {
		result[state.UFID] = state.Status
	}
	return result, nil
}

type detailedTagRow struct {
	ID           string
	Name         string
	CategoryID   string
	CategoryCode string
	CategoryName string
	Color        string
	SourceType   string
	Visibility   string
}

func (s *TagService) GetFileTags(ctx context.Context, userID, ufID string) (*response.FileTagsResponse, error) {
	if err := s.ensureOwnership(ctx, userID, []string{ufID}); err != nil {
		return nil, err
	}
	var rows []detailedTagRow
	err := s.factory.DB().WithContext(ctx).Table("user_file_tag AS uft").
		Select("td.id, td.name, tc.id AS category_id, tc.code AS category_code, tc.name AS category_name, tc.color, uft.source_type, uft.visibility").
		Joins("JOIN tag_definition td ON td.id = uft.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("uft.user_id = ? AND uft.uf_id = ?", userID, ufID).
		Order("tc.sort_order ASC, td.name ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	aggregated := map[string]*response.TagView{}
	for _, row := range rows {
		view := aggregated[row.ID]
		if view == nil {
			view = &response.TagView{
				ID: row.ID, Name: row.Name,
				Category:   response.TagCategoryView{ID: row.CategoryID, Code: row.CategoryCode, Name: row.CategoryName, Color: row.Color},
				Visibility: row.Visibility,
			}
			aggregated[row.ID] = view
		}
		view.Sources = appendUnique(view.Sources, row.SourceType)
		if row.SourceType != models.TagSourceManual {
			view.Automatic = true
		}
		if row.SourceType == models.TagSourceManual {
			view.Visibility = row.Visibility
		}
	}
	responseData := &response.FileTagsResponse{FileID: ufID, Tags: make([]response.TagView, 0, len(aggregated)), Suppressed: []response.TagView{}}
	for _, tag := range aggregated {
		responseData.Tags = append(responseData.Tags, *tag)
	}
	sort.Slice(responseData.Tags, func(i, j int) bool { return responseData.Tags[i].Name < responseData.Tags[j].Name })

	var suppressed []detailedTagRow
	if err := s.factory.DB().WithContext(ctx).Table("user_file_tag_exclusion AS e").
		Select("td.id, td.name, tc.id AS category_id, tc.code AS category_code, tc.name AS category_name, tc.color").
		Joins("JOIN tag_definition td ON td.id = e.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("e.user_id = ? AND e.uf_id = ?", userID, ufID).Scan(&suppressed).Error; err != nil {
		return nil, err
	}
	for _, row := range suppressed {
		responseData.Suppressed = append(responseData.Suppressed, response.TagView{
			ID: row.ID, Name: row.Name, Automatic: true, Suppressed: true,
			Category: response.TagCategoryView{ID: row.CategoryID, Code: row.CategoryCode, Name: row.CategoryName, Color: row.Color},
		})
	}
	var state models.UserFileTagState
	if err := s.factory.DB().WithContext(ctx).Where("uf_id = ?", ufID).First(&state).Error; err == nil {
		responseData.State = state.Status
		responseData.LastError = state.LastError
		responseData.UpdatedAt = state.UpdatedAt
	}
	return responseData, nil
}

func (s *TagService) UpdateManualTags(ctx context.Context, userID string, fileIDs []string, add []request.ManualTagInput, removeTagIDs []string) error {
	if len(fileIDs) == 0 || len(fileIDs) > 100 {
		return errors.New("文件数量必须在1到100之间")
	}
	if err := s.ensureOwnership(ctx, userID, fileIDs); err != nil {
		return err
	}
	return s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, ufID := range fileIDs {
			if len(removeTagIDs) > 0 {
				if err := tx.Where("user_id = ? AND uf_id = ? AND source_type = ? AND tag_id IN ?", userID, ufID, models.TagSourceManual, removeTagIDs).
					Delete(&models.UserFileTag{}).Error; err != nil {
					return err
				}
			}
			for _, input := range add {
				visibility := input.Visibility
				if visibility == "" {
					visibility = models.TagVisibilityPrivate
				}
				if visibility != models.TagVisibilityPrivate && visibility != models.TagVisibilityPublic {
					return errors.New("手工标签公开范围无效")
				}
				tag, err := ensureTagDefinition(tx, input.Name, input.CategoryID)
				if err != nil {
					return err
				}
				binding := models.UserFileTag{
					ID: uuid.NewString(), UserID: userID, UFID: ufID, TagID: tag.ID,
					SourceType: models.TagSourceManual, SourceKey: "user", Visibility: visibility,
					CreatedBy: userID, CreatedAt: time.Now(),
				}
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "uf_id"}, {Name: "tag_id"}, {Name: "source_type"}, {Name: "source_key"}},
					DoUpdates: clause.Assignments(map[string]interface{}{"visibility": visibility}),
				}).Create(&binding).Error; err != nil {
					return err
				}
			}
			var manualCount int64
			if err := tx.Model(&models.UserFileTag{}).Where("user_id = ? AND uf_id = ? AND source_type = ?", userID, ufID, models.TagSourceManual).
				Distinct("tag_id").Count(&manualCount).Error; err != nil {
				return err
			}
			if manualCount > 100 {
				return errors.New("每个文件最多允许100个手工标签")
			}
		}
		return nil
	})
}

func (s *TagService) UpdateExclusions(ctx context.Context, userID, ufID string, suppress, restore []string) error {
	if err := s.ensureOwnership(ctx, userID, []string{ufID}); err != nil {
		return err
	}
	err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, tagID := range suppress {
			var autoCount int64
			if err := tx.Model(&models.UserFileTag{}).Where("user_id = ? AND uf_id = ? AND tag_id = ? AND source_type <> ?", userID, ufID, tagID, models.TagSourceManual).Count(&autoCount).Error; err != nil {
				return err
			}
			if autoCount == 0 {
				continue
			}
			exclusion := models.UserFileTagExclusion{UserID: userID, UFID: ufID, TagID: tagID, CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&exclusion).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ? AND uf_id = ? AND tag_id = ? AND source_type <> ?", userID, ufID, tagID, models.TagSourceManual).
				Delete(&models.UserFileTag{}).Error; err != nil {
				return err
			}
		}
		if len(restore) > 0 {
			if err := tx.Where("user_id = ? AND uf_id = ? AND tag_id IN ?", userID, ufID, restore).
				Delete(&models.UserFileTagExclusion{}).Error; err != nil {
				return err
			}
			if err := tagging.QueueUserFile(ctx, tx, userID, ufID); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && len(restore) > 0 {
		s.Notify()
	}
	return err
}

func (s *TagService) Suggestions(ctx context.Context, userID, keyword string, tagIDs []string, scope string, limit int) ([]response.CompactTagView, error) {
	return s.SuggestionsForTarget(ctx, userID, keyword, tagIDs, scope, "file", limit)
}

func (s *TagService) SuggestionsForTarget(ctx context.Context, userID, keyword string, tagIDs []string, scope, target string, limit int) ([]response.CompactTagView, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "user"
	}
	if scope != "user" && scope != "public" {
		return nil, errors.New("标签建议范围仅支持 user 或 public")
	}
	if target == "" {
		target = "file"
	}
	if target != "file" && target != "directory" {
		return nil, errors.New("标签建议目标仅支持 file 或 directory")
	}
	tagIDs = uniqueTagStrings(tagIDs)
	if len(tagIDs) > maxFileTagFilterCount {
		return nil, fmt.Errorf("标签ID最多允许%d项", maxFileTagFilterCount)
	}
	if len(tagIDs) > 0 {
		limit = len(tagIDs)
	} else if limit < 1 || limit > 50 {
		limit = 20
	}
	type suggestionRow struct {
		ID           string
		Name         string
		CategoryCode string
		Color        string
		SystemCode   string
	}
	query := s.factory.DB().WithContext(ctx).Table("tag_definition td").Distinct().
		Select("td.id, td.name, tc.code AS category_code, tc.color, td.system_code").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").Where("tc.enabled = ?", true)
	if target == "directory" {
		query = query.Where(`td.builtin = ? OR EXISTS (
			SELECT 1 FROM user_file_tag uft JOIN user_files uf ON uf.user_id = uft.user_id AND uf.uf_id = uft.uf_id
			WHERE uft.tag_id = td.id AND uft.user_id = ? AND uf.deleted_at IS NULL
		) OR EXISTS (
			SELECT 1 FROM user_directory_tag udt WHERE udt.tag_id = td.id AND udt.user_id = ?
		)`, true, userID, userID)
	} else if scope == "public" {
		query = query.Joins("JOIN user_file_tag uft ON uft.tag_id = td.id").
			Joins("JOIN user_files uf ON uf.user_id = uft.user_id AND uf.uf_id = uft.uf_id").
			Where("uf.deleted_at IS NULL").
			Where("NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)")
		query = query.Where("uf.public = ? AND (uft.source_type <> ? OR uft.visibility = ?)",
			true, models.TagSourceManual, models.TagVisibilityPublic)
	} else {
		query = query.Joins("JOIN user_file_tag uft ON uft.tag_id = td.id").
			Joins("JOIN user_files uf ON uf.user_id = uft.user_id AND uf.uf_id = uft.uf_id").
			Where("uf.deleted_at IS NULL").
			Where("NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)")
		query = query.Where("uft.user_id = ? OR (uf.public = ? AND (uft.source_type <> ? OR uft.visibility = ?))",
			userID, true, models.TagSourceManual, models.TagVisibilityPublic)
	}
	if len(tagIDs) > 0 {
		query = query.Where("td.id IN ?", tagIDs)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("td.normalized_name LIKE ?", "%"+tagging.Normalize(keyword)+"%")
	}
	var rows []suggestionRow
	if err := query.Order("td.name ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]response.CompactTagView, 0, len(rows))
	for _, row := range rows {
		result = append(result, response.CompactTagView{ID: row.ID, Name: row.Name, CategoryCode: row.CategoryCode, Color: row.Color, SystemCode: row.SystemCode})
	}
	return result, nil
}

func (s *TagService) ensureOwnership(ctx context.Context, userID string, ufIDs []string) error {
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.UserFiles{}).
		Where("user_id = ? AND uf_id IN ? AND deleted_at IS NULL", userID, uniqueTagStrings(ufIDs)).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueTagStrings(ufIDs))) {
		return errors.New("文件不存在或无权访问")
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func uniqueTagStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
