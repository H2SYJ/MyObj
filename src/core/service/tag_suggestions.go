package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
	"myobj/src/pkg/tagging"
)

// 本文件负责文件和目录标签候选查询。
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
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Where("tc.enabled = ?", true)
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
		normalizedKeyword := "%" + tagging.Normalize(keyword) + "%"
		query = query.Where("td.normalized_name LIKE ?", normalizedKeyword)
	}
	var rows []suggestionRow
	if err := query.Order("td.name ASC, td.id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]response.CompactTagView, 0, len(rows))
	for _, row := range rows {
		result = append(result, response.CompactTagView{ID: row.ID, Name: row.Name, CategoryCode: row.CategoryCode, Color: row.Color, SystemCode: row.SystemCode})
	}
	return result, nil
}
