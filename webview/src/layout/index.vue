<template>
  <MobileLayout v-if="isHandheld" />
  <div v-else class="desktop-shell" :class="{ 'is-sidebar-collapsed': sidebarCollapsed || isCompactDesktop }">
    <Sidebar />
    <Header />
    <AppMain />
  </div>
</template>

<script setup lang="ts">
  import { Header, Sidebar, AppMain } from './components'
  import MobileLayout from './mobile/index.vue'
  import { useResponsive } from '@/composables'
  import { useLayoutStore } from '@/stores'

  const layoutStore = useLayoutStore()
  const { isHandheld, isCompactDesktop } = useResponsive()
  const sidebarCollapsed = computed(() => layoutStore.sidebarCollapsed)

  layoutStore.initLayout()
</script>
