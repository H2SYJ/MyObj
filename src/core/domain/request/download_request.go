package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type UniqueHTTPHeaders map[string]string

// UnmarshalJSON 在Gin绑定请求时保留重复名称校验，避免普通map静默覆盖凭据。
func (headers *UniqueHTTPHeaders) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("request_headers必须是字符串键值对象")
	}
	result := UniqueHTTPHeaders{}
	seen := map[string]bool{}
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := nameToken.(string)
		if !ok {
			return fmt.Errorf("请求头名称必须是字符串")
		}
		lower := strings.ToLower(name)
		if seen[lower] {
			return fmt.Errorf("请求头名称重复: %s", name)
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("请求头值必须是字符串")
		}
		seen[lower] = true
		result[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return fmt.Errorf("request_headers包含多余内容")
	}
	*headers = result
	return nil
}

// CreateOfflineDownloadRequest 创建离线下载任务请求
type CreateOfflineDownloadRequest struct {
	// 下载URL
	URL string `json:"url" binding:"required"`
	// 保存的虚拟路径（可选，默认为/离线下载/）
	VirtualPath string `json:"virtual_path"`
	// 是否加密存储
	EnableEncryption bool `json:"enable_encryption"`
	// 文件密码（加密文件必需）
	FilePassword string `json:"file_password"`
	// 下载类型：auto、http、hls，默认为auto
	DownloadType string `json:"download_type"`
	// HLS输出文件名（可选）
	FileName string `json:"file_name"`
	// HTTP/HLS自定义请求头；指针用于区分未传递与显式清空
	RequestHeaders *UniqueHTTPHeaders `json:"request_headers"`
	// 允许携带自定义请求头的额外精确主机
	HeaderHosts *[]string `json:"header_hosts"`
}

// DownloadTaskListRequest 下载任务列表请求
type DownloadTaskListRequest struct {
	// 任务状态（可选，0=初始化,1=下载中,2=暂停,3=完成,4=失败，-1=所有状态）
	State int `form:"state"`
	// 任务类型（可选，0、4、5、9=离线下载，7=网盘文件下载，-1=所有类型）
	Type int `form:"type"`
	// 多任务类型过滤，使用逗号分隔；不能与type同时传递
	Types string `form:"types"`
	// 页码
	Page int `form:"page" binding:"required,min=1"`
	// 每页数量
	PageSize int `form:"pageSize" binding:"required,min=1,max=100"`
}

// TaskOperationRequest 任务操作请求（暂停、恢复、重试、取消）
type TaskOperationRequest struct {
	// 任务ID
	TaskID string `json:"task_id" binding:"required"`
	// 加密任务恢复或重试密码，不会持久化
	FilePassword string `json:"file_password"`
	// 更新HLS自定义请求头；未传递时继续使用原值
	RequestHeaders *UniqueHTTPHeaders `json:"request_headers"`
	// 更新允许携带自定义请求头的额外主机
	HeaderHosts *[]string `json:"header_hosts"`
}

// DeleteTaskRequest 删除任务请求
type DeleteTaskRequest struct {
	// 任务ID
	TaskID string `json:"task_id" binding:"required"`
}

// CreateLocalFileDownloadRequest 创建网盘文件下载任务请求
type CreateLocalFileDownloadRequest struct {
	// 文件ID
	FileID string `json:"file_id" binding:"required"`
	// 文件解密密码（加密文件必需）
	FilePassword string `json:"file_password"`
}

// CreateVideoPlayRequest 创建视频播放任务请求
type CreateVideoPlayRequest struct {
	// 视频文件ID
	FileID string `json:"file_id" binding:"required"`
	// 视频文件解密密码（加密文件必需）
	FilePassword string `json:"file_password"`
}

// ParseTorrentRequest 解析种子/磁力链请求
type ParseTorrentRequest struct {
	// 种子文件内容（Base64编码）或磁力链接（magnet:开头）
	Content string `json:"content" binding:"required"`
}

// StartTorrentDownloadRequest 开始种子/磁力链下载请求
type StartTorrentDownloadRequest struct {
	// 种子文件内容（Base64编码）或磁力链接
	Content string `json:"content" binding:"required"`
	// 要下载的文件索引列表
	FileIndexes []int `json:"file_indexes" binding:"required"`
	// 保存的虚拟路径（可选，默认为/离线下载/）
	VirtualPath string `json:"virtual_path"`
	// 是否加密存储
	EnableEncryption bool `json:"enable_encryption"`
	// 文件密码（加密文件必需）
	FilePassword string `json:"file_password"`
}
