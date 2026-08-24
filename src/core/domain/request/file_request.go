package request

// UploadPrecheckRequest 上传预检查
type UploadPrecheckRequest struct {
	UserID string `json:"user_id"`
	// 文件名
	FileName string `json:"file_name"`
	// 文件大小 字节
	FileSize int64 `json:"file_size"`
	// 文件hash签名
	ChunkSignature string `json:"chunk_signature"`
	// 目录ID
	DirectoryID int `json:"directory_id" binding:"required,min=1"`
	// 文件分片的DM5列表
	FilesMd5 []string `json:"files_md5"`
}

// FileSearchRequest 文件搜索请求
type FileSearchRequest struct {
	Keyword   string `form:"keyword"`
	Type      string `form:"type"`
	SortBy    string `form:"sortBy"`
	SortOrder string `form:"sortOrder"`
	// DirectoryID 大于0时将个人搜索限制在当前目录；公开搜索忽略该字段。
	DirectoryID int    `form:"directory_id"`
	TagIDs      string `form:"tag_ids"`
	TagMode     string `form:"tag_mode"`
	Page        int    `form:"page"`
	PageSize    int    `form:"pageSize"`
}

// FileListRequest 文件列表请求
type FileListRequest struct {
	// 当前目录ID，0表示用户根目录
	DirectoryID int `form:"directory_id" binding:"min=0"`
	// 文件类型
	Type string `form:"type"`
	// 排序字段（name, size, time）
	SortBy string `form:"sortBy"`
	// 排序方向（asc, desc）
	SortOrder string `form:"sortOrder"`
	// TagIDs 为逗号分隔的标签ID；TagMode 支持all或any。
	TagIDs  string `form:"tag_ids"`
	TagMode string `form:"tag_mode"`
	// 页码（从1开始）
	Page int `form:"page" binding:"required,min=1"`
	// 每页数量
	PageSize int `form:"pageSize" binding:"required,min=1,max=100"`
}

// MakeDirRequest 创建文件夹请求
type MakeDirRequest struct {
	ParentID int    `json:"parent_id" binding:"required,min=1"`
	Name     string `json:"name" binding:"required"`
}

// MoveFileRequest 移动文件请求
type MoveFileRequest struct {
	FileID            string `json:"file_id" binding:"required"`
	TargetDirectoryID int    `json:"target_directory_id" binding:"required,min=1"`
}

// MoveItemsRequest 批量移动文件和目录。
type MoveItemsRequest struct {
	FileIDs           []string `json:"file_ids"`
	DirectoryIDs      []int    `json:"directory_ids"`
	TargetDirectoryID int      `json:"target_directory_id" binding:"required,min=1"`
}

// DeleteItemsRequest 批量将文件和目录移动到回收站。
type DeleteItemsRequest struct {
	FileIDs []string `json:"file_ids"`
	DirIDs  []int    `json:"dir_ids"`
}

// DeleteFileRequest 删除文件请求
type DeleteFileRequest struct {
	// 文件ID列表
	FileIDs []string `json:"file_ids" binding:"required"`
}

// EditFileContentRequest 在线编辑文本文件内容请求
type EditFileContentRequest struct {
	// 用户文件ID（UserFiles的UfID）
	FileID string `json:"file_id" binding:"required"`
	// 编辑后的文本内容
	Content string `json:"content" binding:"required"`
	// 文件解密密码（加密文件必填）
	FilePassword string `json:"file_password"`
	// 编辑器加载时的文件哈希，用于并发防覆盖（可选，传了才校验）
	BaseHash string `json:"base_hash"`
}

// FileUploadRequest 文件上传请求
type FileUploadRequest struct {
	// 预检ID
	PrecheckID string `form:"precheck_id" binding:"required"`
	// 分片索引（分片上传时必须，从0开始）
	ChunkIndex *int `form:"chunk_index"`
	// 总分片数（分片上传时必须）
	TotalChunks *int `form:"total_chunks"`
	// 当前分片的MD5（分片上传时必须）
	ChunkMD5 string `form:"chunk_md5"`
	// 是否需要加密
	IsEnc bool `form:"is_enc"`
	// 文件加密密码（加密文件必须）
	FilePassword string `form:"file_password"`
	// 是否在最后一个分片落盘后异步处理文件
	AsyncFinalize bool `form:"async_finalize"`
}

// VideoPlayPrecheckRequest 视频播放预检请求
type VideoPlayPrecheckRequest struct {
	// 文件ID
	FileID string `json:"file_id" binding:"required"`
	// 分享密码（如果是分享链接访问）
	SharePassword string `json:"share_password"`
}

// PublicFileListRequest 公开文件列表请求
type PublicFileListRequest struct {
	// 文件类型
	Type string `form:"type"`
	// 排序字段（name, size, time）
	SortBy  string `form:"sortBy"`
	TagIDs  string `form:"tag_ids"`
	TagMode string `form:"tag_mode"`
	// 页码（从1开始）
	Page int `form:"page" binding:"required,min=1"`
	// 每页数量
	PageSize int `form:"pageSize" binding:"required,min=1,max=100"`
}

// UploadProgressRequest 上传进度查询请求
type UploadProgressRequest struct {
	// 预检ID
	PrecheckID string `form:"precheck_id" binding:"required"`
}

// RetryUploadFinalizeRequest 重新提交失败的文件处理任务
type RetryUploadFinalizeRequest struct {
	PrecheckID   string `json:"precheck_id" binding:"required"`
	FilePassword string `json:"file_password"`
}

// DeleteUploadTaskRequest 删除上传任务请求
type DeleteUploadTaskRequest struct {
	// 任务ID（预检ID）
	TaskID string `json:"task_id" binding:"required"`
}

// RenewExpiredTaskRequest 延期过期任务请求
type RenewExpiredTaskRequest struct {
	// 任务ID（预检ID）
	TaskID string `json:"task_id" binding:"required"`
	// 延期天数（默认7天）
	Days int `json:"days"`
}

// RenameFileRequest 文件重命名请求
type RenameFileRequest struct {
	// 文件ID（uf_id）
	FileID string `json:"file_id" binding:"required"`
	// 新文件名
	NewFileName string `json:"new_file_name" binding:"required"`
}

// RenameDirRequest 目录重命名请求
type RenameDirRequest struct {
	// 目录ID
	DirID int `json:"dir_id" binding:"required"`
	// 新目录名
	NewDirName string `json:"new_dir_name" binding:"required"`
}

// SetFilePublicRequest 设置文件公开状态请求
type SetFilePublicRequest struct {
	// 文件ID（uf_id）
	FileID string `json:"file_id" binding:"required"`
	// 是否公开
	Public bool `json:"public"`
}

// DeleteDirRequest 删除目录请求
type DeleteDirRequest struct {
	// 目录ID
	DirID int `json:"dir_id" binding:"required"`
}

// UploadTaskListRequest 上传任务列表请求
type UploadTaskListRequest struct {
	// 页码（从1开始）
	Page int `form:"page" binding:"required,min=1"`
	// 每页数量
	PageSize int `form:"pageSize" binding:"required,min=1,max=100"`
}
