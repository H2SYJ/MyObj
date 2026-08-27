import { describe, expect, it } from 'vitest'
import type { TagCloudItem } from '@/api/tag'
import { sortTagCloudItems, tagCloudFontSize, tagCloudSizeClass } from './tagCloud'

const item = (id: string, name: string, fileCount: number): TagCloudItem => ({
  id,
  name,
  file_count: fileCount,
  system: false,
  category: { id: 'other', code: 'other', name: '其他', color: '#409eff' }
})

describe('标签云视觉权重', () => {
  it('按文件数、名称和ID稳定排序', () => {
    expect(
      sortTagCloudItems([item('b', '乙', 2), item('c', '甲', 2), item('a', '甲', 2), item('d', '丙', 8)]).map(
        tag => tag.id
      )
    ).toEqual(['d', 'a', 'c', 'b'])
  })

  it('使用对数映射并处理相同计数与极端计数', () => {
    expect(tagCloudFontSize(5, 5, 5, false)).toBe(24)
    expect(tagCloudFontSize(1, 1, 1000, false)).toBe(14)
    expect(tagCloudFontSize(1000, 1, 1000, false)).toBe(34)
    expect(tagCloudFontSize(1000, 1, 1000, true)).toBe(28)
    expect(tagCloudSizeClass(28)).toBe('is-large')
  })
})
