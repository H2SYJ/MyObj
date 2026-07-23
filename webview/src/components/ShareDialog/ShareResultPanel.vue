<template>
  <div class="share-result">
    <el-alert type="success" :closable="false" show-icon class="result-alert">
      <template #title
        ><div class="result-title">{{ t('share.shareSuccess') }}</div></template
      >
    </el-alert>
    <div class="share-link-section">
      <div class="link-label">{{ t('share.shareLink') }}</div>
      <div class="link-content">
        <el-input :model-value="result.shareUrl" readonly class="link-input">
          <template #append>
            <el-button
              :icon="result.copied ? 'Check' : 'CopyDocument'"
              :type="result.copied ? 'success' : 'primary'"
              @click="$emit('copy-link')"
            >
              {{ result.copied ? t('common.copied') : t('share.copyLink') }}
            </el-button>
          </template>
        </el-input>
      </div>

      <div v-if="password" class="password-section">
        <div class="link-label">{{ t('share.sharePassword') }}</div>
        <div class="link-content">
          <el-input :model-value="password" readonly class="link-input">
            <template #append>
              <el-button
                :icon="result.passwordCopied ? 'Check' : 'CopyDocument'"
                :type="result.passwordCopied ? 'success' : 'primary'"
                @click="$emit('copy-password')"
              >
                {{ result.passwordCopied ? t('common.copied') : t('share.copyPassword') }}
              </el-button>
            </template>
          </el-input>
        </div>
      </div>

      <div class="expire-info">
        <el-icon><Clock /></el-icon>
        <span>{{ t('share.expireTime') }}：{{ result.expireText }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { Clock } from '@element-plus/icons-vue'
  import { useI18n } from '@/composables'
  import type { ShareDialogResult } from './types'

  defineProps<{ result: ShareDialogResult; password: string }>()
  defineEmits<{ 'copy-link': []; 'copy-password': [] }>()
  const { t } = useI18n()
</script>

<style scoped>
  .share-result {
    margin-top: 4px;
  }
  .result-alert {
    margin-bottom: 20px;
  }
  .result-title {
    font-size: 15px;
    font-weight: 600;
  }
  .share-link-section {
    padding: 20px;
    border: 1px solid var(--border-color);
    border-radius: 8px;
    background: var(--bg-color);
  }
  .link-label {
    margin-bottom: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 600;
  }
  .link-content {
    margin-bottom: 16px;
  }
  .link-input {
    width: 100%;
  }
  .password-section,
  .expire-info {
    margin-top: 16px;
    padding-top: 16px;
    border-top: 1px solid var(--border-color);
  }
  .expire-info {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-secondary);
    font-size: 13px;
  }
  @media (max-width: 1024px) {
    .share-link-section {
      padding: 16px;
    }
    .link-label,
    .expire-info {
      font-size: 12px;
    }
  }
  @media (max-width: 480px) {
    .share-link-section {
      padding: 16px 12px;
    }
    .link-input :deep(.el-input__append .el-button span) {
      display: none;
    }
  }
</style>
