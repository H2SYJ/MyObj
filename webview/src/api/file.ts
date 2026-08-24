import { get, post, putFormData, upload } from '@/utils/network/request'
import { filterParams } from '@/utils/common/params'
import { API_ENDPOINTS, API_BASE_URL } from '@/config/api'
import type { FileListRequest, FileListResponse, ApiResponse, CompactTag } from '@/types'
import logger from '@/plugins/logger'
import cache from '@/plugins/cache'

const thumbnailCacheVersions = new Map<string, number>()

export const invalidateThumbnailCache = (fileId: string) => {
  const previousVersion = thumbnailCacheVersions.get(fileId) || 0
  thumbnailCacheVersions.set(fileId, Math.max(Date.now(), previousVersion + 1))
}

// 文件搜索请求参数
export interface FileSearchParams {
  keyword?: string
  type?: string
  sortBy?: string
  sortOrder?: 'asc' | 'desc'
  directory_id?: number
  tag_ids?: string
  page?: number
  pageSize?: number
}

// 文件信息
export interface SearchFileItem {
  id: string
  name: string
  size: number
  mime: string
  owner_name?: string
  created_at: string
  updated_at: string
  uf_id: string
  file_name: string
  public: boolean
  is_enc: boolean
  thumbnail_img?: string
  tags?: CompactTag[]
  tag_state?: string
}

// 搜索响应
export interface SearchResponse<TFile = SearchFileItem> {
  code: number
  message: string
  data: {
    files: TFile[]
    total: number
    page?: number
    page_size?: number
  }
}

/**
 * 获取文件列表
 */
export const getFileList = (params: FileListRequest) => {
  const filteredParams = filterParams(params)
  return get<ApiResponse<FileListResponse>>(API_ENDPOINTS.FILE.LIST, filteredParams)
}

/**
 * 获取文件缩略图（带鉴权）
 */
export const getThumbnail = async (fileId: string, refresh = false): Promise<string> => {
  try {
    if (refresh) {
      invalidateThumbnailCache(fileId)
    }
    const version = thumbnailCacheVersions.get(fileId)
    const cacheBust = version ? `?v=${version}` : ''
    const url = `${API_BASE_URL}${API_ENDPOINTS.FILE.THUMBNAIL}/${fileId}${cacheBust}`

    const response = await fetch(url, {
      method: 'GET',
      cache: refresh ? 'no-store' : 'default',
      headers: {
        Authorization: `Bearer ${cache.local.get('token') || ''}`
      }
    })

    if (!response.ok) {
      throw new Error('Failed to fetch thumbnail')
    }

    const blob = await response.blob()
    return URL.createObjectURL(blob)
  } catch (error) {
    logger.error('Error fetching thumbnail:', error)
    return '' // 返回空字符串表示加载失败
  }
}

/**
 * 获取文件缩略图URL
 */
export const getThumbnailUrl = (fileId: string) => `${API_ENDPOINTS.FILE.THUMBNAIL}/${fileId}`

/**
 * 修改文件缩略图
 */
export const updateThumbnail = (fileId: string, thumbnail: File) => {
  const formData = new FormData()
  formData.append('thumbnail', thumbnail)
  return putFormData<ApiResponse>(`${API_ENDPOINTS.FILE.THUMBNAIL}/${fileId}`, formData).then(response => {
    if (response.code === 200) {
      invalidateThumbnailCache(fileId)
    }
    return response
  })
}

/**
 * 搜索当前用户的文件
 */
export const searchUserFiles = (params: FileSearchParams) => {
  const filteredParams = filterParams(params)
  return get<SearchResponse>(API_ENDPOINTS.FILE.SEARCH_USER, filteredParams)
}

/**
 * 搜索广场公开文件
 */
export const searchPublicFiles = (params: FileSearchParams) => {
  const filteredParams = filterParams(params)
  return get<SearchResponse>(API_ENDPOINTS.FILE.SEARCH_PUBLIC, filteredParams)
}

/**
 * 下载文件
 */
export const downloadFile = (fileId: string) =>
  get(`${API_ENDPOINTS.FILE.DOWNLOAD}/${fileId}`).then(() => {
    // 处理下载逻辑
  })

/**
 * 移动文件请求参数
 */
export interface MoveFileRequest {
  file_id: string
  target_directory_id: number
}

/**
 * 移动文件
 */
export const moveFile = (data: MoveFileRequest) => post<ApiResponse>(API_ENDPOINTS.FILE.MOVE, data)

export interface MoveItemsRequest {
  file_ids: string[]
  directory_ids: number[]
  target_directory_id: number
}

export const moveItems = (data: MoveItemsRequest) => post<ApiResponse>(API_ENDPOINTS.FILE.MOVE_BATCH, data)

/**
 * 获取虚拟目录树
 */
export interface DirectoryItem {
  id: number
  name: string
  parent_id: number
  absolute_path: string
  created_at: string
  updated_at: string
}

export const getDirectories = () => get<ApiResponse<DirectoryItem[]>>(API_ENDPOINTS.FILE.DIRECTORIES)

/**
 * 删除文件请求参数
 */
export interface DeleteFileRequest {
  file_ids: string[]
}

/**
 * 删除文件（移动到回收站）
 */
export const deleteFiles = (data: DeleteFileRequest) => post<ApiResponse>(API_ENDPOINTS.FILE.DELETE, data)

export interface DeleteItemsRequest {
  file_ids: string[]
  dir_ids: number[]
}

export const deleteItems = (data: DeleteItemsRequest) => post<ApiResponse>(API_ENDPOINTS.FILE.DELETE_BATCH, data)

/**
 * 文件重命名请求参数
 */
export interface RenameFileRequest {
  file_id: string
  new_file_name: string
}

/**
 * 重命名文件
 */
export const renameFile = (data: RenameFileRequest) => post<ApiResponse>(API_ENDPOINTS.FILE.RENAME, data)

// 上传文件请求参数
export interface uploadPrecheckParams {
  chunk_signature: string
  file_name: string
  file_size: number
  files_md5: string[]
  directory_id: number
}

/**
 * 上传文件预检
 */
export const uploadPrecheck = (data: uploadPrecheckParams) => post<ApiResponse>(API_ENDPOINTS.FILE.PRECHECK, data)

// 上传进度响应
export interface UploadProgressResponse {
  precheck_id: string
  file_name: string
  file_size: number
  uploaded: number // 已上传分片数
  total: number // 总分片数
  progress: number // 进度百分比 (0-100)
  md5: string[] // 已上传分片的MD5列表
  is_complete: boolean // 是否已完成
  status: 'pending' | 'uploading' | 'processing' | 'completed' | 'failed' | 'aborted'
  stage?: 'queued' | 'validating' | 'storing' | 'encrypting' | 'committing' | 'completed'
  error_message?: string
  file_id?: string
  is_enc: boolean
}

/**
 * 查询上传进度
 */
export const getUploadProgress = (precheckId: string) => {
  const filteredParams = filterParams({ precheck_id: precheckId })
  return get<ApiResponse<UploadProgressResponse>>(API_ENDPOINTS.FILE.PROGRESS, filteredParams)
}

// 上传请求参数
export interface uploadParams {
  precheck_id: string
  file: File
  thumbnail?: File
  chunk_index: number
  total_chunks: number
  chunk_md5: string
  is_enc: boolean
  file_password: string
  async_finalize?: boolean
}

/**
 * 上传
 */
export const uploadFile = (
  data: uploadParams,
  onProgress?: (percent: number, loaded?: number, total?: number) => void,
  options?: { onCancel?: (cancel: () => void) => void }
) => {
  const formData = new FormData()
  formData.append('precheck_id', data.precheck_id)
  formData.append('chunk_index', data.chunk_index.toString())
  formData.append('total_chunks', data.total_chunks.toString())
  formData.append('chunk_md5', data.chunk_md5)
  formData.append('is_enc', data.is_enc.toString())
  formData.append('async_finalize', (data.async_finalize ?? true).toString())
  if (data.is_enc && data.file_password) {
    formData.append('file_password', data.file_password)
  }
  if (data.thumbnail) {
    formData.append('thumbnail', data.thumbnail)
  }
  return upload(API_ENDPOINTS.FILE.UPLOAD, data.file, formData, onProgress, options)
}

// 公开文件列表请求参数
export interface PublicFileListParams {
  type?: string
  sortBy?: string
  tag_ids?: string
  page: number
  pageSize: number
}

// 公开文件列表项
export interface PublicFileItem {
  uf_id: string
  file_name: string
  file_size: number
  mime_type: string
  owner_name: string
  has_thumbnail: boolean
  created_at: string
  tags?: CompactTag[]
}

// 公开文件列表响应
export interface PublicFileListResponse {
  files: PublicFileItem[]
  total: number
  page: number
  page_size: number
}

/**
 * 获取公开文件列表（文件广场）
 */
export const getPublicFileList = (params: PublicFileListParams) => {
  // 过滤掉无效参数（undefined、null、空字符串）
  const filteredParams = filterParams(params)
  return get<ApiResponse<PublicFileListResponse>>(API_ENDPOINTS.FILE.PUBLIC_LIST, filteredParams)
}

// 上传任务项
export interface UploadTaskItem {
  id: string
  file_name: string
  file_size: number
  chunk_size: number
  total_chunks: number
  uploaded_chunks: number
  progress: number
  status: string
  processing_stage?: string
  is_enc: boolean
  result_file_id?: string
  error_message?: string
  directory_id: number
  create_time: string
  update_time: string
  expire_time: string
}

export type UncompletedUploadTask = UploadTaskItem

export interface UploadTaskListResponse {
  tasks: UploadTaskItem[]
  total: number
  page: number
  page_size: number
}

/**
 * 分页查询全部上传任务
 */
export const listUploadTasks = (page: number, pageSize: number) =>
  get<ApiResponse<UploadTaskListResponse>>(API_ENDPOINTS.FILE.TASK_LIST, { page, pageSize })

/**
 * 查询未完成的上传任务列表
 */
export const listUncompletedUploads = () => get<ApiResponse<UncompletedUploadTask[]>>(API_ENDPOINTS.FILE.UNCOMPLETED)

/**
 * 删除上传任务请求参数
 */
export interface DeleteUploadTaskRequest {
  task_id: string
}

/**
 * 删除上传任务
 */
export const deleteUploadTask = (taskId: string) =>
  post<ApiResponse>(API_ENDPOINTS.FILE.DELETE_UPLOAD_TASK, {
    task_id: taskId
  })

export const retryUploadFinalize = (precheckId: string, filePassword?: string) =>
  post<ApiResponse<{ task_id: string; status: string }>>(API_ENDPOINTS.FILE.FINALIZE_RETRY, {
    precheck_id: precheckId,
    file_password: filePassword || ''
  })

/**
 * 查询过期的上传任务列表
 */
export const listExpiredUploads = () => get<ApiResponse<UncompletedUploadTask[]>>(API_ENDPOINTS.FILE.EXPIRED)

/**
 * 延期过期任务请求参数
 */
export interface RenewExpiredTaskRequest {
  task_id: string
  days?: number // 延期天数，默认7天
}

/**
 * 延期过期任务（恢复任务）
 */
export const renewExpiredTask = (taskId: string, days?: number) =>
  post<ApiResponse<{ task_id: string; expire_time: string }>>(API_ENDPOINTS.FILE.RENEW_TASK, {
    task_id: taskId,
    days: days || 7
  })

/**
 * 清理过期的上传任务
 */
export const cleanExpiredUploads = () => post<ApiResponse<{ cleaned_count: number }>>(API_ENDPOINTS.FILE.CLEAN_EXPIRED)

/**
 * 设置文件公开状态请求参数
 */
export interface SetFilePublicRequest {
  file_id: string
  public: boolean
}

/**
 * 设置文件公开状态
 */
export const setFilePublic = (data: SetFilePublicRequest) => post<ApiResponse>(API_ENDPOINTS.FILE.SET_PUBLIC, data)

/**
 * 在线编辑保存请求参数
 */
export interface SaveFileContentRequest {
  file_id: string
  content: string
  file_password?: string
  base_hash?: string
}

/**
 * 在线编辑保存结果
 */
export interface SaveFileContentResult {
  file_id: string
  size: number
  file_hash: string
  encoding: string
}

/**
 * 保存文本文件内容（在线编辑）。
 * 使用 fetch 实现以保留 HTTP 状态码：409 表示内容冲突（base_hash 不匹配）。
 */
export const saveFileContent = async (data: SaveFileContentRequest): Promise<ApiResponse<SaveFileContentResult>> => {
  const token = cache.local.get('token')
  const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.FILE.EDIT_SAVE}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: token ? `Bearer ${token}` : ''
    },
    body: JSON.stringify(data)
  })

  let body: ApiResponse<SaveFileContentResult> | null = null
  try {
    body = await response.json()
  } catch {
    // 非 JSON 响应
  }

  if (!response.ok) {
    // 409：内容冲突，抛带状态码的错误供调用方识别
    if (response.status === 409) {
      const conflictError: Error & { status?: number } = new Error(body?.message || '保存冲突')
      conflictError.status = 409
      throw conflictError
    }
    if (response.status === 401 || response.status === 403) {
      throw new Error(body?.message || '身份认证失败或权限不足')
    }
    throw new Error(body?.message || '保存文件失败')
  }

  // 服务端始终返回 { code, message, data }；非 JSON 响应时兜底构造
  if (!body) {
    return { code: 200, message: '保存成功', data: {} as SaveFileContentResult }
  }
  return body
}

/**
 * 加载可编辑文本内容（UTF-8 解码 + 编码/base_hash 元数据）。
 * 用于编辑器加载，也用于加密文本文件的带密码预览。
 */
export const loadFileContent = async (
  fileId: string,
  filePassword?: string
): Promise<{ content: string; encoding: string; baseHash: string }> => {
  const token = cache.local.get('token')
  const query = new URLSearchParams({ file_id: fileId })
  if (filePassword) query.set('file_password', filePassword)

  const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.FILE.EDIT_LOAD}?${query.toString()}`, {
    method: 'GET',
    headers: {
      Authorization: token ? `Bearer ${token}` : ''
    }
  })

  if (!response.ok) {
    let message = '加载文件内容失败'
    try {
      const body = await response.json()
      message = body?.message || message
    } catch {
      // 非 JSON 响应时使用默认错误信息
    }
    throw new Error(message)
  }

  return {
    content: await response.text(),
    encoding: response.headers.get('X-File-Encoding') || 'utf-8',
    baseHash: response.headers.get('X-File-Hash') || ''
  }
}

