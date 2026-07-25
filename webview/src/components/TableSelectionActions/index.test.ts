// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TableSelectionActions from './index.vue'

const mountActions = (mode: 'inline' | 'floating' = 'inline') =>
  mount(TableSelectionActions, {
    props: {
      mode,
      selectedText: '已选择 2 项',
      clearText: '取消选择'
    },
    slots: {
      default: '<button data-test="batch-action">批量删除</button>'
    },
    global: {
      stubs: {
        ElButton: {
          emits: ['click'],
          template: '<button v-bind="$attrs" @click="$emit(\'click\')"><slot /></button>'
        }
      }
    }
  })

describe('TableSelectionActions', () => {
  it('立即渲染顶部选择操作及插槽内容', () => {
    const wrapper = mountActions()

    expect(wrapper.classes()).toContain('table-selection-actions--inline')
    expect(wrapper.text()).toContain('已选择 2 项')
    expect(wrapper.get('[data-test="batch-action"]').text()).toBe('批量删除')
  })

  it('支持移动端悬浮模式', () => {
    const wrapper = mountActions('floating')

    expect(wrapper.classes()).toContain('table-selection-actions--floating')
  })

  it('点击取消选择时发出 clear 事件', async () => {
    const wrapper = mountActions()

    await wrapper.get('[data-test="selection-clear"]').trigger('click')

    expect(wrapper.emitted('clear')).toHaveLength(1)
  })
})
