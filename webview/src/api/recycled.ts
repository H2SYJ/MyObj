import { get, post } from '@/utils/network/request'
import { filterParams } from '@/utils/common/params'
import { API_ENDPOINTS } from '@/config/api'
import type { ApiResponse, BatchOperationResult } from '@/types'

// 回收站文件项
export interface RecycledItem {
  recycled_id: string
  item_type: 'file' | 'folder'
  item_name: string
  item_count: number
  file_id: string
  file_name: string
  file_size: number
  mime_type: string
  is_enc: boolean
  has_thumbnail: boolean
  deleted_at: string
}

// 回收站列表请求
export interface RecycledListRequest {
  page: number
  pageSize: number
}

// 回收站列表响应
export interface RecycledListResponse {
  items: RecycledItem[]
  total: number
  page: number
  pageSize: number
}

/**
 * 获取回收站列表
 */
export const getRecycledList = (params: RecycledListRequest) => {
  const filteredParams = filterParams(params)
  return get<ApiResponse<RecycledListResponse>>(API_ENDPOINTS.RECYCLED.LIST, filteredParams)
}

/**
 * 还原文件
 */
export const restoreFile = (recycledId: string) => {
  return post<ApiResponse>(API_ENDPOINTS.RECYCLED.RESTORE, {
    recycled_id: recycledId
  })
}

export const batchRestoreFiles = (recycledIds: string[]) => {
  return post<ApiResponse<BatchOperationResult>>(API_ENDPOINTS.RECYCLED.RESTORE_BATCH, {
    recycled_ids: recycledIds
  })
}

/**
 * 永久删除文件
 */
export const deleteFilePermanently = (recycledId: string) => {
  return post<ApiResponse>(API_ENDPOINTS.RECYCLED.DELETE, {
    recycled_id: recycledId
  })
}

export const batchDeleteFilesPermanently = (recycledIds: string[]) => {
  return post<ApiResponse<BatchOperationResult>>(API_ENDPOINTS.RECYCLED.DELETE_BATCH, {
    recycled_ids: recycledIds
  })
}

/**
 * 清空回收站
 */
export const emptyRecycled = () => {
  return post<ApiResponse>(API_ENDPOINTS.RECYCLED.EMPTY, {})
}
