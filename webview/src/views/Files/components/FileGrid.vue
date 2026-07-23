<template>
  <div class="file-grid" role="listbox" aria-multiselectable="true">
    <article
      v-for="entry in entries"
      :key="entry.key"
      class="file-card"
      :class="{ selected: isSelected(entry), 'folder-card': entry.type === 'folder' }"
      :data-entry-key="entry.key"
      :aria-selected="isSelected(entry)"
      role="option"
      tabindex="0"
      @click="handleClick(entry, $event)"
      @dblclick.prevent="$emit('entry-open', entry, 'double-click')"
      @contextmenu.prevent="$emit('entry-context', entry, $event)"
      @keydown.enter.prevent="$emit('entry-open', entry, 'keyboard')"
      @keydown.space.prevent="$emit('entry-toggle', entry)"
      @keydown.shift.f10.prevent="$emit('entry-context', entry, $event)"
      @pointerdown="startLongPress(entry, $event)"
      @pointerup="cancelLongPress"
      @pointercancel="cancelLongPress"
      @pointermove="cancelLongPress"
    >
      <div class="file-card-header">
        <el-icon v-if="entry.type === 'folder'" :size="20" class="header-icon folder-icon"><Folder /></el-icon>
        <el-icon v-else :size="20" class="header-icon" :color="getFileIcon(entry.file.mime_type).color">
          <component :is="getFileIcon(entry.file.mime_type).icon" />
        </el-icon>
        <file-name-tooltip
          v-if="entry.type === 'file'"
          :file-name="entry.file.file_name"
          view-mode="list"
          tag="div"
          custom-class="file-name"
        />
        <div v-else class="file-name folder-name">{{ entry.folder.name }}</div>
      </div>

      <button class="more-button" type="button" :aria-label="t('common.more')" @click.stop="openMore(entry, $event)">
        <el-icon><More /></el-icon>
      </button>

      <div class="file-preview">
        <el-icon v-if="entry.type === 'folder'" :size="88" class="folder-icon"><Folder /></el-icon>
        <file-icon
          v-else
          :mime-type="entry.file.mime_type"
          :file-name="entry.file.file_name"
          :thumbnail-url="getThumbnailUrl(entry.file.file_id)"
          :show-thumbnail="entry.file.has_thumbnail"
          :icon-size="72"
          :is-encrypted="entry.file.is_enc"
          fluid
        />
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
  import { useI18n } from '@/composables'
  import { getFileIcon } from '@/utils/file/fileIcon'
  import type { FileEntry } from '../types'

  const props = defineProps<{
    entries: FileEntry[]
    isSelected: (entry: FileEntry) => boolean
    getThumbnailUrl: (fileId: string) => string
  }>()
  const emit = defineEmits<{
    'entry-click': [entry: FileEntry, event: MouseEvent]
    'entry-toggle': [entry: FileEntry]
    'entry-open': [entry: FileEntry, trigger: 'double-click' | 'keyboard']
    'entry-context': [entry: FileEntry, event: MouseEvent | KeyboardEvent]
    'entry-long-press': [entry: FileEntry]
  }>()
  const { t } = useI18n()
  let longPressTimer: ReturnType<typeof setTimeout> | undefined
  let longPressTriggered = false

  const startLongPress = (entry: FileEntry, event: PointerEvent) => {
    if (event.pointerType === 'mouse') return
    cancelLongPress()
    longPressTriggered = false
    longPressTimer = setTimeout(() => {
      longPressTriggered = true
      emit('entry-long-press', entry)
    }, 450)
  }
  const handleClick = (entry: FileEntry, event: MouseEvent) => {
    if (longPressTriggered) {
      longPressTriggered = false
      return
    }
    emit('entry-click', entry, event)
  }
  const cancelLongPress = () => {
    if (longPressTimer) clearTimeout(longPressTimer)
    longPressTimer = undefined
  }
  const openMore = (entry: FileEntry, event: MouseEvent) => {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    const menuEvent = new MouseEvent('contextmenu', { clientX: rect.right, clientY: rect.bottom })
    emit('entry-context', entry, menuEvent)
  }
  onBeforeUnmount(cancelLongPress)
</script>

<style scoped>
  .file-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 16px;
    padding: 6px;
  }
  .file-card {
    position: relative;
    min-width: 0;
    padding: 8px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 14px;
    background: var(--el-fill-color-light);
    cursor: default;
    user-select: none;
    transition:
      border-color 0.16s ease,
      background 0.16s ease,
      box-shadow 0.16s ease;
  }
  .file-card:hover {
    border-color: var(--el-color-primary-light-5);
    background: var(--card-hover-bg);
    box-shadow: 0 8px 20px rgba(15, 23, 42, 0.08);
  }
  .file-card:focus-visible {
    outline: 2px solid var(--el-color-primary);
    outline-offset: 2px;
  }
  .file-card.selected {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    box-shadow: 0 0 0 2px var(--el-color-primary-light-8);
  }
  .more-button {
    position: absolute;
    top: 8px;
    right: 8px;
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--el-text-color-secondary);
    opacity: 0;
    cursor: pointer;
  }
  .file-card:hover .more-button,
  .file-card:focus-within .more-button {
    opacity: 1;
  }
  .more-button:hover {
    background: var(--el-fill-color-darker);
  }
  .file-card-header {
    height: 44px;
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    padding: 0 36px 0 4px;
  }
  .header-icon {
    flex: 0 0 auto;
  }
  .file-preview {
    width: 100%;
    aspect-ratio: 4 / 3;
    display: grid;
    place-items: center;
    overflow: hidden;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 11px;
    background: var(--el-bg-color);
  }
  .file-preview :deep(.file-icon-card),
  .file-preview :deep(.thumbnail-image) {
    border: 0;
    border-radius: 10px;
  }
  .file-preview :deep(.thumbnail-image:hover) {
    transform: none;
    box-shadow: none;
  }
  .folder-icon {
    color: var(--el-color-primary);
  }
  .file-name {
    min-width: 0;
    overflow: hidden;
    color: var(--el-text-color-primary);
    font-size: 14px;
    line-height: 1.4;
    text-align: left;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .folder-name {
    color: var(--el-color-primary);
    font-weight: 600;
  }
  @media (max-width: 1024px) {
    .file-grid {
      grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
      gap: 12px;
    }
    .file-card {
      padding: 7px;
    }
    .more-button {
      opacity: 1;
    }
    .file-card-header {
      height: 40px;
      padding-left: 3px;
    }
  }
  @media (max-width: 480px) {
    .file-grid {
      grid-template-columns: repeat(auto-fill, minmax(132px, 1fr));
      gap: 8px;
    }
    .file-card-header {
      height: 38px;
      gap: 7px;
    }
    .file-name {
      font-size: 12px;
    }
  }
</style>
