<template>
  <div v-if="type === 'pdf'" class="preview-pdf-container">
    <el-alert
      :title="t('preview.pdf.title')"
      :description="t('preview.pdf.description')"
      type="info"
      :closable="false"
      class="mb-4"
    />
    <iframe :src="url" class="preview-pdf" @load="$emit('load')" @error="$emit('error')"></iframe>
    <div class="preview-toolbar">
      <el-button v-if="canPrint" icon="Printer" @click="$emit('print')">{{ t('preview.pdf.print') }}</el-button>
      <el-button icon="Download" @click="$emit('download')">{{ t('preview.pdf.download') }}</el-button>
    </div>
  </div>
  <div v-else class="preview-text-container">
    <div class="preview-text-header">
      <span class="text-type-label">{{ type === 'code' ? t('preview.code.title') : t('preview.text.title') }}</span>
      <el-button-group>
        <el-button v-if="canEdit" icon="EditPen" size="small" type="primary" plain @click="$emit('edit')">{{
          t('preview.text.edit')
        }}</el-button>
        <el-button v-if="canPrint" icon="Printer" size="small" @click="$emit('print')">{{
          t('preview.text.print')
        }}</el-button>
        <el-button icon="Download" size="small" @click="$emit('download')">{{ t('preview.text.download') }}</el-button>
      </el-button-group>
    </div>
    <pre
      :class="['preview-text-content', type === 'code' ? `language-${language}` : '']"
    ><code>{{ content }}</code></pre>
  </div>
</template>

<script setup lang="ts">
  import { useI18n } from '@/composables/core/useI18n'

  defineProps<{
    type: 'pdf' | 'text' | 'code'
    url?: string
    content?: string
    language?: string
    canPrint: boolean
    canEdit?: boolean
  }>()

  defineEmits<{ load: []; error: []; print: []; download: []; edit: [] }>()
  const { t } = useI18n()
</script>

<style scoped>
  .preview-pdf-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .preview-pdf {
    width: 100%;
    height: 70vh;
    border: 1px solid var(--border-color);
    border-radius: 8px;
  }
  .preview-text-container {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .preview-text-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .text-type-label {
    color: var(--text-secondary);
    font-size: 14px;
    font-weight: 500;
  }
  .preview-text-content {
    max-height: 60vh;
    margin: 0;
    padding: 16px;
    overflow: auto;
    border: 1px solid var(--border-color);
    border-radius: 8px;
    background: var(--bg-color);
    font-family: Monaco, Menlo, 'Ubuntu Mono', Consolas, monospace;
    font-size: 14px;
    line-height: 1.6;
  }
  .preview-text-content code {
    padding: 0;
    background: transparent;
    color: var(--text-primary);
  }
  .preview-toolbar {
    min-height: 54px;
    margin-top: auto;
    padding: 16px 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 16px;
    border-top: 1px solid var(--border-color);
    box-sizing: border-box;
  }
  @media (max-width: 1024px) {
    .preview-pdf {
      height: 60vh;
    }
    .preview-text-content {
      max-height: 50vh;
      padding: 12px;
      font-size: 12px;
    }
  }
  @media (max-width: 480px) {
    .preview-pdf {
      height: 50vh;
    }
    .preview-text-content {
      max-height: 45vh;
      font-size: 11px;
    }
  }
</style>
