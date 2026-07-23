import { defineStore } from 'pinia'
import { ref } from 'vue'
import { StoreId } from '@/enums/StoreId'

const SIDEBAR_COLLAPSED_KEY = 'sidebarCollapsed'

/**
 * PC 端只保留固定侧栏的折叠状态。
 * 历史布局、宽度、标签页和背景图案配置不再读取，但也不主动删除。
 */
export const useLayoutStore = defineStore(StoreId.Layout, () => {
  const sidebarCollapsed = ref(false)

  function initLayout() {
    sidebarCollapsed.value = localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === 'true'
  }

  function setSidebarCollapsed(collapsed: boolean) {
    sidebarCollapsed.value = collapsed
    localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(collapsed))
  }

  function toggleSidebarCollapsed() {
    setSidebarCollapsed(!sidebarCollapsed.value)
  }

  function resetLayoutConfig() {
    setSidebarCollapsed(false)
  }

  return {
    sidebarCollapsed,
    initLayout,
    setSidebarCollapsed,
    toggleSidebarCollapsed,
    resetLayoutConfig
  }
})
