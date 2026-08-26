<template>
  <div class="editable-file-tags" :aria-label="t('tags.fileTags')">
    <el-tag
      v-for="tag in tags"
      :key="tag.id"
      size="small"
      effect="plain"
      class="editable-file-tags__tag"
      :class="{ 'is-removing': removingTagIds.has(tag.id) }"
      :style="tagStyle(tag)"
    >
      <span class="editable-file-tags__name" :title="tag.name">{{ tag.name }}</span>
      <button
        v-if="detailsLoaded"
        type="button"
        class="editable-file-tags__remove"
        :aria-label="removeLabel(tag)"
        :title="removeLabel(tag)"
        :disabled="removingTagIds.has(tag.id)"
        @click.stop="removeTag(tag)"
      >
        <el-icon v-if="removingTagIds.has(tag.id)" class="is-loading"><Loading /></el-icon>
        <el-icon v-else><Close /></el-icon>
      </button>
    </el-tag>

    <el-popover
      v-model:visible="addVisible"
      placement="bottom-end"
      trigger="click"
      width="min(380px, calc(100vw - 24px))"
      :show-arrow="false"
      @show="prepareAddPanel"
    >
      <template #reference>
        <button
          type="button"
          class="editable-file-tags__add"
          :aria-label="t('tags.inlineAdd')"
          :title="t('tags.inlineAdd')"
          :disabled="detailsLoading"
        >
          <el-icon :class="{ 'is-loading': detailsLoading }">
            <Loading v-if="detailsLoading" />
            <Plus v-else />
          </el-icon>
        </button>
      </template>

      <div class="editable-file-tags__panel">
        <strong>{{ t('tags.inlineAddTitle') }}</strong>
        <el-select
          v-model="selectedNames"
          multiple
          filterable
          allow-create
          default-first-option
          remote
          reserve-keyword
          :teleported="false"
          :remote-method="loadSuggestions"
          :loading="suggestionsLoading"
          :multiple-limit="20"
          :placeholder="t('tags.inlineAddPlaceholder')"
        >
          <el-option v-for="tag in availableSuggestions" :key="tag.id" :label="tag.name" :value="tag.name" />
        </el-select>

        <div class="editable-file-tags__fields">
          <label>
            <span>{{ t('tags.inlineNewCategory') }}</span>
            <el-select v-model="categoryId" :placeholder="t('tags.categoryPlaceholder')" :teleported="false">
              <el-option
                v-for="category in categories"
                :key="category.id"
                :label="category.name"
                :value="category.id"
              />
            </el-select>
          </label>
          <fieldset>
            <legend>{{ t('tags.visibility') }}</legend>
            <el-radio-group v-model="visibility" size="small">
              <el-radio-button value="private">{{ t('tags.private') }}</el-radio-button>
              <el-radio-button value="public">{{ t('tags.public') }}</el-radio-button>
            </el-radio-group>
          </fieldset>
        </div>

        <div class="editable-file-tags__actions">
          <el-button size="small" @click="addVisible = false">{{ t('common.cancel') }}</el-button>
          <el-button size="small" type="primary" :loading="saving" :disabled="addDisabled" @click="addTags">
            {{ t('common.add') }}
          </el-button>
        </div>
      </div>
    </el-popover>
  </div>
</template>

<script setup lang="ts">
  import { computed, getCurrentInstance, ref, watch, type ComponentInternalInstance } from 'vue'
  import { Close, Loading, Plus } from '@element-plus/icons-vue'
  import {
    getEnabledTagCategories,
    getFileTags,
    getTagSuggestions,
    updateManualTags,
    updateTagExclusions,
    type FileTag,
    type FileTagsData,
    type TagCategory
  } from '@/api/tag'
  import type { CompactTag } from '@/types'
  import { useI18n } from '@/composables'
  import { getTagStyle } from '@/utils/ui'

  const props = withDefaults(defineProps<{ fileId: string; initialTags?: CompactTag[] }>(), {
    initialTags: () => []
  })
  const emit = defineEmits<{ updated: [details: FileTagsData] }>()
  const { t } = useI18n()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance

  const tags = ref<FileTag[]>([])
  const detailsLoaded = ref(false)
  const detailsLoading = ref(false)
  const removingTagIds = ref(new Set<string>())
  const addVisible = ref(false)
  const saving = ref(false)
  const suggestionsLoading = ref(false)
  const categories = ref<TagCategory[]>([])
  const suggestions = ref<CompactTag[]>([])
  const selectedNames = ref<string[]>([])
  const categoryId = ref('other')
  const visibility = ref<'private' | 'public'>('private')
  let detailsRequest = 0
  let suggestionsRequest = 0
  const knownSuggestions = new Map<string, CompactTag>()

  const normalizedName = (name: string) => name.trim().toLocaleLowerCase()

  const previewTags = (items: CompactTag[]): FileTag[] =>
    items.map(tag => ({
      id: tag.id,
      name: tag.name,
      category: {
        id: tag.category_code,
        code: tag.category_code,
        name: tag.category_code,
        color: tag.color
      },
      sources: [],
      visibility: tag.visibility === 'public' ? 'public' : tag.visibility === 'private' ? 'private' : 'inherit',
      automatic: false
    }))

  const activeNames = computed(() => new Set(tags.value.map(tag => normalizedName(tag.name))))
  const availableSuggestions = computed(() =>
    suggestions.value.filter(tag => !activeNames.value.has(normalizedName(tag.name)))
  )
  const cleanSelectedNames = computed(() => {
    const unique = new Map<string, string>()
    for (const value of selectedNames.value) {
      const name = value.trim()
      const normalized = normalizedName(name)
      if (name && !activeNames.value.has(normalized) && !unique.has(normalized)) {
        unique.set(normalized, name)
      }
    }
    return [...unique.values()].slice(0, 20)
  })
  const addDisabled = computed(
    () => saving.value || cleanSelectedNames.value.length === 0 || categories.value.length === 0 || !categoryId.value
  )

  const tagStyle = (tag: FileTag) => getTagStyle(tag.category.color || '')

  const removeLabel = (tag: FileTag) => {
    const manual = tag.sources.includes('manual')
    if (manual && tag.automatic) {
      return t('tags.removeAndSuppress', { name: tag.name })
    }
    return tag.automatic ? t('tags.suppressTag', { name: tag.name }) : t('tags.removeTag', { name: tag.name })
  }

  const applyDetails = (details: FileTagsData, shouldEmit: boolean) => {
    tags.value = details.tags || []
    detailsLoaded.value = true
    if (shouldEmit) {
      emit('updated', details)
    }
  }

  const fetchDetails = async (fileId: string, shouldEmit = false) => {
    const request = ++detailsRequest
    const response = await getFileTags(fileId)
    if (request !== detailsRequest || fileId !== props.fileId) {
      return undefined
    }
    if (response.code !== 200 || !response.data) {
      throw new Error(response.message || t('tags.loadFailed'))
    }
    applyDetails(response.data, shouldEmit)
    return response.data
  }

  const loadDetails = async (fileId: string) => {
    detailsLoading.value = true
    try {
      await fetchDetails(fileId)
    } catch (error) {
      if (fileId === props.fileId) {
        proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.loadFailed'))
      }
    } finally {
      if (fileId === props.fileId) {
        detailsLoading.value = false
      }
    }
  }

  const assertMutationSucceeded = (response: { code: number; message?: string }, fallback: string) => {
    if (response.code !== 200) {
      throw new Error(response.message || fallback)
    }
  }

  const setRemoving = (tagId: string, removing: boolean) => {
    const next = new Set(removingTagIds.value)
    if (removing) {
      next.add(tagId)
    } else {
      next.delete(tagId)
    }
    removingTagIds.value = next
  }

  const removeTag = async (tag: FileTag) => {
    if (removingTagIds.value.has(tag.id)) {
      return
    }
    const fileId = props.fileId
    const operations: Promise<unknown>[] = []
    if (tag.sources.includes('manual')) {
      operations.push(
        updateManualTags(fileId, [], [tag.id]).then(response => assertMutationSucceeded(response, t('tags.saveFailed')))
      )
    }
    if (tag.automatic) {
      operations.push(
        updateTagExclusions(fileId, [tag.id], []).then(response =>
          assertMutationSucceeded(response, t('tags.saveFailed'))
        )
      )
    }
    if (operations.length === 0) {
      return
    }

    setRemoving(tag.id, true)
    const results = await Promise.allSettled(operations)
    try {
      if (fileId === props.fileId) {
        await fetchDetails(fileId, true)
      }
      const failure = results.find(result => result.status === 'rejected')
      if (failure?.status === 'rejected') {
        throw failure.reason
      }
    } catch (error) {
      if (fileId === props.fileId) {
        proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.saveFailed'))
      }
    } finally {
      setRemoving(tag.id, false)
    }
  }

  const ensureCategories = async () => {
    if (categories.value.length > 0) {
      return
    }
    const response = await getEnabledTagCategories()
    if (response.code !== 200) {
      throw new Error(response.message || t('tags.loadFailed'))
    }
    categories.value = response.data || []
    categoryId.value = categories.value.find(category => category.code === 'other')?.id || categories.value[0]?.id || ''
  }

  const loadSuggestions = async (keyword = '') => {
    const request = ++suggestionsRequest
    suggestionsLoading.value = true
    try {
      const response = await getTagSuggestions({ keyword, limit: 50 })
      if (request !== suggestionsRequest) {
        return
      }
      if (response.code !== 200) {
        throw new Error(response.message || t('tags.loadFailed'))
      }
      suggestions.value = response.data || []
      for (const tag of suggestions.value) {
        knownSuggestions.set(normalizedName(tag.name), tag)
      }
    } catch (error) {
      if (request === suggestionsRequest) {
        suggestions.value = []
        proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.loadFailed'))
      }
    } finally {
      if (request === suggestionsRequest) {
        suggestionsLoading.value = false
      }
    }
  }

  const prepareAddPanel = async () => {
    selectedNames.value = []
    visibility.value = 'private'
    try {
      await Promise.all([ensureCategories(), loadSuggestions('')])
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.loadFailed'))
    }
  }

  const addTags = async () => {
    const names = cleanSelectedNames.value
    if (names.length === 0 || !categoryId.value) {
      return
    }
    const fileId = props.fileId
    const categoryByCode = new Map(categories.value.map(category => [category.code, category.id]))
    const add = names.map(name => {
      const suggestion = knownSuggestions.get(normalizedName(name))
      return {
        name,
        category_id: suggestion ? categoryByCode.get(suggestion.category_code) || categoryId.value : categoryId.value,
        visibility: visibility.value
      }
    })

    saving.value = true
    try {
      const response = await updateManualTags(fileId, add, [])
      assertMutationSucceeded(response, t('tags.saveFailed'))
      if (fileId !== props.fileId) {
        return
      }
      await fetchDetails(fileId, true)
      selectedNames.value = []
      addVisible.value = false
      proxy?.$modal.msgSuccess(t('tags.saveSuccess'))
    } catch (error) {
      if (fileId === props.fileId) {
        proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.saveFailed'))
      }
    } finally {
      saving.value = false
    }
  }

  watch(
    () => [props.fileId, props.initialTags] as const,
    ([fileId, initialTags], previous) => {
      const fileChanged = !previous || previous[0] !== fileId
      if (!fileChanged) {
        return
      }
      ++detailsRequest
      ++suggestionsRequest
      detailsLoaded.value = false
      detailsLoading.value = false
      removingTagIds.value = new Set()
      addVisible.value = false
      tags.value = previewTags(initialTags)
      if (fileId) {
        void loadDetails(fileId)
      }
    },
    { immediate: true }
  )
</script>

<style scoped>
  .editable-file-tags {
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
  }
  .editable-file-tags__tag {
    max-width: 180px;
    border-color: var(--el-tag-border-color, var(--el-color-primary-light-8));
    color: var(--el-tag-text-color, var(--el-color-primary));
    background: var(--el-tag-bg-color, var(--el-color-primary-light-9));
  }
  .editable-file-tags__tag.is-removing {
    opacity: 0.62;
  }
  .editable-file-tags__tag :deep(.el-tag__content) {
    min-width: 0;
    display: inline-flex;
    align-items: center;
  }
  .editable-file-tags__name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .editable-file-tags__remove,
  .editable-file-tags__add {
    display: inline-grid;
    place-items: center;
    border: 0;
    color: inherit;
    background: transparent;
    cursor: pointer;
  }
  .editable-file-tags__remove {
    width: 0;
    height: 18px;
    margin-left: 0;
    padding: 0;
    overflow: hidden;
    opacity: 0;
    transition:
      width 0.16s ease,
      margin-left 0.16s ease,
      opacity 0.16s ease;
  }
  .editable-file-tags__tag:hover .editable-file-tags__remove,
  .editable-file-tags__tag:focus-within .editable-file-tags__remove {
    width: 18px;
    margin-left: 3px;
    opacity: 1;
  }
  .editable-file-tags__remove:disabled,
  .editable-file-tags__add:disabled {
    cursor: wait;
  }
  .editable-file-tags__add {
    width: 24px;
    height: 24px;
    flex: 0 0 auto;
    border: 1px dashed var(--el-border-color);
    border-radius: 6px;
    color: var(--el-text-color-secondary);
    transition:
      border-color 0.2s ease,
      color 0.2s ease,
      background 0.2s ease;
  }
  .editable-file-tags__add:hover,
  .editable-file-tags__add:focus-visible {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    outline: none;
  }
  .editable-file-tags__panel {
    display: grid;
    gap: 14px;
  }
  .editable-file-tags__panel :deep(.el-select) {
    width: 100%;
  }
  .editable-file-tags__fields {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: end;
    gap: 12px;
  }
  .editable-file-tags__fields label,
  .editable-file-tags__fields fieldset {
    min-width: 0;
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 0;
    border: 0;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .editable-file-tags__fields legend {
    margin-bottom: 6px;
  }
  .editable-file-tags__actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  @media (hover: none), (pointer: coarse) {
    .editable-file-tags__remove {
      width: 18px;
      margin-left: 3px;
      opacity: 1;
    }
  }
  @media (max-width: 767px) {
    .editable-file-tags {
      justify-content: flex-start;
    }
    .editable-file-tags__fields {
      grid-template-columns: 1fr;
    }
  }
</style>
