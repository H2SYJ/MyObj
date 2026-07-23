package response

// BatchOperationFailedItem 记录批量操作中失败的单项。
type BatchOperationFailedItem struct {
	ItemID string `json:"item_id"`
	Reason string `json:"reason"`
}

// BatchOperationResponse 是资源批量操作的统一响应。
type BatchOperationResponse struct {
	TotalCount   int                        `json:"total_count"`
	SuccessCount int                        `json:"success_count"`
	FailedCount  int                        `json:"failed_count"`
	FailedItems  []BatchOperationFailedItem `json:"failed_items"`
}
