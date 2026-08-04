import { describe, expect, it } from 'vitest'
import { resolveOfflineTaskResultNavigation } from './taskResultNavigation'

describe('resolveOfflineTaskResultNavigation', () => {
  it('为已完成任务生成按最终文件名查询的路由', () => {
    expect(resolveOfflineTaskResultNavigation({ state: 3, file_name: '  示例视频.mp4  ' })).toEqual({
      path: '/files',
      query: { search: '示例视频.mp4' }
    })
  })

  it.each([0, 1, 2, 4, 5])('状态为 %s 时不生成跳转路由', state => {
    expect(resolveOfflineTaskResultNavigation({ state, file_name: '示例视频.mp4' })).toBeNull()
  })

  it('已完成任务缺少有效文件名时不生成跳转路由', () => {
    expect(resolveOfflineTaskResultNavigation({ state: 3, file_name: '   ' })).toBeNull()
  })
})
