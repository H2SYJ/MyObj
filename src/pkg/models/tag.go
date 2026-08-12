package models

import "time"

const (
	TagRuleScopeGlobal = "global"
	TagRuleScopeUser   = "user"

	TagRuleSetDraft    = "draft"
	TagRuleSetActive   = "active"
	TagRuleSetArchived = "archived"

	TagRuleTypeWord     = "word"
	TagRuleTypeStopWord = "stop_word"
	TagRuleTypeAlias    = "alias"
	TagRuleTypeRegex    = "regex"

	TagSourceManual   = "manual"
	TagSourceFilename = "filename"
	TagSourceMetadata = "metadata"
	TagSourceRule     = "rule"

	TagVisibilityInherit = "inherit"
	TagVisibilityPrivate = "private"
	TagVisibilityPublic  = "public"

	TagStatePending = "pending"
	TagStateRunning = "running"
	TagStateReady   = "ready"
	TagStatePartial = "partial"
	TagStateFailed  = "failed"

	TagRebuildFailureFailed   = "failed"
	TagRebuildFailureRetrying = "retrying"
	TagRebuildFailureResolved = "resolved"

	TagSystemCodeCinemaMode = "cinema_mode"
	TagNameCinemaMode       = "影视模式"
)

// TagCategory 定义标签的业务分类和展示样式。
type TagCategory struct {
	ID        string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	Code      string    `gorm:"column:code;type:varchar(64);not null;uniqueIndex" json:"code"`
	Name      string    `gorm:"column:name;type:varchar(64);not null" json:"name"`
	Color     string    `gorm:"column:color;type:varchar(32);not null" json:"color"`
	SortOrder int       `gorm:"column:sort_order;type:integer;not null;default:0" json:"sort_order"`
	Enabled   bool      `gorm:"column:enabled;type:boolean;not null;default:true;index" json:"enabled"`
	Builtin   bool      `gorm:"column:builtin;type:boolean;not null;default:false" json:"builtin"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (TagCategory) TableName() string { return "tag_category" }

// TagDefinition 保存归一化后的标签定义。
type TagDefinition struct {
	ID             string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	Name           string    `gorm:"column:name;type:varchar(255);not null" json:"name"`
	NormalizedName string    `gorm:"column:normalized_name;type:varchar(191);not null;uniqueIndex:uk_tag_definition,priority:1" json:"normalized_name"`
	CategoryID     string    `gorm:"column:category_id;type:varchar(64);not null;uniqueIndex:uk_tag_definition,priority:2;index" json:"category_id"`
	SystemCode     *string   `gorm:"column:system_code;type:varchar(64);uniqueIndex" json:"system_code,omitempty"`
	Builtin        bool      `gorm:"column:builtin;type:boolean;not null;default:false" json:"builtin"`
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
}

func (TagDefinition) TableName() string { return "tag_definition" }

// UserTagPreference 保存用户对共享标签的显示偏好，不改变标签定义和其他用户的数据。
type UserTagPreference struct {
	UserID                string    `gorm:"column:user_id;type:varchar(64);not null;primaryKey;index:idx_user_tag_preference_hidden,priority:1;index:idx_user_tag_preference_display_name,priority:1" json:"user_id"`
	TagID                 string    `gorm:"column:tag_id;type:varchar(64);not null;primaryKey;index" json:"tag_id"`
	Hidden                bool      `gorm:"column:hidden;type:boolean;not null;default:false;index:idx_user_tag_preference_hidden,priority:2" json:"hidden"`
	DisplayName           *string   `gorm:"column:display_name;type:varchar(255)" json:"display_name,omitempty"`
	NormalizedDisplayName *string   `gorm:"column:normalized_display_name;type:varchar(191);index:idx_user_tag_preference_display_name,priority:2" json:"normalized_display_name,omitempty"`
	DisplayCategoryID     *string   `gorm:"column:display_category_id;type:varchar(64);index" json:"display_category_id,omitempty"`
	CreatedAt             time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt             time.Time `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (UserTagPreference) TableName() string { return "user_tag_preference" }

// UserFileTag 保存用户文件与标签之间的一条来源绑定。
type UserFileTag struct {
	ID          string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	UserID      string    `gorm:"column:user_id;type:varchar(64);not null;index:idx_user_tag_file,priority:1;index:idx_user_file_tag,priority:1" json:"user_id"`
	UFID        string    `gorm:"column:uf_id;type:varchar(64);not null;index:idx_user_tag_file,priority:3;index:idx_user_file_tag,priority:2;index:idx_uf_source,priority:1;uniqueIndex:uk_user_file_tag_source,priority:1" json:"uf_id"`
	TagID       string    `gorm:"column:tag_id;type:varchar(64);not null;index:idx_user_tag_file,priority:2;index;uniqueIndex:uk_user_file_tag_source,priority:2" json:"tag_id"`
	SourceType  string    `gorm:"column:source_type;type:varchar(32);not null;index:idx_uf_source,priority:2;uniqueIndex:uk_user_file_tag_source,priority:3" json:"source_type"`
	SourceKey   string    `gorm:"column:source_key;type:varchar(128);not null;default:'';uniqueIndex:uk_user_file_tag_source,priority:4" json:"source_key"`
	RuleVersion int64     `gorm:"column:rule_version;type:bigint;not null;default:0" json:"rule_version"`
	Visibility  string    `gorm:"column:visibility;type:varchar(16);not null;default:'inherit';index" json:"visibility"`
	CreatedBy   string    `gorm:"column:created_by;type:varchar(64);not null;default:''" json:"created_by"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
}

func (UserFileTag) TableName() string { return "user_file_tag" }

// UserDirectoryTag 保存用户目录与手工标签的关联。目录标签不参与自动生成和公开范围。
type UserDirectoryTag struct {
	ID          string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	UserID      string    `gorm:"column:user_id;type:varchar(64);not null;index:idx_user_directory_tag,priority:1;uniqueIndex:uk_user_directory_tag,priority:1" json:"user_id"`
	DirectoryID int       `gorm:"column:directory_id;not null;index:idx_user_directory_tag,priority:2;uniqueIndex:uk_user_directory_tag,priority:2" json:"directory_id"`
	TagID       string    `gorm:"column:tag_id;type:varchar(64);not null;index;uniqueIndex:uk_user_directory_tag,priority:3" json:"tag_id"`
	CreatedBy   string    `gorm:"column:created_by;type:varchar(64);not null" json:"created_by"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
}

func (UserDirectoryTag) TableName() string { return "user_directory_tag" }

// UserFileTagExclusion 记录用户不希望自动标签再次出现的覆盖项。
type UserFileTagExclusion struct {
	UserID    string    `gorm:"column:user_id;type:varchar(64);not null;primaryKey" json:"user_id"`
	UFID      string    `gorm:"column:uf_id;type:varchar(64);not null;primaryKey;index" json:"uf_id"`
	TagID     string    `gorm:"column:tag_id;type:varchar(64);not null;primaryKey" json:"tag_id"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
}

func (UserFileTagExclusion) TableName() string { return "user_file_tag_exclusion" }

// UserFileTagState 保存自动标签生成版本，同时充当持久化文件级任务队列。
type UserFileTagState struct {
	UFID          string     `gorm:"column:uf_id;type:varchar(64);primaryKey" json:"uf_id"`
	UserID        string     `gorm:"column:user_id;type:varchar(64);not null;index" json:"user_id"`
	GlobalVersion int64      `gorm:"column:global_version;type:bigint;not null;default:0;index" json:"global_version"`
	UserVersion   int64      `gorm:"column:user_version;type:bigint;not null;default:0" json:"user_version"`
	MetadataVer   int64      `gorm:"column:metadata_version;type:bigint;not null;default:0" json:"metadata_version"`
	Status        string     `gorm:"column:status;type:varchar(32);not null;index:idx_tag_state_schedule,priority:1" json:"status"`
	LastError     string     `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	RetryCount    int        `gorm:"column:retry_count;type:integer;not null;default:0" json:"retry_count"`
	NextRetryAt   *time.Time `gorm:"column:next_retry_at;type:datetime;index:idx_tag_state_schedule,priority:2" json:"next_retry_at,omitempty"`
	RunToken      string     `gorm:"column:run_token;type:varchar(64);not null;default:'';index" json:"-"`
	LeaseExpires  *time.Time `gorm:"column:lease_expires_at;type:datetime;index" json:"-"`
	GeneratedAt   *time.Time `gorm:"column:generated_at;type:datetime" json:"generated_at,omitempty"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (UserFileTagState) TableName() string { return "user_file_tag_state" }

// FileMetadata 使用键值形式保存可扩展的物理文件元数据。
type FileMetadata struct {
	ID        string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	FileID    string    `gorm:"column:file_id;type:varchar(64);not null;uniqueIndex:uk_file_metadata,priority:1;index" json:"file_id"`
	Provider  string    `gorm:"column:provider;type:varchar(64);not null;uniqueIndex:uk_file_metadata,priority:2" json:"provider"`
	Key       string    `gorm:"column:key_name;type:varchar(128);not null;uniqueIndex:uk_file_metadata,priority:3" json:"key"`
	Value     string    `gorm:"column:value;type:text;not null" json:"value"`
	ValueType string    `gorm:"column:value_type;type:varchar(16);not null;default:'string'" json:"value_type"`
	Version   int64     `gorm:"column:version;type:bigint;not null;default:1" json:"version"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (FileMetadata) TableName() string { return "file_metadata" }

type FileMetadataState struct {
	FileID       string     `gorm:"column:file_id;type:varchar(64);primaryKey" json:"file_id"`
	Version      int64      `gorm:"column:version;type:bigint;not null;default:0" json:"version"`
	Status       string     `gorm:"column:status;type:varchar(32);not null;index" json:"status"`
	LastError    string     `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	RetryCount   int        `gorm:"column:retry_count;type:integer;not null;default:0" json:"retry_count"`
	NextRetryAt  *time.Time `gorm:"column:next_retry_at;type:datetime;index" json:"next_retry_at,omitempty"`
	RunToken     string     `gorm:"column:run_token;type:varchar(64);not null;default:''" json:"-"`
	LeaseExpires *time.Time `gorm:"column:lease_expires_at;type:datetime;index" json:"-"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (FileMetadataState) TableName() string { return "file_metadata_state" }

// TagRuleSet 保存一个可原子发布的规则版本。
type TagRuleSet struct {
	ID             string     `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	ScopeType      string     `gorm:"column:scope_type;type:varchar(16);not null;index:idx_tag_rule_scope,priority:1" json:"scope_type"`
	ScopeID        string     `gorm:"column:scope_id;type:varchar(64);not null;default:'';index:idx_tag_rule_scope,priority:2" json:"scope_id"`
	Version        int64      `gorm:"column:version;type:bigint;not null;index:idx_tag_rule_scope,priority:4" json:"version"`
	Revision       int        `gorm:"column:revision;type:integer;not null;default:1" json:"revision"`
	Status         string     `gorm:"column:status;type:varchar(16);not null;index:idx_tag_rule_scope,priority:3" json:"status"`
	BasedOnVersion int64      `gorm:"column:based_on_version;type:bigint;not null;default:0" json:"based_on_version"`
	CreatedBy      string     `gorm:"column:created_by;type:varchar(64);not null;default:''" json:"created_by"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
	PublishedAt    *time.Time `gorm:"column:published_at;type:datetime" json:"published_at,omitempty"`
	Rules          []TagRule  `gorm:"foreignKey:RuleSetID" json:"rules,omitempty"`
}

func (TagRuleSet) TableName() string { return "tag_rule_set" }

type TagRule struct {
	ID          string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	RuleSetID   string    `gorm:"column:rule_set_id;type:varchar(64);not null;index" json:"rule_set_id"`
	Type        string    `gorm:"column:rule_type;type:varchar(32);not null;index" json:"type"`
	TargetField string    `gorm:"column:target_field;type:varchar(128);not null;default:'filename'" json:"target_field"`
	Pattern     string    `gorm:"column:pattern;type:text;not null" json:"pattern"`
	Replacement string    `gorm:"column:replacement;type:text" json:"replacement"`
	CategoryID  string    `gorm:"column:category_id;type:varchar(64);not null;default:'other';index" json:"category_id"`
	Priority    int       `gorm:"column:priority;type:integer;not null;default:0" json:"priority"`
	Weight      float64   `gorm:"column:weight;type:decimal(8,3);not null;default:1" json:"weight"`
	Enabled     bool      `gorm:"column:enabled;type:boolean;not null;default:true" json:"enabled"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (TagRule) TableName() string { return "tag_rule" }

type TagRebuildJob struct {
	ID            string     `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	ScopeType     string     `gorm:"column:scope_type;type:varchar(16);not null;index" json:"scope_type"`
	ScopeID       string     `gorm:"column:scope_id;type:varchar(64);not null;default:'';index" json:"scope_id"`
	TargetVersion int64      `gorm:"column:target_version;type:bigint;not null" json:"target_version"`
	Status        string     `gorm:"column:status;type:varchar(32);not null;index:idx_tag_job_schedule,priority:1" json:"status"`
	Cursor        string     `gorm:"column:cursor_value;type:varchar(64);not null;default:''" json:"cursor"`
	Total         int64      `gorm:"column:total;type:bigint;not null;default:0" json:"total"`
	Processed     int64      `gorm:"column:processed;type:bigint;not null;default:0" json:"processed"`
	Succeeded     int64      `gorm:"column:succeeded;type:bigint;not null;default:0" json:"succeeded"`
	Failed        int64      `gorm:"column:failed;type:bigint;not null;default:0" json:"failed"`
	LastError     string     `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	RunToken      string     `gorm:"column:run_token;type:varchar(64);not null;default:'';index" json:"-"`
	LeaseExpires  *time.Time `gorm:"column:lease_expires_at;type:datetime;index:idx_tag_job_schedule,priority:2" json:"-"`
	RequestedBy   string     `gorm:"column:requested_by;type:varchar(64);not null;default:''" json:"requested_by"`
	StartedAt     *time.Time `gorm:"column:started_at;type:datetime" json:"started_at,omitempty"`
	FinishedAt    *time.Time `gorm:"column:finished_at;type:datetime" json:"finished_at,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (TagRebuildJob) TableName() string { return "tag_rebuild_job" }

// TagRebuildFailure 保存重建任务的逐文件失败信息，便于管理员定向重试。
type TagRebuildFailure struct {
	JobID      string    `gorm:"column:job_id;type:varchar(64);not null;primaryKey;index:idx_tag_rebuild_failure_status,priority:1" json:"job_id"`
	UFID       string    `gorm:"column:uf_id;type:varchar(64);not null;primaryKey;index" json:"uf_id"`
	UserID     string    `gorm:"column:user_id;type:varchar(64);not null;index" json:"user_id"`
	Status     string    `gorm:"column:status;type:varchar(32);not null;index:idx_tag_rebuild_failure_status,priority:2" json:"status"`
	Error      string    `gorm:"column:error_message;type:text" json:"error,omitempty"`
	RetryCount int       `gorm:"column:retry_count;type:integer;not null;default:0" json:"retry_count"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;not null" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at;type:datetime;not null" json:"updated_at"`
}

func (TagRebuildFailure) TableName() string { return "tag_rebuild_failure" }
