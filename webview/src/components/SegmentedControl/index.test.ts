// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SegmentedControl from './index.vue'

const items = [
  { value: 'all', label: '全部' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频', disabled: true }
]

describe('SegmentedControl', () => {
  it('渲染选中项并在点击时更新', async () => {
    const wrapper = mount(SegmentedControl, {
      props: { modelValue: 'all', items, ariaLabel: '文件类型' },
      global: { stubs: { ElIcon: { template: '<i><slot /></i>' } } }
    })

    expect(wrapper.get('[role="radiogroup"]').attributes('aria-label')).toBe('文件类型')
    expect(wrapper.get('[role="radio"]').attributes('aria-checked')).toBe('true')

    await wrapper.findAll('button')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['image']])
    expect(wrapper.emitted('change')).toEqual([['image']])
  })

  it('支持方向键导航并跳过禁用项', async () => {
    const wrapper = mount(SegmentedControl, {
      props: { modelValue: 'image', items, ariaLabel: '文件类型' },
      attachTo: document.body,
      global: { stubs: { ElIcon: { template: '<i><slot /></i>' } } }
    })

    await wrapper.findAll('button')[1].trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('update:modelValue')).toEqual([['all']])
    expect(document.activeElement).toBe(wrapper.findAll('button')[0].element)
    wrapper.unmount()
  })
})
