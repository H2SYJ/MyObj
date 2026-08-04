// @vitest-environment jsdom

import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { extractSearchHistoryKeyword, parseSearchTagIds, useFileSearchDraft } from './useFileSearchDraft'

const mocks = vi.hoisted(() => ({
  route: { path: '/files', query: {} as Record<string, unknown> },
  replace: vi.fn(),
  getTagSuggestions: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.replace })
}))
vi.mock('@/api/tag', () => ({ getTagSuggestions: mocks.getTagSuggestions }))

const Harness = defineComponent({
  setup() {
    return useFileSearchDraft(ref('user'))
  },
  template: '<div />'
})

describe('useFileSearchDraft', () => {
  beforeEach(() => {
    mocks.route.path = '/files'
    mocks.route.query = {}
    mocks.replace.mockReset()
    mocks.getTagSuggestions.mockReset()
  })

  it('解析并去重路由中的标签ID', () => {
    expect(parseSearchTagIds([' tag-1 ', 'tag-2,tag-1'])).toEqual(['tag-1', 'tag-2'])
  })

  it('搜索历史排除临时井号片段并保留普通关键词', () => {
    expect(extractSearchHistoryKeyword('电影 #科幻 2026')).toBe('电影 2026')
    expect(extractSearchHistoryKeyword('#未选择')).toBe('')
    expect(extractSearchHistoryKeyword('C# 教程')).toBe('C# 教程')
  })

  it('按ID回填标签并清理无效标签及旧筛选参数', async () => {
    mocks.route.query = { search: '电影', tags: 'tag-1,missing', tagMode: 'all', tagScope: 'current' }
    mocks.getTagSuggestions.mockResolvedValue({
      code: 200,
      data: [{ id: 'tag-1', name: '科幻', category_code: 'title', color: '', visibility: 'inherit' }]
    })

    const wrapper = mount(Harness)
    await flushPromises()

    expect(mocks.getTagSuggestions).toHaveBeenCalledWith({ tagIds: ['tag-1', 'missing'], scope: 'user', limit: 2 })
    expect(wrapper.vm.keyword).toBe('电影')
    expect(wrapper.vm.tags).toEqual([
      { id: 'tag-1', name: '科幻', category_code: 'title', color: '', visibility: 'inherit' }
    ])
    expect(mocks.replace).toHaveBeenCalledWith({
      path: '/files',
      query: { search: '电影', tags: 'tag-1', tagMode: undefined, tagScope: undefined }
    })
  })

  it('回填请求失败时保留标签ID，避免静默丢失筛选条件', async () => {
    mocks.route.query = { tags: 'tag-1' }
    mocks.getTagSuggestions.mockRejectedValue(new Error('network'))

    const wrapper = mount(Harness)
    await flushPromises()

    expect(wrapper.vm.tags).toEqual([{ id: 'tag-1', name: 'tag-1', category_code: '', color: '', visibility: '' }])
    expect(mocks.replace).not.toHaveBeenCalled()
  })
})
