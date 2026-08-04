<template>
  <div class="tag-filter" :aria-label="t('tags.filterTitle')">
    <el-select
      :model-value="modelValue"
      class="tag-filter__select"
      multiple
      filterable
      remote
      clearable
      collapse-tags
      collapse-tags-tooltip
      :max-collapse-tags="2"
      :remote-method="loadSuggestions"
      :loading="loading"
      :placeholder="t('tags.filterPlaceholder')"
      @update:model-value="emitTagIds"
      @visible-change="visible => visible && loadSuggestions('')"
    >
      <el-option v-for="tag in options" :key="tag.id" :value="tag.id" :label="tag.name">
        <span
          class="tag-filter__option-dot"
          :style="{ backgroundColor: tag.color || 'var(--el-color-primary)' }"
        ></span>
        <span>{{ tag.name }}</span>
      </el-option>
    </el-select>
    <el-segmented
      :model-value="mode"
      :options="modeOptions"
      size="small"
      @update:model-value="value => $emit('update:mode', value as 'all' | 'any')"
    />
    <el-segmented
      v-if="showScope"
      :model-value="scope"
      :options="scopeOptions"
      size="small"
      @update:model-value="value => $emit('update:scope', value as 'current' | 'all')"
    />
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref } from 'vue'
  import { getTagSuggestions } from '@/api/tag'
  import type { CompactTag } from '@/types'
  import { useI18n } from '@/composables'

  withDefaults(
    defineProps<{
      modelValue: string[]
      mode: 'all' | 'any'
      scope?: 'current' | 'all'
      showScope?: boolean
    }>(),
    { scope: 'current', showScope: false }
  )
  const emit = defineEmits<{
    'update:modelValue': [value: string[]]
    'update:mode': [value: 'all' | 'any']
    'update:scope': [value: 'current' | 'all']
  }>()
  const { t } = useI18n()
  const loading = ref(false)
  const options = ref<CompactTag[]>([])
  let requestSerial = 0

  const modeOptions = computed(() => [
    { label: t('tags.matchAll'), value: 'all' },
    { label: t('tags.matchAny'), value: 'any' }
  ])
  const scopeOptions = computed(() => [
    { label: t('tags.currentDirectory'), value: 'current' },
    { label: t('tags.allDirectories'), value: 'all' }
  ])

  const loadSuggestions = async (keyword: string) => {
    const serial = ++requestSerial
    loading.value = true
    try {
      const response = await getTagSuggestions(keyword, 50)
      if (serial === requestSerial && response.code === 200) {
        options.value = response.data || []
      }
    } catch {
      if (serial === requestSerial) {
        options.value = []
      }
    } finally {
      if (serial === requestSerial) {
        loading.value = false
      }
    }
  }

  const emitTagIds = (value: string[]) => emit('update:modelValue', value)

  onMounted(() => loadSuggestions(''))
</script>

<style scoped>
  .tag-filter {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .tag-filter__select {
    width: min(360px, 42vw);
  }
  .tag-filter__option-dot {
    width: 8px;
    height: 8px;
    margin-right: 7px;
    display: inline-block;
    border-radius: 50%;
  }
  @media (max-width: 767px) {
    .tag-filter {
      width: 100%;
      align-items: stretch;
      flex-direction: column;
    }
    .tag-filter__select {
      width: 100%;
    }
  }
</style>
