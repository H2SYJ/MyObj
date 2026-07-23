package request

type SubscriptionCreateRequest struct {
	Name               string                 `json:"name" binding:"required"`
	PluginID           string                 `json:"plugin_id" binding:"required"`
	Config             map[string]interface{} `json:"config"`
	GrantedPermissions []string               `json:"granted_permissions"`
	ScheduleTime       string                 `json:"schedule_time" binding:"required"`
	SavePath           string                 `json:"save_path" binding:"required"`
	InitialLimit       int                    `json:"initial_limit"`
	MaxItemsPerRun     int                    `json:"max_items_per_run"`
	Enabled            *bool                  `json:"enabled"`
	RunNow             *bool                  `json:"run_now"`
}

type SubscriptionUpdateRequest struct {
	ID                 string                  `json:"id" binding:"required"`
	Name               *string                 `json:"name"`
	Config             *map[string]interface{} `json:"config"`
	GrantedPermissions *[]string               `json:"granted_permissions"`
	ScheduleTime       *string                 `json:"schedule_time"`
	SavePath           *string                 `json:"save_path"`
	InitialLimit       *int                    `json:"initial_limit"`
	MaxItemsPerRun     *int                    `json:"max_items_per_run"`
}

type SubscriptionIDRequest struct {
	ID string `json:"id" binding:"required"`
}

type SubscriptionToggleRequest struct {
	ID      string `json:"id" binding:"required"`
	Enabled bool   `json:"enabled"`
}

type SubscriptionListRequest struct {
	Page     int `form:"page"`
	PageSize int `form:"pageSize"`
}

type SubscriptionHistoryRequest struct {
	SubscriptionID string `form:"subscription_id" binding:"required"`
	Page           int    `form:"page"`
	PageSize       int    `form:"pageSize"`
}

type SubscriptionThumbnailRetryRequest struct {
	ItemID string `json:"item_id" binding:"required"`
}
