package service

import (
	"context"

	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
)

// 本文件负责列表场景的标签批量读取。
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

func (s *TagService) CompactTags(ctx context.Context, ownerUserID, viewerUserID string, ufIDs []string, publicOnly bool) (map[string][]response.CompactTagView, error) {
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
	if ownerUserID != "" {
		query = query.Where("uft.user_id = ?", ownerUserID)
	}
	if publicOnly {
		query = query.Where("uft.source_type <> ? OR uft.visibility = ?", models.TagSourceManual, models.TagVisibilityPublic)
	}
	var rows []compactTagRow
	if err := query.Order("tc.sort_order ASC, td.name ASC, td.id ASC").Scan(&rows).Error; err != nil {
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
		Order("tc.sort_order ASC, td.name ASC, td.id ASC").Scan(&rows).Error
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
