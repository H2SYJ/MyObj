package request

import (
	"fmt"
	"myobj/src/pkg/custom_type"
)

type CreateShareRequest struct {
	// 文件ID
	FileID string `json:"file_id"`
	// 过期时间
	Expire custom_type.JsonTime `json:"expire"`
	// 密码
	Password string `json:"password"`
}

type DeleteShareRequest struct {
	// 分享ID
	ID int `json:"id"`
}

// BatchDeleteShareRequest 批量删除分享请求，单次最多处理 200 项。
type BatchDeleteShareRequest struct {
	IDs []int `json:"ids" binding:"required,min=1,dive,gt=0"`
}

// ValidateUniqueLimit 按去重后的分享 ID 数量校验批量上限。
func (r *BatchDeleteShareRequest) ValidateUniqueLimit(limit int) error {
	seen := make(map[int]struct{}, len(r.IDs))
	for _, id := range r.IDs {
		seen[id] = struct{}{}
		if len(seen) > limit {
			return fmt.Errorf("单次最多处理%d个分享", limit)
		}
	}
	return nil
}

type UpdateSharePasswordRequest struct {
	// 分享ID
	ID int `json:"id"`
	// 新密码
	Password string `json:"password"`
}

type ShareDownloadRequest struct {
	// 分享Token
	Token string `json:"token"`
	// 分享密码（如果有）
	Password string `json:"password"`
}
