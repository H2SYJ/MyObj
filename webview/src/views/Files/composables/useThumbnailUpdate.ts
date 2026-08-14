import { updateThumbnail } from '@/api/file'
import { useI18n } from '@/composables'
import type { FileItem } from '@/types'
import { validateThumbnailFile } from '@/utils/file/thumbnail'

type ThumbnailUpdatedHandler = (file: FileItem) => void | Promise<void>

/** 复用文件列表与影视模式的手动缩略图上传流程。 */
export function useThumbnailUpdate(onUpdated: ThumbnailUpdatedHandler) {
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const { t } = useI18n()
  const thumbnailInputRef = ref<HTMLInputElement>()
  const thumbnailTarget = ref<FileItem | null>(null)
  const updatingThumbnail = ref(false)

  const openThumbnailUpload = (file: FileItem) => {
    if (file.is_enc) {
      proxy?.$modal.msgWarning(t('files.encryptedThumbnailUnsupported'))
      return
    }
    thumbnailTarget.value = file
    if (thumbnailInputRef.value) {
      thumbnailInputRef.value.value = ''
      thumbnailInputRef.value.click()
    }
  }

  const handleThumbnailSelected = async (event: Event) => {
    const input = event.target as HTMLInputElement
    const thumbnail = input.files?.[0]
    const target = thumbnailTarget.value
    if (!thumbnail || !target) {
      return
    }

    updatingThumbnail.value = true
    try {
      await validateThumbnailFile(thumbnail)
      const response = await updateThumbnail(target.file_id, thumbnail)
      if (response.code !== 200) {
        throw new Error(response.message || t('files.updateThumbnailFailed'))
      }
      target.has_thumbnail = true
      await onUpdated(target)
      proxy?.$modal.msgSuccess(t('files.updateThumbnailSuccess'))
    } catch (error) {
      const message = error instanceof Error ? error.message : t('files.updateThumbnailFailed')
      proxy?.$modal.msgError(message)
    } finally {
      updatingThumbnail.value = false
      thumbnailTarget.value = null
      input.value = ''
    }
  }

  return {
    thumbnailInputRef,
    updatingThumbnail,
    openThumbnailUpload,
    handleThumbnailSelected
  }
}
