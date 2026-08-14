<template>
  <FileContextMenu
    :visible="visible"
    :x="x"
    :y="y"
    :title="video?.file_name"
    :items="menuItems"
    @action="handleAction"
    @close="$emit('close')"
  />

  <input
    ref="thumbnailInputRef"
    class="thumbnail-file-input"
    type="file"
    accept=".jpg,.jpeg,image/jpeg"
    hidden
    @change="handleThumbnailSelected"
  />

  <el-dialog v-model="showMoveDialog" :title="t('files.move')" width="500px">
    <el-form label-width="100px">
      <el-form-item :label="t('files.selectedItems')">
        <el-tag>{{ selectedFileName }}</el-tag>
      </el-form-item>
      <el-form-item :label="t('files.targetFolder')">
        <el-tree-select
          v-model="targetFolderId"
          :data="folderTreeData"
          :render-after-expand="false"
          :placeholder="t('files.targetFolderPlaceholder')"
          :default-expanded-keys="[currentDirectoryId]"
          :loading="loadingTree"
          style="width: 100%"
          check-strictly
          node-key="value"
          :props="{ label: 'label', children: 'children', disabled: 'disabled' }"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showMoveDialog = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="moving" @click="handleConfirmMove">{{ t('files.confirmMove') }}</el-button>
    </template>
  </el-dialog>

  <share-dialog
    v-model="showShareDialog"
    :file-info="{
      file_id: shareForm.file_id,
      file_name: shareForm.file_name,
      file_size: getFileSize(shareForm.file_id)
    }"
  />

  <el-dialog v-model="showDownloadPasswordDialog" :title="t('files.downloadPassword')" width="450px">
    <el-form label-width="100px">
      <el-form-item :label="t('files.fileName')">
        <el-text>{{ downloadPasswordForm.file_name }}</el-text>
      </el-form-item>
      <el-form-item :label="t('files.filePassword')">
        <el-input
          v-model="downloadPasswordForm.file_password"
          type="password"
          :placeholder="t('files.filePasswordPlaceholder')"
          show-password
          @keyup.enter="confirmDownloadPassword"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showDownloadPasswordDialog = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="downloadingFile" @click="confirmDownloadPassword">
        {{ t('common.confirm') }}
      </el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="showRenameFileDialog"
    :title="t('files.rename')"
    width="500px"
    @close="handleRenameFileDialogClose"
  >
    <el-form ref="renameFileFormRef" :model="renameFileForm" :rules="renameFileRules" label-width="100px">
      <el-form-item :label="t('files.oldFileName')">
        <el-text>{{ renameFileForm.old_file_name }}</el-text>
      </el-form-item>
      <el-form-item :label="t('files.newFileName')" prop="new_file_name">
        <el-input
          v-model="renameFileForm.new_file_name"
          :placeholder="t('files.fileNamePlaceholder')"
          clearable
          maxlength="255"
          show-word-limit
          @keyup.enter="handleConfirmRenameFile"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="showRenameFileDialog = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="renamingFile" @click="handleConfirmRenameFile">
        {{ t('common.confirm') }}
      </el-button>
    </template>
  </el-dialog>

  <FileTagManager
    v-model="showTagManager"
    :file-ids="tagManagerFileIds"
    :file-name="tagManagerFileName"
    @saved="refreshCinema"
  />
</template>

<script setup lang="ts">
  import {
    Delete,
    Download,
    EditPen,
    FolderOpened,
    Lock,
    PriceTag,
    Share,
    Unlock,
    Upload,
    VideoPlay
  } from '@element-plus/icons-vue'
  import type { CinemaVideo } from '@/api/cinema'
  import { useI18n } from '@/composables'
  import type { FileItem, FileListResponse } from '@/types'
  import FileTagManager from '@/components/FileTagManager/index.vue'
  import FileContextMenu from '@/views/Files/components/FileContextMenu.vue'
  import { useFileOperations } from '@/views/Files/composables/useFileOperations'
  import { useMoveFile } from '@/views/Files/composables/useMoveFile'
  import { useRename } from '@/views/Files/composables/useRename'
  import { useThumbnailUpdate } from '@/views/Files/composables/useThumbnailUpdate'
  import type { ContextMenuItem } from '@/views/Files/types'

  const props = defineProps<{
    visible: boolean
    x: number
    y: number
    video?: CinemaVideo
  }>()

  const emit = defineEmits<{
    close: []
    refresh: []
  }>()

  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const selectedFileIds = ref<string[]>([])
  const selectedFolderIds = ref<number[]>([])
  const selectedFileName = ref('')
  const file = computed<FileItem | undefined>(() => {
    if (!props.video) {
      return undefined
    }
    return {
      file_id: props.video.file_id,
      file_name: props.video.file_name,
      file_size: props.video.file_size,
      mime_type: props.video.mime_type,
      is_enc: props.video.is_enc,
      has_thumbnail: props.video.has_thumbnail,
      public: Boolean(props.video.public),
      created_at: props.video.created_at,
      tags: props.video.tags
    }
  })
  const currentDirectoryId = computed(() => props.video?.directory.id || 0)
  const displayData = computed<FileListResponse>(() => ({
    breadcrumbs: [],
    current_directory_id: currentDirectoryId.value,
    folders: [],
    files: file.value ? [file.value] : [],
    total: file.value ? 1 : 0,
    page: 1,
    page_size: 1
  }))

  const refreshCinema = (): Promise<void> => {
    emit('refresh')
    return Promise.resolve()
  }
  const { thumbnailInputRef, updatingThumbnail, openThumbnailUpload, handleThumbnailSelected } =
    useThumbnailUpdate(refreshCinema)
  const clearSelection = () => {
    selectedFileIds.value = []
  }
  const restoreSelection = (current: FileItem) => {
    selectedFileIds.value = [current.file_id]
    selectedFileName.value = current.file_name
  }

  const {
    showShareDialog,
    shareForm,
    showDownloadPasswordDialog,
    downloadPasswordForm,
    downloadingFile,
    getFileSize,
    handleShareFile,
    handleSelectionDownload,
    handleSelectionDelete,
    handleSetFilePublic,
    confirmDownloadPassword
  } = useFileOperations(displayData, selectedFileIds, selectedFolderIds, clearSelection, refreshCinema)

  const {
    showRenameFileDialog,
    renamingFile,
    renameFileFormRef,
    renameFileForm,
    renameFileRules,
    handleRenameFile,
    handleConfirmRenameFile,
    handleRenameFileDialogClose
  } = useRename(selectedFileIds, selectedFolderIds, refreshCinema)

  const { showMoveDialog, moving, targetFolderId, folderTreeData, loadingTree, handleMoveFile, handleConfirmMove } =
    useMoveFile(currentDirectoryId, selectedFileIds, selectedFolderIds, clearSelection, refreshCinema)

  const showTagManager = ref(false)
  const tagManagerFileIds = ref<string[]>([])
  const tagManagerFileName = ref('')

  const menuItems = computed<ContextMenuItem[]>(() => {
    const current = file.value
    if (!current) {
      return []
    }
    return [
      { key: 'preview', label: t('files.preview'), icon: VideoPlay },
      { key: 'download', label: t('files.download'), icon: Download },
      { key: 'share', label: t('files.share'), icon: Share },
      { key: 'manage-tags', label: t('tags.manage'), icon: PriceTag },
      {
        key: 'update-thumbnail',
        label: t('files.updateThumbnail'),
        icon: Upload,
        disabled: current.is_enc || updatingThumbnail.value
      },
      { key: 'rename', label: t('files.rename'), icon: EditPen },
      { key: 'move', label: t('files.move'), icon: FolderOpened },
      {
        key: current.public ? 'set-private' : 'set-public',
        label: current.public ? t('files.cancelPublic') : t('files.setPublic'),
        icon: current.public ? Lock : Unlock,
        disabled: current.is_enc && !current.public
      },
      { key: 'delete', label: t('files.delete'), icon: Delete, danger: true, divided: true }
    ]
  })

  const handleAction = (key: string) => {
    const current = file.value
    emit('close')
    if (!current) {
      return
    }
    restoreSelection(current)
    switch (key) {
      case 'preview':
        return router.push(`/cinema/${route.params.rootDirectoryId}/watch/${current.file_id}`)
      case 'download':
        return handleSelectionDownload()
      case 'share':
        return handleShareFile(current)
      case 'manage-tags':
        tagManagerFileIds.value = [current.file_id]
        tagManagerFileName.value = current.file_name
        showTagManager.value = true
        return
      case 'update-thumbnail':
        return openThumbnailUpload(current)
      case 'rename':
        return handleRenameFile(current)
      case 'move':
        return handleMoveFile()
      case 'set-public':
        return handleSetFilePublic(current, true)
      case 'set-private':
        return handleSetFilePublic(current, false)
      case 'delete':
        return handleSelectionDelete()
    }
  }

  watch(
    () => props.video,
    video => {
      if (!video) {
        return
      }
      selectedFileIds.value = [video.file_id]
      selectedFileName.value = video.file_name
    },
    { immediate: true }
  )
</script>
