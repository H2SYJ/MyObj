<template>
  <div v-if="tags.length" class="file-tags" :class="{ 'file-tags--compact': compact }" :aria-label="t('tags.fileTags')">
    <el-tag
      v-for="tag in visibleTags"
      :key="tag.id"
      size="small"
      effect="plain"
      :style="{ '--tag-color': tag.color || 'var(--el-color-primary)' }"
      @click.stop="$emit('tag-click', tag)"
    >
      {{ tag.name }}
    </el-tag>
    <el-tag v-if="hiddenCount" size="small" effect="plain" class="file-tags__more">+{{ hiddenCount }}</el-tag>
  </div>
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import type { CompactTag } from '@/types'
  import { useI18n } from '@/composables'

  const props = withDefaults(
    defineProps<{
      tags?: CompactTag[]
      limit?: number
      compact?: boolean
    }>(),
    { tags: () => [], limit: 3, compact: false }
  )
  defineEmits<{ 'tag-click': [tag: CompactTag] }>()
  const { t } = useI18n()
  const visibleTags = computed(() => props.tags.slice(0, props.limit))
  const hiddenCount = computed(() => Math.max(0, props.tags.length - props.limit))
</script>

<style scoped>
  .file-tags {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 5px;
    overflow: hidden;
  }
  .file-tags :deep(.el-tag) {
    max-width: 120px;
    border-color: color-mix(in srgb, var(--tag-color) 52%, var(--el-border-color));
    color: var(--tag-color);
    background: color-mix(in srgb, var(--tag-color) 8%, transparent);
  }
  .file-tags :deep(.el-tag__content) {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .file-tags__more {
    flex: 0 0 auto;
    color: var(--el-text-color-secondary) !important;
  }
  .file-tags--compact :deep(.el-tag) {
    height: 20px;
    padding-inline: 6px;
    font-size: 10px;
  }
  @media (max-width: 767px) {
    .file-tags :deep(.el-tag:nth-child(n + 2):not(.file-tags__more)) {
      display: none;
    }
  }
</style>
