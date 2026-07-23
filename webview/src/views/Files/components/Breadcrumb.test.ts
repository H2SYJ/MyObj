// @vitest-environment jsdom

import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import Breadcrumb from './Breadcrumb.vue'

vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const homeBreadcrumb = { id: 0, name: 'home', absolute_path: '/' }
const childBreadcrumb = { id: 1, name: 'video', absolute_path: '/video' }

describe('Files Breadcrumb', () => {
  it('始终保留返回按钮宽度，避免路径切换时面包屑横向移动', async () => {
    const wrapper = shallowMount(Breadcrumb, {
      props: {
        breadcrumbs: [homeBreadcrumb],
        formatBreadcrumbName: name => name
      },
      global: {
        stubs: {
          ElButton: { template: '<button><slot /></button>' },
          ElBreadcrumb: { template: '<nav><slot /></nav>' },
          ElBreadcrumbItem: { template: '<span><slot /></span>' },
          ElIcon: { template: '<i><slot /></i>' },
          House: true,
          Folder: true
        }
      }
    })

    const rootBackButton = wrapper.get('.nav-button')
    expect(rootBackButton.classes()).toContain('is-placeholder')
    expect(rootBackButton.attributes('aria-hidden')).toBe('true')
    expect(rootBackButton.attributes('tabindex')).toBe('-1')

    await rootBackButton.trigger('click')
    expect(wrapper.emitted('navigate')).toBeUndefined()

    await wrapper.setProps({ breadcrumbs: [homeBreadcrumb, childBreadcrumb] })

    const childBackButton = wrapper.get('.nav-button')
    expect(childBackButton.classes()).not.toContain('is-placeholder')
    expect(childBackButton.attributes('aria-hidden')).toBe('false')
    expect(childBackButton.attributes('tabindex')).toBe('0')

    await childBackButton.trigger('click')
    expect(wrapper.emitted('navigate')).toEqual([[0]])
    expect(wrapper.emitted('go-back')).toHaveLength(1)
  })
})
