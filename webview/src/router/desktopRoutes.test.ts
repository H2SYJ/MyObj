// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'
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
})
