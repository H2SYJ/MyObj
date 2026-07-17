import { uploadTaskManager, type UploadTask } from './uploadTaskManager'
import { listUncompletedUploads, listUploadTasks, type UploadTaskItem } from '@/api/file'
import logger from '@/plugins/logger'

const TASK_PAGE_SIZE = 100

export type BackendUploadTask = UploadTaskItem

function clampProgress(progress: number): number {
  if (!Number.isFinite(progress)) {
    return 0
  }
  return Math.max(0, Math.min(100, Math.floor(progress)))
}

function calculateUploadedSize(uploadedChunks: number, totalChunks: number, fileSize: number): number {
  if (totalChunks <= 0) {
    return 0
  }
  const uploadedSize = Math.floor((uploadedChunks / totalChunks) * fileSize)
  return Math.max(0, Math.min(fileSize, uploadedSize))
}

function mapBackendStatusToFrontend(backendStatus: string): UploadTask['status'] {
  if (backendStatus === 'completed') {
    return 'completed'
  }
  if (backendStatus === 'failed' || backendStatus === 'aborted') {
    return 'failed'
  }
  if (backendStatus === 'uploading' || backendStatus === 'pending') {
    return 'uploading'
  }
  return 'paused'
}

function isTerminalStatus(status: UploadTask['status']): boolean {
  return status === 'completed' || status === 'failed' || status === 'cancelled'
}

export function syncBackendTasksToFrontend(backendTasks: BackendUploadTask[]): {
  created: number
  updated: number
  skipped: number
  removed: number
} {
  return uploadTaskManager.batchUpdate(() => {
    let frontendTasks = uploadTaskManager.getAllTasks()
    const backendTaskIds = new Set(backendTasks.map(task => task.id))
    let created = 0
    let updated = 0
    let skipped = 0

    for (const backendTask of backendTasks) {
      if (uploadTaskManager.isPrecheckIdDeleted(backendTask.id)) {
        skipped++
        continue
      }

      let existingTask = frontendTasks.find(task => task.precheckId === backendTask.id)
      if (!existingTask) {
        existingTask = frontendTasks.find(
          task =>
            task.file_name === backendTask.file_name && task.file_size === backendTask.file_size && !task.precheckId
        )
        if (existingTask) {
          uploadTaskManager.updateTask(existingTask.id, { precheckId: backendTask.id })
        }
      }

      const backendStatus = mapBackendStatusToFrontend(backendTask.status)
      const backendProgress = backendStatus === 'completed' ? 100 : clampProgress(backendTask.progress)
      const uploadedSize =
        backendStatus === 'completed'
          ? backendTask.file_size
          : calculateUploadedSize(backendTask.uploaded_chunks, backendTask.total_chunks, backendTask.file_size)

      if (!existingTask) {
        const taskId = uploadTaskManager.createTask(backendTask.file_name, backendTask.file_size, backendStatus)
        uploadTaskManager.updateTask(taskId, {
          precheckId: backendTask.id,
          pathId: backendTask.path_id,
          created_at: backendTask.create_time,
          progress: backendProgress,
          uploaded_size: uploadedSize,
          status: backendStatus,
          speed: '0 KB/s',
          error: backendTask.error_message,
          isExternal: true
        })
        created++
        frontendTasks = uploadTaskManager.getAllTasks()
        continue
      }

      const updates: Partial<UploadTask> = {
        pathId: backendTask.path_id,
        error: backendTask.error_message
      }

      if (backendStatus === 'completed') {
        updates.status = 'completed'
        updates.progress = 100
        updates.uploaded_size = existingTask.file_size
        updates.speed = '0 KB/s'
      } else if (backendStatus === 'failed') {
        if (existingTask.status !== 'completed') {
          updates.status = 'failed'
          updates.progress = Math.max(existingTask.progress || 0, backendProgress)
          updates.uploaded_size = uploadedSize
          updates.speed = '0 KB/s'
        }
      } else if (!isTerminalStatus(existingTask.status)) {
        if (existingTask.isExternal) {
          updates.status = backendStatus
          updates.speed = '0 KB/s'
        }
        if (existingTask.status !== 'prechecking' && backendProgress >= (existingTask.progress || 0)) {
          updates.progress = backendProgress
          updates.uploaded_size = uploadedSize
        }
      }

      uploadTaskManager.updateTask(existingTask.id, updates)
      updated++
    }

    const removed = uploadTaskManager.removeMissingExternalTasks(backendTaskIds)
    return { created, updated, skipped, removed }
  })
}

async function loadAllBackendUploadTasks(): Promise<BackendUploadTask[]> {
  const tasksById = new Map<string, BackendUploadTask>()
  let page = 1
  let latestTotal = 0

  while (true) {
    const response = await listUploadTasks(page, TASK_PAGE_SIZE)
    if (response.code !== 200) {
      throw new Error(`后端返回错误: code=${response.code}, message=${response.message || '未知错误'}`)
    }

    const data = response.data
    if (!data || !Array.isArray(data.tasks) || !Number.isFinite(data.total) || data.total < 0) {
      throw new Error('后端返回的上传任务分页数据格式错误')
    }

    latestTotal = Math.floor(data.total)
    data.tasks.forEach(task => tasksById.set(task.id, task))

    if (page * TASK_PAGE_SIZE >= latestTotal) {
      break
    }
    if (data.tasks.length === 0) {
      throw new Error('后端上传任务分页提前结束')
    }
    page++
  }

  if (tasksById.size < latestTotal) {
    throw new Error('同步期间上传任务列表发生变化，请稍后重试')
  }
  return Array.from(tasksById.values())
}

export async function loadAndSyncBackendTasks(): Promise<{
  success: boolean
  created: number
  updated: number
  skipped: number
  removed: number
  error?: string
}> {
  try {
    const backendTasks = await loadAllBackendUploadTasks()
    const result = syncBackendTasksToFrontend(backendTasks)
    return { success: true, ...result }
  } catch (error: any) {
    logger.warn('从后端加载上传任务失败:', error)
    return {
      success: false,
      created: 0,
      updated: 0,
      skipped: 0,
      removed: 0,
      error: error.message || '加载失败'
    }
  }
}

/**
 * 查询未完成任务仅用于断点恢复，不参与任务历史同步。
 */
export async function findBackendTask(precheckId: string): Promise<BackendUploadTask | null> {
  try {
    const response = await listUncompletedUploads()
    if (response.code !== 200 || !Array.isArray(response.data)) {
      return null
    }
    return response.data.find(task => task.id === precheckId) || null
  } catch (error: any) {
    logger.warn('查找后端任务失败:', error)
    return null
  }
}
