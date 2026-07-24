package models

import "time"

type InstalledPlugin struct {
	ID            string    `gorm:"column:id;type:varchar(128);primaryKey" json:"id"`
	Name          string    `gorm:"column:name;type:varchar(255);not null" json:"name"`
	Version       string    `gorm:"column:version;type:varchar(64);not null" json:"version"`
	APIVersion    string    `gorm:"column:api_version;type:varchar(32);not null" json:"api_version"`
	Author        string    `gorm:"column:author;type:varchar(255)" json:"author"`
	Description   string    `gorm:"column:description;type:text" json:"description"`
	ManifestJSON  string    `gorm:"column:manifest_json;type:text;not null" json:"-"`
	PackagePath   string    `gorm:"column:package_path;type:text;not null" json:"-"`
	WASMPath      string    `gorm:"column:wasm_path;type:text;not null" json:"-"`
	PackageSHA256 string    `gorm:"column:package_sha256;type:varchar(64);not null" json:"package_sha256"`
	WASMSHA256    string    `gorm:"column:wasm_sha256;type:varchar(64);not null" json:"wasm_sha256"`
	Permissions   string    `gorm:"column:permissions;type:text" json:"-"`
	Enabled       bool      `gorm:"column:enabled;type:boolean;not null;index" json:"enabled"`
	InstalledBy   string    `gorm:"column:installed_by;type:varchar(64)" json:"installed_by"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (InstalledPlugin) TableName() string { return "installed_plugin" }

type Subscription struct {
	ID                 string     `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	UserID             string     `gorm:"column:user_id;type:varchar(64);not null;index:idx_subscription_user" json:"user_id"`
	Name               string     `gorm:"column:name;type:varchar(255);not null" json:"name"`
	PluginID           string     `gorm:"column:plugin_id;type:varchar(128);not null;index" json:"plugin_id"`
	PluginVersion      string     `gorm:"column:plugin_version;type:varchar(64);not null" json:"plugin_version"`
	ConfigEncrypted    string     `gorm:"column:config_encrypted;type:text" json:"-"`
	GrantedPermissions string     `gorm:"column:granted_permissions;type:text" json:"-"`
	ScheduleTime       string     `gorm:"column:schedule_time;type:varchar(5);not null" json:"schedule_time"`
	SavePath           string     `gorm:"column:save_path;type:text;not null" json:"save_path"`
	InitialLimit       int        `gorm:"column:initial_limit;type:integer;not null;default:10" json:"initial_limit"`
	MaxItemsPerRun     int        `gorm:"column:max_items_per_run;type:integer;not null;default:100" json:"max_items_per_run"`
	SourceGeneration   int        `gorm:"column:source_generation;type:integer;not null;default:1" json:"source_generation"`
	Enabled            bool       `gorm:"column:enabled;type:boolean;not null;index:idx_subscription_due,priority:1;index:idx_subscription_dispatch,priority:1" json:"enabled"`
	Status             string     `gorm:"column:status;type:varchar(32);not null;default:'ready';index:idx_subscription_dispatch,priority:2" json:"status"`
	LastError          string     `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	NextRunAt          *time.Time `gorm:"column:next_run_at;type:datetime;index:idx_subscription_due,priority:2;index:idx_subscription_dispatch,priority:3" json:"next_run_at"`
	LastRunAt          *time.Time `gorm:"column:last_run_at;type:datetime" json:"last_run_at"`
	CreatedAt          time.Time  `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (Subscription) TableName() string { return "subscription" }

type SubscriptionRun struct {
	ID             string     `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	SubscriptionID string     `gorm:"column:subscription_id;type:varchar(64);not null;index:idx_subscription_run" json:"subscription_id"`
	Trigger        string     `gorm:"column:trigger;type:varchar(16);not null" json:"trigger"`
	Status         string     `gorm:"column:status;type:varchar(32);not null;index" json:"status"`
	RunToken       string     `gorm:"column:run_token;type:varchar(64);index" json:"-"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at;type:datetime;index" json:"lease_expires_at,omitempty"`
	ItemsFound     int        `gorm:"column:items_found;type:integer;not null;default:0" json:"items_found"`
	TasksCreated   int        `gorm:"column:tasks_created;type:integer;not null;default:0" json:"tasks_created"`
	ItemsSkipped   int        `gorm:"column:items_skipped;type:integer;not null;default:0" json:"items_skipped"`
	ErrorMsg       string     `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	StartedAt      *time.Time `gorm:"column:started_at;type:datetime" json:"started_at,omitempty"`
	FinishedAt     *time.Time `gorm:"column:finished_at;type:datetime" json:"finished_at,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
}

func (SubscriptionRun) TableName() string { return "subscription_run" }

type SubscriptionItem struct {
	ID                      string     `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	SubscriptionID          string     `gorm:"column:subscription_id;type:varchar(64);not null;uniqueIndex:uk_subscription_item,priority:1;index" json:"subscription_id"`
	SourceGeneration        int        `gorm:"column:source_generation;type:integer;not null;uniqueIndex:uk_subscription_item,priority:2" json:"source_generation"`
	ItemKey                 string     `gorm:"column:item_key;type:varchar(64);not null;uniqueIndex:uk_subscription_item,priority:3" json:"item_key"`
	ExternalID              string     `gorm:"column:external_id;type:text" json:"external_id,omitempty"`
	Title                   string     `gorm:"column:title;type:text" json:"title"`
	URL                     string     `gorm:"column:url;type:text;not null" json:"url"`
	DownloadType            string     `gorm:"column:download_type;type:varchar(16);not null" json:"download_type"`
	FileName                string     `gorm:"column:file_name;type:text" json:"file_name,omitempty"`
	SavePath                string     `gorm:"column:save_path;type:text;not null" json:"save_path"`
	ThumbnailURL            string     `gorm:"column:thumbnail_url;type:text" json:"thumbnail_url,omitempty"`
	RequestHeadersEncrypted string     `gorm:"column:request_headers_encrypted;type:text" json:"-"`
	HeaderHostsJSON         string     `gorm:"column:header_hosts_json;type:text" json:"-"`
	HeadersDigest           string     `gorm:"column:headers_digest;type:varchar(64)" json:"-"`
	DownloadTaskID          string     `gorm:"column:download_task_id;type:varchar(64);index" json:"download_task_id,omitempty"`
	Status                  string     `gorm:"column:status;type:varchar(32);not null;index" json:"status"`
	ErrorMsg                string     `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	ThumbnailStatus         string     `gorm:"column:thumbnail_status;type:varchar(32);not null;default:'none';index;index:idx_subscription_thumbnail_dispatch,priority:1" json:"thumbnail_status"`
	ThumbnailRetryCount     int        `gorm:"column:thumbnail_retry_count;type:integer;not null;default:0" json:"thumbnail_retry_count"`
	ThumbnailNextRetryAt    *time.Time `gorm:"column:thumbnail_next_retry_at;type:datetime;index;index:idx_subscription_thumbnail_dispatch,priority:2" json:"thumbnail_next_retry_at,omitempty"`
	ThumbnailError          string     `gorm:"column:thumbnail_error;type:text" json:"thumbnail_error,omitempty"`
	PublishedAt             *time.Time `gorm:"column:published_at;type:datetime;index" json:"published_at,omitempty"`
	CreatedAt               time.Time  `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt               time.Time  `gorm:"column:updated_at;type:datetime;not null;index:idx_subscription_thumbnail_dispatch,priority:3" json:"updated_at"`
}

func (SubscriptionItem) TableName() string { return "subscription_item" }

type PluginAuditLog struct {
	ID             string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	PluginID       string    `gorm:"column:plugin_id;type:varchar(128);not null;index" json:"plugin_id"`
	PluginVersion  string    `gorm:"column:plugin_version;type:varchar(64)" json:"plugin_version"`
	SubscriptionID string    `gorm:"column:subscription_id;type:varchar(64);index" json:"subscription_id,omitempty"`
	UserID         string    `gorm:"column:user_id;type:varchar(64);index" json:"user_id,omitempty"`
	Action         string    `gorm:"column:action;type:varchar(64);not null" json:"action"`
	Summary        string    `gorm:"column:summary;type:text" json:"summary,omitempty"`
	ResultCount    int       `gorm:"column:result_count;type:integer;not null;default:0" json:"result_count"`
	DurationMS     int64     `gorm:"column:duration_ms;type:bigint;not null;default:0" json:"duration_ms"`
	Status         string    `gorm:"column:status;type:varchar(32);not null" json:"status"`
	ErrorMsg       string    `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime;not null;index" json:"created_at"`
}

func (PluginAuditLog) TableName() string { return "plugin_audit_log" }
