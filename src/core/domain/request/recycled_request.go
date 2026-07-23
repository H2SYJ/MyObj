package request

import "fmt"

// RecycledListRequest 回收站列表请求
type RecycledListRequest struct {
	Page     int `form:"page" binding:"required,min=1"`
	PageSize int `form:"pageSize" binding:"required,min=1,max=100"`
}

// RestoreFileRequest 还原文件请求
type RestoreFileRequest struct {
	RecycledID string `json:"recycled_id" binding:"required"`
}

// DeleteRecycledRequest 永久删除文件请求
type DeleteRecycledRequest struct {
	RecycledID string `json:"recycled_id" binding:"required"`
}

// BatchRecycledRequest 批量操作回收站记录请求，单次最多处理 200 项。
type BatchRecycledRequest struct {
	RecycledIDs []string `json:"recycled_ids" binding:"required,min=1,dive,required"`
}

// ValidateUniqueLimit 按去重后的回收站 ID 数量校验批量上限。
func (r *BatchRecycledRequest) ValidateUniqueLimit(limit int) error {
	seen := make(map[string]struct{}, len(r.RecycledIDs))
	for _, id := range r.RecycledIDs {
		seen[id] = struct{}{}
		if len(seen) > limit {
			return fmt.Errorf("单次最多处理%d个回收站项目", limit)
		}
	}
	return nil
}

// EmptyRecycledRequest 清空回收站请求
type EmptyRecycledRequest struct {
	// 可以添加确认字段
}
