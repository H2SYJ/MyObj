<template>
  <aside class="desktop-sidebar" :class="{ 'is-collapsed': collapsed }">
    <router-link to="/files" class="desktop-sidebar__brand" aria-label="MyObj">
      <img :src="logoImage" alt="" />
      <span v-if="!collapsed">MyObj</span>
    </router-link>

    <nav class="desktop-sidebar__nav" :aria-label="t('desktop.primaryNavigation')">
      <template v-for="group in menuGroups" :key="group.title || 'group'">
        <div v-if="group.title && !collapsed" class="desktop-sidebar__group">{{ group.title }}</div>
        <template v-for="item in group.items" :key="item.path">
          <el-tooltip :disabled="!collapsed" :content="item.label" placement="right">
            <router-link
              v-if="item.path && !item.hidden"
              :to="item.path"
              class="desktop-sidebar__item"
              :class="{ 'is-active': activePath === item.path }"
            >
              <el-icon><component :is="item.icon" /></el-icon>
              <span v-if="!collapsed">{{ item.label }}</span>
            </router-link>
          </el-tooltip>
        </template>
      </template>
    </nav>

    <StorageCard v-if="!collapsed" class="desktop-sidebar__storage" />
  </aside>
</template>

<script setup lang="ts">
  import logoImage from '@/assets/images/LOGO.png'
  import StorageCard from '../StorageCard/index.vue'
  import { useI18n, useMenu, useResponsive } from '@/composables'
  import { useLayoutStore } from '@/stores'

  const route = useRoute()
  const layoutStore = useLayoutStore()
  const { menuGroups } = useMenu()
  const { isCompactDesktop } = useResponsive()
  const { t } = useI18n()
  const collapsed = computed(() => layoutStore.sidebarCollapsed || isCompactDesktop.value)
  const activePath = computed(() => {
    if (route.path.startsWith('/admin')) return '/admin'
    return route.path
  })
</script>

<style scoped>
  .desktop-sidebar {
    grid-area: sidebar;
    min-width: 0;
    min-height: 0;
    padding: 0 12px 14px;
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--desktop-border);
    background: var(--desktop-surface);
    z-index: 30;
  }
  .desktop-sidebar__brand {
    height: var(--desktop-header-height);
    padding: 0 10px;
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-primary);
    text-decoration: none;
  }
  .desktop-sidebar__brand img {
    width: 34px;
    height: 34px;
    flex: 0 0 auto;
    object-fit: contain;
  }
  .desktop-sidebar__brand span {
    font-size: 19px;
    font-weight: 780;
    letter-spacing: -0.02em;
  }
  .desktop-sidebar__nav {
    flex: 1;
    min-height: 0;
    padding-top: 12px;
    overflow-y: auto;
  }
  .desktop-sidebar__group {
    padding: 16px 10px 7px;
    color: var(--text-placeholder);
    font-size: 10px;
    font-weight: 750;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .desktop-sidebar__item {
    min-height: 42px;
    margin: 3px 0;
    padding: 0 11px;
    display: flex;
    align-items: center;
    gap: 11px;
    border-radius: 10px;
    color: var(--text-regular);
    font-size: 13px;
    font-weight: 620;
    text-decoration: none;
    transition:
      background 140ms ease,
      color 140ms ease;
  }
  .desktop-sidebar__item .el-icon {
    width: 20px;
    flex: 0 0 20px;
    font-size: 18px;
  }
  .desktop-sidebar__item:hover {
    background: var(--desktop-fill);
    color: var(--text-primary);
  }
  .desktop-sidebar__item.is-active {
    background: var(--desktop-primary-soft);
    color: var(--primary-color);
  }
  .desktop-sidebar__storage {
    margin: 12px 0 !important;
  }
  .desktop-sidebar.is-collapsed {
    padding-inline: 9px;
    align-items: stretch;
  }
  .desktop-sidebar.is-collapsed .desktop-sidebar__brand {
    justify-content: center;
    padding: 0;
  }
  .desktop-sidebar.is-collapsed .desktop-sidebar__item {
    padding: 0;
    justify-content: center;
  }
</style>
