// @vitest-environment jsdom

import { flushPromises, mount, type GlobalMountOptions } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import FileTagManager from './index.vue'

const api = vi.hoisted(() => ({
  batchUpdateTags: vi.fn(),
  getEnabledTagCategories: vi.fn(),
  getFileTags: vi.fn(),
  getTagSuggestions: vi.fn(),
  retryFileTags: vi.fn(),
  updateManualTags: vi.fn(),
  updateTagExclusions: vi.fn()
}))

vi.mock('@/api/tag', () => api)
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const passthrough = (name: string) => ({ name, template: '<div><slot /><slot name="footer" /></div>' })
const ElDialog = {
  name: 'ElDialog',
  props: ['modelValue'],
  emits: ['update:model-value'],
  template: '<div v-if="modelValue"><slot /><slot name="footer" /></div>'
}
const ElTag = {
  name: 'ElTag',
  emits: ['close'],
  template: '<button class="tag" @click="$emit(\'close\')"><slot /></button>'
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

const globalOptions = {
  config: {
    globalProperties: {
      $modal: { msgError: vi.fn(), msgSuccess: vi.fn() }
    }
  },
  directives: { loading: () => undefined },
  stubs: {
    ElDialog,
    ElAlert: passthrough('ElAlert'),
    ElTag,
    ElButton,
    ElSelect,
    ElOption: passthrough('ElOption'),
    ElEmpty: passthrough('ElEmpty'),
    ElForm: passthrough('ElForm'),
    ElFormItem: passthrough('ElFormItem'),
    ElRadioGroup: passthrough('ElRadioGroup'),
    ElRadioButton: passthrough('ElRadioButton'),
    ElRadio: passthrough('ElRadio'),
    ElSwitch: passthrough('ElSwitch')
  }
} as unknown as GlobalMountOptions

const category = { id: 'other', code: 'other', name: '其他', color: '#909399' }

describe('FileTagManager', () => {
  it('区分自动和手工标签，并可屏蔽自动标签', async () => {
    api.getEnabledTagCategories.mockResolvedValue({ code: 200, data: [category] })
    api.getFileTags.mockResolvedValue({
      code: 200,
      data: {
        file_id: 'uf-1',
        state: 'ready',
        suppressed: [],
        tags: [
          {
            id: 'auto-1',
            name: '自动标签',
            category,
            sources: ['filename'],
            visibility: 'inherit',
            automatic: true
          },
          {
            id: 'manual-1',
            name: '手工标签',
            category,
            sources: ['manual'],
            visibility: 'private',
            automatic: false
          }
        ]
      }
    })
    api.updateTagExclusions.mockResolvedValue({ code: 200, data: {} })
    const wrapper = mount(FileTagManager, {
      props: { modelValue: true, fileIds: ['uf-1'], fileName: '示例.mp4' },
      global: globalOptions
    })
    await flushPromises()

    expect(wrapper.text()).toContain('自动标签')
    expect(wrapper.text()).toContain('手工标签')
    const automatic = wrapper.findAll('.tag').find(item => item.text().includes('自动标签'))
    expect(automatic).toBeTruthy()
    await automatic!.trigger('click')
    await flushPromises()

    expect(api.updateTagExclusions).toHaveBeenCalledWith('uf-1', ['auto-1'], [])
  })

  it('批量模式把手工标签一次提交给全部文件', async () => {
    api.getEnabledTagCategories.mockResolvedValue({ code: 200, data: [category] })
    api.getTagSuggestions.mockResolvedValue({ code: 200, data: [] })
    api.batchUpdateTags.mockResolvedValue({ code: 200, data: null })
    const wrapper = mount(FileTagManager, {
      props: { modelValue: true, fileIds: ['uf-1', 'uf-2'] },
      global: globalOptions
    })
    await flushPromises()

    wrapper.getComponent(ElSelect).vm.$emit('update:model-value', ['待整理'])
    await wrapper.vm.$nextTick()
    const submit = wrapper.findAll('.action').find(item => item.text() === 'tags.addManual')
    expect(submit).toBeTruthy()
    await submit!.trigger('click')
    await flushPromises()

    expect(api.batchUpdateTags).toHaveBeenCalledWith(
      ['uf-1', 'uf-2'],
      [{ name: '待整理', category_id: 'other', visibility: 'private' }]
    )
  })
})
