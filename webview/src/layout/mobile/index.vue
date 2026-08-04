<template>
  <div class="mobile-app-shell">
    <MobileTopBar
      :title="pageTitle"
      :show-back="Boolean(route.meta.mobileParent)"
      :searchable="Boolean(route.meta.mobileSearch)"
      @back="handleBack"
      @search="searchVisible = true"
    />

    <main ref="mainRef" class="mobile-app-main" :class="{ 'has-bottom-nav': showBottomNav }">
      <router-view v-slot="{ Component }">
        <Transition name="mobile-page-fade" mode="out-in">
          <component :is="Component" />
        </Transition>
      </router-view>
    </main>

    <MobileBottomNav v-if="showBottomNav" :items="navItems" />

    <MobileFullScreenLayer v-model="searchVisible" title="搜索" history-key="global-search">
      <form class="mobile-search-form" @submit.prevent="submitCurrentSearch">
        <FileSearchInput
          ref="searchInputRef"
          v-model="searchKeyword"
          v-model:tags="searchTags"
          :scope="searchScope"
          :history="searchHistory"
          :placeholder="t('files.searchPlaceholder')"
          @submit="submitSearch"
          @clear="clearSearch"
          @clear-history="clearHistory"
          @delete-history="removeHistory"
        />
        <el-button type="primary" size="large" native-type="submit">搜索</el-button>
      </form>
    </MobileFullScreenLayer>
  </div>
</template>

<script setup lang="ts">
  import { MobileBottomNav, MobileFullScreenLayer, MobileTopBar, type MobileNavItem } from '@/components/mobile'
  import { useI18n, useSearchHistory } from '@/composables'
  import { extractSearchHistoryKeyword, useFileSearchDraft } from '@/composables/business/useFileSearchDraft'
  import FileSearchInput from '@/components/FileSearchInput/index.vue'
  import { resolveDesktopSearchNavigation } from '@/utils/desktop/routeSearch'
  import type { CompactTag } from '@/types'

  const route = useRoute()
  const router = useRouter()
  const { t } = useI18n()
  const mainRef = ref<HTMLElement>()
  const searchVisible = ref(false)
  const searchScope = computed(() => (route.path === '/square' ? 'public' : 'user'))
  const { keyword: searchKeyword, tags: searchTags, syncFromRoute } = useFileSearchDraft(searchScope)
  const { searchHistory, addHistory, clearHistory, removeHistory } = useSearchHistory()
  const searchInputRef = ref()
  const scrollPositions = new Map<string, number>()

  const pageTitle = computed(() => {
    if (route.meta.mobileTitle) return route.meta.mobileTitle
    if (route.meta.i18nKey) return t(String(route.meta.i18nKey))
    return String(route.meta.title || 'MyObj')
  })
  const showBottomNav = computed(() => !route.meta.hideMobileNav && Boolean(route.meta.mobileTab))

  const navItems = computed<MobileNavItem[]>(() => [
    { key: 'files', label: t('mobileTab.files'), icon: 'Folder', path: '/files' },
    { key: 'offline', label: t('mobileTab.offline'), icon: 'Download', path: '/offline' },
    { key: 'tasks', label: t('mobileTab.tasks'), icon: 'List', path: '/tasks' },
    { key: 'square', label: t('mobileTab.square'), icon: 'Grid', path: '/square' },
    { key: 'me', label: t('mobileTab.me'), icon: 'User', path: '/me' }
  ])

  const handleBack = () => router.push(String(route.meta.mobileParent || '/me'))

  const closeSearchLayerForNavigation = async () => {
    if (!searchVisible.value) return
    if (window.history.state?.__myobjMobileLayer === 'global-search') {
      await new Promise<void>(resolve => {
        window.addEventListener('popstate', () => nextTick(resolve), { once: true })
        window.history.back()
      })
      return
    }
    searchVisible.value = false
    await nextTick()
  }

  const submitSearch = async (
    payload: { keyword: string; tags: CompactTag[] } = { keyword: searchKeyword.value.trim(), tags: searchTags.value }
  ) => {
    const historyKeyword = extractSearchHistoryKeyword(payload.keyword)
    if (historyKeyword) addHistory(historyKeyword)
    const location = resolveDesktopSearchNavigation(
      route.path,
      route.query,
      route.meta.desktopSearch,
      payload.keyword,
      payload.tags.map(tag => tag.id)
    )
    await closeSearchLayerForNavigation()
    await router.push(location)
  }

  const clearSearch = () => submitSearch({ keyword: '', tags: [] })
  const submitCurrentSearch = () => {
    if (searchInputRef.value?.submit) {
      searchInputRef.value.submit()
      return
    }
    void submitSearch()
  }

  watch(searchVisible, visible => {
    if (visible) {
      void syncFromRoute().then(() => nextTick(() => searchInputRef.value?.focus?.()))
    }
  })

  watch(
    () => route.fullPath,
    (nextPath, previousPath) => {
      if (mainRef.value) scrollPositions.set(previousPath, mainRef.value.scrollTop)
      searchVisible.value = false
      nextTick(() => {
        if (mainRef.value) mainRef.value.scrollTop = scrollPositions.get(nextPath) || 0
      })
    }
  )
</script>

<style scoped>
  .mobile-app-shell {
    width: 100%;
    height: 100vh;
    height: 100dvh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--bg-color);
  }

  .mobile-app-main {
    flex: 1;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior-y: contain;
    -webkit-overflow-scrolling: touch;
  }

  .mobile-search-form {
    display: grid;
    gap: 16px;
  }

  .mobile-search-form :deep(.file-search-input__control) {
    min-height: 52px;
    border-radius: 16px;
  }

  .mobile-search-form .el-button {
    min-height: 48px;
    border-radius: 16px;
  }

  .mobile-page-fade-enter-active,
  .mobile-page-fade-leave-active {
    transition:
      opacity 180ms ease,
      transform 180ms ease;
  }
  .mobile-page-fade-enter-from {
    opacity: 0;
    transform: translateY(6px);
  }
  .mobile-page-fade-leave-to {
    opacity: 0;
    transform: translateY(-4px);
  }

  @media (prefers-reduced-motion: reduce) {
    .mobile-page-fade-enter-active,
    .mobile-page-fade-leave-active {
      transition: none;
    }
  }
</style>
