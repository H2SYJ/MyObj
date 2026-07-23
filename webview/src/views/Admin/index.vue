<template>
  <MobilePage v-if="isHandheld && route.path === '/admin'" class="mobile-admin-hub">
    <div class="admin-hub-card">
      <router-link v-for="item in adminItems" :key="item.path" :to="item.path" class="admin-hub-item">
        <span><el-icon><component :is="item.icon" /></el-icon></span>
        <div><strong>{{ item.title }}</strong><small>{{ item.description }}</small></div>
        <el-icon><ArrowRight /></el-icon>
      </router-link>
    </div>
  </MobilePage>

  <div v-else-if="isHandheld" class="mobile-admin-content">
    <component :is="activeAdminComponent" />
  </div>

  <div v-else class="admin-page">
    <el-card shadow="never" class="admin-header-card">
      <div class="admin-header">
        <div class="header-left">
          <el-icon :size="28" class="admin-icon"><Setting /></el-icon>
          <h2>{{ t('route.admin') }}</h2>
        </div>
      </div>
    </el-card>

    <el-card shadow="never" class="admin-content-card">
      <el-tabs v-model="activeTab" class="admin-tabs">
        <el-tab-pane :label="t('route.adminUsers')" name="users">
          <AdminUsers />
        </el-tab-pane>
        <el-tab-pane :label="t('route.adminGroups')" name="groups">
          <AdminGroups />
        </el-tab-pane>
        <el-tab-pane :label="t('route.adminPermissions')" name="permissions">
          <AdminPermissions />
        </el-tab-pane>
        <el-tab-pane :label="t('route.adminDisks')" name="disks">
          <AdminDisks />
        </el-tab-pane>
        <el-tab-pane :label="t('route.adminSystem')" name="system">
          <AdminSystem />
        </el-tab-pane>
        <el-tab-pane :label="t('route.adminPlugins')" name="plugins">
          <AdminPlugins />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup lang="ts">
  import AdminUsers from './Users/index.vue'
  import AdminGroups from './Groups/index.vue'
  import AdminPermissions from './Permissions/index.vue'
  import AdminDisks from './Disks/index.vue'
  import AdminSystem from './System/index.vue'
  import AdminPlugins from './Plugins/index.vue'
  import { MobilePage } from '@/components/mobile'
  import { useI18n, useResponsive } from '@/composables'

  const route = useRoute()
  const router = useRouter()
  const { t } = useI18n()
  const { isHandheld } = useResponsive()

  const activeTab = ref('users')
  const adminComponents = {
    users: AdminUsers,
    groups: AdminGroups,
    permissions: AdminPermissions,
    disks: AdminDisks,
    system: AdminSystem,
    plugins: AdminPlugins
  }
  const activeAdminComponent = computed(() => adminComponents[activeTab.value as keyof typeof adminComponents])
  const adminItems = [
    { path: '/admin/users', title: t('route.adminUsers'), description: '账户、状态与用户组', icon: 'User' },
    { path: '/admin/groups', title: t('route.adminGroups'), description: '容量与权限策略', icon: 'UserFilled' },
    { path: '/admin/permissions', title: t('route.adminPermissions'), description: '系统权限定义', icon: 'Lock' },
    { path: '/admin/disks', title: t('route.adminDisks'), description: '存储磁盘与容量', icon: 'Coin' },
    { path: '/admin/system', title: t('route.adminSystem'), description: '服务与安全配置', icon: 'Setting' },
    { path: '/admin/plugins', title: t('route.adminPlugins'), description: '插件、权限与审计', icon: 'Box' }
  ]

  // 根据路由设置活动标签
  watch(
    () => route.name,
    name => {
      if (name === 'AdminUsers') activeTab.value = 'users'
      else if (name === 'AdminGroups') activeTab.value = 'groups'
      else if (name === 'AdminPermissions') activeTab.value = 'permissions'
      else if (name === 'AdminDisks') activeTab.value = 'disks'
      else if (name === 'AdminSystem') activeTab.value = 'system'
      else if (name === 'AdminPlugins') activeTab.value = 'plugins'
    },
    { immediate: true }
  )

  // 标签切换时更新路由
  watch(activeTab, tab => {
    const routeMap: Record<string, string> = {
      users: '/admin/users',
      groups: '/admin/groups',
      permissions: '/admin/permissions',
      disks: '/admin/disks',
      system: '/admin/system',
      plugins: '/admin/plugins'
    }
    if (routeMap[tab] && route.path !== routeMap[tab]) {
      router.push(routeMap[tab])
    }
  })
</script>

<style scoped>
  .admin-page {
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 4px;
  }

  .admin-header-card {
    flex-shrink: 0;
  }

  .admin-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .admin-icon {
    color: var(--primary-color);
    filter: drop-shadow(0 2px 4px rgba(99, 102, 241, 0.3));
  }

  html.dark .admin-icon {
    filter: drop-shadow(0 2px 4px rgba(99, 102, 241, 0.5));
  }

  .admin-header h2 {
    margin: 0;
    font-size: 20px;
    font-weight: 700;
    color: var(--text-primary);
  }

  .admin-content-card {
    flex: 1;
    overflow: hidden;
  }

  .admin-tabs {
    height: 100%;
  }

  .admin-tabs :deep(.el-tabs__content) {
    height: calc(100% - 55px);
    overflow: auto;
  }

  .admin-tabs :deep(.el-tab-pane) {
    height: 100%;
  }

  /* 深色模式样式 */
  html.dark .admin-header-card {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .admin-content-card {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .admin-tabs :deep(.el-tabs__header) {
    background: transparent;
    border-bottom-color: var(--el-border-color);
  }

  html.dark .admin-tabs :deep(.el-tabs__item) {
    color: var(--el-text-color-regular);
  }

  html.dark .admin-tabs :deep(.el-tabs__item.is-active) {
    color: var(--primary-color);
  }

  html.dark .admin-tabs :deep(.el-tabs__item:hover) {
    color: var(--primary-color);
  }

  .mobile-admin-content { min-height: 100%; padding: 12px; }
  .admin-hub-card {
    overflow: hidden;
    border: 1px solid var(--border-light);
    border-radius: 20px;
    background: var(--card-bg);
  }
  .admin-hub-item {
    min-height: 72px;
    padding: 10px 14px;
    display: grid;
    grid-template-columns: 42px 1fr 24px;
    align-items: center;
    gap: 12px;
    color: var(--text-primary);
    text-decoration: none;
    border-top: 1px solid var(--border-light);
  }
  .admin-hub-item:first-child { border-top: 0; }
  .admin-hub-item:active { background: var(--border-light); }
  .admin-hub-item > span {
    width: 42px;
    height: 42px;
    display: grid;
    place-items: center;
    border-radius: 14px;
    color: white;
    background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
  }
  .admin-hub-item > div { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .admin-hub-item strong { font-size: 15px; }
  .admin-hub-item small { color: var(--text-secondary); font-size: 12px; }
</style>
