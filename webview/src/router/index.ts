import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
// 路由后置守卫：更新文档标题和 SEO
import { useAppStore } from '@/stores/app'
import { useSEO } from '@/composables'

export const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/share/:token',
    name: 'ShareDownload',
    component: () => import('@/views/ShareDownload/index.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    name: 'Layout',
    component: () => import('@/layout/index.vue'),
    redirect: '/files',
    meta: { requiresAuth: true },
    children: [
      {
        path: '/files',
        name: 'Files',
        component: () => import('@/views/Files/index.vue'),
        meta: { title: '我的文件', i18nKey: 'route.files', mobileTab: 'files', mobileSearch: true }
      },
      {
        path: '/shares',
        name: 'Shares',
        component: () => import('@/views/Shares/index.vue'),
        meta: { title: '我的分享', i18nKey: 'route.shares', mobileParent: '/me', hideMobileNav: true }
      },
      {
        path: '/offline',
        name: 'Offline',
        component: () => import('@/views/Offline/index.vue'),
        meta: { title: '离线下载', i18nKey: 'route.offline', mobileTab: 'offline' }
      },
      {
        path: '/subscriptions',
        name: 'Subscriptions',
        component: () => import('@/views/Subscriptions/index.vue'),
        meta: { title: '订阅管理', i18nKey: 'route.subscriptions', mobileParent: '/me', hideMobileNav: true }
      },
      {
        path: '/tasks',
        name: 'Tasks',
        component: () => import('@/views/Tasks/index.vue'),
        meta: { title: '任务中心', i18nKey: 'route.tasks', mobileTab: 'tasks' }
      },
      {
        path: '/trash',
        name: 'Trash',
        component: () => import('@/views/Trash/index.vue'),
        meta: { title: '回收站', i18nKey: 'route.trash', mobileParent: '/me', hideMobileNav: true }
      },
      {
        path: '/square',
        name: 'Square',
        component: () => import('@/views/Square/index.vue'),
        meta: { title: '文件广场', i18nKey: 'route.square', mobileTab: 'square', mobileSearch: true }
      },
      {
        path: '/me',
        name: 'Me',
        component: () => import('@/views/Me/index.vue'),
        meta: { title: '我的', i18nKey: 'route.me', mobileTab: 'me' }
      },
      {
        path: '/settings',
        name: 'Settings',
        component: () => import('@/views/Settings/index.vue'),
        meta: { title: '系统设置', i18nKey: 'route.settings', mobileParent: '/me', hideMobileNav: true }
      },
      {
        path: '/settings/profile',
        name: 'SettingsProfile',
        component: () => import('@/views/Settings/index.vue'),
        meta: {
          title: '个人信息',
          mobileTitle: '个人信息',
          settingSection: 'profile',
          mobileParent: '/settings',
          hideMobileNav: true
        }
      },
      {
        path: '/settings/password',
        name: 'SettingsPassword',
        component: () => import('@/views/Settings/index.vue'),
        meta: {
          title: '修改密码',
          mobileTitle: '修改密码',
          settingSection: 'password',
          mobileParent: '/settings',
          hideMobileNav: true
        }
      },
      {
        path: '/settings/appearance',
        name: 'SettingsAppearance',
        component: () => import('@/views/Settings/index.vue'),
        meta: {
          title: '外观设置',
          mobileTitle: '外观设置',
          settingSection: 'appearance',
          mobileParent: '/settings',
          hideMobileNav: true
        }
      },
      {
        path: '/settings/api-key',
        name: 'SettingsApiKey',
        component: () => import('@/views/Settings/index.vue'),
        meta: {
          title: 'API Key',
          mobileTitle: 'API Key',
          settingSection: 'api-key',
          mobileParent: '/settings',
          hideMobileNav: true
        }
      },
      // 协作功能暂时隐藏
      // {
      //   path: '/collaboration',
      //   name: 'Collaboration',
      //   component: () => import('@/views/Collaboration/index.vue'),
      //   meta: { title: '协作', i18nKey: 'route.collaboration' }
      // },
      {
        path: '/admin',
        name: 'Admin',
        component: () => import('@/views/Admin/index.vue'),
        meta: {
          title: '系统管理',
          i18nKey: 'route.admin',
          requiresAdmin: true,
          mobileParent: '/me',
          hideMobileNav: true
        },
        children: [
          {
            path: 'users',
            name: 'AdminUsers',
            component: () => import('@/views/Admin/Users/index.vue'),
            meta: { title: '用户管理', i18nKey: 'route.adminUsers', mobileParent: '/admin', hideMobileNav: true }
          },
          {
            path: 'groups',
            name: 'AdminGroups',
            component: () => import('@/views/Admin/Groups/index.vue'),
            meta: { title: '组管理', i18nKey: 'route.adminGroups', mobileParent: '/admin', hideMobileNav: true }
          },
          {
            path: 'permissions',
            name: 'AdminPermissions',
            component: () => import('@/views/Admin/Permissions/index.vue'),
            meta: {
              title: '权限管理',
              i18nKey: 'route.adminPermissions',
              mobileParent: '/admin',
              hideMobileNav: true
            }
          },
          {
            path: 'disks',
            name: 'AdminDisks',
            component: () => import('@/views/Admin/Disks/index.vue'),
            meta: { title: '磁盘管理', i18nKey: 'route.adminDisks', mobileParent: '/admin', hideMobileNav: true }
          },
          {
            path: 'system',
            name: 'AdminSystem',
            component: () => import('@/views/Admin/System/index.vue'),
            meta: { title: '系统配置', i18nKey: 'route.adminSystem', mobileParent: '/admin', hideMobileNav: true }
          },
          {
            path: 'plugins',
            name: 'AdminPlugins',
            component: () => import('@/views/Admin/Plugins/index.vue'),
            meta: { title: '插件中心', i18nKey: 'route.adminPlugins', mobileParent: '/admin', hideMobileNav: true }
          }
        ]
      }
    ]
  },
  {
    path: '/redirect',
    component: () => import('@/layout/index.vue'),
    meta: { hidden: true },
    children: [
      {
        path: '/redirect/:path(.*)',
        component: () => import('@/views/Redirect/index.vue'),
        meta: { title: '重定向', i18nKey: 'route.redirect', hidden: true }
      }
    ]
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/files'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth && !authStore.token) {
    next('/login')
  } else if (to.path === '/login' && authStore.token) {
    next('/files')
  } else if (to.meta.requiresAdmin) {
    // 检查管理员权限
    const { useAdmin } = await import('@/composables/business/useAdmin')
    const { isAdmin } = useAdmin()
    if (!isAdmin.value) {
      next('/files')
    } else {
      next()
    }
  } else {
    next()
  }
})

router.afterEach(to => {
  const appStore = useAppStore()
  appStore.updateDocumentTitle()

  // 更新 SEO 信息
  const seo = useSEO({
    title: (to.meta.title as string) || 'MyObj 网盘系统',
    description: (to.meta.description as string) || 'MyObj 网盘系统 - 安全、高效的文件存储和管理平台'
  })
  seo.applySEO()
})

export default router
