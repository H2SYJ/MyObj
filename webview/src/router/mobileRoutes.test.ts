// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'
import { routes } from './index'

vi.mock('@/composables', () => ({
  useSEO: () => ({ applySEO: vi.fn() })
}))

const flatten = (items: readonly RouteRecordRaw[]): RouteRecordRaw[] =>
  items.flatMap(item => [item, ...flatten(item.children || [])])

describe('手机路由元数据', () => {
  it('五个根标签与二级页返回目标稳定', () => {
    const records = flatten(routes)
    const byPath = new Map(records.map(record => [record.path, record]))
    expect(['/files', '/offline', '/tasks', '/square', '/me'].map(path => byPath.get(path)?.meta?.mobileTab)).toEqual([
      'files',
      'offline',
      'tasks',
      'square',
      'me'
    ])
    expect(byPath.get('/subscriptions')?.meta).toMatchObject({ mobileParent: '/me', hideMobileNav: true })
    expect(byPath.get('/tags')?.meta).toMatchObject({ mobileParent: '/me', hideMobileNav: true })
    expect(byPath.get('/settings/profile')?.meta).toMatchObject({ mobileParent: '/settings', hideMobileNav: true })
    expect(byPath.get('users')?.meta).toMatchObject({ mobileParent: '/admin', hideMobileNav: true })
  })
})
