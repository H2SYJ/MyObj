package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/core/domain/response"
	"myobj/src/pkg/models"
)

type tagCloudRow struct {
	ID           string
	Name         string
	CategoryID   string
	CategoryCode string
	CategoryName string
	Color        string
	FileCount    int64
	Builtin      bool
	SystemCode   string
}

func cloudItemFromRow(row tagCloudRow) response.TagCloudItem {
	return response.TagCloudItem{
		ID: row.ID, Name: row.Name,
		Category:  response.TagCategoryView{ID: row.CategoryID, Code: row.CategoryCode, Name: row.CategoryName, Color: row.Color},
		FileCount: row.FileCount, System: row.Builtin || row.SystemCode != "", SystemCode: row.SystemCode,
	}
}

func (s *TagService) TagCloud(ctx context.Context, userID string) (*response.TagCloudResponse, error) {
	var rows []tagCloudRow
	if err := s.factory.DB().WithContext(ctx).Table("tag_definition AS td").
		Select(`td.id, td.name, td.builtin, td.system_code,
			tc.id AS category_id, tc.code AS category_code, tc.name AS category_name, tc.color,
			stat.file_count`).
		Joins("JOIN tag_category tc ON tc.id = td.category_id").
		Joins("JOIN user_tag_stat stat ON stat.tag_id = td.id AND stat.user_id = ?", userID).
		Where("stat.file_count > 0").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := &response.TagCloudResponse{Tags: make([]response.TagCloudItem, 0, len(rows))}
	for _, row := range rows {
		result.Tags = append(result.Tags, cloudItemFromRow(row))
	}
	sort.Slice(result.Tags, func(i, j int) bool {
		if result.Tags[i].FileCount != result.Tags[j].FileCount {
			return result.Tags[i].FileCount > result.Tags[j].FileCount
		}
		if result.Tags[i].Name != result.Tags[j].Name {
			return result.Tags[i].Name < result.Tags[j].Name
		}
		return result.Tags[i].ID < result.Tags[j].ID
	})
	return result, nil
}

type tagStatCountRow struct {
	UserID    string
	TagID     string
	FileCount int64
}

type tagStatKey struct {
	UserID string
	TagID  string
}

func (s *TagService) tagIDsForUserFile(ctx context.Context, tx *gorm.DB, userID, ufID string) ([]string, error) {
	return tagIDsForUserFile(ctx, tx, userID, ufID)
}

func tagIDsForUserFile(ctx context.Context, tx *gorm.DB, userID, ufID string) ([]string, error) {
	var tagIDs []string
	if err := tx.WithContext(ctx).Model(&models.UserFileTag{}).
		Where("user_id = ? AND uf_id = ?", userID, ufID).
		Distinct("tag_id").Pluck("tag_id", &tagIDs).Error; err != nil {
		return nil, err
	}
	var excluded []string
	if err := tx.WithContext(ctx).Model(&models.UserFileTagExclusion{}).
		Where("user_id = ? AND uf_id = ?", userID, ufID).
		Distinct("tag_id").Pluck("tag_id", &excluded).Error; err != nil {
		return nil, err
	}
	return uniqueTagStrings(append(tagIDs, excluded...)), nil
}

func (s *TagService) refreshUserTagStats(ctx context.Context, tx *gorm.DB, userID string, tagIDs []string) error {
	return refreshUserTagStats(ctx, tx, userID, tagIDs)
}

func (s *TagService) refreshUserTagStatsBatch(ctx context.Context, tx *gorm.DB, affectedTagIDs map[string][]string) error {
	return refreshUserTagStatsBatch(ctx, tx, affectedTagIDs)
}

func refreshUserTagStats(ctx context.Context, tx *gorm.DB, userID string, tagIDs []string) error {
	return refreshUserTagStatsBatch(ctx, tx, map[string][]string{userID: tagIDs})
}

func refreshUserTagStatsBatch(ctx context.Context, tx *gorm.DB, affectedTagIDs map[string][]string) error {
	userIDs := make([]string, 0, len(affectedTagIDs))
	normalizedTagIDs := make(map[string][]string, len(affectedTagIDs))
	for userID := range affectedTagIDs {
		tagIDs := uniqueTagStrings(affectedTagIDs[userID])
		if userID == "" || len(tagIDs) == 0 {
			continue
		}
		sort.Strings(tagIDs)
		normalizedTagIDs[userID] = tagIDs
		userIDs = append(userIDs, userID)
	}
	if len(userIDs) == 0 {
		return nil
	}
	sort.Strings(userIDs)
	conditions := make([]string, 0, len(userIDs))
	conditionArgs := make([]interface{}, 0, len(userIDs)*2)
	for _, userID := range userIDs {
		conditions = append(conditions, "(uft.user_id = ? AND uft.tag_id IN ?)")
		conditionArgs = append(conditionArgs, userID, normalizedTagIDs[userID])
	}
	var rows []tagStatCountRow
	if err := tx.WithContext(ctx).Table("user_file_tag AS uft").
		Select("uft.user_id, uft.tag_id, COUNT(DISTINCT uft.uf_id) AS file_count").
		Joins("JOIN user_files uf ON uf.uf_id = uft.uf_id AND uf.user_id = uft.user_id AND uf.deleted_at IS NULL").
		Where("("+strings.Join(conditions, " OR ")+")", conditionArgs...).
		Where(`NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e
			WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)`).
		Group("uft.user_id, uft.tag_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	counts := make(map[tagStatKey]int64, len(rows))
	for _, row := range rows {
		counts[tagStatKey{UserID: row.UserID, TagID: row.TagID}] = row.FileCount
	}
	now := time.Now()
	for _, userID := range userIDs {
		for _, tagID := range normalizedTagIDs[userID] {
			count := counts[tagStatKey{UserID: userID, TagID: tagID}]
			if count <= 0 {
				if err := tx.WithContext(ctx).Where("user_id = ? AND tag_id = ?", userID, tagID).Delete(&models.UserTagStat{}).Error; err != nil {
					return err
				}
				continue
			}
			stat := models.UserTagStat{UserID: userID, TagID: tagID, FileCount: count, UpdatedAt: now}
			if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "user_id"}, {Name: "tag_id"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"file_count": count, "updated_at": now,
				}),
			}).Create(&stat).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
