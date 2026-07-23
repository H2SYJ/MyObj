// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useMobileLayerHistory } from './useMobileLayerHistory'

describe('useMobileLayerHistory', () => {
  it('浏览器返回优先关闭最上层', async () => {
    history.replaceState({}, '')
    const wrapper = mount(
      defineComponent({
        setup() {
          const open = ref(false)
          useMobileLayerHistory(open, 'test-layer')
          return { open }
        },
        render: () => h('div')
      })
    )
    wrapper.vm.open = true
    await nextTick()
    expect(history.state.__myobjMobileLayer).toBe('test-layer')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await nextTick()
    expect(wrapper.vm.open).toBe(false)
    wrapper.unmount()
  })
})
