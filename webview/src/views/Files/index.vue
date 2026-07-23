<template>
  <div class="files-page">
    <Breadcrumb
      :breadcrumbs="breadcrumbs"
      :format-breadcrumb-name="formatBreadcrumbName"
      :current-path="currentPath"
      @navigate="navigateToPath"
    />

    <PageToolbar v-if="!isMobile" :selected-count="toolbarSelectedCount">
      <template #primary>
        <template v-if="toolbarSelectedCount > 0">
          <el-button icon="Download" @click="handleSelectionDownload">{{ t('files.download') }}</el-button>
          <el-button icon="FolderOpened" @click="handleMoveFile">{{ t('files.move') }}</el-button>
          <el-button type="danger" plain icon="Delete" @click="handleSelectionDelete">{{
            t('files.delete')
          }}</el-button>
          <el-button text @click="clearCurrentSelection">{{ t('files.cancelSelect') }}</el-button>
        </template>
        <template v-else>
          <el-button type="primary" icon="Upload" @click="handleUpload">{{ t('files.upload') }}</el-button>
          <el-button icon="FolderAdd" @click="showNewFolderDialog = true">{{ t('files.newFolder') }}</el-button>
        </template>
      </template>

      <el-dropdown trigger="click">
        <el-button icon="Sort">{{
          t(`files.sort${sortBy === 'name' ? 'Name' : sortBy === 'size' ? 'Size' : 'Time'}`)
        }}</el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item @click="changeSorting('name')">{{ t('files.sortName') }}</el-dropdown-item>
            <el-dropdown-item @click="changeSorting('time')">{{ t('files.sortTime') }}</el-dropdown-item>
            <el-dropdown-item @click="changeSorting('size')">{{ t('files.sortSize') }}</el-dropdown-item>
            <el-dropdown-item divided @click="changeSorting(sortBy, sortOrder === 'asc' ? 'desc' : 'asc')">
              {{ sortOrder === 'asc' ? t('files.sortDesc') : t('files.sortAsc') }}
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-button-group>
        <el-button
          :type="viewMode === 'grid' ? 'primary' : 'default'"
          icon="Grid"
          :aria-label="t('files.gridView')"
          @click="setViewMode('grid')"
        />
        <el-button
          :type="viewMode === 'list' ? 'primary' : 'default'"
          icon="List"
          :aria-label="t('files.listView')"
          @click="setViewMode('list')"
        />
      </el-button-group>
    </PageToolbar>

    <div
      ref="contentRef"
      class="file-content-area"
      :class="{ 'is-dragging': isDraggingFiles }"
      tabindex="0"
      @contextmenu="handleBlankContextMenu"
      @pointerdown="startBoxSelection"
      @pointermove="updateBoxSelection"
      @pointerup="finishBoxSelection"
      @pointercancel="finishBoxSelection"
      @dragenter.prevent="handleDragOver"
      @dragover.prevent="handleDragOver"
      @dragleave="handleDragLeave"
      @drop.prevent="handleDrop"
    >
      <Skeleton v-if="(fileListLoading || isSearching) && entries.length === 0" :count="12" :view-mode="viewMode" />

      <FileGrid
        v-else-if="viewMode === 'grid'"
        :entries="entries"
        :is-selected="isSelectedEntry"
        :get-thumbnail-url="getThumbnailUrl"
        @entry-click="handleEntryClick"
        @entry-toggle="toggleEntry"
        @entry-open="handleEntryOpen"
        @entry-context="openEntryContextMenu"
        @entry-long-press="handleEntryLongPress"
      />
      <FileList
        v-else
        :entries="entries"
        :is-selected="isSelectedEntry"
        :get-thumbnail-url="getThumbnailUrl"
        @entry-click="handleEntryClick"
        @entry-toggle="toggleEntry"
        @entry-open="handleEntryOpen"
        @entry-context="openEntryContextMenu"
        @entry-long-press="handleEntryLongPress"
      />

      <EmptyState
        v-if="!fileListLoading && !isSearching && entries.length === 0"
        :type="hasSearchKeyword ? 'search' : 'folder'"
        :show-actions="false"
      />

      <div v-if="boxSelection.active" class="selection-box" :style="selectionBoxStyle"></div>
      <div v-if="isDraggingFiles" class="drop-overlay">
        <el-icon :size="44"><UploadFilled /></el-icon>
        <strong>{{ t('files.dropUpload') }}</strong>
        <span>{{ t('files.folderUploadUnsupported') }}</span>
      </div>
    </div>

    <div v-if="!isMobile && displayPagination.total > 0" class="pagination-wrapper">
      <pagination
        :page="displayPagination.page"
        :limit="displayPagination.pageSize"
        :total="displayPagination.total"
        :page-sizes="[20, 50, 100]"
        float="center"
        @pagination="handlePagination"
        class="pagination"
      />
    </div>

    <MobileInfiniteList
      v-if="isMobile && displayPagination.total > 0"
      :loading="fileListLoading || isSearching"
      :has-more="mobileHasMore"
      @load-more="loadNextMobilePage"
      @retry="loadNextMobilePage"
    />

    <button
      v-if="isMobile && !mobileSelectionMode && !hasOpenDialog && !contextMenu.visible"
      type="button"
      class="page-fab"
      :aria-label="t('files.pageActions')"
      @click="openPageMenuFromButton"
    >
      <el-icon><Plus /></el-icon>
    </button>

    <div v-if="isMobile && mobileSelectionMode && !hasOpenDialog" class="mobile-selection-bar">
      <span>{{ t('files.selected', { count: selectedCount }) }}</span>
      <button type="button" :disabled="selectedCount === 0" @click="handleSelectionDownload">
        <el-icon><Download /></el-icon><span>{{ t('files.download') }}</span>
      </button>
      <button type="button" :disabled="selectedCount === 0" @click="handleMoveFile">
        <el-icon><FolderOpened /></el-icon><span>{{ t('files.move') }}</span>
      </button>
      <button type="button" :disabled="selectedCount === 0" class="danger" @click="handleSelectionDelete">
        <el-icon><Delete /></el-icon><span>{{ t('files.delete') }}</span>
      </button>
      <button type="button" @click="clearCurrentSelection">
        <el-icon><Close /></el-icon><span>{{ t('files.cancelSelect') }}</span>
      </button>
    </div>

    <FileContextMenu
      :visible="contextMenu.visible"
      :x="contextMenu.x"
      :y="contextMenu.y"
      :title="contextMenuTitle"
      :items="contextMenuItems"
      @action="handleMenuAction"
      @close="closeContextMenu"
    />

    <el-dialog v-model="showNewFolderDialog" :title="t('files.newFolder')" width="500px" @close="handleDialogClose">
      <el-form ref="folderFormRef" :model="folderForm" :rules="folderRules" label-width="100px">
        <el-form-item :label="t('files.folderName')" prop="dir_path">
          <el-input
            v-model="folderForm.dir_path"
            :placeholder="t('files.folderNamePlaceholder')"
            clearable
            maxlength="50"
            show-word-limit
            @keyup.enter="handleCreateFolder"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showNewFolderDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="creating" @click="handleCreateFolder">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>

    <UploadEncryptDialog v-model="showUploadEncryptDialog" @confirm="handleUploadEncryptConfirm" />

    <el-dialog v-model="showMoveDialog" :title="t('files.move')" width="500px">
      <el-form label-width="100px">
        <el-form-item :label="t('files.selectedItems')">
          <div class="selected-tags">
            <el-tag v-for="entry in selectedEntries" :key="entry.key" class="file-tag">
              {{ entryName(entry) }}
            </el-tag>
          </div>
        </el-form-item>
        <el-form-item :label="t('files.targetFolder')">
          <el-tree-select
            v-model="targetFolderId"
            :data="folderTreeData"
            :render-after-expand="false"
            :placeholder="t('files.targetFolderPlaceholder')"
            :default-expanded-keys="[currentPath]"
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
      @success="handleShareSuccess"
    />

    <el-dialog v-model="showDownloadPasswordDialog" :title="t('files.downloadPassword')" width="450px">
      <el-form label-width="100px">
        <el-form-item :label="t('files.fileName')"
          ><el-text>{{ downloadPasswordForm.file_name }}</el-text></el-form-item
        >
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
        <el-button type="primary" :loading="downloadingFile" @click="confirmDownloadPassword">{{
          t('common.confirm')
        }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="showRenameFileDialog"
      :title="t('files.rename')"
      width="500px"
      @close="handleRenameFileDialogClose"
    >
      <el-form ref="renameFileFormRef" :model="renameFileForm" :rules="renameFileRules" label-width="100px">
        <el-form-item :label="t('files.oldFileName')"
          ><el-text>{{ renameFileForm.old_file_name }}</el-text></el-form-item
        >
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
        <el-button type="primary" :loading="renamingFile" @click="handleConfirmRenameFile">{{
          t('common.confirm')
        }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="showRenameDirDialog"
      :title="t('files.renameDir')"
      width="500px"
      @close="handleRenameDirDialogClose"
    >
      <el-form ref="renameDirFormRef" :model="renameDirForm" :rules="renameDirRules" label-width="100px">
        <el-form-item :label="t('files.oldDirName')"
          ><el-text>{{ renameDirForm.old_dir_name }}</el-text></el-form-item
        >
        <el-form-item :label="t('files.newDirName')" prop="new_dir_name">
          <el-input
            v-model="renameDirForm.new_dir_name"
            :placeholder="t('files.newDirNamePlaceholder')"
            clearable
            maxlength="50"
            show-word-limit
            @keyup.enter="handleConfirmRenameDir"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showRenameDirDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="renamingDir" @click="handleConfirmRenameDir">{{
          t('common.confirm')
        }}</el-button>
      </template>
    </el-dialog>

    <preview v-model="previewVisible" :file="previewFile" />
  </div>
</template>

<script setup lang="ts">
  import {
    Close,
    Delete,
    Download,
    EditPen,
    FolderAdd,
    FolderOpened,
    Grid,
    List,
    Lock,
    Plus,
    Refresh,
    Select,
    Share,
    Sort,
    SortDown,
    SortUp,
    Unlock,
    Upload,
    UploadFilled,
    View
  } from '@element-plus/icons-vue'
  import { useI18n, useResponsive } from '@/composables'
  import { MobileInfiniteList } from '@/components/mobile'
  import { handleFileUpload, uploadMultipleFiles } from '@/utils/file/upload'
  import { useUserStore } from '@/stores'
  import type { FileItem, FileListResponse } from '@/types'
  import Breadcrumb from './components/Breadcrumb.vue'
  import FileContextMenu from './components/FileContextMenu.vue'
  import FileGrid from './components/FileGrid.vue'
  import FileList from './components/FileList.vue'
  import EmptyState from '@/components/EmptyState/index.vue'
  import { fileEntry, folderEntry, getFileSelectionCapabilities, type ContextMenuItem, type FileEntry } from './types'
  import { useFileList, type FileSortBy, type FileSortOrder } from './composables/useFileList'
  import { useFileSelection } from './composables/useFileSelection'
  import { useDelayedSelectionDisplay } from './composables/useDelayedSelectionDisplay'
  import { useFileOperations } from './composables/useFileOperations'
  import { useFolderOperations } from './composables/useFolderOperations'
  import { useRename } from './composables/useRename'
  import { useMoveFile } from './composables/useMoveFile'
  import { useFileSearch } from './composables/useFileSearch'
  import { useFileViewMode } from './composables/useFileViewMode'
  import { PageToolbar } from '@/components/desktop'

  const { t } = useI18n()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const route = useRoute()
  const router = useRouter()
  const userStore = useUserStore()
  const { isHandheld, hasCoarsePointer } = useResponsive()
  const isMobile = isHandheld
  const { viewMode, setViewMode } = useFileViewMode(isMobile)

  const {
    fileListData,
    currentPage,
    pageSize,
    currentPath,
    breadcrumbs,
    formatBreadcrumbName,
    loadFileList,
    navigateToPath,
    getThumbnailUrl,
    loading: fileListLoading,
    sortBy,
    sortOrder,
    setSorting
  } = useFileList()

  const { searchKeyword, isSearching, searchResults, performSearch, clearSearch, hasSearchKeyword } = useFileSearch(
    sortBy,
    sortOrder
  )
  const displayData = computed<FileListResponse>(() =>
    hasSearchKeyword.value ? searchResults.value : fileListData.value
  )
  const entries = computed<FileEntry[]>(() => [
    ...displayData.value.folders.map(folderEntry),
    ...displayData.value.files.map(fileEntry)
  ])
  const displayPagination = computed(() => ({
    page: hasSearchKeyword.value ? searchResults.value.page : currentPage.value,
    pageSize: hasSearchKeyword.value ? searchResults.value.page_size : pageSize.value,
    total: displayData.value.total
  }))
  const mobileHasMore = computed(() => displayData.value.files.length < displayPagination.value.total)
  const loadNextMobilePage = async () => {
    if (!isMobile.value || fileListLoading.value || isSearching.value || !mobileHasMore.value) return
    const nextPage = displayPagination.value.page + 1
    if (hasSearchKeyword.value) {
      await performSearch(searchKeyword.value, nextPage, displayPagination.value.pageSize, true)
    } else {
      currentPage.value = nextPage
      await loadFileList(true)
    }
  }
  const reloadDisplayData = async () => {
    if (hasSearchKeyword.value) {
      await performSearch(searchKeyword.value, displayPagination.value.page, displayPagination.value.pageSize)
    } else {
      await loadFileList()
    }
  }

  const {
    selectedFolderIds,
    selectedFileIds,
    selectedKeys,
    selectedCount,
    selectedEntries,
    isSelectedEntry,
    applyKeys,
    setSingle,
    toggleEntry,
    handleEntryClick: selectEntryFromClick,
    prepareContextSelection,
    selectAll,
    clearSelection
  } = useFileSelection(entries)
  const {
    displayedCount: toolbarSelectedCount,
    scheduleDisplay: scheduleToolbarSelectionDisplay,
    hideDisplay: hideToolbarSelectionDisplay
  } = useDelayedSelectionDisplay(selectedCount)

  const clearCurrentSelection = () => {
    clearSelection()
    mobileSelectionMode.value = false
  }

  const {
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
    confirmDownloadPassword,
    handleSelectionDownload,
    handleSelectionDelete,
    handleSetFilePublic
  } = useFileOperations(displayData, selectedFileIds, selectedFolderIds, clearCurrentSelection, reloadDisplayData)

  const {
    showNewFolderDialog,
    creating,
    folderFormRef,
    folderForm,
    folderRules,
    handleNewFolder,
    handleDialogClose,
    handleCreateFolder
  } = useFolderOperations(currentPath, loadFileList)

  const {
    showRenameFileDialog,
    renamingFile,
    renameFileFormRef,
    renameFileForm,
    renameFileRules,
    showRenameDirDialog,
    renamingDir,
    renameDirFormRef,
    renameDirForm,
    renameDirRules,
    handleRenameFile,
    handleConfirmRenameFile,
    handleRenameFileDialogClose,
    handleRenameDir,
    handleConfirmRenameDir,
    handleRenameDirDialogClose
  } = useRename(selectedFileIds, selectedFolderIds, reloadDisplayData)

  const { showMoveDialog, moving, targetFolderId, folderTreeData, loadingTree, handleMoveFile, handleConfirmMove } =
    useMoveFile(currentPath, selectedFileIds, selectedFolderIds, clearCurrentSelection, reloadDisplayData)

  const contentRef = ref<HTMLElement>()
  const mobileSelectionMode = ref(false)
  const showUploadEncryptDialog = ref(false)
  const pendingDroppedFiles = ref<File[]>([])
  const isDraggingFiles = ref(false)
  const contextMenu = reactive<{
    visible: boolean
    x: number
    y: number
    kind: 'page' | 'entry'
    entry: FileEntry | null
  }>({ visible: false, x: 0, y: 0, kind: 'page', entry: null })

  const boxSelection = reactive({
    active: false,
    pointerId: -1,
    startX: 0,
    startY: 0,
    currentX: 0,
    currentY: 0,
    additive: false,
    baseKeys: [] as string[]
  })
  const selectionBoxStyle = computed(() => ({
    left: `${Math.min(boxSelection.startX, boxSelection.currentX)}px`,
    top: `${Math.min(boxSelection.startY, boxSelection.currentY)}px`,
    width: `${Math.abs(boxSelection.currentX - boxSelection.startX)}px`,
    height: `${Math.abs(boxSelection.currentY - boxSelection.startY)}px`
  }))

  const entryName = (entry: FileEntry) => (entry.type === 'file' ? entry.file.file_name : entry.folder.name)
  const selectionCapabilities = computed(() => getFileSelectionCapabilities(selectedEntries.value))

  const closeContextMenu = () => {
    contextMenu.visible = false
    contextMenu.entry = null
  }

  const openMenu = (kind: 'page' | 'entry', x: number, y: number, entry: FileEntry | null = null) => {
    Object.assign(contextMenu, { visible: true, x, y, kind, entry })
  }

  const eventPosition = (event: MouseEvent | KeyboardEvent) => {
    if (event instanceof MouseEvent && (event.clientX || event.clientY)) return { x: event.clientX, y: event.clientY }
    const rect = (event.currentTarget as HTMLElement | null)?.getBoundingClientRect()
    return rect ? { x: rect.left + 24, y: rect.top + 24 } : { x: window.innerWidth / 2, y: window.innerHeight / 2 }
  }

  const openEntryContextMenu = (entry: FileEntry, event: MouseEvent | KeyboardEvent) => {
    prepareContextSelection(entry)
    const position = eventPosition(event)
    openMenu('entry', position.x, position.y, entry)
  }

  const handleBlankContextMenu = (event: MouseEvent) => {
    if ((event.target as HTMLElement).closest('[data-entry-key]')) return
    event.preventDefault()
    clearCurrentSelection()
    openMenu('page', event.clientX, event.clientY)
  }

  const openPageMenuFromButton = (event: MouseEvent) => {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    openMenu('page', rect.right, rect.top)
  }

  const contextMenuTitle = computed(() => {
    if (contextMenu.kind === 'page') return t('files.pageActions')
    if (selectedCount.value > 1) return t('files.selected', { count: selectedCount.value })
    return contextMenu.entry ? entryName(contextMenu.entry) : ''
  })

  const pageMenuItems = computed<ContextMenuItem[]>(() => [
    { key: 'upload', label: t('files.upload'), icon: Upload },
    { key: 'new-folder', label: t('files.newFolder'), icon: FolderAdd },
    { key: 'refresh', label: t('common.refresh'), icon: Refresh },
    {
      key: 'select-all',
      label: t('files.selectAll'),
      icon: Select,
      divided: true,
      disabled: entries.value.length === 0
    },
    { key: 'clear-selection', label: t('files.cancelSelect'), icon: Close, disabled: selectedCount.value === 0 },
    { key: 'sort-name', label: t('files.sortName'), icon: Sort, divided: true, active: sortBy.value === 'name' },
    { key: 'sort-time', label: t('files.sortTime'), icon: Sort, active: sortBy.value === 'time' },
    { key: 'sort-size', label: t('files.sortSize'), icon: Sort, active: sortBy.value === 'size' },
    { key: 'sort-asc', label: t('files.sortAsc'), icon: SortUp, active: sortOrder.value === 'asc' },
    { key: 'sort-desc', label: t('files.sortDesc'), icon: SortDown, active: sortOrder.value === 'desc' },
    { key: 'view-grid', label: t('files.gridView'), icon: Grid, divided: true, active: viewMode.value === 'grid' },
    { key: 'view-list', label: t('files.listView'), icon: List, active: viewMode.value === 'list' }
  ])

  const itemMenuItems = computed<ContextMenuItem[]>(() => {
    if (selectedCount.value > 1) {
      return [
        { key: 'download-selection', label: t('files.download'), icon: Download },
        {
          key: 'move-selection',
          label: t('files.move'),
          icon: FolderOpened,
          disabled: !selectionCapabilities.value.canMove
        },
        {
          key: 'delete-selection',
          label: t('files.delete'),
          icon: Delete,
          danger: true,
          divided: true,
          disabled: !selectionCapabilities.value.canDelete
        }
      ]
    }
    const entry = contextMenu.entry
    if (!entry) return []
    if (entry.type === 'folder') {
      return [
        { key: 'open', label: t('files.open'), icon: FolderOpened },
        { key: 'download-selection', label: t('files.download'), icon: Download },
        { key: 'rename', label: t('files.rename'), icon: EditPen },
        { key: 'move-selection', label: t('files.move'), icon: FolderOpened },
        { key: 'delete-selection', label: t('files.delete'), icon: Delete, danger: true, divided: true }
      ]
    }
    return [
      { key: 'preview', label: t('files.preview'), icon: View },
      { key: 'download-selection', label: t('files.download'), icon: Download },
      { key: 'share', label: t('files.share'), icon: Share },
      { key: 'rename', label: t('files.rename'), icon: EditPen },
      { key: 'move-selection', label: t('files.move'), icon: FolderOpened },
      {
        key: entry.file.public ? 'set-private' : 'set-public',
        label: entry.file.public ? t('files.cancelPublic') : t('files.setPublic'),
        icon: entry.file.public ? Lock : Unlock,
        disabled: entry.file.is_enc && !entry.file.public
      },
      { key: 'delete-selection', label: t('files.delete'), icon: Delete, danger: true, divided: true }
    ]
  })
  const contextMenuItems = computed(() => (contextMenu.kind === 'page' ? pageMenuItems.value : itemMenuItems.value))

  const changeSorting = async (nextSortBy: FileSortBy, nextSortOrder: FileSortOrder = sortOrder.value) => {
    setSorting(nextSortBy, nextSortOrder)
    clearCurrentSelection()
    currentPage.value = 1
    if (hasSearchKeyword.value) await performSearch(searchKeyword.value, 1, displayPagination.value.pageSize)
    else await loadFileList()
  }

  const handleMenuAction = async (key: string) => {
    const entry = contextMenu.entry
    closeContextMenu()
    switch (key) {
      case 'upload':
        return handleUpload()
      case 'new-folder':
        return handleNewFolder()
      case 'refresh':
        return loadFileList()
      case 'select-all':
        return selectAll()
      case 'clear-selection':
        return clearCurrentSelection()
      case 'sort-name':
        return changeSorting('name')
      case 'sort-time':
        return changeSorting('time')
      case 'sort-size':
        return changeSorting('size')
      case 'sort-asc':
        return changeSorting(sortBy.value, 'asc')
      case 'sort-desc':
        return changeSorting(sortBy.value, 'desc')
      case 'view-grid':
        return setViewMode('grid')
      case 'view-list':
        return setViewMode('list')
      case 'open':
        if (entry) return openEntry(entry)
        break
      case 'preview':
        if (entry?.type === 'file') return handleFilePreview(entry.file)
        break
      case 'download-selection':
        return handleSelectionDownload()
      case 'share':
        if (entry?.type === 'file') return handleShareFile(entry.file)
        break
      case 'rename':
        if (entry?.type === 'file') handleRenameFile(entry.file)
        else if (entry?.type === 'folder') handleRenameDir(entry.folder)
        return
      case 'move-selection':
        return handleMoveFile()
      case 'set-public':
        if (entry?.type === 'file') return handleSetFilePublic(entry.file, true)
        break
      case 'set-private':
        if (entry?.type === 'file') return handleSetFilePublic(entry.file, false)
        break
      case 'delete-selection':
        return handleSelectionDelete()
    }
  }

  const openEntry = async (entry: FileEntry) => {
    if (entry.type === 'folder') navigateToPath(entry.folder.id)
    else await handleOpenFile(entry.file)
  }

  const handleEntryOpen = (entry: FileEntry, trigger: 'double-click' | 'keyboard') => {
    if (trigger === 'double-click') hideToolbarSelectionDisplay()
    return openEntry(entry)
  }

  const handleEntryClick = (entry: FileEntry, event: MouseEvent) => {
    if (isMobile.value) {
      if (mobileSelectionMode.value) toggleEntry(entry)
      else openEntry(entry)
      return
    }
    selectEntryFromClick(entry, event)
    scheduleToolbarSelectionDisplay()
  }

  const handleEntryLongPress = (entry: FileEntry) => {
    if (!isMobile.value || !hasCoarsePointer.value) return
    mobileSelectionMode.value = true
    if (!isSelectedEntry(entry)) setSingle(entry)
    navigator.vibrate?.(25)
  }

  const startBoxSelection = (event: PointerEvent) => {
    if (isMobile.value || event.pointerType !== 'mouse' || event.button !== 0) return
    const target = event.target as HTMLElement
    if (target.closest('[data-entry-key], button, input, textarea, .el-pagination')) return
    closeContextMenu()
    boxSelection.active = true
    boxSelection.pointerId = event.pointerId
    boxSelection.startX = boxSelection.currentX = event.clientX
    boxSelection.startY = boxSelection.currentY = event.clientY
    boxSelection.additive = event.ctrlKey || event.metaKey
    boxSelection.baseKeys = boxSelection.additive ? [...selectedKeys.value] : []
    if (!boxSelection.additive) clearSelection()
    contentRef.value?.setPointerCapture(event.pointerId)
    event.preventDefault()
  }

  const updateBoxSelection = (event: PointerEvent) => {
    if (!boxSelection.active || event.pointerId !== boxSelection.pointerId) return
    boxSelection.currentX = event.clientX
    boxSelection.currentY = event.clientY
    const left = Math.min(boxSelection.startX, event.clientX)
    const right = Math.max(boxSelection.startX, event.clientX)
    const top = Math.min(boxSelection.startY, event.clientY)
    const bottom = Math.max(boxSelection.startY, event.clientY)
    const keys = Array.from(contentRef.value?.querySelectorAll<HTMLElement>('[data-entry-key]') || [])
      .filter(element => {
        const rect = element.getBoundingClientRect()
        return rect.right >= left && rect.left <= right && rect.bottom >= top && rect.top <= bottom
      })
      .map(element => element.dataset.entryKey || '')
      .filter(Boolean)
    applyKeys([...boxSelection.baseKeys, ...keys])
    const bounds = contentRef.value?.getBoundingClientRect()
    if (bounds) {
      if (event.clientY < bounds.top + 36) contentRef.value?.scrollBy({ top: -18 })
      else if (event.clientY > bounds.bottom - 36) contentRef.value?.scrollBy({ top: 18 })
    }
  }

  const finishBoxSelection = (event: PointerEvent) => {
    if (!boxSelection.active || event.pointerId !== boxSelection.pointerId) return
    contentRef.value?.releasePointerCapture(event.pointerId)
    boxSelection.active = false
    boxSelection.pointerId = -1
  }

  const handleUpload = () => {
    pendingDroppedFiles.value = []
    showUploadEncryptDialog.value = true
  }

  const uploadCallbacks = () => ({
    onProgress: (progress: number, fileName: string) => proxy?.$log.debug(`文件 ${fileName} 上传进度: ${progress}%`),
    onSuccess: async (fileName: string) => {
      proxy?.$modal.msgSuccess(t('files.uploadSuccess', { fileName }))
      await reloadDisplayData()
      await userStore.fetchUserInfo()
    },
    onError: (error: Error, fileName: string) => {
      proxy?.$log.error(`文件 ${fileName} 上传失败:`, error)
      proxy?.$modal.msgError(t('files.uploadFailed', { fileName, error: error.message }))
    }
  })

  const handleUploadEncryptConfirm = async (encryptConfig: { is_enc: boolean; file_password: string }) => {
    const callbacks = uploadCallbacks()
    if (pendingDroppedFiles.value.length > 0) {
      const files = [...pendingDroppedFiles.value]
      pendingDroppedFiles.value = []
      await uploadMultipleFiles(
        files,
        currentPath.value,
        { chunkSize: 5 * 1024 * 1024 },
        callbacks.onProgress,
        callbacks.onSuccess,
        callbacks.onError,
        encryptConfig.is_enc,
        encryptConfig.file_password
      )
      return
    }
    await handleFileUpload(
      currentPath.value,
      { chunkSize: 5 * 1024 * 1024 },
      callbacks.onProgress,
      callbacks.onSuccess,
      callbacks.onError,
      true,
      () => router.push({ path: '/tasks', query: { tab: 'upload' } }),
      encryptConfig
    )
  }

  const handleDragOver = (event: DragEvent) => {
    if (event.dataTransfer?.types.includes('Files')) isDraggingFiles.value = true
  }
  const handleDragLeave = (event: DragEvent) => {
    if (!contentRef.value?.contains(event.relatedTarget as Node | null)) isDraggingFiles.value = false
  }
  const handleDrop = (event: DragEvent) => {
    isDraggingFiles.value = false
    const items = Array.from(event.dataTransfer?.items || [])
    const hasDirectory = items.some(item => {
      const enhanced = item as DataTransferItem & { webkitGetAsEntry?: () => { isDirectory: boolean } | null }
      return enhanced.webkitGetAsEntry?.()?.isDirectory
    })
    if (hasDirectory) proxy?.$modal.msgWarning(t('files.folderUploadUnsupported'))
    const files = Array.from(event.dataTransfer?.files || []).filter(file => !hasDirectory || file.size > 0)
    if (files.length === 0 || hasDirectory) return
    pendingDroppedFiles.value = files
    showUploadEncryptDialog.value = true
  }

  const handlePagination = ({ page, limit }: { page: number; limit: number }) => {
    clearCurrentSelection()
    if (hasSearchKeyword.value) performSearch(searchKeyword.value, page, limit)
    else {
      currentPage.value = page
      pageSize.value = limit
      loadFileList()
    }
  }

  const isEditableTarget = (target: EventTarget | null) => {
    const element = target as HTMLElement | null
    return Boolean(element?.closest('input, textarea, [contenteditable="true"], .el-dialog, [role="menu"]'))
  }
  const handleGlobalKeydown = (event: KeyboardEvent) => {
    if (event.defaultPrevented || isEditableTarget(event.target)) return
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') {
      event.preventDefault()
      selectAll()
      return
    }
    if (event.key === 'Escape') {
      if (contextMenu.visible) closeContextMenu()
      else clearCurrentSelection()
      return
    }
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
      event.preventDefault()
      const entryElement = (event.target as HTMLElement | null)?.closest<HTMLElement>('[data-entry-key]')
      const entry = entries.value.find(item => item.key === entryElement?.dataset.entryKey)
      if (entry && entryElement) {
        prepareContextSelection(entry)
        const rect = entryElement.getBoundingClientRect()
        openMenu('entry', rect.left + 24, rect.top + 24, entry)
        return
      }
      openMenu('page', window.innerWidth / 2, window.innerHeight / 2)
    }
  }

  const hasOpenDialog = computed(() =>
    [
      showNewFolderDialog.value,
      showUploadEncryptDialog.value,
      showMoveDialog.value,
      showShareDialog.value,
      showDownloadPasswordDialog.value,
      showRenameFileDialog.value,
      showRenameDirDialog.value,
      previewVisible.value
    ].some(Boolean)
  )

  watch(
    () => route.query.directoryId,
    () => {
      clearCurrentSelection()
      if (hasSearchKeyword.value) clearSearch()
    }
  )
  watch(selectedCount, count => {
    if (count === 0) mobileSelectionMode.value = false
  })

  const handleGlobalSearch = (event: Event) => {
    const keyword = (event as CustomEvent<{ keyword: string }>).detail.keyword.trim()
    clearCurrentSelection()
    if (keyword) {
      searchKeyword.value = keyword
      performSearch(keyword, 1, pageSize.value)
    } else if (hasSearchKeyword.value) {
      clearSearch()
      loadFileList()
    }
  }

  onMounted(() => {
    window.addEventListener('keydown', handleGlobalKeydown)
    window.addEventListener('files-search', handleGlobalSearch)
    if (route.query.search && typeof route.query.search === 'string') {
      searchKeyword.value = route.query.search
      performSearch(route.query.search, 1, pageSize.value)
    }
  })
  onBeforeUnmount(() => {
    window.removeEventListener('keydown', handleGlobalKeydown)
    window.removeEventListener('files-search', handleGlobalSearch)
  })
  watch(
    () => route.query.search,
    value => {
      const keyword = typeof value === 'string' ? value.trim() : ''
      if (keyword === searchKeyword.value.trim()) return
      clearCurrentSelection()
      if (keyword) {
        searchKeyword.value = keyword
        performSearch(keyword, 1, pageSize.value)
      } else if (hasSearchKeyword.value) {
        clearSearch()
        currentPage.value = 1
        loadFileList()
      }
    }
  )
</script>

<style scoped>
  .files-page {
    position: relative;
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 8px;
    overflow: hidden;
    padding: 4px;
  }
  /* 复杂选择器需整体声明为全局，避免 scoped 编译后误作用到桌面壳层。 */
  :global(.desktop-shell .files-page) {
    padding: var(--desktop-page-padding);
    gap: 12px;
  }
  :global(.desktop-shell .file-content-area) {
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    background: var(--desktop-surface);
    box-shadow: var(--desktop-shadow-sm);
  }
  .file-content-area {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: auto;
    border-radius: 12px;
    outline: none;
  }
  .file-content-area:focus-visible {
    box-shadow: inset 0 0 0 2px var(--el-color-primary-light-5);
  }
  .selection-box {
    position: fixed;
    z-index: 20;
    border: 1px solid var(--el-color-primary);
    background: color-mix(in srgb, var(--el-color-primary) 14%, transparent);
    pointer-events: none;
  }
  .drop-overlay {
    position: absolute;
    inset: 8px;
    z-index: 30;
    display: grid;
    place-content: center;
    justify-items: center;
    gap: 10px;
    border: 2px dashed var(--el-color-primary);
    border-radius: 16px;
    background: color-mix(in srgb, var(--el-bg-color) 88%, transparent);
    color: var(--el-color-primary);
    pointer-events: none;
    backdrop-filter: blur(8px);
  }
  .drop-overlay span {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .pagination-wrapper {
    flex-shrink: 0;
    padding-top: 8px;
    border-top: 1px solid var(--el-border-color-lighter);
  }
  .pagination {
    justify-content: center;
  }
  .selected-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    max-height: 120px;
    overflow: auto;
  }
  .file-tag {
    max-width: 220px;
  }
  .page-fab {
    position: fixed;
    right: 22px;
    bottom: calc(78px + env(safe-area-inset-bottom));
    z-index: 1000;
    width: 52px;
    height: 52px;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 50%;
    background: var(--el-color-primary);
    color: white;
    box-shadow: 0 10px 24px color-mix(in srgb, var(--el-color-primary) 35%, transparent);
  }
  .mobile-selection-bar {
    position: fixed;
    left: 12px;
    right: 12px;
    bottom: calc(74px + env(safe-area-inset-bottom));
    z-index: 1100;
    min-height: 62px;
    display: flex;
    align-items: center;
    justify-content: space-around;
    gap: 4px;
    padding: 7px 10px;
    border: 1px solid var(--el-border-color-light);
    border-radius: 16px;
    background: var(--el-bg-color-overlay);
    box-shadow: var(--el-box-shadow-light);
  }
  .mobile-selection-bar > span {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .mobile-selection-bar button {
    min-width: 48px;
    display: grid;
    justify-items: center;
    gap: 2px;
    border: 0;
    background: transparent;
    color: var(--el-text-color-regular);
    font-size: 11px;
  }
  .mobile-selection-bar button.danger {
    color: var(--el-color-danger);
  }
  .mobile-selection-bar button:disabled {
    opacity: 0.4;
  }
  @media (max-width: 767px) {
    .files-page {
      gap: 4px;
    }
    .file-content-area {
      padding-bottom: 72px;
    }
    .pagination-wrapper {
      padding-bottom: 72px;
    }
  }
</style>
