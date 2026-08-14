package models

import (
	"myobj/src/pkg/custom_type"
	"time"
)

// DownloadTask 下载任务表
type DownloadTask struct {
	// 任务 ID
	ID string `gorm:"column:id;type:varchar(64);primaryKey"`
	// 用户 ID
	UserID string `gorm:"column:user_id;type:varchar(64);index;index:idx_download_user_type_state_create,priority:1"`
	// 文件 ID
	FileID string `gorm:"column:file_id;type:text"`
	// 文件名
	FileName string `gorm:"column:file_name;type:text"`
	// 文件大小
	FileSize int64 `gorm:"column:file_size;type:bigint"`
	// 已下载大小
	DownloadedSize int64 `gorm:"column:downloaded_size;type:bigint;default:0"`
	// 下载进度 (0-100)
	Progress int `gorm:"column:progress;type:integer;default:0"`
	// 下载速度 (字节/秒)
	Speed int64 `gorm:"column:speed;type:bigint;default:0"`
	// 任务类型
	Type int `gorm:"column:type;type:integer;not null;index:idx_download_user_type_state_create,priority:2;index:idx_download_schedule,priority:2"`
	// 下载URL
	URL string `gorm:"column:url;type:text"`
	// 下载路径
	Path string `gorm:"column:path;type:text"`
	// 保存目录的用户虚拟绝对路径
	SavePath string `gorm:"column:save_path;type:text"`
	// 任务状态
	State int `gorm:"column:state;type:integer;index:idx_download_user_type_state_create,priority:3;index:idx_download_schedule,priority:1"`
	// 错误信息
	ErrorMsg string `gorm:"column:error_msg;type:text"`
	// 目标临时目录
	TargetDir string `gorm:"column:target_dir;type:text"`
	// 是否支持断点续传
	SupportRange bool `gorm:"column:support_range;type:boolean;default:false"`
	// 是否加密存储
	EnableEncryption bool `gorm:"column:enable_encryption;type:boolean;default:false"`
	// 种子InfoHash（BT/磁力链任务）
	InfoHash string `gorm:"column:info_hash;type:text;index,length:255"`
	// 种子内文件索引（BT/磁力链任务）
	FileIndex int `gorm:"column:file_index;type:integer"`
	// 种子名称（BT/磁力链任务）
	TorrentName string `gorm:"column:torrent_name;type:text"`
	// 批次ID，同一种子选择的多个文件共用一个批次
	BatchID string `gorm:"column:batch_id;type:varchar(64);index:idx_download_batch_id"`
	// 本次执行令牌，用于阻止旧goroutine覆盖新状态
	RunToken string `gorm:"column:run_token;type:varchar(64);index:idx_download_run_token"`
	// 当前工作进程ID
	WorkerID string `gorm:"column:worker_id;type:varchar(128)"`
	// 当前任务租约到期时间
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at;type:datetime;index:idx_download_lease_expires"`
	// 已重试次数
	RetryCount int `gorm:"column:retry_count;type:integer;default:0"`
	// 下次允许重试时间
	NextRetryAt *time.Time `gorm:"column:next_retry_at;type:datetime;index:idx_download_next_retry;index:idx_download_schedule,priority:3"`
	// 已预留的用户空间
	ReservedSize int64 `gorm:"column:reserved_size;type:bigint;default:0"`
	// HTTP/HLS自定义请求头密文，永不通过API返回
	RequestHeadersEncrypted string `gorm:"column:request_headers_encrypted;type:text"`
	// HTTP/HLS请求头允许发送的精确主机列表（JSON）
	HeaderHostsJSON string `gorm:"column:header_hosts_json;type:text"`
	// 是否需要用户更新HTTP/HLS请求头后再恢复
	RequiresHeaders bool `gorm:"column:requires_headers;type:boolean;default:false"`
	// 创建时间
	CreateTime custom_type.JsonTime `gorm:"column:create_time;type:datetime;index:idx_download_user_type_state_create,priority:4;index:idx_download_schedule,priority:4"`
	// 更新时间
	UpdateTime custom_type.JsonTime `gorm:"column:update_time;type:datetime"`
	// 完成时间
	FinishTime custom_type.JsonTime `gorm:"column:finish_time;type:datetime"`
}

func (DownloadTask) TableName() string {
	return "download_task"
}
