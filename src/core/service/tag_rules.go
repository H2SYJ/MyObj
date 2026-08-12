package service

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

func (s *TagService) ListCategories(ctx context.Context, enabledOnly bool) ([]models.TagCategory, error) {
	var categories []models.TagCategory
	query := s.factory.DB().WithContext(ctx).Order("sort_order ASC, code ASC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	err := query.Find(&categories).Error
	return categories, err
}

func (s *TagService) SaveCategory(ctx context.Context, input request.AdminTagCategoryRequest) (*models.TagCategory, error) {
	input.Code = tagging.Normalize(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	if input.Code == "" || !tagging.ValidTagName(input.Name) {
		return nil, errors.New("标签分类代码或名称无效")
	}
	if input.Color == "" {
		input.Color = "#909399"
	}
	now := time.Now()
	if input.ID == "" {
		category := &models.TagCategory{
			ID: uuid.NewString(), Code: input.Code, Name: input.Name, Color: input.Color,
			SortOrder: input.SortOrder, Enabled: input.Enabled, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.factory.DB().WithContext(ctx).Create(category).Error; err != nil {
			return nil, err
		}
		return category, nil
	}
	var category models.TagCategory
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", input.ID).First(&category).Error; err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name": input.Name, "color": input.Color, "sort_order": input.SortOrder,
		"enabled": input.Enabled, "updated_at": now,
	}
	if !category.Builtin {
		updates["code"] = input.Code
	}
	if err := s.factory.DB().WithContext(ctx).Model(&category).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", input.ID).First(&category).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

func (s *TagService) DeleteCategory(ctx context.Context, id string) error {
	var category models.TagCategory
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", id).First(&category).Error; err != nil {
		return err
	}
	if category.Builtin {
		return errors.New("内置标签分类不能删除")
	}
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagDefinition{}).Where("category_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		if err := s.factory.DB().WithContext(ctx).Model(&models.TagRule{}).Where("category_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
	}
	if count > 0 {
		return errors.New("该分类已被标签或规则使用，不能删除")
	}
	return s.factory.DB().WithContext(ctx).Delete(&category).Error
}

func (s *TagService) GlobalRuleSets(ctx context.Context) ([]models.TagRuleSet, error) {
	var sets []models.TagRuleSet
	err := s.factory.DB().WithContext(ctx).Where("scope_type = ? AND scope_id = ''", models.TagRuleScopeGlobal).
		Order("version DESC, created_at DESC").Find(&sets).Error
	return sets, err
}

func (s *TagService) RuleSet(ctx context.Context, id string) (*models.TagRuleSet, error) {
	var ruleSet models.TagRuleSet
	err := s.factory.DB().WithContext(ctx).Preload("Rules", func(db *gorm.DB) *gorm.DB {
		return db.Order("priority DESC, id ASC")
	}).Where("id = ?", id).First(&ruleSet).Error
	return &ruleSet, err
}

func (s *TagService) CreateGlobalDraft(ctx context.Context, adminID string) (*models.TagRuleSet, error) {
	var draft models.TagRuleSet
	err := s.factory.DB().WithContext(ctx).Preload("Rules").
		Where("scope_type = ? AND scope_id = '' AND status = ?", models.TagRuleScopeGlobal, models.TagRuleSetDraft).
		Order("created_at DESC").First(&draft).Error
	if err == nil {
		return &draft, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	active, err := s.loadActiveRuleSet(ctx, models.TagRuleScopeGlobal, "")
	if err != nil {
		return nil, err
	}
	var maxVersion int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagRuleSet{}).
		Where("scope_type = ? AND scope_id = ''", models.TagRuleScopeGlobal).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	draft = models.TagRuleSet{
		ID: uuid.NewString(), ScopeType: models.TagRuleScopeGlobal, Version: maxVersion + 1,
		Revision: 1, Status: models.TagRuleSetDraft, BasedOnVersion: active.Version,
		CreatedBy: adminID, CreatedAt: now, UpdatedAt: now,
	}
	return &draft, s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&draft).Error; err != nil {
			return err
		}
		rules := cloneRules(active.Rules, draft.ID, now)
		if len(rules) > 0 {
			return tx.Create(&rules).Error
		}
		return nil
	})
}

func (s *TagService) SaveGlobalDraft(ctx context.Context, id string, revision int, inputs []request.TagRuleInput) (*models.TagRuleSet, error) {
	draft, err := s.RuleSet(ctx, id)
	if err != nil {
		return nil, err
	}
	if draft.ScopeType != models.TagRuleScopeGlobal || draft.Status != models.TagRuleSetDraft {
		return nil, errors.New("只能修改全局规则草稿")
	}
	if draft.Revision != revision {
		return nil, fmt.Errorf("规则草稿已被其他操作修改")
	}
	rules, err := buildRules(inputs, draft.ID, false)
	if err != nil {
		return nil, err
	}
	if err := s.validateRuleCategories(ctx, inputs); err != nil {
		return nil, err
	}
	candidate := *draft
	candidate.Rules = rules
	if _, err := tagging.CompileSnapshot([]models.TagRuleSet{candidate}, int(s.autoLimit.Load())); err != nil {
		return nil, err
	}
	now := time.Now()
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.TagRuleSet{}).Where("id = ? AND revision = ? AND status = ?", id, revision, models.TagRuleSetDraft).
			Updates(map[string]interface{}{"revision": revision + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("规则草稿版本冲突")
		}
		if err := tx.Where("rule_set_id = ?", id).Delete(&models.TagRule{}).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			return tx.Create(&rules).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.RuleSet(ctx, id)
}

func (s *TagService) PublishGlobalDraft(ctx context.Context, id, adminID string) (*models.TagRuleSet, *models.TagRebuildJob, error) {
	draft, err := s.RuleSet(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if draft.ScopeType != models.TagRuleScopeGlobal || draft.Status != models.TagRuleSetDraft {
		return nil, nil, errors.New("只能发布全局规则草稿")
	}
	snapshot, err := tagging.CompileSnapshot([]models.TagRuleSet{*draft}, int(s.autoLimit.Load()))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	job := &models.TagRebuildJob{
		ID: uuid.NewString(), ScopeType: models.TagRuleScopeGlobal, TargetVersion: draft.Version,
		Status: "pending", RequestedBy: adminID, CreatedAt: now, UpdatedAt: now,
	}
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.TagRuleSet{}).
			Where("scope_type = ? AND scope_id = '' AND status = ?", models.TagRuleScopeGlobal, models.TagRuleSetActive).
			Updates(map[string]interface{}{"status": models.TagRuleSetArchived, "updated_at": now}).Error; err != nil {
			return err
		}
		result := tx.Model(&models.TagRuleSet{}).Where("id = ? AND status = ?", id, models.TagRuleSetDraft).
			Updates(map[string]interface{}{"status": models.TagRuleSetActive, "published_at": now, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("规则草稿状态已变化")
		}
		if err := tx.Model(&models.TagRebuildJob{}).
			Where("scope_type = ? AND status IN ?", models.TagRuleScopeGlobal, []string{"pending", "running"}).
			Updates(map[string]interface{}{"status": "superseded", "finished_at": now, "updated_at": now, "run_token": "", "lease_expires_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.UserFiles{}).Where("deleted_at IS NULL").Count(&job.Total).Error; err != nil {
			return err
		}
		return tx.Create(job).Error
	})
	if err != nil {
		return nil, nil, err
	}
	draft.Status = models.TagRuleSetActive
	draft.PublishedAt = &now
	s.runtimeMu.Lock()
	s.globalRuntime.Store(&globalTagRuntime{ruleSet: draft, snapshot: snapshot})
	s.degraded.Store(false)
	s.degradedReason.Store("")
	s.runtimeMu.Unlock()
	s.clearUserCache()
	s.markRuntimeReady()
	s.notifyRules()
	s.notifyRebuild()
	return draft, job, nil
}

func (s *TagService) RollbackGlobalRules(ctx context.Context, sourceID, adminID string) (*models.TagRuleSet, *models.TagRebuildJob, error) {
	source, err := s.RuleSet(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}
	if source.ScopeType != models.TagRuleScopeGlobal {
		return nil, nil, errors.New("只能回滚全局规则版本")
	}
	var maxVersion int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagRuleSet{}).
		Where("scope_type = ? AND scope_id = ''", models.TagRuleScopeGlobal).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return nil, nil, err
	}
	now := time.Now()
	draft := &models.TagRuleSet{
		ID: uuid.NewString(), ScopeType: models.TagRuleScopeGlobal, Version: maxVersion + 1,
		Revision: 1, Status: models.TagRuleSetDraft, BasedOnVersion: source.Version,
		CreatedBy: adminID, CreatedAt: now, UpdatedAt: now,
	}
	rules := cloneRules(source.Rules, draft.ID, now)
	if err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(draft).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			return tx.Create(&rules).Error
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	draft.Rules = rules
	return s.PublishGlobalDraft(ctx, draft.ID, adminID)
}

func (s *TagService) PersonalDictionary(ctx context.Context, userID string) (*models.TagRuleSet, error) {
	set, err := s.loadActiveRuleSet(ctx, models.TagRuleScopeUser, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.TagRuleSet{ScopeType: models.TagRuleScopeUser, ScopeID: userID, Status: models.TagRuleSetActive, Rules: []models.TagRule{}}, nil
	}
	return set, err
}

func (s *TagService) SavePersonalDictionary(ctx context.Context, userID string, inputs []request.TagRuleInput) (*models.TagRuleSet, *models.TagRebuildJob, error) {
	ruleSet, job, err := s.savePersonalDictionary(ctx, userID, inputs, s.factory.DB())
	if err != nil {
		return nil, nil, err
	}
	s.afterPersonalDictionarySaved(userID)
	return ruleSet, job, nil
}

func (s *TagService) savePersonalDictionary(ctx context.Context, userID string, inputs []request.TagRuleInput, db *gorm.DB) (*models.TagRuleSet, *models.TagRebuildJob, error) {
	var maxVersion int64
	if err := db.WithContext(ctx).Model(&models.TagRuleSet{}).
		Where("scope_type = ? AND scope_id = ?", models.TagRuleScopeUser, userID).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return nil, nil, err
	}
	now := time.Now()
	ruleSet := &models.TagRuleSet{
		ID: uuid.NewString(), ScopeType: models.TagRuleScopeUser, ScopeID: userID,
		Version: maxVersion + 1, Revision: 1, Status: models.TagRuleSetActive,
		CreatedBy: userID, CreatedAt: now, UpdatedAt: now, PublishedAt: &now,
	}
	rules, err := buildRules(inputs, ruleSet.ID, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.validateRuleCategories(ctx, inputs); err != nil {
		return nil, nil, err
	}
	ruleSet.Rules = rules
	runtime := s.globalRuntime.Load()
	if runtime == nil || runtime.ruleSet == nil {
		return nil, nil, errors.New("全局标签规则尚未加载")
	}
	if _, err := tagging.CompileSnapshot([]models.TagRuleSet{*runtime.ruleSet, *ruleSet}, int(s.autoLimit.Load())); err != nil {
		return nil, nil, err
	}
	job := &models.TagRebuildJob{
		ID: uuid.NewString(), ScopeType: models.TagRuleScopeUser, ScopeID: userID,
		TargetVersion: ruleSet.Version, Status: "pending", RequestedBy: userID,
		CreatedAt: now, UpdatedAt: now,
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.TagRuleSet{}).
			Where("scope_type = ? AND scope_id = ? AND status = ?", models.TagRuleScopeUser, userID, models.TagRuleSetActive).
			Updates(map[string]interface{}{"status": models.TagRuleSetArchived, "updated_at": now}).Error; err != nil {
			return err
		}
		// 规则由下方显式批量写入；这里跳过关联，避免 GORM 自动保存 Rules 后再次插入同一主键。
		if err := tx.Omit("Rules").Create(ruleSet).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			if err := tx.Create(&rules).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&models.TagRebuildJob{}).
			Where("scope_type = ? AND scope_id = ? AND status IN ?", models.TagRuleScopeUser, userID, []string{"pending", "running"}).
			Updates(map[string]interface{}{"status": "superseded", "finished_at": now, "updated_at": now, "run_token": "", "lease_expires_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.UserFiles{}).Where("user_id = ? AND deleted_at IS NULL", userID).Count(&job.Total).Error; err != nil {
			return err
		}
		return tx.Create(job).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return ruleSet, job, nil
}

func (s *TagService) afterPersonalDictionarySaved(userID string) {
	s.invalidateUserCache(userID)
	s.notifyRules()
	s.notifyRebuild()
}

func (s *TagService) PreviewRules(ctx context.Context, userID string, samples []string, inputs []request.TagRuleInput, personal bool) ([]response.TagPreviewItem, error) {
	if len(samples) == 0 || len(samples) > 100 {
		return nil, errors.New("预览样例数量必须在1到100之间")
	}
	scopeType, scopeID := models.TagRuleScopeGlobal, ""
	if personal {
		scopeType, scopeID = models.TagRuleScopeUser, userID
	}
	ruleSet := models.TagRuleSet{ID: "preview", ScopeType: scopeType, ScopeID: scopeID, Version: 1}
	rules, err := buildRules(inputs, ruleSet.ID, personal)
	if err != nil {
		return nil, err
	}
	if err := s.validateRuleCategories(ctx, inputs); err != nil {
		return nil, err
	}
	ruleSet.Rules = rules
	sets := []models.TagRuleSet{ruleSet}
	if personal {
		runtime := s.globalRuntime.Load()
		if runtime == nil || runtime.ruleSet == nil {
			return nil, errors.New("全局标签规则尚未加载")
		}
		sets = []models.TagRuleSet{*runtime.ruleSet, ruleSet}
	}
	snapshot, err := tagging.CompileSnapshot(sets, int(s.autoLimit.Load()))
	if err != nil {
		return nil, err
	}
	categories, err := s.ListCategories(ctx, false)
	if err != nil {
		return nil, err
	}
	categoryMap := make(map[string]models.TagCategory, len(categories))
	for _, category := range categories {
		categoryMap[category.ID] = category
	}
	result := make([]response.TagPreviewItem, 0, len(samples))
	for _, sample := range samples {
		mimeType := mime.TypeByExtension(filepath.Ext(sample))
		candidates := snapshot.Generate(tagging.Input{Filename: sample, MIME: mimeType})
		item := response.TagPreviewItem{Input: sample, Tags: make([]response.CompactTagView, 0, len(candidates))}
		for _, candidate := range candidates {
			category := categoryMap[candidate.CategoryID]
			item.Tags = append(item.Tags, response.CompactTagView{
				Name: candidate.Name, CategoryCode: category.Code, Color: category.Color,
			})
		}
		result = append(result, item)
	}
	return result, nil
}

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
		if _, err := s.CreateRebuildJob(ctx, models.TagRuleScopeGlobal, "", runtime.snapshot.GlobalVersion, "settings"); err != nil {
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

func (s *TagService) CreateRebuildJob(ctx context.Context, scopeType, scopeID string, targetVersion int64, requestedBy string) (*models.TagRebuildJob, error) {
	job, err := s.createRebuildJob(ctx, scopeType, scopeID, targetVersion, requestedBy, s.factory.DB())
	if err != nil {
		return nil, err
	}
	s.notifyRebuild()
	return job, nil
}

func (s *TagService) createRebuildJob(ctx context.Context, scopeType, scopeID string, targetVersion int64, requestedBy string, db *gorm.DB) (*models.TagRebuildJob, error) {
	now := time.Now()
	job := &models.TagRebuildJob{
		ID: uuid.NewString(), ScopeType: scopeType, ScopeID: scopeID, TargetVersion: targetVersion,
		Status: "pending", RequestedBy: requestedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		unfinished := tx.Model(&models.TagRebuildJob{}).
			Where("scope_type = ? AND scope_id = ? AND status IN ?", scopeType, scopeID, []string{"pending", "running"})
		if err := unfinished.Updates(map[string]interface{}{
			"status": "superseded", "finished_at": now, "updated_at": now,
			"run_token": "", "lease_expires_at": nil,
		}).Error; err != nil {
			return err
		}
		query := tx.Model(&models.UserFiles{}).Where("deleted_at IS NULL")
		if scopeType == models.TagRuleScopeUser {
			query = query.Where("user_id = ?", scopeID)
		}
		if err := query.Count(&job.Total).Error; err != nil {
			return err
		}
		return tx.Create(job).Error
	}); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *TagService) RebuildJobs(ctx context.Context, limit int) ([]models.TagRebuildJob, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var jobs []models.TagRebuildJob
	err := s.factory.DB().WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

func (s *TagService) CancelRebuildJob(ctx context.Context, id string) error {
	now := time.Now()
	result := s.factory.DB().WithContext(ctx).Model(&models.TagRebuildJob{}).
		Where("id = ? AND status IN ?", id, []string{"pending", "running"}).
		Updates(map[string]interface{}{"status": "cancelled", "finished_at": now, "run_token": "", "lease_expires_at": nil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("重建任务不存在或当前状态不可取消")
	}
	return nil
}

func (s *TagService) RetryRebuildJob(ctx context.Context, id string) error {
	now := time.Now()
	err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.TagRebuildJob{}).
			Where("id = ? AND status IN ?", id, []string{"failed", "completed_with_errors", "cancelled"}).
			Updates(map[string]interface{}{
				"status": "pending", "cursor_value": "", "processed": 0, "succeeded": 0, "failed": 0,
				"last_error": "", "run_token": "", "lease_expires_at": nil, "started_at": nil, "finished_at": nil, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("重建任务不存在或当前状态不可重试")
		}
		return tx.Where("job_id = ?", id).Delete(&models.TagRebuildFailure{}).Error
	})
	if err == nil {
		s.notifyRebuild()
	}
	return err
}

func buildRules(inputs []request.TagRuleInput, ruleSetID string, personal bool) ([]models.TagRule, error) {
	now := time.Now()
	rules := make([]models.TagRule, 0, len(inputs))
	for _, input := range inputs {
		if containsControl(input.Pattern) || containsControl(input.Replacement) {
			return nil, errors.New("规则内容不能包含控制字符或 BOM")
		}
		if personal && input.Type != models.TagRuleTypeWord && input.Type != models.TagRuleTypeStopWord && input.Type != models.TagRuleTypeAlias {
			return nil, errors.New("个人词典仅支持自定义词、停用词和别名")
		}
		if input.Type != models.TagRuleTypeWord && input.Type != models.TagRuleTypeStopWord && input.Type != models.TagRuleTypeAlias && input.Type != models.TagRuleTypeRegex {
			return nil, fmt.Errorf("不支持的规则类型: %s", input.Type)
		}
		if input.TargetField != "" && input.TargetField != "filename" && input.TargetField != "basename" &&
			input.TargetField != "extension" && input.TargetField != "mime" &&
			!strings.HasPrefix(input.TargetField, "metadata.") {
			return nil, fmt.Errorf("不支持的规则目标字段: %s", input.TargetField)
		}
		if strings.HasPrefix(input.TargetField, "metadata.") && strings.TrimPrefix(input.TargetField, "metadata.") == "" {
			return nil, errors.New("元数据规则必须指定 metadata.key")
		}
		if !tagging.ValidTagName(input.Pattern) && input.Type != models.TagRuleTypeRegex {
			return nil, errors.New("规则词语无效")
		}
		if input.Type == models.TagRuleTypeAlias && !tagging.ValidTagName(input.Replacement) {
			return nil, errors.New("别名目标无效")
		}
		id := input.ID
		// 个人词典每次保存都会生成新的活动规则集，不能复用已归档规则的主键。
		if personal || id == "" {
			id = uuid.NewString()
		}
		categoryID := input.CategoryID
		if categoryID == "" {
			categoryID = "other"
		}
		targetField := input.TargetField
		if targetField == "" {
			targetField = "basename"
		}
		weight := input.Weight
		if weight <= 0 {
			weight = 1
		}
		rules = append(rules, models.TagRule{
			ID: id, RuleSetID: ruleSetID, Type: input.Type, TargetField: targetField,
			Pattern: strings.TrimSpace(input.Pattern), Replacement: strings.TrimSpace(input.Replacement),
			CategoryID: categoryID, Priority: input.Priority, Weight: weight,
			Enabled: input.Enabled, CreatedAt: now, UpdatedAt: now,
		})
	}
	return rules, nil
}

func (s *TagService) validateRuleCategories(ctx context.Context, inputs []request.TagRuleInput) error {
	categoryIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, input := range inputs {
		if input.Type == models.TagRuleTypeStopWord {
			continue
		}
		categoryID := input.CategoryID
		if categoryID == "" {
			categoryID = "other"
		}
		if _, exists := seen[categoryID]; exists {
			continue
		}
		seen[categoryID] = struct{}{}
		categoryIDs = append(categoryIDs, categoryID)
	}
	if len(categoryIDs) == 0 {
		return nil
	}
	var count int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagCategory{}).
		Where("id IN ?", categoryIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(categoryIDs)) {
		return errors.New("规则引用了不存在的标签分类")
	}
	return nil
}

func containsControl(value string) bool {
	for _, char := range value {
		if unicode.IsControl(char) || char == rune(0xFEFF) {
			return true
		}
	}
	return false
}

func cloneRules(source []models.TagRule, ruleSetID string, now time.Time) []models.TagRule {
	result := make([]models.TagRule, 0, len(source))
	for _, rule := range source {
		rule.ID = uuid.NewString()
		rule.RuleSetID = ruleSetID
		rule.CreatedAt = now
		rule.UpdatedAt = now
		result = append(result, rule)
	}
	return result
}
