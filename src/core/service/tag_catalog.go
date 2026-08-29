package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/core/domain/request"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

// 本文件负责标签分类和共享标签定义。
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
		input.Color = "#78716c"
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
