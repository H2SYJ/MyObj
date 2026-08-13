package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/core/domain/request"
	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
)

// 本文件负责目录手工标签和影视目录判定。
func (s *TagService) GetDirectoryTags(ctx context.Context, userID string, directoryID int) (*response.DirectoryTagsResponse, error) {
	if err := s.ensureDirectoryOwnership(ctx, userID, directoryID); err != nil {
		return nil, err
	}
	var rows []detailedTagRow
	err := s.factory.DB().WithContext(ctx).Table("user_directory_tag AS udt").
		Select("td.id, COALESCE(pref.display_name, td.name) AS name, COALESCE(display.id, tc.id) AS category_id, COALESCE(display.code, tc.code) AS category_code, COALESCE(display.name, tc.name) AS category_name, COALESCE(display.color, tc.color) AS color").
		Joins("JOIN tag_definition td ON td.id = udt.tag_id").
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Joins("LEFT JOIN user_tag_preference pref ON pref.tag_id = td.id AND pref.user_id = ?", userID).
		Joins("LEFT JOIN tag_category display ON display.id = pref.display_category_id AND display.enabled = ?", true).
		Where("udt.user_id = ? AND udt.directory_id = ?", userID, directoryID).
		Where("COALESCE(pref.hidden, ?) = ?", false, false).
		Order("tc.sort_order ASC, COALESCE(pref.display_name, td.name) ASC, td.id ASC").Scan(&rows).Error
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
		affectedTagIDs := append([]string(nil), removeTagIDs...)
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
			affectedTagIDs = append(affectedTagIDs, tag.ID)
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
		return s.refreshUserTagStats(ctx, tx, userID, affectedTagIDs)
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
