package service

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
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

// 本文件负责全局规则集的版本管理与预览。
func (s *TagService) GlobalRuleSets(ctx context.Context) ([]models.TagRuleSet, error) {
	var sets []models.TagRuleSet
	err := s.factory.DB().WithContext(ctx).Order("version DESC, created_at DESC").Find(&sets).Error
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
		Where("status = ?", models.TagRuleSetDraft).
		Order("created_at DESC").First(&draft).Error
	if err == nil {
		return &draft, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	active, err := s.loadActiveRuleSet(ctx)
	if err != nil {
		return nil, err
	}
	var maxVersion int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagRuleSet{}).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	draft = models.TagRuleSet{
		ID: uuid.NewString(), Version: maxVersion + 1,
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
	if draft.Status != models.TagRuleSetDraft {
		return nil, errors.New("只能修改全局规则草稿")
	}
	if draft.Revision != revision {
		return nil, fmt.Errorf("规则草稿已被其他操作修改")
	}
	rules, err := buildRules(inputs, draft.ID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRuleCategories(ctx, inputs); err != nil {
		return nil, err
	}
	candidate := *draft
	candidate.Rules = rules
	if _, err := tagging.CompileSnapshot(candidate, int(s.autoLimit.Load())); err != nil {
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
	if draft.Status != models.TagRuleSetDraft {
		return nil, nil, errors.New("只能发布全局规则草稿")
	}
	snapshot, err := tagging.CompileSnapshot(*draft, int(s.autoLimit.Load()))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	job := &models.TagRebuildJob{
		ID: uuid.NewString(), TargetVersion: draft.Version,
		Status: "pending", RequestedBy: adminID, CreatedAt: now, UpdatedAt: now,
	}
	err = s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.TagRuleSet{}).
			Where("status = ?", models.TagRuleSetActive).
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
			Where("status IN ?", []string{"pending", "running"}).
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
	var maxVersion int64
	if err := s.factory.DB().WithContext(ctx).Model(&models.TagRuleSet{}).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return nil, nil, err
	}
	now := time.Now()
	draft := &models.TagRuleSet{
		ID: uuid.NewString(), Version: maxVersion + 1,
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

func (s *TagService) PreviewRules(ctx context.Context, samples []string, inputs []request.TagRuleInput) ([]response.TagPreviewItem, error) {
	if len(samples) == 0 || len(samples) > 100 {
		return nil, errors.New("预览样例数量必须在1到100之间")
	}
	ruleSet := models.TagRuleSet{ID: "preview", Version: 1}
	rules, err := buildRules(inputs, ruleSet.ID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRuleCategories(ctx, inputs); err != nil {
		return nil, err
	}
	ruleSet.Rules = rules
	snapshot, err := tagging.CompileSnapshot(ruleSet, int(s.autoLimit.Load()))
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

func buildRules(inputs []request.TagRuleInput, ruleSetID string) ([]models.TagRule, error) {
	now := time.Now()
	rules := make([]models.TagRule, 0, len(inputs))
	for _, input := range inputs {
		if containsControl(input.Pattern) || containsControl(input.Replacement) {
			return nil, errors.New("规则内容不能包含控制字符或 BOM")
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
		if id == "" {
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
