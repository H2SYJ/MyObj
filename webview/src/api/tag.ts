import { del, get, post, put } from '@/utils/network/request'
import { API_BASE_URL, API_ENDPOINTS } from '@/config/api'
import type { ApiResponse, CompactTag } from '@/types'
import cache from '@/plugins/cache'

export interface TagCategory {
  id: string
  code: string
  name: string
  color: string
  sort_order?: number
  enabled?: boolean
  builtin?: boolean
}

export interface FileTag {
  id: string
  name: string
  category: TagCategory
  sources: string[]
  visibility: 'inherit' | 'private' | 'public'
  automatic: boolean
  suppressed?: boolean
}

export interface FileTagsData {
  file_id: string
  tags: FileTag[]
  suppressed: FileTag[]
  state: string
  last_error?: string
  updated_at?: string
}

export interface DirectoryTagsData {
  directory_id: number
  tags: FileTag[]
}

export interface ManualTagInput {
  name: string
  category_id?: string
  visibility?: 'private' | 'public'
}

export interface TagRuleInput {
  id?: string
  type: 'word' | 'stop_word' | 'alias' | 'regex'
  target_field?: string
  pattern: string
  replacement?: string
  category_id?: string
  priority?: number
  weight?: number
  enabled: boolean
}

export interface TagRuleSet {
  id: string
  scope_type: 'global' | 'user'
  scope_id: string
  version: number
  revision: number
  status: 'draft' | 'active' | 'archived'
  based_on_version: number
  rules: TagRuleInput[]
  created_at?: string
  published_at?: string
}

export interface TagPreviewItem {
  input: string
  tags: CompactTag[]
}

export interface TagRebuildJob {
  id: string
  scope_type: string
  scope_id: string
  target_version: number
  status: string
  total: number
  processed: number
  succeeded: number
  failed: number
  last_error?: string
  created_at: string
  updated_at?: string
  started_at?: string
  finished_at?: string
  cursor?: string
}

export interface TagRebuildFailure {
  job_id: string
  uf_id: string
  user_id: string
  status: 'failed' | 'retrying' | 'resolved'
  error?: string
  retry_count: number
  created_at: string
  updated_at: string
}

export interface TagProviderSettings {
  basic: { available: boolean }
  image: { available: boolean }
  ffprobe: { available: boolean }
  states: Record<string, number>
}

export interface AdminTagSettings {
  enabled: boolean
  limit: number
  active_version: number
  degraded: boolean
  degraded_reason?: string
  providers: TagProviderSettings
}

export interface TagCloudItem {
  id: string
  name: string
  base_name: string
  category: TagCategory
  base_category: TagCategory
  file_count: number
  hidden: boolean
  system: boolean
  system_code?: string
}

export interface TagCloudData {
  tags: TagCloudItem[]
  hidden: TagCloudItem[]
}

export interface TagCloudEditorData {
  tag: TagCloudItem
  aliases: string[]
}

export const getTagCloud = () => get<ApiResponse<TagCloudData>>(API_ENDPOINTS.FILE.TAG_CLOUD)
export const getTagCloudItem = (tagId: string) =>
  get<ApiResponse<TagCloudEditorData>>(`${API_ENDPOINTS.FILE.TAG_CLOUD}/${tagId}`)
export const updateTagCloudItem = (tagId: string, displayName: string, displayCategoryId: string, aliases: string[]) =>
  put<ApiResponse<{ editor: TagCloudEditorData; rebuild_job?: TagRebuildJob }>>(
    `${API_ENDPOINTS.FILE.TAG_CLOUD}/${tagId}`,
    { display_name: displayName, display_category_id: displayCategoryId, aliases }
  )
export const hideTagCloudItem = (tagId: string) => del<ApiResponse<null>>(`${API_ENDPOINTS.FILE.TAG_CLOUD}/${tagId}`)
export const restoreTagCloudItem = (tagId: string) =>
  post<ApiResponse<{ rebuild_job?: TagRebuildJob }>>(`${API_ENDPOINTS.FILE.TAG_CLOUD}/${tagId}/restore`)

export const getFileTags = (fileId: string) => get<ApiResponse<FileTagsData>>(`${API_ENDPOINTS.FILE.TAGS}/${fileId}`)

export const updateManualTags = (fileId: string, add: ManualTagInput[], removeTagIds: string[]) =>
  put<ApiResponse<FileTagsData>>(`${API_ENDPOINTS.FILE.TAGS}/${fileId}/manual`, {
    add,
    remove_tag_ids: removeTagIds
  })

export const updateTagExclusions = (fileId: string, suppressTagIds: string[], restoreTagIds: string[]) =>
  put<ApiResponse<FileTagsData>>(`${API_ENDPOINTS.FILE.TAGS}/${fileId}/exclusions`, {
    suppress_tag_ids: suppressTagIds,
    restore_tag_ids: restoreTagIds
  })

export const retryFileTags = (fileId: string) => post<ApiResponse<null>>(`${API_ENDPOINTS.FILE.TAGS}/${fileId}/retry`)

export const batchUpdateTags = (fileIds: string[], add: ManualTagInput[], removeTagIds: string[] = []) =>
  post<ApiResponse<null>>(`${API_ENDPOINTS.FILE.TAGS}/batch`, {
    file_ids: fileIds,
    add,
    remove_tag_ids: removeTagIds
  })

export type TagSuggestionScope = 'user' | 'public'

export interface TagSuggestionParams {
  keyword?: string
  tagIds?: string[]
  scope?: TagSuggestionScope
  limit?: number
  target?: 'file' | 'directory'
}

export const getTagSuggestions = (params: TagSuggestionParams = {}) =>
  get<ApiResponse<CompactTag[]>>(API_ENDPOINTS.FILE.TAG_SUGGESTIONS, {
    keyword: params.keyword || undefined,
    tag_ids: params.tagIds?.join(',') || undefined,
    scope: params.scope || 'user',
    target: params.target || undefined,
    limit: params.limit || 30
  })

export const getEnabledTagCategories = () => get<ApiResponse<TagCategory[]>>(API_ENDPOINTS.FILE.TAG_CATEGORIES)

export const getDirectoryTags = (directoryId: number) =>
  get<ApiResponse<DirectoryTagsData>>(`/file/directories/${directoryId}/tags`)

export const updateDirectoryTags = (directoryId: number, add: ManualTagInput[], removeTagIds: string[]) =>
  put<ApiResponse<DirectoryTagsData>>(`/file/directories/${directoryId}/tags/manual`, {
    add,
    remove_tag_ids: removeTagIds
  })

export const getPersonalTagDictionary = () => get<ApiResponse<TagRuleSet>>(API_ENDPOINTS.FILE.TAG_DICTIONARY)

export const updatePersonalTagDictionary = (rules: TagRuleInput[]) =>
  put<ApiResponse<{ rule_set: TagRuleSet; rebuild_job: TagRebuildJob }>>(API_ENDPOINTS.FILE.TAG_DICTIONARY, { rules })

export const previewPersonalTagDictionary = (samples: string[], rules: TagRuleInput[]) =>
  post<ApiResponse<TagPreviewItem[]>>(`${API_ENDPOINTS.FILE.TAG_DICTIONARY}/preview`, { samples, rules })

const adminTag = API_ENDPOINTS.ADMIN.TAG

export const getAdminTagSettings = () => get<ApiResponse<AdminTagSettings>>(adminTag.SETTINGS)
export const updateAdminTagSettings = (enabled: boolean, limit: number) =>
  put<ApiResponse<AdminTagSettings>>(adminTag.SETTINGS, { enabled, limit })
export const getTagCategories = () => get<ApiResponse<TagCategory[]>>(adminTag.CATEGORIES)
export const saveTagCategory = (category: TagCategory) => post<ApiResponse<TagCategory>>(adminTag.CATEGORIES, category)
export const deleteTagCategory = (id: string) => del<ApiResponse<null>>(`${adminTag.CATEGORIES}/${id}`)
export const getGlobalRuleSets = () => get<ApiResponse<TagRuleSet[]>>(adminTag.RULE_SETS)
export const getGlobalRuleSet = (id: string) => get<ApiResponse<TagRuleSet>>(`${adminTag.RULE_SETS}/${id}`)
export const createGlobalDraft = () => post<ApiResponse<TagRuleSet>>(adminTag.DRAFTS)
export const saveGlobalDraft = (id: string, revision: number, rules: TagRuleInput[]) =>
  put<ApiResponse<TagRuleSet>>(`${adminTag.DRAFTS}/${id}`, { revision, rules })
export const previewGlobalDraft = (id: string, samples: string[], rules: TagRuleInput[]) =>
  post<ApiResponse<TagPreviewItem[]>>(`${adminTag.DRAFTS}/${id}/preview`, { samples, rules })
export const publishGlobalDraft = (id: string) =>
  post<ApiResponse<{ active_version: number; rebuild_job_id: string; rebuild_job: TagRebuildJob }>>(
    `${adminTag.DRAFTS}/${id}/publish`
  )
export const rollbackGlobalRuleSet = (id: string) =>
  post<ApiResponse<{ rule_set: TagRuleSet; rebuild_job: TagRebuildJob }>>(`${adminTag.RULE_SETS}/${id}/rollback`)
export const getRuleSetDiff = (id: string) =>
  get<ApiResponse<{ base?: TagRuleSet; target: TagRuleSet }>>(`${adminTag.RULE_SETS}/${id}/diff`)
export const getTagRebuildJobs = () => get<ApiResponse<TagRebuildJob[]>>(adminTag.REBUILD_JOBS)
export const getTagRebuildJob = (id: string) => get<ApiResponse<TagRebuildJob>>(`${adminTag.REBUILD_JOBS}/${id}`)
export const getTagRebuildFailures = (id: string, status = '', limit = 50) =>
  get<ApiResponse<TagRebuildFailure[]>>(`${adminTag.REBUILD_JOBS}/${id}/failures`, { status, limit })
export const retryTagRebuildFailure = (jobId: string, fileId: string) =>
  post<ApiResponse<null>>(`${adminTag.REBUILD_JOBS}/${jobId}/failures/${fileId}/retry`)
export const cancelTagRebuildJob = (id: string) => post<ApiResponse<null>>(`${adminTag.REBUILD_JOBS}/${id}/cancel`)
export const retryTagRebuildJob = (id: string) => post<ApiResponse<null>>(`${adminTag.REBUILD_JOBS}/${id}/retry`)

const authorizedHeaders = () => ({ Authorization: `Bearer ${cache.local.get('token') || ''}` })

export const importGlobalDraft = async (id: string, revision: number, format: 'json' | 'csv', file: File) => {
  const bytes = new Uint8Array(await file.arrayBuffer())
  if (bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) {
    throw new Error('导入文件必须是 UTF-8 无 BOM')
  }
  new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  const response = await fetch(`${API_BASE_URL}${adminTag.DRAFTS}/${id}/import?revision=${revision}&format=${format}`, {
    method: 'POST',
    headers: { ...authorizedHeaders(), 'Content-Type': file.type || 'application/octet-stream' },
    body: bytes
  })
  const result = (await response.json()) as ApiResponse<TagRuleSet>
  if (!response.ok || result.code !== 200) {
    throw new Error(result.message || '导入失败')
  }
  return result
}

export const exportGlobalRuleSet = async (id: string, format: 'json' | 'csv') => {
  const response = await fetch(`${API_BASE_URL}${adminTag.RULE_SETS}/${id}/export?format=${format}`, {
    headers: authorizedHeaders()
  })
  if (!response.ok) {
    throw new Error('导出失败')
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `tag-rules-${id}.${format}`
  anchor.click()
  URL.revokeObjectURL(url)
}
