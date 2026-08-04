<template>
  <el-dialog
    :model-value="modelValue"
    :title="dialogTitle"
    width="min(760px, 94vw)"
    destroy-on-close
    append-to-body
    @update:model-value="value => $emit('update:modelValue', value)"
  >
    <div v-loading="loading" class="tag-manager">
      <template v-if="!batchMode">
        <el-alert :type="stateAlertType" :closable="false" show-icon class="tag-manager__state">
          <template #title>{{ t(`tags.state.${details?.state || 'pending'}`) }}</template>
          <template v-if="details?.last_error" #default>{{ details.last_error }}</template>
          <el-button
            v-if="details?.state === 'failed' || details?.state === 'partial'"
            size="small"
            text
            type="primary"
            @click="handleRetry"
          >
            {{ t('common.retry') }}
          </el-button>
        </el-alert>

        <section class="tag-manager__section">
          <div class="tag-manager__section-title">
            <strong>{{ t('tags.automatic') }}</strong>
            <small>{{ t('tags.automaticHint') }}</small>
          </div>
          <div v-if="automaticTags.length" class="tag-manager__tags">
            <el-tag
              v-for="tag in automaticTags"
              :key="tag.id"
              closable
              effect="plain"
              :style="tagStyle(tag)"
              @close="suppressTag(tag.id)"
            >
              {{ tag.name }} · {{ tag.category.name }}
            </el-tag>
          </div>
          <el-empty v-else :description="t('tags.noAutomatic')" :image-size="52" />
        </section>

        <section v-if="suppressedTags.length" class="tag-manager__section">
          <div class="tag-manager__section-title">
            <strong>{{ t('tags.suppressed') }}</strong>
            <small>{{ t('tags.suppressedHint') }}</small>
          </div>
          <div class="tag-manager__tags">
            <el-tag
              v-for="tag in suppressedTags"
              :key="tag.id"
              type="info"
              closable
              :disable-transitions="true"
              @close="restoreTag(tag.id)"
            >
              {{ tag.name }} · {{ t('tags.restore') }}
            </el-tag>
          </div>
        </section>

        <section class="tag-manager__section">
          <div class="tag-manager__section-title">
            <strong>{{ t('tags.manual') }}</strong>
            <small>{{ t('tags.manualHint') }}</small>
          </div>
          <div v-if="manualTags.length" class="tag-manager__manual-list">
            <div v-for="tag in manualTags" :key="tag.id" class="tag-manager__manual-item">
              <el-tag effect="plain" :style="tagStyle(tag)">{{ tag.name }} · {{ tag.category.name }}</el-tag>
              <el-switch
                :model-value="tag.visibility === 'public'"
                :active-text="t('tags.public')"
                :inactive-text="t('tags.private')"
                inline-prompt
                @change="value => changeVisibility(tag, Boolean(value))"
              />
              <el-button
                text
                type="danger"
                icon="Delete"
                :aria-label="t('common.delete')"
                @click="removeManual(tag.id)"
              />
            </div>
          </div>
          <el-empty v-else :description="t('tags.noManual')" :image-size="52" />
        </section>
      </template>

      <el-form label-position="top" class="tag-manager__form">
        <el-form-item :label="batchMode ? t('tags.batchOperation') : t('tags.addManual')">
          <el-radio-group v-if="batchMode" v-model="batchAction">
            <el-radio-button value="add">{{ t('tags.batchAdd') }}</el-radio-button>
            <el-radio-button value="remove">{{ t('tags.batchRemove') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <template v-if="!batchMode || batchAction === 'add'">
          <el-form-item :label="t('tags.names')">
            <el-select
              v-model="newTagNames"
              multiple
              filterable
              allow-create
              default-first-option
              :placeholder="t('tags.namesPlaceholder')"
              :multiple-limit="20"
            />
          </el-form-item>
          <div class="tag-manager__form-row">
            <el-form-item :label="t('tags.category')">
              <el-select v-model="newCategoryId" :placeholder="t('tags.categoryPlaceholder')">
                <el-option
                  v-for="category in categories"
                  :key="category.id"
                  :value="category.id"
                  :label="category.name"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('tags.visibility')">
              <el-radio-group v-model="newVisibility">
                <el-radio value="private">{{ t('tags.private') }}</el-radio>
                <el-radio value="public">{{ t('tags.public') }}</el-radio>
              </el-radio-group>
            </el-form-item>
          </div>
        </template>

        <el-form-item v-else :label="t('tags.removeTags')">
          <el-select
            v-model="removeTagIds"
            multiple
            filterable
            remote
            :remote-method="loadSuggestions"
            :loading="suggestionsLoading"
            :placeholder="t('tags.removePlaceholder')"
          >
            <el-option v-for="tag in suggestions" :key="tag.id" :value="tag.id" :label="tag.name" />
          </el-select>
        </el-form-item>
      </el-form>
    </div>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">{{ t('common.close') }}</el-button>
      <el-button type="primary" :loading="saving" :disabled="submitDisabled" @click="submit">
        {{ batchMode && batchAction === 'remove' ? t('tags.batchRemove') : t('tags.addManual') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
  import { computed, getCurrentInstance, ref, watch, type ComponentInternalInstance } from 'vue'
  import {
    batchUpdateTags,
    getEnabledTagCategories,
    getFileTags,
    getTagSuggestions,
    retryFileTags,
    updateManualTags,
    updateTagExclusions,
    type FileTag,
    type FileTagsData,
    type TagCategory
  } from '@/api/tag'
  import type { CompactTag } from '@/types'
  import { useI18n } from '@/composables'

  const props = defineProps<{ modelValue: boolean; fileIds: string[]; fileName?: string }>()
  const emit = defineEmits<{ 'update:modelValue': [value: boolean]; saved: [] }>()
  const { t } = useI18n()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const loading = ref(false)
  const saving = ref(false)
  const suggestionsLoading = ref(false)
  const details = ref<FileTagsData>()
  const categories = ref<TagCategory[]>([])
  const suggestions = ref<CompactTag[]>([])
  const newTagNames = ref<string[]>([])
  const newCategoryId = ref('other')
  const newVisibility = ref<'private' | 'public'>('private')
  const batchAction = ref<'add' | 'remove'>('add')
  const removeTagIds = ref<string[]>([])

  const batchMode = computed(() => props.fileIds.length > 1)
  const dialogTitle = computed(() =>
    batchMode.value
      ? t('tags.batchTitle', { count: props.fileIds.length })
      : t('tags.manageTitle', { name: props.fileName || '' })
  )
  const automaticTags = computed(() => details.value?.tags.filter(tag => tag.automatic) || [])
  const manualTags = computed(() => details.value?.tags.filter(tag => tag.sources.includes('manual')) || [])
  const suppressedTags = computed(() => details.value?.suppressed || [])
  const stateAlertType = computed(() => {
    if (details.value?.state === 'failed') {
      return 'error'
    }
    if (details.value?.state === 'partial') {
      return 'warning'
    }
    if (details.value?.state === 'ready') {
      return 'success'
    }
    return 'info'
  })
  const submitDisabled = computed(() =>
    batchMode.value && batchAction.value === 'remove' ? removeTagIds.value.length === 0 : newTagNames.value.length === 0
  )

  const tagStyle = (tag: FileTag) => ({
    borderColor: tag.category.color,
    color: tag.category.color
  })

  const load = async () => {
    loading.value = true
    try {
      const categoryResponse = await getEnabledTagCategories()
      if (categoryResponse.code === 200) {
        categories.value = categoryResponse.data || []
        if (!categories.value.some(category => category.id === newCategoryId.value)) {
          newCategoryId.value =
            categories.value.find(category => category.code === 'other')?.id || categories.value[0]?.id || ''
        }
      }
      if (!batchMode.value && props.fileIds[0]) {
        const response = await getFileTags(props.fileIds[0])
        if (response.code === 200) {
          details.value = response.data
        }
      } else {
        await loadSuggestions('')
      }
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.loadFailed'))
    } finally {
      loading.value = false
    }
  }

  const reloadDetails = async () => {
    if (!props.fileIds[0] || batchMode.value) {
      return
    }
    const response = await getFileTags(props.fileIds[0])
    if (response.code === 200) {
      details.value = response.data
    }
  }

  const loadSuggestions = async (keyword: string) => {
    suggestionsLoading.value = true
    try {
      const response = await getTagSuggestions({ keyword, limit: 50 })
      if (response.code === 200) {
        suggestions.value = response.data || []
      }
    } finally {
      suggestionsLoading.value = false
    }
  }

  const runMutation = async (mutation: () => Promise<void>) => {
    try {
      await mutation()
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.saveFailed'))
    }
  }

  const suppressTag = (tagId: string) =>
    runMutation(async () => {
      await updateTagExclusions(props.fileIds[0], [tagId], [])
      await reloadDetails()
      emit('saved')
    })
  const restoreTag = (tagId: string) =>
    runMutation(async () => {
      await updateTagExclusions(props.fileIds[0], [], [tagId])
      await reloadDetails()
      emit('saved')
    })
  const removeManual = (tagId: string) =>
    runMutation(async () => {
      await updateManualTags(props.fileIds[0], [], [tagId])
      await reloadDetails()
      emit('saved')
    })
  const changeVisibility = (tag: FileTag, isPublic: boolean) =>
    runMutation(async () => {
      await updateManualTags(
        props.fileIds[0],
        [{ name: tag.name, category_id: tag.category.id, visibility: isPublic ? 'public' : 'private' }],
        []
      )
      await reloadDetails()
      emit('saved')
    })
  const handleRetry = () =>
    runMutation(async () => {
      await retryFileTags(props.fileIds[0])
      await reloadDetails()
      proxy?.$modal.msgSuccess(t('tags.retryQueued'))
    })

  const submit = async () => {
    saving.value = true
    try {
      if (batchMode.value && batchAction.value === 'remove') {
        await batchUpdateTags(props.fileIds, [], removeTagIds.value)
        removeTagIds.value = []
      } else {
        const add = newTagNames.value.map(name => ({
          name: name.trim(),
          category_id: newCategoryId.value,
          visibility: newVisibility.value
        }))
        if (batchMode.value) {
          await batchUpdateTags(props.fileIds, add)
        } else {
          await updateManualTags(props.fileIds[0], add, [])
        }
        newTagNames.value = []
        await reloadDetails()
      }
      proxy?.$modal.msgSuccess(t('tags.saveSuccess'))
      emit('saved')
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('tags.saveFailed'))
    } finally {
      saving.value = false
    }
  }

  watch(
    () => props.modelValue,
    visible => {
      if (visible) {
        details.value = undefined
        newTagNames.value = []
        removeTagIds.value = []
        load()
      }
    },
    { immediate: true }
  )
</script>

<style scoped>
  .tag-manager {
    min-height: 180px;
    display: grid;
    gap: 18px;
  }
  .tag-manager__state :deep(.el-alert__content) {
    min-width: 0;
  }
  .tag-manager__section {
    padding-bottom: 16px;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .tag-manager__section-title {
    margin-bottom: 10px;
    display: flex;
    align-items: baseline;
    gap: 10px;
  }
  .tag-manager__section-title small {
    color: var(--el-text-color-secondary);
  }
  .tag-manager__tags {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .tag-manager__manual-list {
    display: grid;
    gap: 8px;
  }
  .tag-manager__manual-item {
    min-width: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 10px;
  }
  .tag-manager__manual-item .el-tag {
    justify-self: start;
    max-width: 100%;
  }
  .tag-manager__form {
    padding: 14px;
    border-radius: 12px;
    background: var(--el-fill-color-lighter);
  }
  .tag-manager__form-row {
    display: grid;
    grid-template-columns: minmax(180px, 1fr) minmax(220px, 1fr);
    gap: 16px;
  }
  .tag-manager__form :deep(.el-select) {
    width: 100%;
  }
  @media (max-width: 767px) {
    .tag-manager__form-row {
      grid-template-columns: 1fr;
      gap: 0;
    }
    .tag-manager__manual-item {
      grid-template-columns: minmax(0, 1fr) auto;
    }
    .tag-manager__manual-item .el-switch {
      grid-column: 1 / -1;
      grid-row: 2;
      justify-self: start;
    }
  }
</style>
