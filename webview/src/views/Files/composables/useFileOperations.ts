import { deleteItems, setFilePublic } from '@/api/file'
import { createPackage, downloadPackage, getPackageProgress, type PackageProgressResponse } from '@/api/package'
import { useFileDownload } from '@/composables/business/useFileDownload'
import { useI18n } from '@/composables/core/useI18n'
import cache from '@/plugins/cache'
import { useUserStore } from '@/stores'
import type { FileItem, FileListResponse } from '@/types'
import { waitForTaskTerminal } from '@/utils/waitForTask'

export function useFileOperations(
  displayData: Ref<FileListResponse>,
  selectedFileIds: Ref<string[]>,
  selectedFolderIds: Ref<number[]>,
  clearSelection: () => void,
  loadFileList: () => Promise<void>
) {
  const { t } = useI18n()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const router = useRouter()
  const userStore = useUserStore()

  const previewVisible = ref(false)
  const previewFile = ref<FileItem | null>(null)
  const showShareDialog = ref(false)
  const shareForm = reactive({ file_id: '', file_name: '', file_size: 0 })

  const {
    showDownloadPasswordDialog,
    downloadPasswordForm,
    downloadingFile,
    handleDownload: handleFileDownload,
    confirmDownloadPassword
  } = useFileDownload({
    onTaskReady: () => router.push({ path: '/tasks', query: { tab: 'download' } })
  })

  const getDisplayedFile = (fileId: string) => displayData.value.files.find(file => file.file_id === fileId)
  const getFileSize = (fileId: string) => getDisplayedFile(fileId)?.file_size || 0

  const handleShareSuccess = () => clearSelection()

  const handleFilePreview = (file: FileItem) => {
    previewFile.value = file
    previewVisible.value = true
  }

  const canPreview = (file: FileItem) => {
    const mime = file.mime_type.toLowerCase()
    return (
      mime.startsWith('image/') ||
      mime.startsWith('audio/') ||
      mime.startsWith('video/') ||
      mime.startsWith('text/') ||
      mime === 'application/pdf' ||
      /\.(pdf|txt|md|json|csv|log)$/i.test(file.file_name)
    )
  }

  const handleOpenFile = async (file: FileItem) => {
    if (canPreview(file)) handleFilePreview(file)
    else await handleFileDownload(file)
  }

  const handleShareFile = (file: FileItem) => {
    shareForm.file_id = file.file_id
    shareForm.file_name = file.file_name
    shareForm.file_size = file.file_size
    showShareDialog.value = true
  }

  const downloadPackageFile = async (packageId: string, packageName: string) => {
    const token = cache.local.get('token')
    const response = await fetch(downloadPackage(packageId), {
      headers: { Authorization: token ? `Bearer ${token}` : '' },
      credentials: 'include'
    })
    const contentType = response.headers.get('content-type') || ''
    if (!response.ok || contentType.includes('application/json')) {
      const error = await response.json().catch(() => ({}))
      throw new Error(error.message || t('files.downloadFailed'))
    }
    const blobUrl = URL.createObjectURL(await response.blob())
    const anchor = document.createElement('a')
    anchor.href = blobUrl
    anchor.download = packageName
    anchor.style.display = 'none'
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    URL.revokeObjectURL(blobUrl)
  }

  const waitForPackageReady = async (packageId: string): Promise<PackageProgressResponse> => {
    return waitForTaskTerminal<PackageProgressResponse, PackageProgressResponse>({
      eventKind: 'package.task',
      resourceId: packageId,
      reconcile: async () => {
        const response = await getPackageProgress(packageId)
        return response.code === 200 ? response.data || null : null
      },
      evaluate: task => {
        if (task.status === 'ready') return { status: 'success', value: task as PackageProgressResponse }
        if (task.status === 'failed') {
          return { status: 'error', error: new Error(task.error_msg || t('files.packageFailed')) }
        }
        return { status: 'pending' }
      },
      timeoutMs: 5 * 60_000,
      timeoutError: () => new Error(t('files.packageTimeout')),
      onReconcileError: error => proxy?.$log.warn('查询打包进度失败:', error)
    })
  }

  const requestPackagePassword = async (): Promise<string | undefined> => {
    const hasEncryptedFile = selectedFileIds.value.some(id => getDisplayedFile(id)?.is_enc)
    if (!hasEncryptedFile && selectedFolderIds.value.length === 0) return ''
    try {
      const result = await proxy?.$modal.prompt(
        selectedFolderIds.value.length > 0 ? t('files.packageFolderPasswordTip') : t('files.packagePasswordTip')
      )
      return (result as { value?: string } | undefined)?.value || ''
    } catch {
      return undefined
    }
  }

  const handlePackageDownload = async () => {
    if (selectedFileIds.value.length === 0 && selectedFolderIds.value.length === 0) {
      proxy?.$modal.msgWarning(t('files.selectItemsFirst'))
      return
    }
    const password = await requestPackagePassword()
    if (password === undefined) return
    const singleFolder =
      selectedFolderIds.value.length === 1 && selectedFileIds.value.length === 0
        ? displayData.value.folders.find(folder => folder.id === selectedFolderIds.value[0])
        : undefined
    const packageName = singleFolder ? `${singleFolder.name}.zip` : `files_${Date.now()}.zip`

    proxy?.$modal.loading(t('files.creatingPackage'))
    try {
      const res = await createPackage({
        file_ids: [...selectedFileIds.value],
        dir_ids: [...selectedFolderIds.value],
        file_password: password,
        package_name: packageName
      })
      if (res.code !== 200 || !res.data) throw new Error(res.message || t('files.createPackageFailed'))
      if (res.data.status === 'ready')
        await downloadPackageFile(res.data.package_id, res.data.package_name || packageName)
      else {
        await waitForPackageReady(res.data.package_id)
        await downloadPackageFile(res.data.package_id, res.data.package_name || packageName)
      }
      proxy?.$modal.msgSuccess(t('files.downloadStart'))
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || t('files.packageDownloadFailed'))
      proxy?.$log.error(error)
    } finally {
      proxy?.$modal.closeLoading()
    }
  }

  const handleSelectionDownload = async () => {
    if (selectedFileIds.value.length === 1 && selectedFolderIds.value.length === 0) {
      const file = getDisplayedFile(selectedFileIds.value[0])
      if (file) await handleFileDownload(file)
      return
    }
    await handlePackageDownload()
  }

  const deleteSelection = async (fileIds: string[], dirIds: number[], confirmText: string) => {
    try {
      await proxy?.$modal.confirm(confirmText)
      const result = await deleteItems({ file_ids: fileIds, dir_ids: dirIds })
      if (result.code !== 200) throw new Error(result.message || t('files.deleteFailed'))
      proxy?.$modal.msgSuccess(result.message || t('files.deleteSuccess'))
      clearSelection()
      await loadFileList()
      await userStore.fetchUserInfo()
    } catch (error: any) {
      if (error !== 'cancel' && error?.message !== 'cancel') {
        proxy?.$modal.msgError(error.message || t('files.deleteFailed'))
      }
    }
  }

  const handleDeleteFile = (file: FileItem) =>
    deleteSelection([file.file_id], [], t('files.confirmDeleteFile', { fileName: file.file_name }))

  const handleSelectionDelete = () => {
    const count = selectedFileIds.value.length + selectedFolderIds.value.length
    if (!count) {
      proxy?.$modal.msgWarning(t('files.selectItemsFirst'))
      return
    }
    return deleteSelection(
      [...selectedFileIds.value],
      [...selectedFolderIds.value],
      t('files.confirmDeleteItems', { count })
    )
  }

  const handleSetFilePublic = async (file: FileItem, isPublic: boolean) => {
    if (isPublic && file.is_enc) {
      proxy?.$modal.msgError(t('files.encryptedFileNotPublic'))
      return
    }
    try {
      const result = await setFilePublic({ file_id: file.file_id, public: isPublic })
      if (result.code !== 200) throw new Error(result.message || t('files.operationFailed'))
      proxy?.$modal.msgSuccess(isPublic ? t('files.filePublic') : t('files.filePrivate'))
      await loadFileList()
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || t('files.operationFailed'))
    }
  }

  return {
    previewVisible,
    previewFile,
    showShareDialog,
    shareForm,
    showDownloadPasswordDialog,
    downloadPasswordForm,
    downloadingFile,
    getFileSize,
    handleShareSuccess,
    handleFilePreview,
    handleOpenFile,
    handleShareFile,
    handleDownloadFile: handleFileDownload,
    confirmDownloadPassword,
    handleDeleteFile,
    handleSelectionDownload,
    handleSelectionDelete,
    handleSetFilePublic
  }
}
