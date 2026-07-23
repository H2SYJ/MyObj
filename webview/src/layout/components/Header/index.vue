<template>
  <header class="desktop-header">
    <div class="desktop-header__left">
      <el-tooltip
        :content="sidebarCollapsed ? t('layout.sidebar.expand') : t('layout.sidebar.collapse')"
        placement="bottom"
      >
        <el-button
          class="desktop-header__collapse"
          :icon="sidebarCollapsed ? 'Expand' : 'Fold'"
          circle
          text
          :aria-label="sidebarCollapsed ? t('layout.sidebar.expand') : t('layout.sidebar.collapse')"
          @click="layoutStore.toggleSidebarCollapsed"
        />
      </el-tooltip>
      <div class="desktop-header__route">
        <span>{{ routeTitle }}</span>
        <small v-if="routeDescription">{{ routeDescription }}</small>
      </div>
    </div>

    <div ref="searchWrapperRef" class="desktop-header__search">
      <el-input
        v-model="searchKeyword"
        :placeholder="t('files.searchPlaceholder')"
        prefix-icon="Search"
        clearable
        @keyup.enter="submitSearch"
        @focus="showSuggestions = true"
        @blur="handleSearchBlur"
        @clear="submitSearch"
      />
      <SearchSuggestions
        v-if="showSuggestions"
        :suggestions="searchSuggestions"
        :visible="searchSuggestions.length > 0"
        @select="selectSuggestion"
        @clear="clearHistory"
        @delete="removeHistory"
      />
    </div>

    <div class="desktop-header__actions">
      <el-tooltip :content="isDark ? t('header.switchToLight') : t('header.switchToDark')" placement="bottom">
        <el-button :icon="isDark ? 'Sunny' : 'Moon'" circle text @click="toggleTheme" />
      </el-tooltip>

      <el-dropdown trigger="click" @command="handleCommand">
        <button type="button" class="desktop-user" :aria-label="t('header.userMenu')">
          <el-avatar :size="34" :style="{ background: avatarColor }">{{ avatarText }}</el-avatar>
          <span
            ><strong>{{ displayName }}</strong
            ><small>{{ userStore.email || t('header.account') }}</small></span
          >
          <el-icon><ArrowDown /></el-icon>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="settings" icon="Setting">{{ t('menu.settings') }}</el-dropdown-item>
            <el-dropdown-item command="fullscreen" icon="FullScreen">
              {{ isFullscreen ? t('header.exitFullscreen') : t('header.fullscreen') }}
            </el-dropdown-item>
            <el-dropdown-item divided command="logout" icon="SwitchButton">{{ t('header.logout') }}</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </header>
</template>

<script setup lang="ts">
  import { useFullscreen } from '@vueuse/core'
  import { useI18n, useSearchHistory, useTheme } from '@/composables'
  import { useAuthStore, useLayoutStore, useUserStore } from '@/stores'
  import { resolveDesktopSearchNavigation } from '@/utils/desktop/routeSearch'

  const route = useRoute()
  const router = useRouter()
  const layoutStore = useLayoutStore()
  const userStore = useUserStore()
  const authStore = useAuthStore()
  const { t } = useI18n()
  const { isDark, toggleTheme } = useTheme()
  const { isFullscreen, toggle: toggleFullscreen } = useFullscreen()
  const { searchHistory, addHistory, clearHistory, removeHistory } = useSearchHistory()

  const searchKeyword = ref('')
  const showSuggestions = ref(false)
  const searchWrapperRef = ref<HTMLElement>()
  let searchBlurTimer: number | null = null
  const sidebarCollapsed = computed(() => layoutStore.sidebarCollapsed)
  const displayName = computed(() => userStore.nickname || userStore.username || 'MyObj')
  const avatarText = computed(() => displayName.value.charAt(0).toUpperCase())
  const avatarColor = computed(
    () => `hsl(${[...displayName.value].reduce((sum, char) => sum + char.charCodeAt(0), 0) % 360} 68% 48%)`
  )
  const routeTitle = computed(() => {
    const key = route.meta.i18nKey
    return key ? t(key) : String(route.meta.title || 'MyObj')
  })
  const routeDescription = computed(() =>
    route.meta.desktopSearch ? t(`desktop.searchScope.${route.meta.desktopSearch}`) : ''
  )
  const searchSuggestions = computed(() => {
    const keyword = searchKeyword.value.trim().toLowerCase()
    const source = keyword
      ? searchHistory.value.filter(item => item.toLowerCase().includes(keyword))
      : searchHistory.value
    return source.slice(0, 5)
  })

  const submitSearch = async () => {
    const keyword = searchKeyword.value.trim()
    if (keyword) addHistory(keyword)
    const location = resolveDesktopSearchNavigation(route.path, route.query, route.meta.desktopSearch, keyword)
    await router.push(location)
    showSuggestions.value = false
  }

  const selectSuggestion = (keyword: string) => {
    searchKeyword.value = keyword
    void submitSearch()
  }

  const handleSearchBlur = () => {
    if (searchBlurTimer) window.clearTimeout(searchBlurTimer)
    searchBlurTimer = window.setTimeout(() => {
      searchBlurTimer = null
      showSuggestions.value = false
    }, 160)
  }
  const handleCommand = async (command: string) => {
    if (command === 'settings') await router.push('/settings')
    if (command === 'fullscreen') await toggleFullscreen()
    if (command === 'logout') {
      authStore.logout()
      await router.replace('/login')
    }
  }

  watch(
    () => [route.path, route.query.search],
    () => {
      searchKeyword.value = route.meta.desktopSearch ? String(route.query.search || '') : ''
    },
    { immediate: true }
  )
  onBeforeUnmount(() => {
    if (searchBlurTimer) window.clearTimeout(searchBlurTimer)
  })
</script>

<style scoped>
  .desktop-header {
    grid-area: header;
    min-width: 0;
    height: var(--desktop-header-height);
    padding: 0 22px;
    display: grid;
    grid-template-columns: minmax(180px, 1fr) minmax(280px, 560px) minmax(240px, 1fr);
    align-items: center;
    gap: 20px;
    border-bottom: 1px solid var(--desktop-border);
    background: color-mix(in srgb, var(--desktop-surface) 94%, transparent);
    backdrop-filter: blur(14px);
    z-index: 20;
  }
  .desktop-header__left,
  .desktop-header__actions {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .desktop-header__actions {
    justify-content: flex-end;
  }
  .desktop-header__route {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .desktop-header__route span {
    overflow: hidden;
    color: var(--text-primary);
    font-size: 14px;
    font-weight: 720;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .desktop-header__route small {
    margin-top: 2px;
    overflow: hidden;
    color: var(--text-secondary);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .desktop-header__search {
    position: relative;
    min-width: 0;
  }
  .desktop-user {
    max-width: 220px;
    padding: 4px 7px;
    display: grid;
    grid-template-columns: 34px minmax(0, 1fr) 16px;
    align-items: center;
    gap: 9px;
    border: 0;
    border-radius: 10px;
    background: transparent;
    color: inherit;
    cursor: pointer;
    text-align: left;
  }
  .desktop-user:hover {
    background: var(--desktop-fill);
  }
  .desktop-user > span {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .desktop-user strong,
  .desktop-user small {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .desktop-user strong {
    font-size: 12px;
    font-weight: 700;
  }
  .desktop-user small {
    color: var(--text-secondary);
    font-size: 10px;
  }
  @media (max-width: 1100px) {
    .desktop-header {
      grid-template-columns: 52px minmax(260px, 1fr) auto;
      padding: 0 16px;
    }
    .desktop-header__route {
      display: none;
    }
    .desktop-user > span {
      display: none;
    }
    .desktop-user {
      width: 58px;
      grid-template-columns: 34px 14px;
    }
  }
</style>
