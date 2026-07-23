<template>
  <div class="breadcrumb-container glass-panel-sm">
    <div class="breadcrumb-content">
      <!-- 左侧：导航操作 -->
      <div class="breadcrumb-left">
        <!-- 返回上一级按钮 -->
        <el-button
          v-if="canGoBack"
          icon="ArrowLeft"
          circle
          size="small"
          class="nav-button"
          @click="handleGoBack"
          :title="t('files.goBack')"
        />

        <!-- 面包屑导航 -->
        <el-breadcrumb separator="/" class="breadcrumb-nav">
          <el-breadcrumb-item
            v-for="(item, index) in breadcrumbs"
            :key="item.id"
            :class="{ 'is-current': index === breadcrumbs.length - 1 }"
          >
            <span class="breadcrumb-link" @click="handleNavigate(item.id, index)">
              <el-icon v-if="index === 0" class="breadcrumb-icon"><House /></el-icon>
              <el-icon v-else class="breadcrumb-icon"><Folder /></el-icon>
              <span class="breadcrumb-text">{{ formatBreadcrumbName(item.name) }}</span>
            </span>
          </el-breadcrumb-item>
        </el-breadcrumb>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import type { Breadcrumb } from '@/types'
  import { useI18n } from '@/composables'

  const { t } = useI18n()

  interface Props {
    breadcrumbs: Breadcrumb[]
    formatBreadcrumbName: (name: string) => string
    currentPath?: number
  }

  const props = withDefaults(defineProps<Props>(), {
    currentPath: 0
  })

  const emit = defineEmits<{
    navigate: [directoryId: number]
    'go-back': []
  }>()

  // 计算属性
  const canGoBack = computed(() => {
    return props.breadcrumbs.length > 1
  })

  // 方法
  const handleNavigate = (directoryId: number, index: number) => {
    // 如果是当前项，不执行导航
    if (index === props.breadcrumbs.length - 1) return
    emit('navigate', directoryId)
  }

  const handleGoBack = () => {
    if (props.breadcrumbs.length > 1) {
      const previousDirectoryId = props.breadcrumbs[props.breadcrumbs.length - 2].id
      emit('navigate', previousDirectoryId)
    }
    emit('go-back')
  }
</script>

<style scoped>
  .breadcrumb-container {
    --breadcrumb-row-height: 24px;
    margin-bottom: 12px;
    padding: 8px 16px;
    border-radius: 8px;
    transition: all 0.3s;
  }

  .breadcrumb-content {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: var(--breadcrumb-row-height);
  }

  .breadcrumb-left {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }

  .breadcrumb-container .breadcrumb-left .nav-button {
    width: var(--breadcrumb-row-height);
    height: var(--breadcrumb-row-height);
    min-height: var(--breadcrumb-row-height);
    flex-shrink: 0;
    color: var(--text-secondary);
    transition: all 0.2s;
    padding: 4px;
    background-color: transparent;
    border-color: transparent;
  }

  .nav-button:hover {
    color: var(--primary-color);
    background: var(--el-color-primary-light-9);
  }

  html.dark .nav-button {
    color: var(--el-text-color-secondary);
    background-color: transparent;
    border-color: var(--el-border-color);
  }

  html.dark .nav-button:hover {
    color: var(--primary-color);
    background: rgba(59, 130, 246, 0.15);
    border-color: var(--primary-color);
  }

  html.dark .nav-button.is-loading {
    color: var(--primary-color);
  }

  .breadcrumb-nav {
    flex: 1;
    min-width: 0;
  }

  .breadcrumb-nav :deep(.el-breadcrumb__item) {
    display: flex;
    align-items: center;
  }

  .breadcrumb-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 6px;
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.2s;
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 500;
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .breadcrumb-link:hover {
    color: var(--primary-color);
    background: var(--el-color-primary-light-9);
  }

  html.dark .breadcrumb-link:hover {
    background: rgba(64, 158, 255, 0.15);
  }

  .breadcrumb-nav :deep(.el-breadcrumb__item.is-current .breadcrumb-link) {
    color: var(--primary-color);
    font-weight: 600;
    cursor: default;
  }

  .breadcrumb-nav :deep(.el-breadcrumb__item.is-current .breadcrumb-link:hover) {
    background: transparent;
  }

  .breadcrumb-icon {
    font-size: 14px;
    flex-shrink: 0;
  }

  .breadcrumb-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* 移动端响应式 */
  @media (max-width: 1024px) {
    .breadcrumb-container {
      padding: 6px 12px;
    }

    .breadcrumb-left {
      gap: 6px;
    }

    .breadcrumb-link {
      max-width: 150px;
      font-size: 12px;
      padding: 2px 4px;
    }

    .breadcrumb-icon {
      font-size: 12px;
    }
  }

  @media (max-width: 768px) {
    .breadcrumb-container {
      padding: 6px 10px;
      margin-bottom: 8px;
    }

    .breadcrumb-link {
      max-width: 120px;
      font-size: 12px;
      padding: 2px 4px;
    }

    .nav-button {
      padding: 2px;
    }
  }
</style>
