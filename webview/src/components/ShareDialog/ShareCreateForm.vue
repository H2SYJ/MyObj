<template>
  <div class="share-create-state">
    <div class="file-info-card">
      <el-icon :size="48" class="share-file-icon"><Document /></el-icon>
      <div class="file-info-content">
        <div class="file-name">{{ fileInfo.file_name || t('common.noData') }}</div>
        <div v-if="fileInfo.file_size" class="file-size">{{ formatSize(fileInfo.file_size) }}</div>
      </div>
    </div>

    <el-form label-width="100px" class="share-form">
      <el-form-item :label="t('share.expireTime')">
        <el-select
          :model-value="expireDays"
          class="expire-select mobile-only"
          @update:model-value="$emit('update:expireDays', Number($event))"
        >
          <el-option v-for="option in expireOptions" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
        <el-radio-group
          :model-value="expireDays"
          class="expire-options desktop-only"
          @update:model-value="$emit('update:expireDays', Number($event))"
        >
          <el-radio-button v-for="option in expireOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('share.sharePassword')">
        <el-input
          :model-value="password"
          :placeholder="t('share.sharePassword')"
          maxlength="20"
          show-word-limit
          clearable
          @update:model-value="$emit('update:password', String($event))"
        >
          <template #append>
            <el-button icon="Refresh" @click="$emit('generate')">{{ t('common.generate') }}</el-button>
          </template>
        </el-input>
        <div class="form-tip">{{ t('share.passwordTip') }}</div>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup lang="ts">
  import { Document } from '@element-plus/icons-vue'
  import { formatSize } from '@/utils'
  import { useI18n } from '@/composables'
  import type { ShareDialogFileInfo, ShareExpireOption } from './types'

  defineProps<{
    fileInfo: ShareDialogFileInfo
    expireOptions: ShareExpireOption[]
    expireDays: number
    password: string
  }>()

  defineEmits<{
    'update:expireDays': [value: number]
    'update:password': [value: string]
    generate: []
  }>()

  const { t } = useI18n()
</script>

<style scoped>
  .file-info-card {
    margin-bottom: 24px;
    padding: 20px;
    display: flex;
    align-items: center;
    gap: 16px;
    overflow: hidden;
    border: 1px solid color-mix(in srgb, var(--el-color-primary) 20%, transparent);
    border-radius: 12px;
    background: color-mix(in srgb, var(--el-color-primary) 6%, var(--el-bg-color));
  }
  .share-file-icon {
    color: var(--el-color-primary);
    flex-shrink: 0;
  }
  .file-info-content {
    flex: 1;
    min-width: 0;
  }
  .file-name {
    margin-bottom: 4px;
    overflow: hidden;
    color: var(--text-primary);
    font-size: 16px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-size,
  .form-tip {
    color: var(--text-secondary);
    font-size: 12px;
  }
  .share-form {
    margin-bottom: 24px;
  }
  .form-tip {
    margin-top: 4px;
    line-height: 1.5;
  }
  .expire-select.mobile-only {
    display: none;
    width: 100%;
  }
  .expire-options.desktop-only {
    width: 100%;
    display: flex;
    gap: 8px;
  }
  .expire-options :deep(.el-radio-button) {
    flex: 1;
    min-width: 0;
  }
  .expire-options :deep(.el-radio-button__inner) {
    width: 100%;
  }

  @media (max-width: 1024px) {
    .file-info-card {
      padding: 16px;
      gap: 12px;
    }
    .file-name {
      font-size: 14px;
    }
    .share-form :deep(.el-form-item) {
      margin-bottom: 24px;
      display: flex;
      flex-direction: column;
      align-items: flex-start;
    }
    .share-form :deep(.el-form-item__label) {
      width: 100% !important;
      margin-bottom: 10px;
      padding: 0;
      justify-content: flex-start;
      font-weight: 600;
    }
    .share-form :deep(.el-form-item__content) {
      width: 100%;
      margin-left: 0 !important;
    }
    .expire-select.mobile-only {
      display: block;
    }
    .expire-options.desktop-only {
      display: none;
    }
  }

  @media (max-width: 480px) {
    .file-info-card {
      margin-bottom: 20px;
      padding: 16px 12px;
      flex-direction: column;
      text-align: center;
    }
    .file-info-content {
      width: 100%;
      text-align: center;
    }
    .file-name {
      overflow: visible;
      white-space: normal;
      word-break: break-all;
    }
    .share-form :deep(.el-input__append .el-button span) {
      display: none;
    }
  }
</style>
