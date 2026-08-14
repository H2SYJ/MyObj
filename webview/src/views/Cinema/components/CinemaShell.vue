<template>
  <div class="cinema-shell">
    <header>
      <button type="button" aria-label="返回我的文件" @click="router.push('/files')">
        <el-icon><ArrowLeft /></el-icon>
      </button>
      <router-link :to="`/cinema/${route.params.rootDirectoryId}`" class="cinema-brand">
        <el-icon><Film /></el-icon>
        <span>
          <small>影视模式</small>
          {{ rootName || '正在加载…' }}
        </span>
      </router-link>
      <span class="cinema-shell__spacer"></span>
    </header>
    <main><router-view :key="contentRefreshKey" /></main>
    <CinemaFileContextMenu
      :visible="fileContextMenu.visible"
      :x="fileContextMenu.x"
      :y="fileContextMenu.y"
      :video="fileContextMenu.video"
      @close="fileContextMenu.visible = false"
      @refresh="refreshContent"
    />
  </div>
</template>

<script setup lang="ts">
  import { computed, getCurrentInstance, provide, reactive, ref, watch, type ComponentInternalInstance } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { ArrowLeft, Film } from '@element-plus/icons-vue'
  import { getCinemaHome, type CinemaVideo } from '@/api/cinema'
  import { cinemaFileContextMenuKey } from '../cinemaFileContext'
  import CinemaFileContextMenu from './CinemaFileContextMenu.vue'

  const router = useRouter()
  const route = useRoute()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const rootId = computed(() => Number(route.params.rootDirectoryId))
  const rootName = ref('')
  const contentRefreshKey = ref(0)
  const fileContextMenu = reactive<{ visible: boolean; x: number; y: number; video?: CinemaVideo }>({
    visible: false,
    x: 0,
    y: 0
  })
  let requestId = 0

  const eventPosition = (event: MouseEvent | KeyboardEvent) => {
    if (event instanceof MouseEvent && (event.clientX || event.clientY)) {
      return { x: event.clientX, y: event.clientY }
    }
    const rect = (event.currentTarget as HTMLElement | null)?.getBoundingClientRect()
    return rect ? { x: rect.left + 24, y: rect.top + 24 } : { x: window.innerWidth / 2, y: window.innerHeight / 2 }
  }

  provide(cinemaFileContextMenuKey, (video, event) => {
    const position = eventPosition(event)
    Object.assign(fileContextMenu, { visible: true, x: position.x, y: position.y, video })
  })

  const refreshContent = () => {
    fileContextMenu.visible = false
    contentRefreshKey.value++
  }

  const loadRoot = async () => {
    const request = ++requestId
    rootName.value = ''
    try {
      const response = await getCinemaHome(rootId.value, 1, 1)
      if (response.code !== 200 || !response.data) {
        throw new Error(response.message || '影视文件夹已失效')
      }
      if (request === requestId) {
        rootName.value = response.data.root.name
      }
    } catch (error) {
      if (request !== requestId) {
        return
      }
      proxy?.$modal.msgError(error instanceof Error ? error.message : '影视文件夹已失效')
      void router.replace('/files')
    }
  }

  watch(rootId, () => void loadRoot(), { immediate: true })
</script>

<style scoped>
  .cinema-shell {
    --cinema-accent: #168cff;
    --cinema-accent-soft: #eef7ff;
    --cinema-text: #18191c;
    --cinema-muted: #6b7280;
    --cinema-border: #e8edf2;
    --cinema-shadow: 0 8px 28px rgba(24, 25, 28, 0.06);
    --cinema-home-card-width: 340px;
    --el-bg-color: #fff;
    --el-bg-color-page: #fff;
    --el-text-color-primary: var(--cinema-text);
    --el-text-color-regular: #3f4752;
    --el-text-color-secondary: var(--cinema-muted);
    --el-text-color-placeholder: #a8b1bd;
    --el-border-color-lighter: var(--cinema-border);
    --el-fill-color-light: var(--cinema-accent-soft);
    --el-color-primary: var(--cinema-accent);
    width: 100%;
    height: 100vh;
    height: 100dvh;
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior-y: contain;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: none;
    color: var(--cinema-text);
    background: #fff;
    color-scheme: light;
  }
  .cinema-shell::-webkit-scrollbar {
    display: none;
  }
  header {
    position: sticky;
    z-index: 30;
    top: 0;
    height: 60px;
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 0 max(18px, calc((100vw - 1440px) / 2));
    border-bottom: 1px solid var(--cinema-border);
    background: #fff;
    box-shadow: 0 4px 18px rgba(24, 25, 28, 0.035);
  }
  header button {
    width: 38px;
    height: 38px;
    display: grid;
    place-items: center;
    border: 1px solid var(--cinema-border);
    border-radius: 12px;
    color: var(--cinema-text);
    background: #fff;
    cursor: pointer;
    transition:
      border-color 0.2s ease,
      color 0.2s ease,
      background 0.2s ease;
  }
  header button:hover {
    border-color: #cfe6ff;
    color: var(--cinema-accent);
    background: var(--cinema-accent-soft);
  }
  .cinema-brand {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--cinema-text);
    font-size: 18px;
    font-weight: 700;
    text-decoration: none;
  }
  .cinema-brand > :deep(.el-icon) {
    width: 36px;
    height: 36px;
    flex: none;
    border-radius: 12px;
    color: var(--cinema-accent);
    background: var(--cinema-accent-soft);
  }
  .cinema-brand span {
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: 8px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cinema-brand small {
    color: var(--cinema-muted);
    font-size: 12px;
    font-weight: 500;
  }
  .cinema-shell__spacer {
    flex: 1;
  }
  main {
    width: min(1440px, 100%);
    margin: 0 auto;
    padding: 28px 24px 48px;
    box-sizing: border-box;
  }
  :deep(.el-loading-mask) {
    background: rgba(255, 255, 255, 0.9);
  }
  @media (max-width: 767px) {
    header {
      height: 52px;
      padding: 0 12px;
    }
    main {
      padding: 16px 12px 32px;
    }
  }
</style>
