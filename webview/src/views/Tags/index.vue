<template>
  <WorkspacePage :title="t('tagCloud.title')" :description="t('tagCloud.description')">
    <template #icon
      ><el-icon :size="24"><CollectionTag /></el-icon
    ></template>
    <template #actions>
      <el-button icon="Refresh" :loading="loading" @click="load">{{ t('common.refresh') }}</el-button>
    </template>

    <section v-loading="loading" class="tag-cloud-page">
      <div class="tag-cloud-summary">
        <div>
          <strong>{{ t('tagCloud.total', { count: tags.length }) }}</strong>
          <span>{{ t('tagCloud.hint') }}</span>
        </div>
        <el-icon><InfoFilled /></el-icon>
      </div>

      <div v-if="tags.length" class="tag-cloud" role="list">
        <el-tooltip v-for="tag in tags" :key="tag.id" :content="tooltip(tag)" placement="top" :show-after="420">
          <div
            role="listitem"
            class="tag-cloud-item"
            :class="[tagCloudSizeClass(fontSize(tag)), { 'is-system': tag.system }]"
            :style="tagStyle(tag)"
            tabindex="0"
            @click="handleTagClick(tag)"
            @keydown="onKeydown(tag, $event)"
            @contextmenu.prevent="openActions(tag, $event)"
            @pointerdown="startLongPress(tag, $event)"
            @pointerup="cancelLongPress"
            @pointercancel="cancelLongPress"
            @pointermove="cancelLongPress"
          >
            <span class="tag-cloud-item__name">{{ tag.name }}</span>
            <span class="tag-cloud-item__count">{{ tag.file_count }}</span>
            <el-icon v-if="tag.system" class="tag-cloud-item__lock"><Lock /></el-icon>
            <button
              v-else-if="isHandheld"
              type="button"
              class="tag-cloud-item__more"
              :aria-label="t('tagCloud.more', { name: tag.name })"
              @click.stop="openActions(tag)"
            >
              <el-icon><MoreFilled /></el-icon>
            </button>
          </div>
        </el-tooltip>
      </div>
      <el-empty v-else-if="!loading" :description="t('tagCloud.empty')" />

      <el-collapse v-if="hiddenTags.length" class="hidden-tags">
        <el-collapse-item name="hidden">
          <template #title>
            <span class="hidden-tags__title"
              ><el-icon><Hide /></el-icon>{{ t('tagCloud.hiddenTitle') }}</span
            >
            <small>{{ t('tagCloud.hiddenCount', { count: hiddenTags.length }) }}</small>
          </template>
          <div class="hidden-tags__list">
            <div
              v-for="tag in hiddenTags"
              :key="tag.id"
              class="hidden-tag"
              :style="{ '--tag-color': tag.category.color }"
            >
              <el-icon><Hide /></el-icon><span>{{ tag.name }}</span
              ><small>{{ tag.file_count }}</small>
              <el-button link type="primary" :loading="operationId === tag.id" @click="openRestore(tag)">{{
                t('tagCloud.restore')
              }}</el-button>
            </div>
          </div>
        </el-collapse-item>
      </el-collapse>
    </section>

    <template #overlays>
      <div
        v-if="contextMenu.visible"
        class="tag-context-menu"
        :style="{ left: `${contextMenu.x}px`, top: `${contextMenu.y}px` }"
        @click.stop
      >
        <button type="button" @click="edit(contextMenu.tag!)">
          <el-icon><Edit /></el-icon>{{ t('tagCloud.edit') }}
        </button>
        <button type="button" class="is-danger" @click="hide(contextMenu.tag!)">
          <el-icon><Hide /></el-icon>{{ t('tagCloud.hide') }}
        </button>
      </div>

      <MobileActionSheet
        v-model="mobileActionsVisible"
        :title="actionTag ? t('tagCloud.more', { name: actionTag.name }) : ''"
        :actions="mobileActions"
        history-key="tag-cloud-actions"
        @select="handleMobileAction"
      />

      <el-dialog
        v-model="editorVisible"
        :title="t('tagCloud.editTitle', { name: editorTag?.name || '' })"
        width="560px"
        :fullscreen="isHandheld"
      >
        <el-form v-loading="editorLoading" label-position="top">
          <el-form-item :label="t('tagCloud.displayName')">
            <el-input
              v-model="displayName"
              maxlength="255"
              show-word-limit
              :placeholder="t('tagCloud.displayNamePlaceholder')"
            />
            <small class="form-hint">{{ t('tagCloud.displayNameHint') }}</small>
          </el-form-item>
          <el-form-item :label="t('tagCloud.displayCategory')">
            <el-select v-model="displayCategoryId" style="width: 100%">
              <el-option
                v-for="category in categories"
                :key="category.id"
                :label="category.name"
                :value="category.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('tagCloud.aliases')">
            <el-select
              v-model="aliases"
              multiple
              filterable
              allow-create
              default-first-option
              :placeholder="t('tagCloud.aliasPlaceholder')"
              style="width: 100%"
            />
            <small class="form-hint">{{ t('tagCloud.aliasHint') }}</small>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="editorVisible = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" :loading="saving" @click="saveEditor">{{ t('common.save') }}</el-button>
        </template>
      </el-dialog>
    </template>
  </WorkspacePage>
</template>

<script setup lang="ts">
  import {
    computed,
    getCurrentInstance,
    onBeforeUnmount,
    onMounted,
    reactive,
    ref,
    type ComponentInternalInstance
  } from 'vue'
  import { useRouter } from 'vue-router'
  import {
    getEnabledTagCategories,
    getTagCloud,
    getTagCloudItem,
    hideTagCloudItem,
    restoreTagCloudItem,
    updateTagCloudItem,
    type TagCategory,
    type TagCloudItem
  } from '@/api/tag'
  import WorkspacePage from '@/components/WorkspacePage/index.vue'
  import { MobileActionSheet } from '@/components/mobile'
  import type { MobileSheetAction } from '@/components/mobile/types'
  import { useI18n, useResponsive } from '@/composables'
  import { sortTagCloudItems, tagCloudFontSize, tagCloudSizeClass } from './tagCloud'
  import { getContrastText } from '@/utils/ui'

  const router = useRouter()
  const { t } = useI18n()
  const { isHandheld } = useResponsive()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const loading = ref(false)
  const operationId = ref('')
  const tags = ref<TagCloudItem[]>([])
  const hiddenTags = ref<TagCloudItem[]>([])
  const categories = ref<TagCategory[]>([])
  const actionTag = ref<TagCloudItem>()
  const mobileActionsVisible = ref(false)
  const editorVisible = ref(false)
  const editorLoading = ref(false)
  const saving = ref(false)
  const editorTag = ref<TagCloudItem>()
  const displayName = ref('')
  const displayCategoryId = ref('')
  const aliases = ref<string[]>([])
  const contextMenu = reactive<{ visible: boolean; x: number; y: number; tag?: TagCloudItem }>({
    visible: false,
    x: 0,
    y: 0
  })
  let longPressTimer: ReturnType<typeof setTimeout> | undefined
  let suppressNextClick = false

  const counts = computed(() => tags.value.map(tag => tag.file_count))
  const minCount = computed(() => (counts.value.length ? Math.min(...counts.value) : 0))
  const maxCount = computed(() => (counts.value.length ? Math.max(...counts.value) : 0))
  const mobileActions = computed<MobileSheetAction[]>(() =>
    actionTag.value?.hidden
      ? [{ key: 'restore', label: t('tagCloud.restore'), icon: 'RefreshLeft', tone: 'primary' }]
      : [
          { key: 'edit', label: t('tagCloud.edit'), icon: 'Edit', tone: 'primary' },
          { key: 'hide', label: t('tagCloud.hide'), icon: 'Hide', tone: 'danger' }
        ]
  )

  const load = async () => {
    loading.value = true
    try {
      const response = await getTagCloud()
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      tags.value = sortTagCloudItems(response.data?.tags || [])
      hiddenTags.value = sortTagCloudItems(response.data?.hidden || [])
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tagCloud.loadFailed'))
    } finally {
      loading.value = false
    }
  }
  const fontSize = (tag: TagCloudItem) =>
    tagCloudFontSize(tag.file_count, minCount.value, maxCount.value, isHandheld.value)
  const tagStyle = (tag: TagCloudItem) => ({
    '--tag-color': tag.category.color,
    '--tag-font-size': `${fontSize(tag)}px`,
    '--tag-text-color': getContrastText(tag.category.color)
  })
  const tooltip = (tag: TagCloudItem) =>
    tag.system
      ? `${tag.name} · ${t('tagCloud.lockedHint')}`
      : `${tag.name} · ${t('tagCloud.fileCount', { count: tag.file_count })}`
  const search = (tag: TagCloudItem) => router.push({ path: '/files', query: { tags: tag.id } })
  const handleTagClick = (tag: TagCloudItem) => {
    if (suppressNextClick) {
      suppressNextClick = false
      return
    }
    void search(tag)
  }
  const closeContextMenu = () => {
    contextMenu.visible = false
    contextMenu.tag = undefined
  }
  const openActions = (tag: TagCloudItem, event?: Pick<MouseEvent, 'clientX' | 'clientY'>) => {
    if (tag.system) {
      return
    }
    actionTag.value = tag
    if (isHandheld.value || !event) {
      mobileActionsVisible.value = true
      return
    }
    contextMenu.x = Math.min(event.clientX, window.innerWidth - 180)
    contextMenu.y = Math.min(event.clientY, window.innerHeight - 110)
    contextMenu.tag = tag
    contextMenu.visible = true
  }
  const startLongPress = (tag: TagCloudItem, event: PointerEvent) => {
    if (!isHandheld.value || event.pointerType === 'mouse' || tag.system) {
      return
    }
    cancelLongPress()
    longPressTimer = setTimeout(() => {
      suppressNextClick = true
      navigator.vibrate?.(25)
      openActions(tag)
    }, 520)
  }
  const cancelLongPress = () => {
    if (longPressTimer) {
      clearTimeout(longPressTimer)
    }
    longPressTimer = undefined
  }
  const onKeydown = (tag: TagCloudItem, event: KeyboardEvent) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      void search(tag)
    }
    if ((event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) && !tag.system) {
      event.preventDefault()
      const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
      openActions(tag, { clientX: rect.left + 12, clientY: rect.bottom + 4 })
    }
  }
  const edit = async (tag: TagCloudItem) => {
    closeContextMenu()
    mobileActionsVisible.value = false
    editorVisible.value = true
    editorLoading.value = true
    try {
      const [editorResponse, categoryResponse] = await Promise.all([getTagCloudItem(tag.id), getEnabledTagCategories()])
      if (editorResponse.code !== 200) {
        throw new Error(editorResponse.message)
      }
      editorTag.value = editorResponse.data.tag
      displayName.value = editorTag.value.name
      aliases.value = editorResponse.data.aliases || []
      categories.value = categoryResponse.code === 200 ? categoryResponse.data || [] : []
      displayCategoryId.value = editorTag.value.category.id
    } catch (error) {
      editorVisible.value = false
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tagCloud.operationFailed'))
    } finally {
      editorLoading.value = false
    }
  }
  const saveEditor = async () => {
    if (!editorTag.value) {
      return
    }
    const name = displayName.value.trim()
    if (!name) {
      proxy?.$modal.msgWarning(t('tagCloud.nameRequired'))
      return
    }
    saving.value = true
    try {
      const response = await updateTagCloudItem(editorTag.value.id, name, displayCategoryId.value, aliases.value)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      proxy?.$modal.msgSuccess(response.data?.rebuild_job ? t('tagCloud.rebuildQueued') : t('tagCloud.saveSuccess'))
      editorVisible.value = false
      await load()
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tagCloud.operationFailed'))
    } finally {
      saving.value = false
    }
  }
  const hide = async (tag: TagCloudItem) => {
    closeContextMenu()
    mobileActionsVisible.value = false
    try {
      await proxy?.$modal.confirm(t('tagCloud.hideConfirm', { name: tag.name }))
      operationId.value = tag.id
      const response = await hideTagCloudItem(tag.id)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      proxy?.$modal.msgSuccess(t('tagCloud.hideSuccess'))
      await load()
    } catch (error) {
      if (error instanceof Error) {
        proxy?.$modal.msgError(error.message)
      }
    } finally {
      operationId.value = ''
    }
  }
  const restore = async (tag: TagCloudItem) => {
    operationId.value = tag.id
    try {
      const response = await restoreTagCloudItem(tag.id)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      proxy?.$modal.msgSuccess(t('tagCloud.restoreSuccess'))
      await load()
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tagCloud.operationFailed'))
    } finally {
      operationId.value = ''
    }
  }
  const openRestore = (tag: TagCloudItem) => {
    if (!isHandheld.value) {
      void restore(tag)
      return
    }
    actionTag.value = tag
    mobileActionsVisible.value = true
  }
  const handleMobileAction = (key: string) => {
    if (!actionTag.value) {
      return
    }
    if (key === 'edit') {
      void edit(actionTag.value)
    } else if (key === 'restore') {
      void restore(actionTag.value)
    } else {
      void hide(actionTag.value)
    }
  }

  onMounted(() => {
    void load()
    document.addEventListener('click', closeContextMenu)
    window.addEventListener('resize', closeContextMenu)
  })
  onBeforeUnmount(() => {
    cancelLongPress()
    document.removeEventListener('click', closeContextMenu)
    window.removeEventListener('resize', closeContextMenu)
  })
</script>

<style scoped>
  .tag-cloud-page {
    min-height: 100%;
    padding: 12px;
    display: grid;
    align-content: start;
    gap: 18px;
  }
  .tag-cloud-summary {
    padding: 14px 16px;
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    color: var(--primary-color);
    background: var(--desktop-primary-soft);
  }
  .tag-cloud-summary div {
    min-width: 0;
    display: grid;
    gap: 4px;
  }
  .tag-cloud-summary strong {
    color: var(--text-primary);
    font-size: 15px;
  }
  .tag-cloud-summary span {
    color: var(--text-secondary);
    font-size: 12px;
  }
  .tag-cloud {
    min-height: 220px;
    padding: 26px;
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    display: flex;
    align-content: center;
    align-items: center;
    justify-content: center;
    flex-wrap: wrap;
    gap: 12px;
    background: var(--desktop-surface-muted);
  }
  .tag-cloud-item {
    --tag-color: var(--primary-color);
    min-width: 0;
    min-height: 38px;
    padding: 7px 10px 7px 15px;
    border: 1px solid color-mix(in srgb, var(--tag-color) 62%, rgb(0 0 0));
    border-radius: 999px;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--tag-text-color);
    background: var(--tag-color);
    box-shadow: 0 4px 12px color-mix(in srgb, var(--tag-color) 8%, transparent);
    cursor: pointer;
    user-select: none;
    transition:
      transform 160ms ease,
      border-color 160ms ease,
      box-shadow 160ms ease,
      background 160ms ease;
  }
  .tag-cloud-item.is-medium {
    min-height: 44px;
    padding: 9px 12px 9px 18px;
  }
  .tag-cloud-item.is-large {
    min-height: 50px;
    padding: 10px 14px 10px 21px;
    gap: 10px;
  }
  .tag-cloud-item:hover {
    transform: translateY(-2px);
    border-color: color-mix(in srgb, var(--tag-color) 70%, rgb(0 0 0));
    background: color-mix(in srgb, var(--tag-color) 88%, rgb(0 0 0));
    box-shadow: 0 9px 20px color-mix(in srgb, var(--tag-color) 18%, transparent);
  }
  .tag-cloud-item:active {
    transform: translateY(0);
  }
  .tag-cloud-item:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--primary-color) 35%, transparent);
    outline-offset: 3px;
  }
  .tag-cloud-item__name {
    max-width: min(360px, 46vw);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--tag-font-size);
    font-weight: 720;
    line-height: 1.15;
    letter-spacing: -0.018em;
  }
  .tag-cloud-item__count {
    min-width: 24px;
    height: 24px;
    padding-inline: 7px;
    border-radius: 999px;
    display: inline-grid;
    place-items: center;
    color: var(--tag-text-color);
    background: rgba(0, 0, 0, 0.14);
    font-size: 11px;
    font-weight: 750;
  }
  .is-large .tag-cloud-item__count {
    min-width: 30px;
    height: 30px;
    font-size: 12px;
  }
  .tag-cloud-item__lock {
    font-size: 13px;
    opacity: 0.7;
  }
  .tag-cloud-item__more {
    width: 30px;
    height: 30px;
    margin-right: -5px;
    border: 0;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: inherit;
    background: transparent;
  }
  .tag-cloud-item__more:active {
    background: color-mix(in srgb, var(--tag-color) 16%, transparent);
  }
  .hidden-tags {
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    overflow: hidden;
    background: var(--desktop-surface);
  }
  .hidden-tags :deep(.el-collapse-item__header) {
    min-height: 52px;
    padding: 0 16px;
    gap: 10px;
    background: transparent;
  }
  .hidden-tags__title {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text-primary);
    font-weight: 700;
  }
  .hidden-tags small {
    color: var(--text-secondary);
  }
  .hidden-tags__list {
    padding: 4px 14px 14px;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .hidden-tag {
    --tag-color: var(--text-secondary);
    min-height: 36px;
    padding: 5px 7px 5px 12px;
    border: 1px dashed color-mix(in srgb, var(--tag-color) 32%, var(--desktop-border));
    border-radius: 999px;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    color: var(--text-secondary);
    background: color-mix(in srgb, var(--tag-color) 4%, var(--desktop-fill));
  }
  .hidden-tag > span {
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tag-context-menu {
    position: fixed;
    z-index: 3100;
    width: 172px;
    padding: 6px;
    border: 1px solid var(--desktop-border);
    border-radius: 12px;
    background: var(--desktop-surface);
    box-shadow: var(--desktop-shadow-lg);
  }
  .tag-context-menu button {
    width: 100%;
    min-height: 38px;
    padding: 0 10px;
    border: 0;
    border-radius: 8px;
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--text-primary);
    background: transparent;
    text-align: left;
  }
  .tag-context-menu button:hover {
    background: var(--desktop-fill);
  }
  .tag-context-menu button.is-danger {
    color: var(--danger-color);
  }
  .form-hint {
    display: block;
    margin-top: 7px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
  html.dark .tag-cloud-item {
    color: var(--tag-text-color);
    background: var(--tag-color);
    border-color: color-mix(in srgb, var(--tag-color) 62%, rgb(255 255 255));
  }
  html.dark .tag-cloud-item__count {
    color: var(--tag-text-color);
    background: rgba(255, 255, 255, 0.14);
  }
  @media (max-width: 767px) {
    .tag-cloud-page {
      padding: 4px;
      gap: 12px;
    }
    .tag-cloud {
      min-height: 180px;
      padding: 16px 10px;
      justify-content: flex-start;
      gap: 9px;
    }
    .tag-cloud-item {
      min-height: 44px;
    }
    .tag-cloud-item__name {
      max-width: 58vw;
    }
    .tag-cloud-summary {
      align-items: flex-start;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .tag-cloud-item {
      transition: none;
    }
    .tag-cloud-item:hover,
    .tag-cloud-item:active {
      transform: none;
    }
  }
</style>
