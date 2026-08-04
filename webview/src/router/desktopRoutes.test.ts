// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'
import enUS from '@/i18n/locales/en-US'
import zhCN from '@/i18n/locales/zh-CN'
import { routes } from './index'

vi.mock('@/composables', () => ({ useSEO: () => ({ applySEO: vi.fn() }) }))

const flatten = (items: readonly RouteRecordRaw[]): RouteRecordRaw[] =>
  items.flatMap(item => [item, ...flatten(item.children || [])])

describe('桌面路由元数据', () => {
  it('文件和广场使用各自搜索范围且账户页保持直接访问', () => {
    const byPath = new Map(flatten(routes).map(record => [record.path, record]))
    expect(byPath.get('/files')?.meta?.desktopSearch).toBe('files')
    expect(byPath.get('/square')?.meta?.desktopSearch).toBe('square')
    expect(byPath.get('/me')?.name).toBe('Me')
  })

  it('公共页不要求登录且管理工作区保留管理员权限边界', () => {
    const byPath = new Map(flatten(routes).map(record => [record.path, record]))
    expect(byPath.get('/login')?.meta?.requiresAuth).toBe(false)
    expect(byPath.get('/share/:token')?.meta?.requiresAuth).toBe(false)
    expect(byPath.get('/admin')?.meta?.requiresAdmin).toBe(true)
    expect(['users', 'groups', 'permissions', 'disks', 'system', 'plugins'].every(path => byPath.has(path))).toBe(true)
  })

  it('传输页使用存在的路由翻译键', () => {
    const byPath = new Map(flatten(routes).map(record => [record.path, record]))
    expect(byPath.get('/tasks')?.meta?.i18nKey).toBe('route.tasks')
    expect(zhCN.route.tasks).toBe('传输列表')
    expect(enUS.route.tasks).toBe('Transfer List')
  })

  it('影视模式使用独立鉴权路由并提供首页、目录和播放页', () => {
    const cinema = routes.find(record => record.path === '/cinema/:rootDirectoryId')
    expect(cinema?.meta?.requiresAuth).toBe(true)
    expect(cinema?.children?.map(record => record.path)).toEqual(['', 'folder/:directoryId', 'watch/:fileId'])
    const appLayout = routes.find(record => record.name === 'Layout')
    expect(appLayout?.children?.some(record => String(record.path).startsWith('/cinema'))).toBe(false)
  })
})
