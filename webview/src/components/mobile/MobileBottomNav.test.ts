// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import MobileBottomNav from './MobileBottomNav.vue'

describe('MobileBottomNav', () => {
  it('展示五个根标签并跟随路由激活', async () => {
    const paths = ['/files', '/offline', '/tasks', '/square', '/me']
    const router = createRouter({
      history: createMemoryHistory(),
      routes: paths.map(path => ({ path, component: { template: '<div />' } }))
    })
    await router.push('/tasks')
    await router.isReady()
    const wrapper = mount(MobileBottomNav, {
      props: { items: paths.map(path => ({ key: path, label: path.slice(1), icon: 'Document', path })) },
      global: { plugins: [router], stubs: { ElIcon: { template: '<i><slot /></i>' } } }
    })
    expect(wrapper.findAll('a')).toHaveLength(5)
    expect(wrapper.get('a[href="/tasks"]').classes()).toContain('router-link-active')
  })
})
