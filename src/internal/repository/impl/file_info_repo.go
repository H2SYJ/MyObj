package impl

import (
	"context"
	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
	"myobj/src/pkg/repository"

	"gorm.io/gorm"
)

type fileInfoRepository struct {
	db *gorm.DB
}

// NewFileInfoRepository 创建文件信息仓储实例
func NewFileInfoRepository(db *gorm.DB) repository.FileInfoRepository {
	return &fileInfoRepository{db: db}
}

func (r *fileInfoRepository) Create(ctx context.Context, file *models.FileInfo) error {
	return r.db.WithContext(ctx).Create(file).Error
}

func (r *fileInfoRepository) GetByID(ctx context.Context, id string) (*models.FileInfo, error) {
	var file models.FileInfo
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&file).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *fileInfoRepository) GetByHash(ctx context.Context, hash string) (*models.FileInfo, error) {
	var file models.FileInfo
	err := r.db.WithContext(ctx).Where("file_hash = ?", hash).First(&file).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

// GetByChunkSignature 根据分片签名和文件大小查询文件（用于快速秒传预检）
func (r *fileInfoRepository) GetByChunkSignature(ctx context.Context, signature string, fileSize int64) (*models.FileInfo, error) {
	var files models.FileInfo
	err := r.db.WithContext(ctx).
		Where("chunk_signature = ? AND size = ?", signature, fileSize).
		Find(&files).Error
	if err != nil {
		return nil, err
	}
	return &files, nil
}

func (r *fileInfoRepository) Update(ctx context.Context, file *models.FileInfo) error {
	return r.db.WithContext(ctx).Save(file).Error
}

// ListUnencryptedVideosAfter 按文件ID游标查询未加密视频，避免更新缩略图后分页漏项。
func (r *fileInfoRepository) ListUnencryptedVideosAfter(ctx context.Context, afterID string, limit int) ([]*models.FileInfo, error) {
	var files []*models.FileInfo
	query := r.db.WithContext(ctx).
		Where("mime LIKE ? AND is_enc = ?", "video/%", false)
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}
	err := query.Order("id ASC").Limit(limit).Find(&files).Error
	return files, err
}

// UpdateThumbnailPath 只更新缩略图路径和更新时间，避免覆盖并发修改的其他字段。
func (r *fileInfoRepository) UpdateThumbnailPath(ctx context.Context, id, thumbnailPath string) error {
	return r.db.WithContext(ctx).
		Model(&models.FileInfo{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"thumbnail_img": thumbnailPath,
			"updated_at":    custom_type.Now(),
		}).Error
}

func (r *fileInfoRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.FileInfo{}).Error
}

func (r *fileInfoRepository) List(ctx context.Context, offset, limit int) ([]*models.FileInfo, error) {
	var files []*models.FileInfo
	err := r.db.WithContext(ctx).Offset(offset).Limit(limit).Find(&files).Error
	return files, err
}

func (r *fileInfoRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.FileInfo{}).Count(&count).Error
	return count, err
}

func (r *fileInfoRepository) BatchCreate(ctx context.Context, files []*models.FileInfo) error {
	return r.db.WithContext(ctx).Create(files).Error
}

// SearchByName 根据文件名模糊搜索
func (r *fileInfoRepository) SearchByName(ctx context.Context, keyword string, offset, limit int) ([]*models.FileInfo, error) {
	var files []*models.FileInfo
	err := r.db.WithContext(ctx).
		Where("name LIKE ?", "%"+keyword+"%").
		Offset(offset).Limit(limit).
		Find(&files).Error
	return files, err
}

// CountByName 根据文件名统计数量
func (r *fileInfoRepository) CountByName(ctx context.Context, keyword string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.FileInfo{}).
		Where("name LIKE ?", "%"+keyword+"%").
		Count(&count).Error
	return count, err
}

// ListByVirtualPath 查询指定虚拟路径下的文件
func (r *fileInfoRepository) ListByVirtualPath(ctx context.Context, userID, virtualPath string, offset, limit int) ([]*models.FileInfo, error) {
	var files []*models.FileInfo
	// 通过user_files关联查询，virtualPath字段存储的是虚拟路径ID（字符串格式）
	err := r.db.WithContext(ctx).
		Select("id, user_files.file_name as name, random_name, size, mime, virtual_path, thumbnail_img, path, file_hash, file_enc_hash, chunk_signature, first_chunk_hash, second_chunk_hash, third_chunk_hash, has_full_hash, is_enc, is_chunk, chunk_count, enc_path, file_info.created_at, file_info.updated_at").
		Joins("JOIN user_files ON file_info.id = user_files.file_id").
		Where("user_files.user_id = ? AND user_files.virtual_path = ? AND user_files.deleted_at is null", userID, virtualPath).
		Order("file_info.created_at DESC").
		Offset(offset).Limit(limit).
		Find(&files).Error
	return files, err
}

// CountByVirtualPath 统计指定虚拟路径下的文件数量
func (r *fileInfoRepository) CountByVirtualPath(ctx context.Context, userID, virtualPath string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.FileInfo{}).
		Joins("JOIN user_files ON file_info.id = user_files.file_id").
		Where("user_files.user_id = ? AND user_files.virtual_path = ? AND user_files.deleted_at IS NULL", userID, virtualPath).
		Count(&count).Error
	return count, err
}
