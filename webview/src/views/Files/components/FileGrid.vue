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
      @dblclick.prevent="$emit('entry-open', entry)"
      @contextmenu.prevent="$emit('entry-context', entry, $event)"
      @keydown.enter.prevent="$emit('entry-open', entry)"
      @keydown.space.prevent="$emit('entry-toggle', entry)"
      @keydown.shift.f10.prevent="$emit('entry-context', entry, $event)"
      @pointerdown="startLongPress(entry, $event)"
      @pointerup="cancelLongPress"
      @pointercancel="cancelLongPress"
      @pointermove="cancelLongPress"
    >
      <button class="more-button" type="button" :aria-label="t('common.more')" @click.stop="openMore(entry, $event)">
        <el-icon><More /></el-icon>
      </button>

      <div class="file-icon">
        <el-icon v-if="entry.type === 'folder'" :size="58" class="folder-icon"><Folder /></el-icon>
        <file-icon
          v-else
          :mime-type="entry.file.mime_type"
          :file-name="entry.file.file_name"
          :thumbnail-url="getThumbnailUrl(entry.file.file_id)"
          :show-thumbnail="entry.file.has_thumbnail"
          :icon-size="54"
          :is-encrypted="entry.file.is_enc"
        />
      </div>
      <file-name-tooltip
        v-if="entry.type === 'file'"
        :file-name="entry.file.file_name"
        view-mode="grid"
        tag="div"
        custom-class="file-name"
      />
      <div v-else class="file-name folder-name">{{ cleanName(entry.folder.name) }}</div>
      <div class="file-info">
        <span v-if="entry.type === 'file'"
          >{{ formatSize(entry.file.file_size) }} · {{ formatDate(entry.file.created_at) }}</span
        >
        <span v-else>{{ formatDate(entry.folder.created_time) }}</span>
        <span v-if="entry.type === 'file'" class="file-tags">
          <el-icon v-if="entry.file.is_enc" class="encrypted"><Lock /></el-icon>
          <el-icon v-if="entry.file.public" class="public"><Share /></el-icon>
        </span>
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
  import { formatDate, formatSize } from '@/utils'
  import { useI18n } from '@/composables'
  import type { FileEntry } from '../types'

  const props = defineProps<{
    entries: FileEntry[]
    isSelected: (entry: FileEntry) => boolean
    getThumbnailUrl: (fileId: string) => string
  }>()
  const emit = defineEmits<{
    'entry-click': [entry: FileEntry, event: MouseEvent]
    'entry-toggle': [entry: FileEntry]
    'entry-open': [entry: FileEntry]
    'entry-context': [entry: FileEntry, event: MouseEvent | KeyboardEvent]
    'entry-long-press': [entry: FileEntry]
  }>()
  const { t } = useI18n()
  let longPressTimer: ReturnType<typeof setTimeout> | undefined
  let longPressTriggered = false

  const cleanName = (name: string) => name.replace(/^\/+/, '')
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
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 14px;
    padding: 6px;
  }
  .file-card {
    position: relative;
    min-width: 0;
    padding: 16px 12px 14px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 14px;
    background: var(--card-bg);
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
    top: 7px;
    right: 7px;
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
    background: var(--el-fill-color);
  }
  .file-icon {
    height: 74px;
    display: grid;
    place-items: center;
  }
  .folder-icon {
    color: var(--el-color-primary);
  }
  .file-name {
    margin-top: 7px;
    color: var(--el-text-color-primary);
    font-size: 13px;
    text-align: center;
  }
  .folder-name {
    overflow: hidden;
    color: var(--el-color-primary);
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-info {
    min-height: 20px;
    margin-top: 5px;
    color: var(--el-text-color-secondary);
    font-size: 11px;
    text-align: center;
  }
  .file-tags {
    margin-left: 5px;
    display: inline-flex;
    gap: 3px;
  }
  .encrypted {
    color: var(--el-color-warning);
  }
  .public {
    color: var(--el-color-success);
  }
  @media (max-width: 1024px) {
    .file-grid {
      grid-template-columns: repeat(auto-fill, minmax(106px, 1fr));
      gap: 10px;
    }
    .file-card {
      padding: 12px 8px;
    }
    .more-button {
      opacity: 1;
    }
  }
  @media (max-width: 480px) {
    .file-grid {
      grid-template-columns: repeat(auto-fill, minmax(88px, 1fr));
      gap: 8px;
    }
    .file-icon {
      height: 60px;
    }
  }
</style>
