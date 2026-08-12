package request

type ManualTagInput struct {
	Name       string `json:"name" binding:"required"`
	CategoryID string `json:"category_id"`
	Visibility string `json:"visibility"`
}

type UpdateManualTagsRequest struct {
	Add          []ManualTagInput `json:"add"`
	RemoveTagIDs []string         `json:"remove_tag_ids"`
}

type UpdateDirectoryTagsRequest struct {
	Add          []ManualTagInput `json:"add"`
	RemoveTagIDs []string         `json:"remove_tag_ids"`
}

type UpdateTagExclusionsRequest struct {
	SuppressTagIDs []string `json:"suppress_tag_ids"`
	RestoreTagIDs  []string `json:"restore_tag_ids"`
}

type BatchUpdateTagsRequest struct {
	FileIDs      []string         `json:"file_ids" binding:"required,min=1,max=100"`
	Add          []ManualTagInput `json:"add"`
	RemoveTagIDs []string         `json:"remove_tag_ids"`
}

type TagSuggestionRequest struct {
	Keyword string `form:"keyword"`
	Limit   int    `form:"limit"`
}

type TagRuleInput struct {
	ID          string  `json:"id"`
	Type        string  `json:"type" binding:"required"`
	TargetField string  `json:"target_field"`
	Pattern     string  `json:"pattern" binding:"required"`
	Replacement string  `json:"replacement"`
	CategoryID  string  `json:"category_id"`
	Priority    int     `json:"priority"`
	Weight      float64 `json:"weight"`
	Enabled     bool    `json:"enabled"`
}

type UpdatePersonalDictionaryRequest struct {
	Rules []TagRuleInput `json:"rules"`
}

type TagPreviewRequest struct {
	Samples []string       `json:"samples" binding:"required,min=1,max=100"`
	Rules   []TagRuleInput `json:"rules"`
}

type UpdateTagCloudItemRequest struct {
	DisplayName       string   `json:"display_name"`
	DisplayCategoryID string   `json:"display_category_id"`
	Aliases           []string `json:"aliases" binding:"max=100"`
}

type AdminTagCategoryRequest struct {
	ID        string `json:"id"`
	Code      string `json:"code" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Color     string `json:"color"`
	SortOrder int    `json:"sort_order"`
	Enabled   bool   `json:"enabled"`
}

type AdminSaveTagDraftRequest struct {
	Revision int            `json:"revision" binding:"required,min=1"`
	Rules    []TagRuleInput `json:"rules"`
}

type AdminTagSettingsRequest struct {
	Enabled bool `json:"enabled"`
	Limit   int  `json:"limit" binding:"required,min=1,max=100"`
}

type TagJobOperationRequest struct {
	JobID string `json:"job_id" binding:"required"`
}
