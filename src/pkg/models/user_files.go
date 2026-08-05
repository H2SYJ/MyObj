package models

import (
	"myobj/src/pkg/custom_type"

	"gorm.io/gorm"
)

// UserFiles 用户文件表
type UserFiles struct {
	UserID      string               `gorm:"type:VARCHAR;not null;index:idx_user_files_user_active,priority:1;index:idx_user_files_user_directory_active,priority:1" json:"user_id"`                                                   // 用户ID
	FileID      string               `gorm:"type:VARCHAR;not null" json:"file_id"`                                                                                                                                                     // 文件ID
	FileName    string               `gorm:"type:TEXT;not null" json:"file_name"`                                                                                                                                                      // 文件名
	DirectoryID int                  `gorm:"type:INTEGER;not null;index;index:idx_user_files_user_directory_active,priority:2" json:"directory_id"`                                                                                    // 虚拟目录ID
	IsPublic    bool                 `gorm:"type:BOOLEAN;not null;column:public;index:idx_user_files_public_active,priority:1" json:"public"`                                                                                          // 是否公开
	CreatedAt   custom_type.JsonTime `gorm:"type:DATETIME;not null" json:"created_at"`                                                                                                                                                 // 创建时间
	DeletedAt   gorm.DeletedAt       `gorm:"type:DATETIME;not null;index:idx_user_files_user_active,priority:2;index:idx_user_files_public_active,priority:2;index:idx_user_files_user_directory_active,priority:3" json:"deleted_at"` // 删除时间
	UfID        string               `gorm:"type:VARCHAR;not null;" json:"uf_id"`                                                                                                                                                      // 用户文件ID
}

func (UserFiles) TableName() string {
	return "user_files"
}
