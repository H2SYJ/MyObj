package models

import (
	"myobj/src/pkg/custom_type"
)

// UploadTask 上传任务表（用于持久化上传任务，支持断点续传）
type UploadTask struct {
	// 任务ID（使用 precheck_id 作为主键）
	ID string `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	// 用户ID
	UserID string `gorm:"column:user_id;type:varchar(64);index" json:"user_id"`
	// 文件名
	FileName string `gorm:"column:file_name;type:text;not null" json:"file_name"`
	// 文件大小（字节）
	FileSize int64 `gorm:"column:file_size;type:bigint;not null" json:"file_size"`
	// 分片大小（字节，默认5MB）
	ChunkSize int64 `gorm:"column:chunk_size;type:bigint;not null;default:5242880" json:"chunk_size"`
	// 总分片数
	TotalChunks int `gorm:"column:total_chunks;type:integer;not null" json:"total_chunks"`
	// 已上传分片数
	UploadedChunks int `gorm:"column:uploaded_chunks;type:integer;default:0" json:"uploaded_chunks"`
	// 文件hash签名（用于秒传检测）
	ChunkSignature string `gorm:"column:chunk_signature;type:text" json:"chunk_signature"`
	// 目录ID
	DirectoryID int `gorm:"column:directory_id;type:integer;not null" json:"directory_id"`
	// 临时目录路径
	TempDir string `gorm:"column:temp_dir;type:text" json:"temp_dir"`
	// 预检阶段选中的磁盘ID
	DiskID string `gorm:"column:disk_id;type:text" json:"disk_id"`
	// 是否为加密上传。加密密码不会持久化。
	IsEnc bool `gorm:"column:is_enc;type:boolean;default:false" json:"is_enc"`
	// 秒传校验需要的前三个分片哈希
	FirstChunkHash  string `gorm:"column:first_chunk_hash;type:text" json:"first_chunk_hash"`
	SecondChunkHash string `gorm:"column:second_chunk_hash;type:text" json:"second_chunk_hash"`
	ThirdChunkHash  string `gorm:"column:third_chunk_hash;type:text" json:"third_chunk_hash"`
	// 任务状态（pending/uploading/processing/completed/failed/aborted）
	Status string `gorm:"column:status;type:varchar(20);default:'pending'" json:"status"`
	// 后台处理阶段（queued/validating/storing/encrypting/committing）
	ProcessingStage string `gorm:"column:processing_stage;type:varchar(20)" json:"processing_stage"`
	// 后台处理完成后生成的文件ID
	ResultFileID string `gorm:"column:result_file_id;type:text" json:"result_file_id"`
	// 错误信息
	ErrorMessage string `gorm:"column:error_message;type:text" json:"error_message"`
	// 创建时间
	CreateTime custom_type.JsonTime `gorm:"column:create_time;type:datetime" json:"create_time"`
	// 更新时间
	UpdateTime custom_type.JsonTime `gorm:"column:update_time;type:datetime" json:"update_time"`
	// 过期时间（7天后自动清理）
	ExpireTime custom_type.JsonTime `gorm:"column:expire_time;type:datetime" json:"expire_time"`
}

func (UploadTask) TableName() string {
	return "upload_task"
}
