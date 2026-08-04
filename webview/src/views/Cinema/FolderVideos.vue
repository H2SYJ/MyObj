<template>
  <div v-loading="loading && videos.length === 0" class="cinema-folder">
    <div class="cinema-folder__heading">
      <button type="button" @click="router.back()">
        <el-icon><ArrowLeft /></el-icon>
      </button>
      <div>
        <h1>{{ directory?.name || '视频列表' }}</h1>
        <p v-if="directory">{{ directory.path }} · {{ total }} 个视频</p>
      </div>
    </div>
    <div class="cinema-video-grid">
      <CinemaVideoCard v-for="video in videos" :key="video.file_id" :video="video" @open="openVideo(video.file_id)" />
    </div>
    <el-empty v-if="!loading && videos.length === 0" description="该文件夹没有可播放视频" />
    <div ref="sentinel" class="cinema-sentinel">
      <el-icon v-if="loading"><Loading class="is-loading" /></el-icon>
    </div>
  </div>
</template>

<script setup lang="ts">
  import {
    computed,
    getCurrentInstance,
    nextTick,
    onBeforeUnmount,
    onMounted,
    ref,
    watch,
    type ComponentInternalInstance
  } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { ArrowLeft, Loading } from '@element-plus/icons-vue'
  import { getCinemaFolderVideos, type CinemaDirectory, type CinemaVideo } from '@/api/cinema'
  import CinemaVideoCard from './components/CinemaVideoCard.vue'

  const route = useRoute()
  const router = useRouter()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const rootId = computed(() => Number(route.params.rootDirectoryId))
  const directoryId = computed(() => Number(route.params.directoryId))
  const directory = ref<CinemaDirectory>()
  const videos = ref<CinemaVideo[]>([])
  const total = ref(0)
  const page = ref(1)
  const hasMore = ref(true)
  const loading = ref(false)
  const sentinel = ref<HTMLElement>()
  let observer: IntersectionObserver | undefined
  let generation = 0

  const loadMore = async () => {
    if (loading.value || !hasMore.value) {
      return
    }
    const requestGeneration = generation
    const requestedRootId = rootId.value
    const requestedDirectoryId = directoryId.value
    const requestedPage = page.value
    loading.value = true
    try {
      const response = await getCinemaFolderVideos(requestedRootId, requestedDirectoryId, requestedPage, 24)
      if (requestGeneration !== generation) {
        return
      }
      if (response.code !== 200 || !response.data) {
        throw new Error(response.message || '加载视频失败')
      }
      directory.value = response.data.directory
      total.value = response.data.total
      videos.value.push(...(response.data.videos || []))
      hasMore.value = response.data.has_more
      page.value = requestedPage + 1
    } catch (error) {
      if (requestGeneration !== generation) {
        return
      }
      proxy?.$modal.msgError(error instanceof Error ? error.message : '加载视频失败')
      if (videos.value.length === 0) {
        void router.replace(`/cinema/${requestedRootId}`)
      }
      hasMore.value = false
    } finally {
      if (requestGeneration === generation) {
        loading.value = false
        await nextTick()
        const top = sentinel.value?.getBoundingClientRect().top
        if (hasMore.value && top !== undefined && top <= window.innerHeight + 400) {
          void loadMore()
        }
      }
    }
  }
  const openVideo = (fileId: string) => router.push(`/cinema/${rootId.value}/watch/${fileId}`)

  const reset = () => {
    generation++
    loading.value = false
    directory.value = undefined
    videos.value = []
    total.value = 0
    page.value = 1
    hasMore.value = true
    void loadMore()
  }

  onMounted(() => {
    observer = new IntersectionObserver(
      entries => {
        if (entries[0]?.isIntersecting) {
          void loadMore()
        }
      },
      { rootMargin: '400px' }
    )
    if (sentinel.value) {
      observer.observe(sentinel.value)
    }
  })
  watch([rootId, directoryId], reset, { immediate: true })
  onBeforeUnmount(() => observer?.disconnect())
</script>

<style scoped>
  .cinema-folder__heading {
    display: flex;
    align-items: center;
    gap: 14px;
    margin-bottom: 24px;
    padding-bottom: 18px;
    border-bottom: 1px solid var(--cinema-border, #e8edf2);
  }
  .cinema-folder__heading button {
    width: 38px;
    height: 38px;
    display: grid;
    place-items: center;
    border: 1px solid var(--cinema-border, #e8edf2);
    border-radius: 12px;
    color: var(--cinema-text, #18191c);
    background: #fff;
    cursor: pointer;
    transition:
      border-color 0.2s ease,
      color 0.2s ease,
      background 0.2s ease;
  }
  .cinema-folder__heading button:hover {
    border-color: #cfe6ff;
    color: var(--cinema-accent, #168cff);
    background: var(--cinema-accent-soft, #eef7ff);
  }
  h1 {
    margin: 0;
    color: var(--cinema-text, #18191c);
    font-size: 28px;
    font-weight: 700;
  }
  p {
    margin: 5px 0 0;
    color: var(--cinema-muted, #6b7280);
    font-size: 13px;
  }
  .cinema-video-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 24px 18px;
  }
  .cinema-sentinel {
    min-height: 52px;
    display: grid;
    place-items: center;
  }
  @media (max-width: 767px) {
    h1 {
      font-size: 23px;
    }
    .cinema-video-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px 10px;
    }
  }
</style>
