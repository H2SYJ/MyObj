package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"myobj/src/core/domain/request"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

const maxTagDictionaryImportBytes = 1024 * 1024

type tagRuleExport struct {
	Rules []request.TagRuleInput `json:"rules"`
}

func (s *TagService) ExportRuleSet(ctx context.Context, id, format string) ([]byte, string, error) {
	ruleSet, err := s.RuleSet(ctx, id)
	if err != nil {
		return nil, "", err
	}
	rules := ruleInputs(ruleSet.Rules)
	switch strings.ToLower(format) {
	case "", "json":
		data, err := json.MarshalIndent(tagRuleExport{Rules: rules}, "", "  ")
		if err != nil {
			return nil, "", err
		}
		return append(data, '\n'), "application/json; charset=utf-8", nil
	case "csv":
		var buffer bytes.Buffer
		writer := csv.NewWriter(&buffer)
		_ = writer.Write([]string{"id", "type", "target_field", "pattern", "replacement", "category_id", "priority", "weight", "enabled"})
		for _, rule := range rules {
			_ = writer.Write([]string{rule.ID, rule.Type, rule.TargetField, rule.Pattern, rule.Replacement, rule.CategoryID, strconv.Itoa(rule.Priority), strconv.FormatFloat(rule.Weight, 'f', -1, 64), strconv.FormatBool(rule.Enabled)})
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, "", err
		}
		return buffer.Bytes(), "text/csv; charset=utf-8", nil
	default:
		return nil, "", errors.New("导出格式仅支持 json 或 csv")
	}
}

func (s *TagService) ImportGlobalDraft(ctx context.Context, id string, revision int, format string, data []byte) (*models.TagRuleSet, error) {
	if len(data) == 0 || len(data) > maxTagDictionaryImportBytes {
		return nil, errors.New("导入文件大小必须在1字节到1MB之间")
	}
	if !utf8.Valid(data) || bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, errors.New("导入文件必须是 UTF-8 无 BOM")
	}
	var rules []request.TagRuleInput
	var err error
	switch strings.ToLower(format) {
	case "", "json":
		var envelope tagRuleExport
		if err = json.Unmarshal(data, &envelope); err != nil {
			if listErr := json.Unmarshal(data, &rules); listErr != nil {
				return nil, fmt.Errorf("解析 JSON 词典失败: %w", err)
			}
		} else {
			rules = envelope.Rules
		}
	case "csv":
		rules, err = parseTagRuleCSV(data)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("导入格式仅支持 json 或 csv")
	}
	// 导出文件中的规则ID属于源版本，导入草稿时必须生成新ID，避免与活动或归档版本主键冲突。
	for index := range rules {
		rules[index].ID = ""
	}
	return s.SaveGlobalDraft(ctx, id, revision, rules)
}

func (s *TagService) RuleSetDiff(ctx context.Context, id string) (map[string]interface{}, error) {
	target, err := s.RuleSet(ctx, id)
	if err != nil {
		return nil, err
	}
	baseVersion := target.BasedOnVersion
	if baseVersion == 0 && target.Status != models.TagRuleSetDraft {
		baseVersion = target.Version - 1
	}
	var base *models.TagRuleSet
	if baseVersion > 0 {
		var found models.TagRuleSet
		if err := s.factory.DB().WithContext(ctx).Preload("Rules").
			Where("scope_type = ? AND scope_id = ? AND version = ?", target.ScopeType, target.ScopeID, baseVersion).
			First(&found).Error; err == nil {
			base = &found
		}
	}
	return map[string]interface{}{"base": base, "target": target}, nil
}

func (s *TagService) RebuildJob(ctx context.Context, id string) (*models.TagRebuildJob, error) {
	var job models.TagRebuildJob
	if err := s.factory.DB().WithContext(ctx).Where("id = ?", id).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *TagService) RebuildFailures(ctx context.Context, jobID, status string, limit int) ([]models.TagRebuildFailure, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if status != "" {
		if status != models.TagRebuildFailureFailed && status != models.TagRebuildFailureRetrying && status != models.TagRebuildFailureResolved {
			return nil, errors.New("失败明细状态无效")
		}
	}
	db := s.factory.DB().WithContext(ctx)
	var job models.TagRebuildJob
	if err := db.Select("id").Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, err
	}
	query := db.Where("job_id = ?", jobID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var failures []models.TagRebuildFailure
	err := query.Order("updated_at DESC, uf_id ASC").Limit(limit).Find(&failures).Error
	return failures, err
}

func (s *TagService) RetryRebuildFailure(ctx context.Context, jobID, ufID string) error {
	now := time.Now()
	err := s.factory.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var failure models.TagRebuildFailure
		if err := tx.Where("job_id = ? AND uf_id = ?", jobID, ufID).First(&failure).Error; err != nil {
			return err
		}
		if failure.Status == models.TagRebuildFailureResolved {
			return errors.New("该失败项已经处理完成")
		}
		var count int64
		if err := tx.Model(&models.UserFiles{}).
			Where("user_id = ? AND uf_id = ? AND deleted_at IS NULL", failure.UserID, failure.UFID).
			Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("失败项对应的文件不存在")
		}
		if err := tagging.QueueUserFile(ctx, tx, failure.UserID, failure.UFID); err != nil {
			return err
		}
		result := tx.Model(&models.TagRebuildFailure{}).
			Where("job_id = ? AND uf_id = ? AND status <> ?", jobID, ufID, models.TagRebuildFailureResolved).
			Updates(map[string]interface{}{
				"status": models.TagRebuildFailureRetrying, "error_message": "",
				"retry_count": gorm.Expr("retry_count + 1"), "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("失败项状态已变化")
		}
		return nil
	})
	if err == nil {
		s.notifyPending()
	}
	return err
}

func parseTagRuleCSV(data []byte) ([]request.TagRuleInput, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析 CSV 词典失败: %w", err)
	}
	if len(records) == 0 {
		return []request.TagRuleInput{}, nil
	}
	header := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		header[strings.TrimSpace(name)] = index
	}
	for _, required := range []string{"type", "pattern"} {
		if _, exists := header[required]; !exists {
			return nil, fmt.Errorf("CSV 缺少 %s 列", required)
		}
	}
	value := func(record []string, name string) string {
		index, exists := header[name]
		if !exists || index >= len(record) {
			return ""
		}
		return record[index]
	}
	rules := make([]request.TagRuleInput, 0, len(records)-1)
	for rowIndex, record := range records[1:] {
		if len(record) == 1 && strings.TrimSpace(record[0]) == "" {
			continue
		}
		priority, priorityErr := strconv.Atoi(defaultString(value(record, "priority"), "0"))
		weight, weightErr := strconv.ParseFloat(defaultString(value(record, "weight"), "1"), 64)
		enabled, enabledErr := strconv.ParseBool(defaultString(value(record, "enabled"), "true"))
		if priorityErr != nil || weightErr != nil || enabledErr != nil {
			return nil, fmt.Errorf("CSV 第%d行的优先级、权重或启用状态无效", rowIndex+2)
		}
		rules = append(rules, request.TagRuleInput{
			ID: value(record, "id"), Type: value(record, "type"), TargetField: value(record, "target_field"),
			Pattern: value(record, "pattern"), Replacement: value(record, "replacement"), CategoryID: value(record, "category_id"),
			Priority: priority, Weight: weight, Enabled: enabled,
		})
	}
	return rules, nil
}

func ruleInputs(rules []models.TagRule) []request.TagRuleInput {
	result := make([]request.TagRuleInput, 0, len(rules))
	for _, rule := range rules {
		result = append(result, request.TagRuleInput{ID: rule.ID, Type: rule.Type, TargetField: rule.TargetField, Pattern: rule.Pattern, Replacement: rule.Replacement, CategoryID: rule.CategoryID, Priority: rule.Priority, Weight: rule.Weight, Enabled: rule.Enabled})
	}
	return result
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
