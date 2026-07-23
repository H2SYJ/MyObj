import { describe, expect, it } from 'vitest'
import { resolveDesktopSearchNavigation } from './routeSearch'

describe('resolveDesktopSearchNavigation', () => {
  it('在支持搜索的页面保留筛选条件并清除分页', () => {
    expect(resolveDesktopSearchNavigation('/square', { page: '4', type: 'video' }, 'square', '  电影  ')).toEqual({
      path: '/square',
      query: { page: undefined, type: 'video', search: '电影' }
    })
  })

  it('从非搜索页面跳转到文件搜索且空关键词可恢复', () => {
    expect(resolveDesktopSearchNavigation('/settings', { section: 'profile' }, undefined, '   ')).toEqual({
      path: '/files',
      query: { search: undefined, page: undefined }
    })
  })
})
