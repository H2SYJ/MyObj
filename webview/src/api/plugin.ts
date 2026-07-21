import { get, post } from '@/utils/network/request'
import { API_BASE_URL, API_ENDPOINTS } from '@/config/api'
import cache from '@/plugins/cache'
import type { ApiResponse } from '@/types'

export interface PluginConfigField {
  key: string
  label: string
  description?: string
  type: 'text' | 'password' | 'number' | 'boolean' | 'select'
  required?: boolean
  secret?: boolean
  affects_source?: boolean
  default?: unknown
  options?: Array<{ label: string; value: unknown }>
}

export interface InstalledPlugin {
  id: string
  name: string
  version: string
  author?: string
  description?: string
  enabled: boolean
  package_sha256: string
  wasm_sha256: string
  permissions: string[]
  config_fields: PluginConfigField[]
  signed: boolean
  trust_status: string
}

export interface PluginAudit {
  id: string
  plugin_id: string
  plugin_version: string
  action: string
  summary: string
  status: string
  error_msg?: string
  created_at: string
}

export const listPlugins = () => get<ApiResponse<InstalledPlugin[]>>(API_ENDPOINTS.ADMIN.PLUGIN.LIST)
export const togglePlugin = (id: string, enabled: boolean) =>
  post<ApiResponse>(API_ENDPOINTS.ADMIN.PLUGIN.TOGGLE, { id, enabled })
export const uninstallPlugin = (id: string) => post<ApiResponse>(API_ENDPOINTS.ADMIN.PLUGIN.UNINSTALL, { id })
export const listPluginAudit = (page = 1, pageSize = 20) =>
  get<ApiResponse<{ items: PluginAudit[]; total: number }>>(API_ENDPOINTS.ADMIN.PLUGIN.AUDIT, { page, pageSize })

const uploadPluginRequest = async (file: File, fields: Record<string, string>): Promise<ApiResponse> => {
  const data = new FormData()
  data.append('plugin', file)
  for (const [key, value] of Object.entries(fields)) data.append(key, value)
  const token = cache.local.get('token')
  const response = await fetch(API_BASE_URL + API_ENDPOINTS.ADMIN.PLUGIN.INSTALL, {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    body: data
  })
  const result = (await response.json()) as ApiResponse
  if (!response.ok) throw new Error(result.message || '安装插件失败')
  return result
}

export const inspectPlugin = (file: File) =>
  uploadPluginRequest(file, { review_only: 'true' }) as Promise<
    ApiResponse<{
      manifest: InstalledPlugin & { api_version: string }
      package_sha256: string
      wasm_sha256: string
      signed: false
      warning: string
    }>
  >

export const installPlugin = (file: File, permissions: string[]) =>
  uploadPluginRequest(file, {
    trust_unsigned: 'true',
    approved_permissions: JSON.stringify(permissions)
  })
