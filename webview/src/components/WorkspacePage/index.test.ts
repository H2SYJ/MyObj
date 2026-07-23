// @vitest-environment jsdom

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import WorkspacePage from './index.vue'

describe('WorkspacePage', () => {
  it('按照统一层级渲染页面内容', () => {
    const wrapper = mount(WorkspacePage, {
      props: { title: '页面标题', description: '页面说明' },
      slots: {
        icon: '<span data-test="icon">I</span>',
        meta: '<span data-test="meta">共 2 项</span>',
        'header-extra': '<span data-test="header-extra">已选择 1 项</span>',
        actions: '<button data-test="action">刷新</button>',
        toolbar: '<div data-test="toolbar">筛选</div>',
        default: '<div data-test="content">内容</div>',
        footer: '<div data-test="footer">分页</div>',
        floating: '<button data-test="floating">新建</button>',
        overlays: '<div data-test="overlay">弹窗</div>'
      }
    })

    expect(wrapper.get('h2').text()).toBe('页面标题')
    expect(wrapper.get('.workspace-page__description').text()).toBe('页面说明')
    expect(wrapper.get('.workspace-page__header [data-test="meta"]').text()).toBe('共 2 项')
    expect(wrapper.get('.workspace-page__header-extra').text()).toContain('已选择 1 项')
    expect(wrapper.get('.workspace-page__actions').text()).toBe('刷新')
    expect(wrapper.get('.workspace-page__toolbar').text()).toBe('筛选')
    expect(wrapper.get('.workspace-page__content').text()).toBe('内容')
    expect(wrapper.get('.workspace-page__footer').text()).toBe('分页')
    expect(wrapper.get('[data-test="floating"]').text()).toBe('新建')
    expect(wrapper.get('[data-test="overlay"]').text()).toBe('弹窗')
  })

  it('未提供可选内容时不渲染对应区域', () => {
    const wrapper = mount(WorkspacePage, {
      props: { title: '页面标题' },
      slots: { default: '<div>内容</div>' }
    })

    expect(wrapper.find('.workspace-page__description').exists()).toBe(false)
    expect(wrapper.find('.workspace-page__actions').exists()).toBe(false)
    expect(wrapper.find('.workspace-page__toolbar').exists()).toBe(false)
    expect(wrapper.find('.workspace-page__footer').exists()).toBe(false)
  })
})
