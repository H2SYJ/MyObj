package models

import (
	"myobj/src/pkg/custom_type"
)

// VirtualDirectory 用户虚拟目录节点。完整路径由父子关系动态解析，不在节点中重复存储。
type VirtualDirectory struct {
	ID        int                  `gorm:"type:INTEGER;not null;primaryKey" json:"id"`
	UserID    string               `gorm:"type:VARCHAR(64);not null;uniqueIndex:uk_virtual_directory_sibling,priority:1;index:idx_virtual_directory_parent,priority:1" json:"user_id"`
	Name      string               `gorm:"type:VARCHAR(100);not null;uniqueIndex:uk_virtual_directory_sibling,priority:3" json:"name"`
	ParentID  int                  `gorm:"type:INTEGER;not null;default:0;uniqueIndex:uk_virtual_directory_sibling,priority:2;index:idx_virtual_directory_parent,priority:2" json:"parent_id"`
	CreatedAt custom_type.JsonTime `gorm:"type:DATETIME;not null" json:"created_at"`
	UpdatedAt custom_type.JsonTime `gorm:"type:DATETIME;not null" json:"updated_at"`
}

func (VirtualDirectory) TableName() string {
	return "virtual_directory"
}
