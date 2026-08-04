<template>
  <div v-loading="loading && sections.length === 0" class="cinema-home">
    <div class="cinema-home__title">
      <h1>{{ root?.name || '影视库' }}</h1>
      <p v-if="root">{{ root.path }}</p>
    </div>

    <section v-for="section in sections" :key="section.directory.id" class="cinema-section">
      <div class="cinema-section__header">
        <div>
          <h2>{{ section.directory.name }}</h2>
          <p>{{ section.directory.path }}</p>
        </div>
        <router-link :to="`/cinema/${rootId}/folder/${section.directory.id}`">更多 &gt;&gt;</router-link>
      </div>
      <div
        class="cinema-section__rail"
        tabindex="0"
        :aria-label="`${section.directory.name}视频列表`"
        @keydown="handleRailKeydown"
      >
        <CinemaVideoCard
          v-for="video in section.videos"
          :key="video.file_id"
          :video="video"
          @open="openVideo(video.file_id)"
        />
      </div>
    </section>

    <el-empty v-if="!loading && sections.length === 0" description="当前影视文件夹及子文件夹中没有可播放视频" />
    <div ref="sentinel" class="cinema-sentinel">
      <el-icon v-if="loading && sections.length"><Loading class="is-loading" /></el-icon>
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
  import { Loading } from '@element-plus/icons-vue'
  import { getCinemaHome, type CinemaDirectory, type CinemaSection } from '@/api/cinema'
  import CinemaVideoCard from './components/CinemaVideoCard.vue'

  const route = useRoute()
  const router = useRouter()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const rootId = computed(() => Number(route.params.rootDirectoryId))
  const root = ref<CinemaDirectory>()
  const sections = ref<CinemaSection[]>([])
  const page = ref(1)
  const hasMore = ref(true)
  const loading = ref(false)
  const sentinel = ref<HTMLElement>()
  let observer: IntersectionObserver | undefined
  let generation = 0

  const failAndExit = (message: string) => {
    proxy?.$modal.msgError(message)
    void router.replace('/files')
  }

  const loadMore = async () => {
    if (loading.value || !hasMore.value || !Number.isInteger(rootId.value)) {
      return
    }
    const requestGeneration = generation
    const requestedRootId = rootId.value
    const requestedPage = page.value
    loading.value = true
    try {
      const response = await getCinemaHome(requestedRootId, requestedPage, 20)
      if (requestGeneration !== generation) {
        return
      }
      if (response.code !== 200 || !response.data) {
        failAndExit(response.message || '加载影视目录失败')
        return
      }
      root.value = response.data.root
      sections.value.push(...(response.data.sections || []))
      hasMore.value = response.data.has_more
      page.value = requestedPage + 1
    } catch (error) {
      if (requestGeneration !== generation) {
        return
      }
      failAndExit(error instanceof Error ? error.message : '加载影视目录失败')
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
  const handleRailKeydown = (event: KeyboardEvent) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') {
      return
    }
    event.preventDefault()
    const direction = event.key === 'ArrowLeft' ? -1 : 1
    const rail = event.currentTarget as HTMLElement
    rail.scrollBy({ left: direction * 320, behavior: 'smooth' })
  }

  const reset = () => {
    generation++
    loading.value = false
    root.value = undefined
    sections.value = []
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
  watch(rootId, reset, { immediate: true })
  onBeforeUnmount(() => observer?.disconnect())
</script>

<style scoped>
  .cinema-home__title h1 {
    margin: 0;
    color: var(--cinema-text, #18191c);
    font-size: clamp(26px, 3vw, 34px);
    font-weight: 700;
    letter-spacing: -0.02em;
  }
  .cinema-home__title p,
  .cinema-section__header p {
    margin: 6px 0 0;
    color: var(--cinema-muted, #6b7280);
    font-size: 13px;
  }
  .cinema-section {
    margin-top: 34px;
    padding: 20px;
    border: 1px solid var(--cinema-border, #e8edf2);
    border-radius: 20px;
    background: #fff;
    box-shadow: var(--cinema-shadow, 0 8px 28px rgba(24, 25, 28, 0.06));
  }
  .cinema-section__header {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 14px;
  }
  .cinema-section__header h2 {
    margin: 0;
    color: var(--cinema-text, #18191c);
    font-size: 21px;
    font-weight: 650;
  }
  .cinema-section__header a {
    flex: none;
    padding: 7px 12px;
    border-radius: 999px;
    color: var(--cinema-accent, #168cff);
    background: var(--cinema-accent-soft, #eef7ff);
    font-size: 13px;
    text-decoration: none;
  }
  .cinema-section__header a:hover {
    color: #0878df;
    background: #e3f2ff;
  }
  .cinema-section__rail {
    display: grid;
    grid-auto-columns: minmax(210px, 1fr);
    grid-auto-flow: column;
    grid-template-rows: 1fr;
    gap: 16px;
    overflow-x: auto;
    overscroll-behavior-inline: contain;
    scroll-snap-type: inline proximity;
    scrollbar-width: none;
  }
  .cinema-section__rail::-webkit-scrollbar {
    display: none;
  }
  .cinema-section__rail > * {
    scroll-snap-align: start;
  }
  .cinema-sentinel {
    min-height: 48px;
    display: grid;
    place-items: center;
  }
  @media (min-width: 1280px) {
    .cinema-section__rail {
      grid-auto-columns: calc((100% - 80px) / 6);
    }
  }
  @media (max-width: 767px) {
    .cinema-section {
      margin-top: 28px;
      padding: 14px;
      border-radius: 16px;
      box-shadow: 0 5px 18px rgba(24, 25, 28, 0.045);
    }
    .cinema-section__rail {
      grid-auto-columns: minmax(166px, 72vw);
      gap: 12px;
    }
  }
</style>
