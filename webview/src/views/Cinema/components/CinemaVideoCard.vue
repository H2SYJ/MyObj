<template>
  <article class="cinema-video-card" tabindex="0" role="button" @click="$emit('open')" @keydown.enter="$emit('open')">
    <div class="cinema-video-card__poster">
      <img v-if="thumbnailUrl" :src="thumbnailUrl" :alt="video.file_name" />
      <el-icon v-else :size="44"><VideoPlay /></el-icon>
      <span v-if="video.is_enc" class="cinema-video-card__lock"
        ><el-icon><Lock /></el-icon
      ></span>
    </div>
    <h3 :title="video.file_name">{{ video.file_name }}</h3>
    <p v-if="showDirectory" :title="video.directory.path">{{ video.directory.name }}</p>
  </article>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, ref, watch } from 'vue'
  import { Lock, VideoPlay } from '@element-plus/icons-vue'
  import { getThumbnail } from '@/api/file'
  import type { CinemaVideo } from '@/api/cinema'

  const props = withDefaults(defineProps<{ video: CinemaVideo; showDirectory?: boolean }>(), { showDirectory: false })
  defineEmits<{ open: [] }>()
  const thumbnailUrl = ref('')
  let thumbnailRequest = 0

  const loadThumbnail = async () => {
    if (!props.video.has_thumbnail) {
      return
    }
    const request = ++thumbnailRequest
    try {
      const url = await getThumbnail(props.video.file_id)
      if (request !== thumbnailRequest || props.video.file_id === '') {
        URL.revokeObjectURL(url)
        return
      }
      thumbnailUrl.value = url
    } catch {
      if (request === thumbnailRequest) {
        thumbnailUrl.value = ''
      }
    }
  }

  watch(
    () => props.video.file_id,
    () => {
      thumbnailRequest++
      if (thumbnailUrl.value) {
        URL.revokeObjectURL(thumbnailUrl.value)
      }
      thumbnailUrl.value = ''
      void loadThumbnail()
    },
    { immediate: true }
  )

  onBeforeUnmount(() => {
    thumbnailRequest++
    if (thumbnailUrl.value) {
      URL.revokeObjectURL(thumbnailUrl.value)
    }
  })
</script>

<style scoped>
  .cinema-video-card {
    min-width: 0;
    padding: 8px 8px 12px;
    border: 1px solid var(--cinema-border, #e8edf2);
    border-radius: 16px;
    background: #fff;
    box-shadow: 0 4px 16px rgba(24, 25, 28, 0.045);
    cursor: pointer;
    outline: none;
    transition:
      transform 0.2s ease,
      border-color 0.2s ease,
      box-shadow 0.2s ease;
  }
  .cinema-video-card:hover {
    border-color: #d4e8fb;
    box-shadow: 0 10px 26px rgba(24, 25, 28, 0.09);
    transform: translateY(-2px);
  }
  .cinema-video-card:focus-visible {
    border-color: var(--cinema-accent, #168cff);
    box-shadow: 0 0 0 3px rgba(22, 140, 255, 0.16);
  }
  .cinema-video-card__poster {
    position: relative;
    aspect-ratio: 16 / 9;
    display: grid;
    place-items: center;
    overflow: hidden;
    border-radius: 12px;
    color: var(--cinema-accent, #168cff);
    background: #f4f9ff;
  }
  .cinema-video-card__poster img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    transition: transform 0.2s ease;
  }
  .cinema-video-card:hover img {
    transform: scale(1.035);
  }
  .cinema-video-card__lock {
    position: absolute;
    right: 8px;
    bottom: 8px;
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: white;
    background: rgba(0, 0, 0, 0.7);
  }
  h3 {
    margin: 10px 2px 0;
    display: -webkit-box;
    overflow: hidden;
    color: var(--cinema-text, #18191c);
    font-size: 14px;
    font-weight: 600;
    line-height: 1.45;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }
  p {
    margin: 5px 2px 0;
    overflow: hidden;
    color: var(--cinema-muted, #6b7280);
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (max-width: 767px) {
    .cinema-video-card {
      padding: 6px 6px 10px;
      border-radius: 14px;
      box-shadow: 0 3px 12px rgba(24, 25, 28, 0.04);
    }
    .cinema-video-card__poster {
      border-radius: 10px;
    }
    h3 {
      margin-top: 8px;
      font-size: 13px;
    }
  }
</style>
