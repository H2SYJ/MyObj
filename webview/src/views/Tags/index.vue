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
            @click="search(tag)"
            @keydown.enter.prevent="search(tag)"
            @keydown.space.prevent="search(tag)"
          >
            <span class="tag-cloud-item__name">{{ tag.name }}</span>
            <span class="tag-cloud-item__count">{{ tag.file_count }}</span>
            <el-icon v-if="tag.system" class="tag-cloud-item__lock"><Lock /></el-icon>
          </div>
        </el-tooltip>
      </div>
      <el-empty v-else-if="!loading" :description="t('tagCloud.empty')" />
    </section>
  </WorkspacePage>
</template>

<script setup lang="ts">
  import { computed, getCurrentInstance, onMounted, ref, type ComponentInternalInstance } from 'vue'
  import { useRouter } from 'vue-router'
  import { getTagCloud, type TagCloudItem } from '@/api/tag'
  import WorkspacePage from '@/components/WorkspacePage/index.vue'
  import { useI18n, useResponsive } from '@/composables'
  import { getContrastText } from '@/utils/ui'
  import { sortTagCloudItems, tagCloudFontSize, tagCloudSizeClass } from './tagCloud'

  const router = useRouter()
  const { t } = useI18n()
  const { isHandheld } = useResponsive()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const loading = ref(false)
  const tags = ref<TagCloudItem[]>([])

  const counts = computed(() => tags.value.map(tag => tag.file_count))
  const minCount = computed(() => (counts.value.length ? Math.min(...counts.value) : 0))
  const maxCount = computed(() => (counts.value.length ? Math.max(...counts.value) : 0))

  const load = async () => {
    loading.value = true
    try {
      const response = await getTagCloud()
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      tags.value = sortTagCloudItems(response.data?.tags || [])
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

  onMounted(() => void load())
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
