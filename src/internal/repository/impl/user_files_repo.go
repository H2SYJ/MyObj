package impl

import (
	"context"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"
	"strings"

	"gorm.io/gorm"
)

func (r *userFilesRepository) filteredQuery(ctx context.Context, input repository.UserFileQuery) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&models.UserFiles{}).
		Select("user_files.*").
		Joins("JOIN file_info ON user_files.file_id = file_info.id")
	if input.UserID != "" {
		query = query.Where("user_files.user_id = ?", input.UserID)
	}
	if input.PublicOnly {
		query = query.Where("user_files.public = ?", true)
	}
	if input.DirectoryID != nil {
		query = query.Where("user_files.directory_id = ?", *input.DirectoryID)
	}
	query = applyFileTypeFilter(query, input.FileType)
	for _, term := range input.SearchTerms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		like := "%" + term + "%"
		tagVisibility := ""
		args := []interface{}{like, like}
		if input.PublicOnly {
			tagVisibility = " AND (uft.source_type <> ? OR uft.visibility = ?)"
			args = append(args, models.TagSourceManual, models.TagVisibilityPublic)
		}
		query = query.Where(
			"(user_files.file_name LIKE ? OR EXISTS (SELECT 1 FROM user_file_tag uft JOIN tag_definition td ON td.id = uft.tag_id WHERE uft.user_id = user_files.user_id AND uft.uf_id = user_files.uf_id AND td.normalized_name LIKE ?"+
				tagVisibility+" AND NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id)))",
			args...,
		)
	}
	tagIDs := uniqueNonEmpty(input.TagIDs)
	if len(tagIDs) > 0 {
		if input.TagMode == "any" {
			query = query.Where(tagExistsSQL(input.PublicOnly, true), tagExistsArgs(input.PublicOnly, tagIDs)...)
		} else {
			for _, tagID := range tagIDs {
				query = query.Where(tagExistsSQL(input.PublicOnly, false), tagExistsArgs(input.PublicOnly, tagID)...)
			}
		}
	}
	return query
}

func (r *userFilesRepository) ListFiltered(ctx context.Context, query repository.UserFileQuery) ([]*models.UserFiles, error) {
	var files []*models.UserFiles
	err := r.filteredQuery(ctx, query).
		Order(userFileOrder(query.SortBy, query.SortOrder)).
		Offset(query.Offset).Limit(query.Limit).Find(&files).Error
	return files, err
}

func (r *userFilesRepository) CountFiltered(ctx context.Context, query repository.UserFileQuery) (int64, error) {
	var count int64
	err := r.filteredQuery(ctx, query).Count(&count).Error
	return count, err
}

func tagExistsSQL(publicOnly, multiple bool) string {
	operator := "= ?"
	if multiple {
		operator = "IN ?"
	}
	visibility := ""
	if publicOnly {
		visibility = " AND (uft.source_type <> ? OR uft.visibility = ?)"
	}
	return "EXISTS (SELECT 1 FROM user_file_tag uft WHERE uft.user_id = user_files.user_id AND uft.uf_id = user_files.uf_id AND uft.tag_id " + operator + visibility +
		" AND NOT EXISTS (SELECT 1 FROM user_file_tag_exclusion e WHERE e.user_id = uft.user_id AND e.uf_id = uft.uf_id AND e.tag_id = uft.tag_id))"
}

func tagExistsArgs(publicOnly bool, tagIDs interface{}) []interface{} {
	args := []interface{}{tagIDs}
	if publicOnly {
		args = append(args, models.TagSourceManual, models.TagVisibilityPublic)
	}
	return args
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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

func applyFileTypeFilter(query *gorm.DB, fileType string) *gorm.DB {
	switch fileType {
	case "", "all":
		return query
	case "image", "video", "audio":
		return query.Where("file_info.mime LIKE ?", fileType+"/%")
	case "archive":
		return query.Where("file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ?", "%zip%", "%rar%", "%7z%", "%tar%", "%gzip%")
	case "doc":
		return query.Where("file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ?", "%pdf%", "%word%", "%spreadsheet%", "%excel%", "%presentation%", "%document%")
	case "other":
		return query.Where(`NOT (
			file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR
			file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR
			file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ? OR file_info.mime LIKE ?
		)`,
			"image/%", "video/%", "audio/%",
			"%zip%", "%rar%", "%7z%", "%tar%", "%gzip%",
			"%pdf%", "%word%", "%spreadsheet%", "%excel%", "%presentation%", "%document%")
	default:
		return query
	}
}

type userFilesRepository struct {
	db *gorm.DB
}

// NewUserFilesRepository 创建用户文件关联仓储实例
func NewUserFilesRepository(db *gorm.DB) repository.UserFilesRepository {
	return &userFilesRepository{db: db}
}

func (r *userFilesRepository) Create(ctx context.Context, userFile *models.UserFiles) error {
	return r.db.WithContext(ctx).Create(userFile).Error
}

func (r *userFilesRepository) GetByUserIDAndFileID(ctx context.Context, userID, fileID string) (*models.UserFiles, error) {
	var userFile models.UserFiles
	err := r.db.WithContext(ctx).Where("user_id = ? AND file_id = ?", userID, fileID).First(&userFile).Error
	if err != nil {
		return nil, err
	}
	return &userFile, nil
}

func (r *userFilesRepository) Update(ctx context.Context, userFile *models.UserFiles) error {
	return r.db.WithContext(ctx).Where("user_id = ? and file_id = ?", userFile.UserID, userFile.FileID).Save(userFile).Error
}

func (r *userFilesRepository) Delete(ctx context.Context, userID, fileID string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND uf_id = ?", userID, fileID).
		Delete(&models.UserFiles{}).Error
}

func (r *userFilesRepository) ListByUserID(ctx context.Context, userID string, offset, limit int) ([]*models.UserFiles, error) {
	var userFiles []*models.UserFiles
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Offset(offset).Limit(limit).Find(&userFiles).Error
	return userFiles, err
}

func (r *userFilesRepository) Count(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.UserFiles{}).
		Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// ListPublicFiles 获取所有公开文件
func (r *userFilesRepository) ListPublicFiles(ctx context.Context, offset, limit int) ([]*models.UserFiles, error) {
	var userFiles []*models.UserFiles
	err := r.db.WithContext(ctx).Where("public = ?", true).
		Offset(offset).Limit(limit).Find(&userFiles).Error
	return userFiles, err
}

// CountPublicFiles 统计公开文件数量
func (r *userFilesRepository) CountPublicFiles(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.UserFiles{}).
		Where("public = ?", true).Count(&count).Error
	return count, err
}

// SearchPublicFiles 搜索公开文件（根据文件名）
func (r *userFilesRepository) SearchPublicFiles(ctx context.Context, keyword string, offset, limit int) ([]*models.UserFiles, error) {
	var userFiles []*models.UserFiles
	err := r.db.WithContext(ctx).
		Joins("JOIN file_info ON user_files.file_id = file_info.id").
		Where("user_files.public = ? AND file_info.name LIKE ?", true, "%"+keyword+"%").
		Offset(offset).Limit(limit).
		Find(&userFiles).Error
	return userFiles, err
}

// CountPublicFilesByKeyword 统计匹配关键词的公开文件数量
func (r *userFilesRepository) CountPublicFilesByKeyword(ctx context.Context, keyword string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.UserFiles{}).
		Joins("JOIN file_info ON user_files.file_id = file_info.id").
		Where("user_files.public = ? AND file_info.name LIKE ?", true, "%"+keyword+"%").
		Count(&count).Error
	return count, err
}

// SearchUserFiles 搜索用户文件（根据文件名）
func (r *userFilesRepository) SearchUserFiles(ctx context.Context, userID, keyword string, offset, limit int) ([]*models.UserFiles, error) {
	return r.SearchUserFilesSorted(ctx, userID, keyword, "time", "desc", offset, limit)
}

func (r *userFilesRepository) SearchUserFilesSorted(ctx context.Context, userID, keyword, sortBy, sortOrder string, offset, limit int) ([]*models.UserFiles, error) {
	var userFiles []*models.UserFiles
	err := r.db.WithContext(ctx).
		Select("user_files.*").
		Joins("JOIN file_info ON user_files.file_id = file_info.id").
		Where("user_files.user_id = ? AND user_files.file_name LIKE ?", userID, "%"+keyword+"%").
		Order(userFileOrder(sortBy, sortOrder)).
		Offset(offset).Limit(limit).
		Find(&userFiles).Error
	return userFiles, err
}

// CountUserFilesByKeyword 统计用户匹配关键词的文件数量
func (r *userFilesRepository) CountUserFilesByKeyword(ctx context.Context, userID, keyword string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.UserFiles{}).
		Joins("JOIN file_info ON user_files.file_id = file_info.id").
		Where("user_files.user_id = ? AND user_files.file_name LIKE ?", userID, "%"+keyword+"%").
		Count(&count).Error
	return count, err
}

// GetByUserIDAndUfID 获取用户文件关联
func (r *userFilesRepository) GetByUserIDAndUfID(ctx context.Context, userID, ufID string) (*models.UserFiles, error) {
	var userFile models.UserFiles
	err := r.db.WithContext(ctx).Where("user_id = ? AND uf_id = ?", userID, ufID).First(&userFile).Error
	if err != nil {
		return nil, err
	}
	return &userFile, nil
}

// GetByUfID 通过 uf_id 查询文件（用于公开文件访问，不要求 user_id）
func (r *userFilesRepository) GetByUfID(ctx context.Context, ufID string) (*models.UserFiles, error) {
	var userFile models.UserFiles
	err := r.db.WithContext(ctx).Where("uf_id = ?", ufID).First(&userFile).Error
	if err != nil {
		return nil, err
	}
	return &userFile, nil
}

// ListByDirectoryID 查询指定目录下的user_files记录（避免file_id重复问题）
// 直接从 user_files 表查询，每个uf_id都是唯一的，避免了秒传场景下同一file_id有多条记录的问题
func (r *userFilesRepository) ListByDirectoryID(ctx context.Context, userID string, directoryID int, offset, limit int) ([]*models.UserFiles, error) {
	return r.ListByDirectoryIDSorted(ctx, userID, directoryID, "time", "desc", offset, limit)
}

func (r *userFilesRepository) ListByDirectoryIDSorted(ctx context.Context, userID string, directoryID int, sortBy, sortOrder string, offset, limit int) ([]*models.UserFiles, error) {
	var userFiles []*models.UserFiles
	err := r.db.WithContext(ctx).
		Select("user_files.*").
		Joins("JOIN file_info ON user_files.file_id = file_info.id").
		Where("user_files.user_id = ? AND user_files.directory_id = ?", userID, directoryID).
		Order(userFileOrder(sortBy, sortOrder)).
		Offset(offset).Limit(limit).
		Find(&userFiles).Error
	return userFiles, err
}

func userFileOrder(sortBy, sortOrder string) string {
	direction := "DESC"
	if sortOrder == "asc" {
		direction = "ASC"
	}
	column := "user_files.created_at"
	switch sortBy {
	case "name":
		column = "user_files.file_name"
	case "size":
		column = "file_info.size"
	case "time":
		column = "user_files.created_at"
	}
	return column + " " + direction + ", user_files.uf_id ASC"
}
