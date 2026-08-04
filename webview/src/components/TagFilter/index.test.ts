// @vitest-environment jsdom

import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TagFilter from './index.vue'

const api = vi.hoisted(() => ({ getTagSuggestions: vi.fn() }))

vi.mock('@/api/tag', () => api)
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const ElSelect = {
  name: 'ElSelect',
  props: ['modelValue'],
  emits: ['update:model-value', 'visible-change'],
  template: '<div class="select"><slot /></div>'
}
const ElOption = {
  name: 'ElOption',
  props: ['value', 'label'],
  template: '<div class="option"><slot /></div>'
}
const ElSegmented = {
  name: 'ElSegmented',
  props: ['modelValue', 'options'],
  emits: ['update:model-value'],
  template: '<div class="segmented"></div>'
}

describe('TagFilter', () => {
  it('加载标签建议并透传标签、匹配模式和目录范围', async () => {
    api.getTagSuggestions.mockResolvedValue({
      code: 200,
      data: [
        { id: 'tag-1', name: '科幻', category_code: 'title', color: '#409eff' },
        { id: 'tag-2', name: '4K', category_code: 'resolution', color: '#67c23a' }
      ]
    })
    const wrapper = mount(TagFilter, {
      props: { modelValue: [], mode: 'all', scope: 'current', showScope: true },
      global: { stubs: { ElSelect, ElOption, ElSegmented } }
    })
    await flushPromises()

    expect(api.getTagSuggestions).toHaveBeenCalledWith('', 50)
    expect(wrapper.findAllComponents(ElOption)).toHaveLength(2)

    wrapper.getComponent(ElSelect).vm.$emit('update:model-value', ['tag-1'])
    const segments = wrapper.findAllComponents(ElSegmented)
    segments[0].vm.$emit('update:model-value', 'any')
    segments[1].vm.$emit('update:model-value', 'all')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['tag-1']])
    expect(wrapper.emitted('update:mode')?.[0]).toEqual(['any'])
    expect(wrapper.emitted('update:scope')?.[0]).toEqual(['all'])
  })

  it('只接受最后一次远程建议请求的结果', async () => {
    let resolveFirst: (value: unknown) => void = () => undefined
    api.getTagSuggestions
      .mockImplementationOnce(() => new Promise(resolve => (resolveFirst = resolve)))
      .mockResolvedValueOnce({
        code: 200,
        data: [{ id: 'new', name: '新结果', category_code: 'other', color: '#409eff' }]
      })
    const wrapper = mount(TagFilter, {
      props: { modelValue: [], mode: 'all' },
      global: { stubs: { ElSelect, ElOption, ElSegmented } }
    })
    const select = wrapper.getComponent(ElSelect)
    select.vm.$emit('visible-change', true)
    await flushPromises()
    resolveFirst({ code: 200, data: [{ id: 'old', name: '旧结果' }] })
    await flushPromises()

    expect(wrapper.text()).toContain('新结果')
    expect(wrapper.text()).not.toContain('旧结果')
  })
})
