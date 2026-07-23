/**
 * 文件下载 Composable
 * 统一处理文件下载逻辑，支持加密文件
 */
import {
  createLocalFileDownload,
  getLocalFileDownloadTask,
  getLocalFileDownloadUrl,
  type OfflineDownloadTask
} from '@/api/download'
import cache from '@/plugins/cache'
import type { FileItem } from '@/types'
import { useI18n } from '@/composables'
import { taskEventClient, type TaskEvent } from '@/utils/taskEvents'

export interface DownloadPasswordForm {
  file_id: string
  file_name: string
  file_password: string
}

export function useFileDownload(options?: {
  onTaskReady?: () => void // 任务准备完成时的回调（可选，用于跳转等）
}) {
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const { t } = useI18n()

  const showDownloadPasswordDialog = ref(false)
  const downloadPasswordForm = reactive<DownloadPasswordForm>({
    file_id: '',
    file_name: '',
    file_password: ''
  })
  const downloadingFile = ref(false)

  const waitForDownloadReady = async (taskId: string): Promise<OfflineDownloadTask> => {
    let timeoutTimer: number | null = null
    let unsubscribeTask: () => void = () => {}
    let unsubscribeSync: () => void = () => {}
    let reconcilePromise: Promise<OfflineDownloadTask | null> | null = null

    const result = new Promise<OfflineDownloadTask>((resolve, reject) => {
      let settled = false

      const settleFromTask = (task: Partial<OfflineDownloadTask> | null | undefined) => {
        if (settled || !task) return
        if (task.state === 3) {
          settled = true
          resolve(task as OfflineDownloadTask)
        } else if (task.state === 4) {
          settled = true
          reject(new Error(task.error_msg || t('tasks.downloadPrepareFailed') || '下载准备失败'))
        } else if (task.state === 5) {
          settled = true
          reject(new Error(t('tasks.cancelled') || '下载任务已取消'))
        }
      }

      const reconcile = () => {
        if (settled) return Promise.resolve(null)
        if (reconcilePromise) return reconcilePromise
        reconcilePromise = getLocalFileDownloadTask(taskId)
          .then(response => {
            const task = response.code === 200 ? response.data || null : null
            settleFromTask(task)
            return task
          })
          .catch(error => {
            proxy?.$log.warn('查询下载准备任务失败:', error)
            return null
          })
          .finally(() => {
            reconcilePromise = null
          })
        return reconcilePromise
      }

      unsubscribeTask = taskEventClient.subscribe('download.task', taskId, (event: TaskEvent) => {
        settleFromTask(event.payload as Partial<OfflineDownloadTask> | undefined)
      })
      unsubscribeSync = taskEventClient.subscribe('sync', undefined, () => {
        void reconcile()
      })

      void reconcile()
      timeoutTimer = window.setTimeout(async () => {
        const finalTask = await reconcile()
        if (settled) return
        settleFromTask(finalTask)
        if (!settled) {
          settled = true
          reject(new Error(t('tasks.prepareTimeout') || '准备超时，请到任务中心查看'))
        }
      }, 30_000)
    })

    try {
      return await result
    } finally {
      if (timeoutTimer !== null) window.clearTimeout(timeoutTimer)
      unsubscribeTask()
      unsubscribeSync()
    }
  }

  const downloadPreparedFile = async (taskId: string, fileName: string) => {
    const downloadUrl = getLocalFileDownloadUrl(taskId)
    const token = cache.local.get('token')
    const response = await fetch(downloadUrl, {
      method: 'GET',
      headers: { Authorization: token ? `Bearer ${token}` : '' },
      credentials: 'include'
    })
    const contentType = response.headers.get('content-type') || ''
    if (contentType.includes('application/json') || !response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.message || t('tasks.downloadFailed') || '下载失败')
    }

    const blobUrl = window.URL.createObjectURL(await response.blob())
    const link = document.createElement('a')
    link.href = blobUrl
    link.download = fileName || 'download'
    link.style.display = 'none'
    document.body.appendChild(link)
    link.click()
    link.remove()
    window.URL.revokeObjectURL(blobUrl)
  }

  /**
   * 处理文件下载
   * @param file 文件信息
   */
  const handleDownload = async (file: FileItem) => {
    // 如果是加密文件，需要输入密码
    if (file.is_enc) {
      downloadPasswordForm.file_id = file.file_id
      downloadPasswordForm.file_name = file.file_name
      downloadPasswordForm.file_password = ''
      showDownloadPasswordDialog.value = true
    } else {
      await executeDownload(file.file_id, '')
    }
  }

  /**
   * 执行下载（任务式下载）
   * @param fileId 文件ID
   * @param password 文件密码（加密文件必需）
   */
  const executeDownload = async (fileId: string, password: string) => {
    try {
      downloadingFile.value = true
      const res = await createLocalFileDownload({
        file_id: fileId,
        file_password: password
      })

      if (res.code !== 200 || !res.data?.task_id) {
        throw new Error(res.message || t('tasks.createDownloadTaskFailed') || '创建下载任务失败')
      }

      proxy?.$modal.msgSuccess(t('tasks.preparingDownload') || '准备下载中，请稍候...')
      showDownloadPasswordDialog.value = false
      const task = await waitForDownloadReady(res.data.task_id)
      if (options?.onTaskReady) options.onTaskReady()
      await downloadPreparedFile(res.data.task_id, task.file_name || res.data.file_name)
      proxy?.$modal.msgSuccess(t('tasks.downloadStarted') || '下载已开始')
    } catch (error: any) {
      proxy?.$log.error('下载文件失败:', error)
      proxy?.$modal.msgError(error.message || t('tasks.downloadFailed') || '下载失败')
    } finally {
      downloadingFile.value = false
    }
  }

  /**
   * 确认下载密码
   */
  const confirmDownloadPassword = async () => {
    if (!downloadPasswordForm.file_password) {
      proxy?.$modal.msgWarning(t('preview.downloadPassword.placeholder') || '请输入文件密码')
      return
    }
    await executeDownload(downloadPasswordForm.file_id, downloadPasswordForm.file_password)
  }

  return {
    showDownloadPasswordDialog,
    downloadPasswordForm,
    downloadingFile,
    handleDownload,
    confirmDownloadPassword
  }
}
