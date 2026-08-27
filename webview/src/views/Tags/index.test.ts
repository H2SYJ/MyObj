// @vitest-environment jsdom

import { config, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TagCloudItem } from '@/api/tag'
import TagsPage from './index.vue'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  getTagCloud: vi.fn(),
  handheld: { value: false }
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  useResponsive: () => ({ isHandheld: ref(mocks.handheld.value) })
}))
vi.mock('@/api/tag', async importOriginal => {
  const actual = await importOriginal<typeof import('@/api/tag')>()
  return { ...actual, getTagCloud: mocks.getTagCloud }
})

const cloudTag = (id: string, system = false): TagCloudItem => ({
  id,
  name: id,
  file_count: system ? 1 : 3,
  system,
  category: { id: 'other', code: 'other', name: '其他', color: '#409eff' }
})

const mountPage = async () => {
  const wrapper = mount(TagsPage, {
    global: {
      config: {
        globalProperties: {
          $modal: { msgError: vi.fn() }
        } as unknown as typeof config.global.config.globalProperties
      },
      directives: { loading: {} },
      stubs: {
        WorkspacePage: { template: '<div><slot/></div>' },
        ElTooltip: { template: '<div><slot/></div>' },
        ElIcon: { template: '<i><slot/></i>' },
        ElEmpty: true,
        ElButton: { template: '<button @click="$emit(\'click\')"><slot/></button>' },
        CollectionTag: true,
        InfoFilled: true,
        Lock: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('标签云页面交互', () => {
  beforeEach(() => {
    config.global.renderStubDefaultSlot = true
    mocks.push.mockReset()
    mocks.handheld.value = false
    mocks.getTagCloud.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { tags: [cloudTag('normal'), cloudTag('system', true)] }
    })
  })

  it('点击、回车和空格只携带单个标签条件进入文件列表', async () => {
    const wrapper = await mountPage()
    const normal = wrapper.findAll('.tag-cloud-item')[0]

    await normal.trigger('click')
    await normal.trigger('keydown', { key: 'Enter' })
    await normal.trigger('keydown', { key: ' ' })

    expect(mocks.push).toHaveBeenCalledTimes(3)
    expect(mocks.push).toHaveBeenLastCalledWith({ path: '/files', query: { tags: 'normal' } })
  })

  it('标签云仅提供只读标签项，不渲染编辑或隐藏操作', async () => {
    const wrapper = await mountPage()

    expect(wrapper.findAll('.tag-cloud-item')).toHaveLength(2)
    expect(wrapper.find('.tag-context-menu').exists()).toBe(false)
    expect(wrapper.find('.hidden-tags').exists()).toBe(false)
  })
})
