<template>
  <div v-loading="loading" class="cinema-watch">
    <article v-if="video" class="cinema-watch__main">
      <h1 :title="video.file_name">{{ video.file_name }}</h1>
      <div class="cinema-player-frame">
        <XgPlayer v-if="videoUrl" :src="videoUrl" autoplay @error="handlePlayerError" />
        <button v-else type="button" class="cinema-poster" :disabled="playLoading" @click="startPlayback">
          <img v-if="posterUrl" :src="posterUrl" :alt="video.file_name" />
          <span class="cinema-poster__play">
            <el-icon v-if="!playLoading" :size="34"><VideoPlay /></el-icon>
            <el-icon v-else :size="30"><Loading class="is-loading" /></el-icon>
          </span>
          <span v-if="video.is_enc" class="cinema-poster__encrypted"
            ><el-icon><Lock /></el-icon> 加密视频</span
          >
        </button>
      </div>
      <div class="cinema-watch__meta">
        <span>{{ video.directory.path }}</span>
        <FileTags :tags="video.tags" :limit="6" />
      </div>
    </article>

    <aside v-if="video" class="cinema-related">
      <h2>相关视频</h2>
      <div class="cinema-related__grid">
        <CinemaVideoCard
          v-for="item in related"
          :key="item.file_id"
          :video="item"
          show-directory
          @open="openRelated(item.file_id)"
        />
      </div>
      <el-empty v-if="!relatedLoading && related.length === 0" :image-size="70" description="暂无相关视频" />
      <div ref="sentinel" class="cinema-related__sentinel">
        <el-icon v-if="relatedLoading"><Loading class="is-loading" /></el-icon>
      </div>
    </aside>
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
  import { ElMessageBox } from 'element-plus'
  import { Loading, Lock, VideoPlay } from '@element-plus/icons-vue'
  import { getCinemaVideo, getRelatedCinemaVideos, type CinemaVideo } from '@/api/cinema'
  import { getThumbnail } from '@/api/file'
  import { createVideoPlayPrecheck, getVideoStreamUrl } from '@/api/video'
  import cache from '@/plugins/cache'
  import CinemaVideoCard from './components/CinemaVideoCard.vue'
  import XgPlayer from '@/components/XgPlayer/index.vue'
  import FileTags from '@/components/FileTags/index.vue'

  const route = useRoute()
  const router = useRouter()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const rootId = computed(() => Number(route.params.rootDirectoryId))
  const fileId = computed(() => String(route.params.fileId || ''))
  const video = ref<CinemaVideo>()
  const related = ref<CinemaVideo[]>([])
  const loading = ref(false)
  const playLoading = ref(false)
  const relatedLoading = ref(false)
  const relatedPage = ref(1)
  const relatedHasMore = ref(true)
  const videoUrl = ref('')
  const posterUrl = ref('')
  const sentinel = ref<HTMLElement>()
  let observer: IntersectionObserver | undefined
  let detailRequest = 0

  const cleanupPoster = () => {
    if (posterUrl.value) {
      URL.revokeObjectURL(posterUrl.value)
    }
    posterUrl.value = ''
  }

  const loadPoster = async (request: number, item: CinemaVideo) => {
    cleanupPoster()
    if (!item.has_thumbnail) {
      return
    }
    try {
      const url = await getThumbnail(item.file_id)
      if (request !== detailRequest) {
        URL.revokeObjectURL(url)
        return
      }
      posterUrl.value = url
    } catch {
      if (request === detailRequest) {
        posterUrl.value = ''
      }
    }
  }

  const loadDetail = async () => {
    const request = ++detailRequest
    const requestedRootId = rootId.value
    const requestedFileId = fileId.value
    loading.value = true
    playLoading.value = false
    videoUrl.value = ''
    video.value = undefined
    cleanupPoster()
    related.value = []
    relatedPage.value = 1
    relatedHasMore.value = true
    relatedLoading.value = false
    try {
      const response = await getCinemaVideo(requestedRootId, requestedFileId)
      if (response.code !== 200 || !response.data) {
        throw new Error(response.message || '加载视频失败')
      }
      if (request !== detailRequest) {
        return
      }
      video.value = response.data.video
      await loadPoster(request, response.data.video)
      if (request !== detailRequest) {
        return
      }
      await loadRelated()
    } catch (error) {
      if (request !== detailRequest) {
        return
      }
      proxy?.$modal.msgError(error instanceof Error ? error.message : '加载视频失败')
      void router.replace(`/cinema/${requestedRootId}`)
    } finally {
      if (request === detailRequest) {
        loading.value = false
      }
    }
  }

  const requestPassword = async () => {
    const result = await ElMessageBox.prompt('请输入文件密码，密码仅在本次播放请求中使用', '播放加密视频', {
      inputType: 'password',
      inputPlaceholder: '文件密码',
      confirmButtonText: '播放',
      cancelButtonText: '取消',
      closeOnClickModal: false
    })
    return result.value
  }

  const isPromptCancellation = (error: unknown) =>
    error === 'cancel' ||
    (typeof error === 'object' && error !== null && 'action' in error && error.action === 'cancel')

  const startPlayback = async () => {
    if (!video.value || playLoading.value) {
      return
    }
    const request = detailRequest
    const currentVideo = video.value
    playLoading.value = true
    try {
      const password = currentVideo.is_enc ? await requestPassword() : undefined
      if (request !== detailRequest) {
        return
      }
      const response = await createVideoPlayPrecheck(currentVideo.file_id, password)
      if (response.code !== 200 || !response.data) {
        throw new Error(response.message || '获取播放令牌失败')
      }
      if (request !== detailRequest) {
        return
      }
      videoUrl.value = getVideoStreamUrl(response.data.play_token, cache.local.get('token') || undefined)
    } catch (error: unknown) {
      if (request !== detailRequest) {
        return
      }
      if (!isPromptCancellation(error)) {
        proxy?.$modal.msgError(error instanceof Error ? error.message : '播放失败')
      }
    } finally {
      if (request === detailRequest) {
        playLoading.value = false
      }
    }
  }

  const handlePlayerError = (message: string) => {
    proxy?.$modal.msgError(message)
    videoUrl.value = ''
  }

  const loadRelated = async () => {
    if (relatedLoading.value || !relatedHasMore.value) {
      return
    }
    const requestedRootId = rootId.value
    const requestedFileId = fileId.value
    const requestedPage = relatedPage.value
    const request = detailRequest
    relatedLoading.value = true
    try {
      const response = await getRelatedCinemaVideos(requestedRootId, requestedFileId, requestedPage, 20)
      if (response.code !== 200 || !response.data) {
        throw new Error(response.message || '加载相关视频失败')
      }
      if (request !== detailRequest) {
        return
      }
      related.value.push(...(response.data.videos || []))
      relatedHasMore.value = response.data.has_more
      relatedPage.value = requestedPage + 1
    } catch (error) {
      if (request !== detailRequest) {
        return
      }
      proxy?.$log.error('加载相关视频失败', error)
      relatedHasMore.value = false
    } finally {
      if (request === detailRequest) {
        relatedLoading.value = false
        await nextTick()
        const top = sentinel.value?.getBoundingClientRect().top
        if (relatedHasMore.value && top !== undefined && top <= window.innerHeight + 300) {
          void loadRelated()
        }
      }
    }
  }

  const openRelated = (nextFileId: string) => router.push(`/cinema/${rootId.value}/watch/${nextFileId}`)

  watch([rootId, fileId], () => void loadDetail(), { immediate: true })
  onMounted(() => {
    observer = new IntersectionObserver(
      entries => {
        if (entries[0]?.isIntersecting) {
          void loadRelated()
        }
      },
      { rootMargin: '300px' }
    )
    if (sentinel.value) {
      observer.observe(sentinel.value)
    }
  })
  watch(sentinel, (element, previous) => {
    if (previous) {
      observer?.unobserve(previous)
    }
    if (element) {
      observer?.observe(element)
    }
  })
  onBeforeUnmount(() => {
    observer?.disconnect()
    cleanupPoster()
  })
</script>

<style scoped>
  .cinema-watch {
    display: block;
  }
  .cinema-watch__main {
    width: min(100%, 1180px);
    min-width: 0;
    display: flex;
    flex-direction: column;
    margin: 0 auto;
    padding: 20px;
    border: 1px solid var(--cinema-border, #e8edf2);
    border-radius: 20px;
    background: #fff;
    box-shadow: var(--cinema-shadow, 0 8px 28px rgba(24, 25, 28, 0.06));
  }
  h1 {
    order: 1;
    margin: 0 0 14px;
    overflow: hidden;
    font-size: clamp(20px, 2.2vw, 28px);
    font-weight: 700;
    line-height: 1.35;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cinema-player-frame {
    order: 2;
    width: min(100%, 1100px);
    aspect-ratio: 16 / 9;
    margin: 0 auto;
    overflow: hidden;
    border-radius: 14px;
    background: #000;
    box-shadow: 0 12px 30px rgba(24, 25, 28, 0.14);
  }
  .cinema-player-frame :deep(.xg-player-wrapper) {
    border-radius: 0;
  }
  .cinema-poster {
    position: relative;
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    overflow: hidden;
    border: 0;
    color: white;
    background: #101010;
    cursor: pointer;
  }
  .cinema-poster > img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    opacity: 0.82;
  }
  .cinema-poster__play {
    z-index: 1;
    width: 74px;
    height: 74px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.7);
    backdrop-filter: blur(4px);
  }
  .cinema-poster__encrypted {
    position: absolute;
    z-index: 1;
    right: 16px;
    bottom: 16px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 6px 10px;
    border-radius: 999px;
    font-size: 12px;
    background: rgba(0, 0, 0, 0.7);
  }
  .cinema-watch__meta {
    order: 3;
    min-height: 32px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    margin-top: 12px;
    color: var(--cinema-muted, #6b7280);
    font-size: 13px;
  }
  .cinema-related h2 {
    margin: 0 0 14px;
    color: var(--cinema-text, #18191c);
    font-size: 20px;
    font-weight: 700;
  }
  .cinema-related {
    margin-top: 30px;
    padding: 18px;
    border: 1px solid var(--cinema-border, #e8edf2);
    border-radius: 20px;
    background: #fff;
    box-shadow: var(--cinema-shadow, 0 8px 28px rgba(24, 25, 28, 0.06));
  }
  .cinema-related__grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
    gap: 22px 16px;
  }
  .cinema-related :deep(.cinema-video-card) {
    min-width: 0;
  }
  .cinema-related__sentinel {
    min-height: 40px;
    display: grid;
    place-items: center;
  }
  @media (max-width: 900px) {
    .cinema-watch {
      display: block;
    }
    .cinema-watch__main {
      width: 100%;
      display: flex;
      margin: 0;
      padding: 0;
      border: 0;
      border-radius: 0;
      box-shadow: none;
    }
    h1 {
      order: 2;
      margin: 14px 0 8px;
      font-size: 20px;
      white-space: normal;
    }
    .cinema-player-frame {
      order: 1;
      width: 100%;
      margin: 0;
      border-radius: 14px;
      box-shadow: none;
    }
    .cinema-watch__meta {
      order: 3;
      align-items: flex-start;
      flex-direction: column;
      margin-top: 0;
    }
    .cinema-related {
      margin-top: 28px;
      padding: 14px;
      border-radius: 16px;
      box-shadow: 0 5px 18px rgba(24, 25, 28, 0.045);
    }
    .cinema-related__grid {
      grid-template-columns: 1fr;
      gap: 14px;
    }
    .cinema-related :deep(.cinema-video-card) {
      display: grid;
      grid-template-columns: 42% minmax(0, 1fr);
      column-gap: 12px;
    }
    .cinema-related :deep(.cinema-video-card__poster) {
      grid-row: 1 / 3;
    }
  }
</style>
