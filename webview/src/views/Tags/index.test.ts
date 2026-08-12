// @vitest-environment jsdom

import { config, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TagCloudItem } from '@/api/tag'
import TagsPage from './index.vue'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  getTagCloud: vi.fn(),
  getTagCloudItem: vi.fn(),
  getEnabledTagCategories: vi.fn(),
  updateTagCloudItem: vi.fn(),
  handheld: { value: false }
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  useResponsive: () => ({ isHandheld: ref(mocks.handheld.value) })
}))
vi.mock('@/api/tag', async importOriginal => {
  const actual = await importOriginal<typeof import('@/api/tag')>()
  return {
    ...actual,
    getTagCloud: mocks.getTagCloud,
    getTagCloudItem: mocks.getTagCloudItem,
    getEnabledTagCategories: mocks.getEnabledTagCategories,
    hideTagCloudItem: vi.fn(),
    restoreTagCloudItem: vi.fn(),
    updateTagCloudItem: mocks.updateTagCloudItem
  }
})

const cloudTag = (id: string, system = false): TagCloudItem => ({
  id,
  name: id,
  base_name: id,
  file_count: system ? 1 : 3,
  hidden: false,
  system,
  category: { id: 'other', code: 'other', name: '其他', color: '#409eff' },
  base_category: { id: 'other', code: 'other', name: '其他', color: '#409eff' }
})

const mountPage = async () => {
  const modal = {
    msg: vi.fn(),
    msgError: vi.fn(),
    msgSuccess: vi.fn(),
    msgWarning: vi.fn(),
    alert: vi.fn(),
    alertError: vi.fn(),
    alertSuccess: vi.fn(),
    alertWarning: vi.fn(),
    notify: vi.fn(),
    notifyError: vi.fn(),
    notifySuccess: vi.fn(),
    notifyWarning: vi.fn(),
    confirm: vi.fn(),
    prompt: vi.fn(),
    loading: vi.fn(),
    closeLoading: vi.fn()
  }
  const wrapper = mount(TagsPage, {
    global: {
      config: {
        globalProperties: { $modal: modal } as unknown as typeof config.global.config.globalProperties
      },
      directives: { loading: {} },
      stubs: {
        WorkspacePage: { template: '<div><slot/><slot name="overlays"/></div>' },
        ElTooltip: { template: '<div><slot/></div>' },
        ElIcon: { template: '<i><slot/></i>' },
        ElCollapse: { template: '<div><slot/></div>' },
        ElCollapseItem: { template: '<div><slot name="title"/><slot/></div>' },
        ElDialog: { template: '<div v-if="modelValue"><slot/><slot name="footer"/></div>', props: ['modelValue'] },
        ElEmpty: true,
        ElForm: { template: '<form><slot/></form>' },
        ElFormItem: { template: '<label><slot/></label>' },
        ElInput: {
          template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
          props: ['modelValue']
        },
        ElSelect: true,
        ElOption: true,
        ElButton: { template: '<button @click="$emit(\'click\')"><slot/></button>' },
        MobileActionSheet: true,
        CollectionTag: true,
        InfoFilled: true,
        Lock: true,
        MoreFilled: true,
        Hide: true,
        Edit: true
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
      data: { tags: [cloudTag('normal'), cloudTag('system', true)], hidden: [] }
    })
    mocks.getTagCloudItem.mockReset()
    mocks.getEnabledTagCategories.mockReset()
    mocks.updateTagCloudItem.mockReset()
  })

  it('点击和回车只携带单个标签条件进入文件第一页', async () => {
    const wrapper = await mountPage()
    const normal = wrapper.findAll('.tag-cloud-item')[0]

    await normal.trigger('click')
    expect(mocks.push).toHaveBeenLastCalledWith({ path: '/files', query: { tags: 'normal' } })

    await normal.trigger('keydown', { key: 'Enter' })
    expect(mocks.push).toHaveBeenCalledTimes(2)
  })

  it('普通标签支持右键和键盘菜单，系统标签保持只读', async () => {
    const wrapper = await mountPage()
    const [normal, system] = wrapper.findAll('.tag-cloud-item')

    await normal.trigger('contextmenu', { clientX: 20, clientY: 30 })
    expect(wrapper.find('.tag-context-menu').exists()).toBe(true)

    document.dispatchEvent(new MouseEvent('click'))
    await wrapper.vm.$nextTick()
    await system.trigger('contextmenu', { clientX: 20, clientY: 30 })
    expect(wrapper.find('.tag-context-menu').exists()).toBe(false)

    await wrapper.findAll('.tag-cloud-item')[0].trigger('keydown', { key: 'ContextMenu' })
    expect(wrapper.find('.tag-context-menu').exists()).toBe(true)
  })

  it('编辑时提交当前用户的显示名称', async () => {
    const tag = cloudTag('normal')
    mocks.getTagCloudItem.mockResolvedValue({ code: 200, message: 'ok', data: { tag, aliases: [] } })
    mocks.getEnabledTagCategories.mockResolvedValue({ code: 200, message: 'ok', data: [tag.category] })
    mocks.updateTagCloudItem.mockResolvedValue({ code: 200, message: 'ok', data: { editor: { tag, aliases: [] } } })
    const wrapper = await mountPage()

    await wrapper.findAll('.tag-cloud-item')[0].trigger('contextmenu', { clientX: 20, clientY: 30 })
    await wrapper.find('.tag-context-menu button').trigger('click')
    await flushPromises()
    await wrapper.find('input').setValue('个人名称')
    await wrapper
      .findAll('button')
      .find(button => button.text() === 'common.save')!
      .trigger('click')
    await flushPromises()

    expect(mocks.updateTagCloudItem).toHaveBeenCalledWith('normal', '个人名称', 'other', [])
  })
})
