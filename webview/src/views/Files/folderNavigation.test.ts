import { describe, expect, it } from 'vitest'
import { cinemaRouteForFolder } from './folderNavigation'

const folder = (cinemaMode: boolean) => ({
  id: 7,
  name: '影视库',
  parent_id: 1,
  absolute_path: '/影视库',
  created_at: '2026-08-04 10:00:00',
  cinema_mode: cinemaMode
})

describe('文件夹影视模式导航', () => {
  it('已标记文件夹进入独立影视路由', () => {
    expect(cinemaRouteForFolder(folder(true))).toBe('/cinema/7')
  })

  it('普通文件夹保留原文件列表导航', () => {
    expect(cinemaRouteForFolder(folder(false))).toBeUndefined()
  })
})
