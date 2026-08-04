// @vitest-environment jsdom

import { flushPromises, mount, type GlobalMountOptions } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DirectoryTagManager from './index.vue'

const api = vi.hoisted(() => ({
  getDirectoryTags: vi.fn(),
  getEnabledTagCategories: vi.fn(),
  getTagSuggestions: vi.fn(),
  updateDirectoryTags: vi.fn()
}))

vi.mock('@/api/tag', () => api)

const passthrough = (name: string) => ({ name, template: '<div><slot /><slot name="footer" /></div>' })
const ElDialog = {
  name: 'ElDialog',
  props: ['modelValue'],
  template: '<div v-if="modelValue"><slot /><slot name="footer" /></div>'
}
const ElButton = {
  name: 'ElButton',
  emits: ['click'],
  template: '<button class="action" @click="$emit(\'click\')"><slot /></button>'
}
const ElSelect = {
  name: 'ElSelect',
  props: ['modelValue'],
  emits: ['update:model-value'],
  template: '<div class="select"><slot /></div>'
}

const modal = { msgError: vi.fn(), msgSuccess: vi.fn() }
const globalOptions = {
  config: { globalProperties: { $modal: modal, $log: { error: vi.fn() } } },
  directives: { loading: () => undefined },
  stubs: {
    ElDialog,
    ElButton,
    ElSelect,
    ElTag: passthrough('ElTag'),
    ElOption: passthrough('ElOption'),
    ElEmpty: passthrough('ElEmpty'),
    ElForm: passthrough('ElForm'),
    ElFormItem: passthrough('ElFormItem')
  }
} as unknown as GlobalMountOptions

const categories = [
  { id: 'other', code: 'other', name: '其他', color: '#909399' },
  { id: 'title', code: 'title', name: '标题', color: '#409eff' }
]

describe('DirectoryTagManager', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getEnabledTagCategories.mockResolvedValue({ code: 200, data: categories })
    api.getDirectoryTags.mockResolvedValue({ code: 200, data: { directory_id: 7, tags: [] } })
    api.getTagSuggestions.mockResolvedValue({
      code: 200,
      data: [
        {
          id: 'cinema',
          name: '影视模式',
          category_code: 'other',
          color: '#909399',
          visibility: 'private',
          system_code: 'cinema_mode'
        }
      ]
    })
  })

  it('选择内置影视标签时固定提交系统标签分类', async () => {
    api.updateDirectoryTags.mockResolvedValue({ code: 200, data: { directory_id: 7, tags: [] } })
    const wrapper = mount(DirectoryTagManager, {
      props: { modelValue: true, directoryId: 7, directoryName: '影视库' },
      global: globalOptions
    })
    await flushPromises()

    const selects = wrapper.findAllComponents(ElSelect)
    selects[0].vm.$emit('update:model-value', ['影视模式'])
    selects[1].vm.$emit('update:model-value', 'title')
    await wrapper.vm.$nextTick()
    await wrapper
      .findAll('.action')
      .find(button => button.text() === '添加标签')!
      .trigger('click')
    await flushPromises()

    expect(api.updateDirectoryTags).toHaveBeenCalledWith(7, [{ name: '影视模式', category_id: 'other' }], [])
    expect(modal.msgSuccess).toHaveBeenCalled()
  })

  it('业务失败时不发送成功提示', async () => {
    api.updateDirectoryTags.mockResolvedValue({ code: 400, message: '每个文件夹最多允许100个手工标签' })
    const wrapper = mount(DirectoryTagManager, {
      props: { modelValue: true, directoryId: 7, directoryName: '影视库' },
      global: globalOptions
    })
    await flushPromises()

    wrapper.findAllComponents(ElSelect)[0].vm.$emit('update:model-value', ['新标签'])
    await wrapper.vm.$nextTick()
    await wrapper
      .findAll('.action')
      .find(button => button.text() === '添加标签')!
      .trigger('click')
    await flushPromises()

    expect(modal.msgError).toHaveBeenCalledWith('每个文件夹最多允许100个手工标签')
    expect(modal.msgSuccess).not.toHaveBeenCalled()
  })
})
