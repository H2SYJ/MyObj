export type DesktopSearchScope = 'files' | 'square'

export interface DesktopRouteMeta {
  desktopSearch?: DesktopSearchScope
  hidden?: boolean
}

export interface DesktopNavItem {
  path: string
  label: string
  icon: string
  hidden?: boolean
}

export interface PageAction {
  key: string
  label: string
  icon?: string
  type?: 'default' | 'primary' | 'success' | 'warning' | 'danger'
  disabled?: boolean
  loading?: boolean
}

export interface BatchOperationFailedItem {
  item_id: string
  reason: string
}

export interface BatchOperationResult {
  total_count: number
  success_count: number
  failed_count: number
  failed_items: BatchOperationFailedItem[]
}

export type ResourceEntryType = 'file' | 'folder'

export interface ResourceEntry {
  key: string
  id: string | number
  type: ResourceEntryType
  name: string
  size: number
  createdAt: string
  mimeType?: string
  encrypted?: boolean
  public?: boolean
  hasThumbnail?: boolean
}

export interface ListState<T> {
  items: T[]
  loading: boolean
  error: string
  page: number
  pageSize: number
  total: number
}
