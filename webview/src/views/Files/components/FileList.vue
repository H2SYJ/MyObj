<template>
  <div class="file-list" role="listbox" aria-multiselectable="true">
    <div class="list-header" aria-hidden="true">
      <span>{{ t('trash.name') }}</span>
      <span>{{ t('trash.size') }}</span>
      <span>{{ t('admin.users.createTime') }}</span>
      <span></span>
    </div>
    <article
      v-for="entry in entries"
      :key="entry.key"
      class="list-row"
      :class="{ selected: isSelected(entry) }"
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
      <div class="name-cell">
        <el-icon v-if="entry.type === 'folder'" :size="48" class="folder-icon"><Folder /></el-icon>
        <file-icon
          v-else
          :mime-type="entry.file.mime_type"
          :file-name="entry.file.file_name"
          :thumbnail-url="getThumbnailUrl(entry.file.file_id)"
          :show-thumbnail="entry.file.has_thumbnail"
          :icon-size="48"
          :show-badge="false"
          :is-encrypted="entry.file.is_enc"
        />
        <div class="name-content">
          <file-name-tooltip v-if="entry.type === 'file'" :file-name="entry.file.file_name" view-mode="table" />
          <span v-else class="folder-name">{{ entry.folder.name }}</span>
          <FileTags
            v-if="entry.type === 'file'"
            :tags="entry.file.tags"
            :limit="tagLimit"
            compact
            @tag-click="tag => $emit('tag-click', tag)"
          />
          <span v-if="entry.type === 'file'" class="mobile-meta">
            {{ formatSize(entry.file.file_size) }} · {{ formatDate(entry.file.created_at) }}
          </span>
        </div>
        <span v-if="entry.type === 'file'" class="badges">
          <el-icon v-if="entry.file.is_enc" class="encrypted"><Lock /></el-icon>
          <el-icon v-if="entry.file.public" class="public"><Share /></el-icon>
        </span>
      </div>
      <span class="desktop-only size-cell">{{ entry.type === 'file' ? formatSize(entry.file.file_size) : '-' }}</span>
      <span class="desktop-only time-cell">{{
        formatDate(entry.type === 'file' ? entry.file.created_at : entry.folder.created_at)
      }}</span>
      <button class="more-button" type="button" :aria-label="t('common.more')" @click.stop="openMore(entry, $event)">
        <el-icon><More /></el-icon>
      </button>
    </article>
  </div>
</template>

<script setup lang="ts">
  import { formatDate, formatSize } from '@/utils'
  import { useI18n } from '@/composables'
  import type { FileEntry } from '../types'
  import type { CompactTag } from '@/types'
  import FileTags from '@/components/FileTags/index.vue'

  withDefaults(
    defineProps<{
      entries: FileEntry[]
      isSelected: (entry: FileEntry) => boolean
      getThumbnailUrl: (fileId: string) => string
      tagLimit?: number
    }>(),
    { tagLimit: 3 }
  )
  const emit = defineEmits<{
    'entry-click': [entry: FileEntry, event: MouseEvent]
    'entry-toggle': [entry: FileEntry]
    'entry-open': [entry: FileEntry, trigger: 'double-click' | 'keyboard']
    'entry-context': [entry: FileEntry, event: MouseEvent | KeyboardEvent]
    'entry-long-press': [entry: FileEntry]
    'tag-click': [tag: CompactTag]
  }>()
  const { t } = useI18n()
  let longPressTimer: ReturnType<typeof setTimeout> | undefined
  let longPressTriggered = false
  const startLongPress = (entry: FileEntry, event: PointerEvent) => {
    if (event.pointerType === 'mouse') {
      return
    }
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
    if (longPressTimer) {
      clearTimeout(longPressTimer)
    }
    longPressTimer = undefined
  }
  const openMore = (entry: FileEntry, event: MouseEvent) => {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    emit('entry-context', entry, new MouseEvent('contextmenu', { clientX: rect.right, clientY: rect.bottom }))
  }
  onBeforeUnmount(cancelLongPress)
</script>

<style scoped>
  .file-list {
    width: 100%;
    padding: 4px;
  }
  .list-header,
  .list-row {
    display: grid;
    grid-template-columns: minmax(280px, 1fr) 120px 180px 48px;
    align-items: center;
  }
  .list-header {
    min-height: 44px;
    padding: 0 16px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .list-row {
    min-height: 76px;
    padding: 8px 10px 8px 16px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
    user-select: none;
  }
  .list-row:hover {
    background: var(--el-fill-color-light);
  }
  .list-row.selected {
    background: var(--el-color-primary-light-9);
    box-shadow: inset 3px 0 0 var(--el-color-primary);
  }
  .list-row:focus-visible {
    outline: 2px solid var(--el-color-primary);
    outline-offset: -2px;
  }
  .name-cell {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .name-content {
    min-width: 0;
    flex: 1;
  }
  .name-content :deep(.file-tags) {
    margin-top: 4px;
  }
  .name-content :deep(.file-name-text--table),
  .folder-name {
    display: block;
    max-width: 100%;
    overflow: hidden;
    font-size: 15px;
    line-height: 1.45;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .folder-icon,
  .folder-name {
    color: var(--el-color-primary);
  }
  .folder-name {
    font-weight: 600;
  }
  .badges {
    display: inline-flex;
    gap: 5px;
  }
  .encrypted {
    color: var(--el-color-warning);
  }
  .public {
    color: var(--el-color-success);
  }
  .size-cell,
  .time-cell {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .more-button {
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--el-text-color-secondary);
    opacity: 0;
    cursor: pointer;
  }
  .list-row:hover .more-button,
  .list-row:focus-within .more-button {
    opacity: 1;
  }
  .more-button:hover {
    background: var(--el-fill-color);
  }
  .mobile-meta {
    display: none;
    margin-top: 3px;
    color: var(--el-text-color-secondary);
    font-size: 11px;
  }
  @media (max-width: 1024px) {
    .list-header,
    .desktop-only {
      display: none;
    }
    .list-row {
      grid-template-columns: 1fr 44px;
      min-height: 80px;
      padding: 8px 4px 8px 10px;
    }
    .more-button {
      opacity: 1;
    }
    .mobile-meta {
      display: block;
    }
  }
  @media (max-width: 480px) {
    .name-cell {
      gap: 11px;
    }
    .name-content :deep(.file-name-text--table),
    .folder-name {
      font-size: 14px;
    }
  }
</style>
