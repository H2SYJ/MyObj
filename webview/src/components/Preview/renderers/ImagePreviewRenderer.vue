<template>
  <div class="preview-image-container">
    <div class="image-wrapper" @wheel="$emit('wheel', $event)" @dblclick="$emit('reset')">
      <img
        :src="url"
        :style="imageStyle"
        class="preview-image"
        :alt="fileName"
        @load="$emit('load')"
        @error="$emit('error')"
        @mousedown="$emit('mouse-down', $event)"
      />
      <div v-if="zoom > 1" class="image-nav-hint">
        <el-icon><InfoFilled /></el-icon>
        <span>{{ t('preview.image.hint') }}</span>
      </div>
    </div>
    <div class="preview-toolbar">
      <div class="toolbar-left">
        <el-button-group>
          <el-button icon="ZoomIn" @click="$emit('zoom', 0.1)">{{ t('preview.image.zoomIn') }}</el-button>
          <el-button icon="ZoomOut" @click="$emit('zoom', -0.1)">{{ t('preview.image.zoomOut') }}</el-button>
          <el-button icon="RefreshRight" @click="$emit('rotate', 90)">{{ t('preview.image.rotate') }}</el-button>
          <el-button icon="Refresh" @click="$emit('reset')">{{ t('preview.image.reset') }}</el-button>
        </el-button-group>
      </div>
      <div class="toolbar-right">
        <el-button-group>
          <el-button v-if="canPrint" icon="Printer" @click="$emit('print')">{{ t('preview.image.print') }}</el-button>
          <el-button icon="Download" @click="$emit('download')">{{ t('preview.image.download') }}</el-button>
          <el-button @click="$emit('fullscreen')">
            <el-icon><FullScreen /></el-icon>
            {{ t('preview.image.fullscreen') }}
          </el-button>
        </el-button-group>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import type { StyleValue } from 'vue'
  import { FullScreen, InfoFilled } from '@element-plus/icons-vue'
  import { useI18n } from '@/composables/core/useI18n'

  defineProps<{
    url: string
    fileName?: string
    imageStyle: StyleValue
    zoom: number
    canPrint: boolean
  }>()

  defineEmits<{
    wheel: [event: WheelEvent]
    reset: []
    load: []
    error: []
    'mouse-down': [event: MouseEvent]
    zoom: [delta: number]
    rotate: [angle: number]
    print: []
    download: []
    fullscreen: []
  }>()

  const { t } = useI18n()
</script>

<style scoped>
  .preview-image-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .image-wrapper {
    min-height: 400px;
    padding: 20px;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    border-radius: 8px;
    background: var(--bg-color);
    cursor: grab;
  }
  .image-wrapper:active {
    cursor: grabbing;
  }
  .preview-image {
    max-width: 100%;
    max-height: 70vh;
    object-fit: contain;
  }
  .image-nav-hint {
    position: absolute;
    top: 12px;
    left: 50%;
    z-index: 10;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 16px;
    transform: translateX(-50%);
    border-radius: 20px;
    background: rgba(0, 0, 0, 0.7);
    color: #fff;
    font-size: 12px;
    backdrop-filter: blur(8px);
  }
  .preview-toolbar {
    min-height: 54px;
    margin-top: auto;
    padding: 16px 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    border-top: 1px solid var(--border-color);
    box-sizing: border-box;
  }
  .toolbar-left,
  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  @media (max-width: 1024px) {
    .image-wrapper {
      min-height: 300px;
      padding: 12px;
    }
    .preview-image {
      max-height: 60vh;
    }
    .preview-toolbar {
      padding-top: 12px;
    }
    .preview-toolbar :deep(.el-button-group) {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    .preview-toolbar :deep(.el-button) {
      flex: 1;
      min-width: 0;
      padding: 8px 12px;
    }
    .preview-toolbar :deep(.el-button span) {
      display: none;
    }
  }

  @media (max-width: 480px) {
    .preview-image {
      max-height: 50vh;
    }
  }
</style>
