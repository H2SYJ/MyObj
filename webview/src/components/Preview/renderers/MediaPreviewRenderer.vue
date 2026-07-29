<template>
  <div v-if="type === 'video'" class="preview-video-container">
    <xg-player
      v-if="url"
      :src="url"
      :autoplay="autoplay"
      :loop="loop"
      class="preview-video-xgplayer"
      @ready="$emit('ready')"
      @error="$emit('error', $event)"
    />
    <div class="preview-toolbar">
      <el-button v-if="canPrint" icon="Printer" @click="$emit('print')">{{ t('preview.video.print') }}</el-button>
      <el-button icon="Download" @click="$emit('download')">{{ t('preview.video.download') }}</el-button>
    </div>
  </div>
  <div v-else class="preview-audio-container">
    <div class="audio-wrapper">
      <el-icon :size="64" color="var(--primary-color)"><Headset /></el-icon>
      <p class="audio-filename">{{ fileName }}</p>
      <audio
        :src="url"
        :autoplay="autoplay"
        :loop="loop"
        :controls="controls"
        class="preview-audio"
        @loadstart="$emit('ready')"
        @error="$emit('error', t('preview.audio.loadFailed'))"
      >
        {{ t('preview.audio.notSupported') }}
      </audio>
    </div>
    <div class="preview-toolbar">
      <el-button icon="Download" @click="$emit('download')">{{ t('preview.audio.download') }}</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { Headset } from '@element-plus/icons-vue'
  import { useI18n } from '@/composables/core/useI18n'

  defineProps<{
    type: 'video' | 'audio'
    url: string
    fileName?: string
    autoplay: boolean
    loop: boolean
    controls: boolean
    canPrint: boolean
  }>()

  defineEmits<{
    ready: []
    error: [message: string]
    print: []
    download: []
  }>()

  const { t } = useI18n()
</script>

<style scoped>
  .preview-video-container {
    flex: 1;
    min-height: 0;
    height: 100%;
    max-height: 100%;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .preview-video-xgplayer:not(.xgplayer-rotate-fullscreen):not(.xgplayer-is-fullscreen):not(
      .xgplayer-is-cssfullscreen
    ) {
    width: 100%;
    min-height: 400px;
    flex: 1;
    overflow: hidden;
    border-radius: 8px;
    background: var(--el-bg-color-page, #000);
  }
  .preview-video-xgplayer :deep(video) {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .preview-audio-container {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .audio-wrapper {
    padding: 40px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
  }
  .audio-filename {
    margin: 0;
    color: var(--text-primary);
    font-size: 16px;
    font-weight: 600;
  }
  .preview-audio {
    width: 100%;
    max-width: 500px;
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
    flex-shrink: 0;
  }

  @media (max-width: 1024px) {
    .audio-wrapper {
      padding: 24px 16px;
      gap: 12px;
    }
    .audio-filename {
      font-size: 14px;
    }
    .preview-audio {
      max-width: 100%;
    }
    .preview-toolbar :deep(.el-button span) {
      display: none;
    }
  }
</style>
