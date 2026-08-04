// @vitest-environment jsdom

import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FileSearchInput from './index.vue'

const api = vi.hoisted(() => ({ getTagSuggestions: vi.fn() }))

vi.mock('@/api/tag', () => api)
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const ElIcon = { template: '<span><slot /></span>' }
const ElTag = {
  props: ['closable', 'color'],
  emits: ['close'],
  template: '<span class="tag"><slot /><button class="tag-close" @click="$emit(\'close\')">x</button></span>'
}

const mountSearch = (props: Record<string, unknown> = {}) =>
  mount(FileSearchInput, {
    props: {
      modelValue: '',
      tags: [],
      history: [],
      ...props
    },
    global: { stubs: { ElIcon, ElTag, Search: true, CircleClose: true, Clock: true, Close: true } }
  })

const lastEmission = (events: unknown[][] | undefined) => events?.[events.length - 1]

describe('FileSearchInput', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    api.getTagSuggestions.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('输入井号后防抖模糊搜索已有标签', async () => {
    api.getTagSuggestions.mockResolvedValue({ code: 200, data: [] })
    const wrapper = mountSearch({ scope: 'public' })
    const input = wrapper.get('input')
    await input.trigger('focus')
    await input.setValue('电影 #科')

    expect(api.getTagSuggestions).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(200)
    await flushPromises()

    expect(api.getTagSuggestions).toHaveBeenCalledWith({ keyword: '科', scope: 'public', limit: 50 })
  })

  it('选择建议只生成标签胶囊，不立即提交搜索', async () => {
    api.getTagSuggestions.mockResolvedValue({
      code: 200,
      data: [{ id: 'tag-1', name: '科幻', category_code: 'title', color: '#409eff', visibility: 'inherit' }]
    })
    const wrapper = mountSearch({ modelValue: '#科' })
    const input = wrapper.get('input')
    await input.trigger('focus')
    await vi.advanceTimersByTimeAsync(200)
    await flushPromises()
    await wrapper.get('.file-search-input__option').trigger('click')

    expect(lastEmission(wrapper.emitted('update:modelValue'))).toEqual([''])
    expect(lastEmission(wrapper.emitted('update:tags'))?.[0]).toEqual([
      { id: 'tag-1', name: '科幻', category_code: 'title', color: '#409eff', visibility: 'inherit' }
    ])
    expect(wrapper.emitted('submit')).toBeUndefined()
  })

  it('回车提交普通关键词和已选标签', async () => {
    const tag = { id: 'tag-1', name: '科幻', category_code: 'title', color: '#409eff', visibility: 'inherit' }
    const wrapper = mountSearch({ modelValue: '电影', tags: [tag] })
    await wrapper.get('input').trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('submit')?.[0]).toEqual([{ keyword: '电影', tags: [tag] }])
  })

  it('标签下拉打开时回车选择标签，普通历史下拉不拦截搜索提交', async () => {
    api.getTagSuggestions.mockResolvedValue({
      code: 200,
      data: [{ id: 'tag-1', name: '科幻', category_code: 'title', color: '', visibility: 'inherit' }]
    })
    const wrapper = mountSearch({ modelValue: '#科' })
    const input = wrapper.get('input')
    await input.trigger('focus')
    await vi.advanceTimersByTimeAsync(200)
    await flushPromises()
    await input.trigger('keydown', { key: 'Enter' })
    expect(lastEmission(wrapper.emitted('update:tags'))?.[0]).toHaveLength(1)
    expect(wrapper.emitted('submit')).toBeUndefined()

    const historyWrapper = mountSearch({ modelValue: '电影', history: ['电影历史'] })
    await historyWrapper.get('input').trigger('focus')
    await historyWrapper.get('input').trigger('keydown', { key: 'Enter' })
    expect(historyWrapper.emitted('submit')?.[0]).toEqual([{ keyword: '电影', tags: [] }])
  })

  it('删除标签不提交，清空按钮立即发出清空事件', async () => {
    const tag = { id: 'tag-1', name: '科幻', category_code: 'title', color: '#409eff', visibility: 'inherit' }
    const wrapper = mountSearch({ modelValue: '电影', tags: [tag] })
    await wrapper.get('.tag-close').trigger('click')
    expect(lastEmission(wrapper.emitted('update:tags'))).toEqual([[]])
    expect(wrapper.emitted('submit')).toBeUndefined()

    await wrapper.get('.file-search-input__clear').trigger('click')
    expect(lastEmission(wrapper.emitted('update:modelValue'))).toEqual([''])
    expect(lastEmission(wrapper.emitted('update:tags'))).toEqual([[]])
    expect(wrapper.emitted('clear')).toHaveLength(1)
  })

  it('空关键词按退格删除最后一个标签', async () => {
    const tags = [
      { id: 'tag-1', name: '科幻', category_code: 'title', color: '', visibility: 'inherit' },
      { id: 'tag-2', name: '4K', category_code: 'resolution', color: '', visibility: 'inherit' }
    ]
    const wrapper = mountSearch({ tags })
    await wrapper.get('input').trigger('keydown', { key: 'Backspace' })
    expect(lastEmission(wrapper.emitted('update:tags'))).toEqual([[tags[0]]])
    expect(wrapper.emitted('submit')).toBeUndefined()
  })

  it('只接受最后一次标签建议请求结果', async () => {
    let resolveFirst: (value: unknown) => void = () => undefined
    api.getTagSuggestions
      .mockImplementationOnce(() => new Promise(resolve => (resolveFirst = resolve)))
      .mockResolvedValueOnce({
        code: 200,
        data: [{ id: 'new', name: '新结果', category_code: 'other', color: '', visibility: 'inherit' }]
      })
    const wrapper = mountSearch({ modelValue: '#' })
    const input = wrapper.get('input')
    await input.trigger('focus')
    await vi.advanceTimersByTimeAsync(200)
    await wrapper.setProps({ modelValue: '#新' })
    input.element.setSelectionRange(2, 2)
    await input.trigger('input')
    await vi.advanceTimersByTimeAsync(200)
    await flushPromises()
    resolveFirst({
      code: 200,
      data: [{ id: 'old', name: '旧结果', category_code: 'other', color: '', visibility: 'inherit' }]
    })
    await flushPromises()

    expect(wrapper.text()).toContain('新结果')
    expect(wrapper.text()).not.toContain('旧结果')
  })
})
