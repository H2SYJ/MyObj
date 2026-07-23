// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useResponsive } from './useResponsive'

const setViewport = (width: number, coarse = false) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn().mockReturnValue({ matches: coarse })
  })
  window.dispatchEvent(new Event('resize'))
}

describe('useResponsive', () => {
  afterEach(() => vi.restoreAllMocks())

  it('区分手机、紧凑桌面和标准桌面，指针类型不影响布局', async () => {
    setViewport(390, true)
    const wrapper = mount(
      defineComponent({
        setup() {
          return { responsive: useResponsive() }
        },
        render: () => h('div')
      })
    )
    expect(wrapper.vm.responsive.isHandheld.value).toBe(true)
    expect(wrapper.vm.responsive.isCompactDesktop.value).toBe(false)
    expect(wrapper.vm.responsive.hasCoarsePointer.value).toBe(true)

    setViewport(820, true)
    await nextTick()
    expect(wrapper.vm.responsive.isHandheld.value).toBe(false)
    expect(wrapper.vm.responsive.isCompactDesktop.value).toBe(true)

    setViewport(1280, false)
    await nextTick()
    expect(wrapper.vm.responsive.isHandheld.value).toBe(false)
    expect(wrapper.vm.responsive.isCompactDesktop.value).toBe(false)
    expect(wrapper.vm.responsive.isDesktop.value).toBe(true)
    wrapper.unmount()
  })
})
