import { get, post } from '@/utils/network/request'
import { API_ENDPOINTS } from '@/config/api'
import type { ApiResponse } from '@/types'
import type { InstalledPlugin } from './plugin'

export interface Subscription {
  id: string
  name: string
  plugin_id: string
  plugin_version: string
  schedule_time: string
  default_path: string
  initial_limit: number
  max_items_per_run: number
  enabled: boolean
  status: string
  last_error?: string
  next_run_at?: string
  last_run_at?: string
  config?: Record<string, unknown>
  granted_permissions: string[]
  secret_fields_configured: string[]
}

export interface SubscriptionItem {
  id: string
  title: string
  url: string
  download_type: string
  save_path: string
  status: string
  download_task_id?: string
  download_state?: number
  download_error?: string
  has_request_headers: boolean
  request_header_names: string[]
  requires_headers: boolean
  thumbnail_url?: string
  thumbnail_status: string
  thumbnail_error?: string
  published_at?: string
}

export interface SubscriptionRun {
  id: string
  trigger: string
  status: string
  items_found: number
  tasks_created: number
  items_skipped: number
  error_msg?: string
  created_at: string
}

export interface SubscriptionPayload {
  id?: string
  name: string
  plugin_id?: string
  config: Record<string, unknown>
  granted_permissions: string[]
  schedule_time: string
  default_path: string
  initial_limit: number
  max_items_per_run: number
  enabled?: boolean
  run_now?: boolean
}

export const availablePlugins = () => get<ApiResponse<InstalledPlugin[]>>(API_ENDPOINTS.SUBSCRIPTION.PLUGINS)
export const listSubscriptions = (page = 1, pageSize = 100) =>
  get<ApiResponse<{ subscriptions: Subscription[]; total: number }>>(API_ENDPOINTS.SUBSCRIPTION.LIST, {
    page,
    pageSize
  })
export const createSubscription = (data: SubscriptionPayload) =>
  post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.CREATE, data)
export const updateSubscription = (data: SubscriptionPayload & { id: string }) =>
  post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.UPDATE, data)
export const deleteSubscription = (id: string) => post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.DELETE, { id })
export const toggleSubscription = (id: string, enabled: boolean) =>
  post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.TOGGLE, { id, enabled })
export const runSubscription = (id: string) => post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.RUN, { id })
export const updateSubscriptionPermissions = (id: string, granted_permissions: string[]) =>
  post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.PERMISSIONS, { id, granted_permissions })
export const listSubscriptionRuns = (subscriptionId: string, page = 1, pageSize = 50) =>
  get<ApiResponse<{ items: SubscriptionRun[]; total: number }>>(API_ENDPOINTS.SUBSCRIPTION.RUNS, {
    subscription_id: subscriptionId,
    page,
    pageSize
  })
export const listSubscriptionItems = (subscriptionId: string, page = 1, pageSize = 50) =>
  get<ApiResponse<{ items: SubscriptionItem[]; total: number }>>(API_ENDPOINTS.SUBSCRIPTION.ITEMS, {
    subscription_id: subscriptionId,
    page,
    pageSize
  })
export const retrySubscriptionThumbnail = (itemId: string) =>
  post<ApiResponse>(API_ENDPOINTS.SUBSCRIPTION.THUMBNAIL_RETRY, { item_id: itemId })
