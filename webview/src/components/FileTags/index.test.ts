// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import FileTags from './index.vue'

vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const ElTag = {
  name: 'ElTag',
  emits: ['click'],
  template: '<button class="tag" @click="$emit(\'click\', $event)"><slot /></button>'
}

describe('FileTags', () => {
  it('限制摘要数量并显示剩余计数', () => {
    const wrapper = mount(FileTags, {
      props: {
        limit: 2,
        tags: [
          { id: '1', name: '科幻', category_code: 'title', color: '#409eff' },
          { id: '2', name: '4K', category_code: 'resolution', color: '#67c23a' },
          { id: '3', name: '国语', category_code: 'language', color: '#e6a23c' }
        ]
      },
      global: { stubs: { ElTag } }
    })

    expect(wrapper.text()).toContain('科幻')
    expect(wrapper.text()).toContain('4K')
    expect(wrapper.text()).toContain('+1')
    expect(wrapper.text()).not.toContain('国语')
  })

  it('点击标签时向上层传递完整标签', async () => {
    const tag = { id: '1', name: '科幻', category_code: 'title', color: '#409eff' }
    const wrapper = mount(FileTags, {
      props: { tags: [tag] },
      global: { stubs: { ElTag } }
    })

    await wrapper.get('.tag').trigger('click')

    expect(wrapper.emitted('tag-click')?.[0]).toEqual([tag])
  })
})
