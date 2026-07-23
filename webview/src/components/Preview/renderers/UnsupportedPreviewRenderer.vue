<template>
  <div class="preview-unsupported">
    <el-icon :size="64" class="unsupported-icon"><Document /></el-icon>
    <p class="unsupported-title">{{ t('preview.notSupported.title') }}</p>
    <p class="unsupported-desc">
      {{ t('preview.notSupported.mimeType') }}: {{ mimeType || t('preview.notSupported.unknown') }}
    </p>
    <div class="unsupported-actions">
      <el-button v-if="canPrint" type="primary" icon="Printer" @click="$emit('print')">{{
        t('preview.notSupported.print')
      }}</el-button>
      <el-button icon="Download" @click="$emit('download')">{{ t('preview.notSupported.download') }}</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { Document } from '@element-plus/icons-vue'
  import { useI18n } from '@/composables/core/useI18n'

  defineProps<{ mimeType?: string; canPrint: boolean }>()
  defineEmits<{ print: []; download: [] }>()
  const { t } = useI18n()
</script>

<style scoped>
  .preview-unsupported {
    min-height: 400px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
  }
  .unsupported-icon {
    color: var(--el-text-color-placeholder);
  }
  .unsupported-title {
    margin: 0;
    color: var(--text-primary);
    font-size: 18px;
    font-weight: 600;
  }
  .unsupported-desc {
    margin: 0;
    color: var(--text-secondary);
    font-size: 14px;
  }
  .unsupported-actions {
    margin-top: 16px;
    display: flex;
    justify-content: center;
    gap: 12px;
  }
  @media (max-width: 1024px) {
    .preview-unsupported {
      min-height: 300px;
      gap: 12px;
    }
  }
</style>
