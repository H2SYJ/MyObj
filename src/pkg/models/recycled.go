package models

import "myobj/src/pkg/custom_type"

// Recycled 回收站表
type Recycled struct {
	// 回收站ID
	ID string `gorm:"type:VARCHAR(64);not null;primaryKey;unique" json:"id"`
	// 文件ID
	FileID string `gorm:"type:VARCHAR(64);not null" json:"file_id"`
	// ItemType 区分普通文件和整目录回收记录，旧数据默认视为文件。
	ItemType string `gorm:"type:VARCHAR(16);not null;default:file;index:idx_recycled_user_type_created,priority:2" json:"item_type"`
	// ItemName 用于目录回收记录展示；文件记录仍以 user_files.file_name 为准。
	ItemName string `gorm:"type:TEXT" json:"item_name"`
	// OriginalParentID 保存目录删除前的父目录，恢复时优先回到原位置。
	OriginalParentID int `gorm:"not null;default:0" json:"original_parent_id"`
	// TotalSize 和 ItemCount 是目录树的汇总信息。
	TotalSize int64 `gorm:"not null;default:0" json:"total_size"`
	ItemCount int   `gorm:"not null;default:1" json:"item_count"`
	// 用户ID
	UserID string `gorm:"type:VARCHAR(64);not null;index:idx_recycled_user_type_created,priority:1" json:"user_id"`
	// 删除时间
	CreatedAt custom_type.JsonTime `gorm:"type:DATETIME;not null;index:idx_recycled_user_type_created,priority:3,sort:desc" json:"created_at"`
}

const (
	RecycledItemTypeFile   = "file"
	RecycledItemTypeFolder = "folder"
)

// RecycledDirectoryNode 保存整目录回收记录中的目录树。
type RecycledDirectoryNode struct {
	ID               uint   `gorm:"primaryKey" json:"id"`
	RecycledID       string `gorm:"type:VARCHAR(64);not null;index;uniqueIndex:idx_recycled_original_dir" json:"recycled_id"`
	OriginalDirID    int    `gorm:"not null;uniqueIndex:idx_recycled_original_dir" json:"original_dir_id"`
	ParentOriginalID int    `gorm:"not null;default:0;index" json:"parent_original_id"`
	Name             string `gorm:"type:TEXT;not null" json:"name"`
	Depth            int    `gorm:"not null;default:0;index" json:"depth"`
}

func (RecycledDirectoryNode) TableName() string {
	return "recycled_directory_node"
}

// RecycledDirectoryFile 保存目录回收记录中的文件和原目录映射。
type RecycledDirectoryFile struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	RecycledID    string `gorm:"type:VARCHAR(64);not null;index;uniqueIndex:idx_recycled_directory_file" json:"recycled_id"`
	FileID        string `gorm:"type:VARCHAR(64);not null;uniqueIndex:idx_recycled_directory_file" json:"file_id"`
	OriginalDirID int    `gorm:"not null;index" json:"original_dir_id"`
}

func (RecycledDirectoryFile) TableName() string {
	return "recycled_directory_file"
}

// RecycledDirectoryTag 保存目录进入回收站前的手工标签，恢复时映射到新目录ID。
type RecycledDirectoryTag struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	RecycledID    string `gorm:"type:VARCHAR(64);not null;index;uniqueIndex:uk_recycled_directory_tag" json:"recycled_id"`
	OriginalDirID int    `gorm:"not null;index;uniqueIndex:uk_recycled_directory_tag" json:"original_dir_id"`
	TagID         string `gorm:"type:VARCHAR(64);not null;uniqueIndex:uk_recycled_directory_tag" json:"tag_id"`
}

func (RecycledDirectoryTag) TableName() string { return "recycled_directory_tag" }

func (Recycled) TableName() string {
	return "recycled"
}
