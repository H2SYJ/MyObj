import type { RouteLocationRaw } from 'vue-router'
import type { OfflineDownloadTask } from '@/api/download'

type OfflineTaskResultSource = Pick<OfflineDownloadTask, 'state' | 'file_name'>

export function resolveOfflineTaskResultNavigation(task: OfflineTaskResultSource): RouteLocationRaw | null {
  if (task.state !== 3) return null

  const keyword = task.file_name.trim()
  if (!keyword) return null

  return {
    path: '/files',
    query: { search: keyword }
  }
}
