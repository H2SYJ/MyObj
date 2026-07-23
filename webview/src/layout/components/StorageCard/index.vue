<template>
  <section class="storage-summary" :aria-label="t('storage.title')">
    <div class="storage-summary__header">
      <span
        ><el-icon><PieChart /></el-icon>{{ t('storage.title') }}</span
      >
      <strong>{{ storageInfo.isUnlimited ? '∞' : `${storageInfo.percentage}%` }}</strong>
    </div>
    <el-progress
      :percentage="storageInfo.isUnlimited ? 100 : storageInfo.percentage"
      :show-text="false"
      :stroke-width="6"
      :color="progressColor"
    />
    <p>{{ storageDescription }}</p>
  </section>
</template>

<script setup lang="ts">
  import { useI18n } from '@/composables'
  import { useUserStore } from '@/stores'

  const userStore = useUserStore()
  const { t } = useI18n()
  const storageInfo = computed(() => userStore.storageInfo)

  const formatStorageSize = (bytes: number) => {
    if (!bytes) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
    const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
    return `${(bytes / 1024 ** index).toFixed(index > 1 ? 1 : 0)} ${units[index]}`
  }

  const storageDescription = computed(() => {
    const used = formatStorageSize(storageInfo.value.used)
    return storageInfo.value.isUnlimited
      ? `${used} / ${t('storage.unlimited')}`
      : `${used} / ${formatStorageSize(storageInfo.value.total)}`
  })
  const progressColor = computed(() => {
    if (storageInfo.value.isUnlimited || storageInfo.value.percentage < 70) return 'var(--primary-color)'
    if (storageInfo.value.percentage < 90) return 'var(--warning-color)'
    return 'var(--danger-color)'
  })
</script>

<style scoped>
  .storage-summary {
    padding: 13px;
    border: 1px solid var(--desktop-border);
    border-radius: 12px;
    background: var(--desktop-surface-muted);
  }
  .storage-summary__header {
    margin-bottom: 10px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .storage-summary__header span {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-regular);
    font-size: 12px;
    font-weight: 650;
  }
  .storage-summary__header strong {
    color: var(--primary-color);
    font-size: 12px;
  }
  .storage-summary p {
    margin: 8px 0 0;
    overflow: hidden;
    color: var(--text-secondary);
    font-size: 10px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
