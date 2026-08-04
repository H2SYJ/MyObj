<template>
  <MobilePage v-if="isHandheld && route.path === '/admin'" class="mobile-admin-hub">
    <div class="admin-hub-card">
      <router-link v-for="item in adminItems" :key="item.path" :to="item.path" class="admin-hub-item">
        <span
          ><el-icon><component :is="item.icon" /></el-icon
        ></span>
        <div>
          <strong>{{ item.title }}</strong
          ><small>{{ item.description }}</small>
        </div>
        <el-icon><ArrowRight /></el-icon>
      </router-link>
    </div>
  </MobilePage>

  <div v-else-if="isHandheld" class="mobile-admin-content"><component :is="activeAdminComponent" /></div>

  <DesktopPage v-else :title="t('route.admin')" :description="t('admin.workspaceDescription')" full-height>
    <div class="admin-workspace">
      <nav class="admin-workspace__nav" :aria-label="t('route.admin')">
        <router-link
          v-for="item in adminItems"
          :key="item.path"
          :to="item.path"
          :aria-label="item.title"
          :title="item.title"
        >
          <span
            ><el-icon><component :is="item.icon" /></el-icon
          ></span>
          <div>
            <strong>{{ item.title }}</strong
            ><small>{{ item.description }}</small>
          </div>
          <el-icon><ArrowRight /></el-icon>
        </router-link>
      </nav>
      <section class="admin-workspace__content"><component :is="activeAdminComponent" /></section>
    </div>
  </DesktopPage>
</template>

<script setup lang="ts">
  import AdminUsers from './Users/index.vue'
  import AdminGroups from './Groups/index.vue'
  import AdminPermissions from './Permissions/index.vue'
  import AdminDisks from './Disks/index.vue'
  import AdminSystem from './System/index.vue'
  import AdminPlugins from './Plugins/index.vue'
  import AdminTags from './Tags/index.vue'
  import { DesktopPage } from '@/components/desktop'
  import { MobilePage } from '@/components/mobile'
  import { useI18n, useResponsive } from '@/composables'

  const route = useRoute()
  const { t } = useI18n()
  const { isHandheld } = useResponsive()
  const adminItems = computed(() => [
    { path: '/admin/users', title: t('route.adminUsers'), description: t('admin.nav.users'), icon: 'User' },
    {
      path: '/admin/groups',
      title: t('route.adminGroups'),
      description: t('admin.nav.groups'),
      icon: 'UserFilled'
    },
    {
      path: '/admin/permissions',
      title: t('route.adminPermissions'),
      description: t('admin.nav.permissions'),
      icon: 'Key'
    },
    { path: '/admin/disks', title: t('route.adminDisks'), description: t('admin.nav.disks'), icon: 'Coin' },
    {
      path: '/admin/system',
      title: t('route.adminSystem'),
      description: t('admin.nav.system'),
      icon: 'Operation'
    },
    {
      path: '/admin/plugins',
      title: t('route.adminPlugins'),
      description: t('admin.nav.plugins'),
      icon: 'Connection'
    },
    {
      path: '/admin/tags',
      title: t('route.adminTags'),
      description: t('admin.nav.tags'),
      icon: 'CollectionTag'
    }
  ])
  const activeAdminComponent = computed(() => {
    if (route.path.endsWith('/groups')) {
      return AdminGroups
    }
    if (route.path.endsWith('/permissions')) {
      return AdminPermissions
    }
    if (route.path.endsWith('/disks')) {
      return AdminDisks
    }
    if (route.path.endsWith('/system')) {
      return AdminSystem
    }
    if (route.path.endsWith('/plugins')) {
      return AdminPlugins
    }
    if (route.path.endsWith('/tags')) {
      return AdminTags
    }
    return AdminUsers
  })
</script>

<style scoped>
  .admin-workspace {
    min-height: 680px;
    display: grid;
    grid-template-columns: 232px minmax(0, 1fr);
    gap: 18px;
  }
  .admin-workspace__nav,
  .admin-workspace__content {
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    background: var(--desktop-surface);
    box-shadow: var(--desktop-shadow-sm);
  }
  .admin-workspace__nav {
    align-self: start;
    padding: 8px;
    display: grid;
    gap: 3px;
  }
  .admin-workspace__nav a {
    min-height: 66px;
    padding: 9px 10px;
    display: grid;
    grid-template-columns: 36px minmax(0, 1fr) 14px;
    align-items: center;
    gap: 9px;
    border-radius: 11px;
    color: var(--text-regular);
    text-decoration: none;
  }
  .admin-workspace__nav a:hover {
    background: var(--desktop-fill);
  }
  .admin-workspace__nav a.router-link-active {
    background: var(--desktop-primary-soft);
    color: var(--primary-color);
  }
  .admin-workspace__nav a > span {
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    border-radius: 10px;
    background: var(--desktop-fill);
  }
  .admin-workspace__nav a > div {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .admin-workspace__nav strong {
    font-size: 12px;
  }
  .admin-workspace__nav small {
    overflow: hidden;
    color: var(--text-secondary);
    font-size: 10px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .admin-workspace__content {
    min-width: 0;
    padding: 20px;
    overflow: hidden;
  }
  .admin-workspace__content :deep(.admin-users),
  .admin-workspace__content :deep(.admin-groups),
  .admin-workspace__content :deep(.admin-permissions),
  .admin-workspace__content :deep(.admin-disks),
  .admin-workspace__content :deep(.admin-system),
  .admin-workspace__content :deep(.plugin-center) {
    height: 100%;
  }
  .admin-workspace__content :deep(.admin-tags) {
    height: 100%;
  }
  @media (max-width: 991px) {
    .admin-workspace {
      grid-template-columns: 68px minmax(0, 1fr);
    }
    .admin-workspace__nav a {
      grid-template-columns: 36px;
      justify-content: center;
      padding-inline: 8px;
    }
    .admin-workspace__nav a > div,
    .admin-workspace__nav a > .el-icon:last-child {
      display: none;
    }
    .admin-workspace__content {
      padding: 14px;
    }
  }

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
  .admin-hub-item:first-child {
    border-top: 0;
  }
  .admin-hub-item > span {
    width: 42px;
    height: 42px;
    display: grid;
    place-items: center;
    border-radius: 14px;
    color: var(--primary-color);
    background: color-mix(in srgb, var(--primary-color) 10%, transparent);
  }
  .admin-hub-item > div {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .admin-hub-item small {
    color: var(--text-secondary);
    font-size: 12px;
  }
  .mobile-admin-content {
    min-height: 100%;
  }
</style>
