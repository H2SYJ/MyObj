// @vitest-environment jsdom

import { flushPromises, mount, type GlobalMountOptions } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EditableFileTags from './index.vue'
import type { FileTag, FileTagsData } from '@/api/tag'

const api = vi.hoisted(() => ({
  getEnabledTagCategories: vi.fn(),
  getFileTags: vi.fn(),
  getTagSuggestions: vi.fn(),
  updateManualTags: vi.fn(),
  updateTagExclusions: vi.fn()
}))

vi.mock('@/api/tag', () => api)
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string, values?: { name?: string }) => `${key}${values?.name ? `:${values.name}` : ''}` })
}))

const ElPopover = {
  name: 'ElPopover',
  props: ['visible'],
  emits: ['update:visible', 'show'],
  template: `
    <div class="popover">
      <span class="popover-reference" @click="$emit('update:visible', true); $emit('show')"><slot name="reference" /></span>
      <div class="popover-content"><slot /></div>
    </div>
  `
}
const ElSelect = {
  name: 'ElSelect',
  props: ['modelValue', 'teleported'],
  emits: ['update:modelValue'],
  template: '<div class="select"><slot /></div>'
}
const ElRadioGroup = {
  name: 'ElRadioGroup',
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<div class="radio-group"><slot /></div>'
}
const ElButton = {
  name: 'ElButton',
  emits: ['click'],
  template: '<button class="action" @click="$emit(\'click\')"><slot /></button>'
}
const passthrough = (name: string) => ({ name, template: '<span><slot /></span>' })
const modal = { msgError: vi.fn(), msgSuccess: vi.fn() }
const globalOptions = {
  config: { globalProperties: { $modal: modal } },
  stubs: {
    ElTag: passthrough('ElTag'),
    ElPopover,
    ElSelect,
    ElOption: passthrough('ElOption'),
    ElRadioGroup,
    ElRadioButton: passthrough('ElRadioButton'),
    ElButton,
    ElIcon: passthrough('ElIcon'),
    Close: true,
    Loading: true,
    Plus: true
  }
} as unknown as GlobalMountOptions

const category = { id: 'other', code: 'other', name: '其他', color: '#909399' }
const tag = (overrides: Partial<FileTag> = {}): FileTag => ({
  id: 'tag-1',
  name: '科幻',
  category,
  sources: ['manual'],
  visibility: 'private',
  automatic: false,
  ...overrides
})
const details = (tags: FileTag[], fileId = 'uf-1'): FileTagsData => ({
  file_id: fileId,
  tags,
  suppressed: [],
  state: 'ready'
})
const ok = <T>(data: T) => ({ code: 200, data })

describe('EditableFileTags', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getFileTags.mockResolvedValue(ok(details([tag()])))
    api.getEnabledTagCategories.mockResolvedValue(ok([category]))
    api.getTagSuggestions.mockResolvedValue(ok([]))
    api.updateManualTags.mockResolvedValue(ok(details([])))
    api.updateTagExclusions.mockResolvedValue(ok(details([])))
  })

  it('先显示初始摘要，再用文件标签详情替换', async () => {
    let resolveDetails: (value: unknown) => void = () => undefined
    api.getFileTags.mockReturnValue(new Promise(resolve => (resolveDetails = resolve)))
    const wrapper = mount(EditableFileTags, {
      props: {
        fileId: 'uf-1',
        initialTags: [
          { id: 'preview', name: '预览标签', category_code: 'other', color: '#409eff', visibility: 'private' }
        ]
      },
      global: globalOptions
    })

    expect(wrapper.text()).toContain('预览标签')
    expect(wrapper.find('.editable-file-tags__remove').exists()).toBe(false)

    resolveDetails(ok(details([tag({ name: '完整标签' })])))
    await flushPromises()

    expect(wrapper.text()).toContain('完整标签')
    expect(wrapper.text()).not.toContain('预览标签')
    expect(wrapper.find('.editable-file-tags__remove').exists()).toBe(true)
  })

  it('手工标签删除绑定，自动标签加入屏蔽', async () => {
    const manual = tag({ id: 'manual', name: '手工', sources: ['manual'], automatic: false })
    const automatic = tag({ id: 'auto', name: '自动', sources: ['filename'], automatic: true })
    api.getFileTags
      .mockResolvedValueOnce(ok(details([manual, automatic])))
      .mockResolvedValueOnce(ok(details([automatic])))
      .mockResolvedValueOnce(ok(details([])))
    const wrapper = mount(EditableFileTags, { props: { fileId: 'uf-1' }, global: globalOptions })
    await flushPromises()

    await wrapper.findAll('.editable-file-tags__remove')[0].trigger('click')
    await flushPromises()
    expect(api.updateManualTags).toHaveBeenCalledWith('uf-1', [], ['manual'])
    expect(api.updateTagExclusions).not.toHaveBeenCalled()

    await wrapper.findAll('.editable-file-tags__remove')[0].trigger('click')
    await flushPromises()
    expect(api.updateTagExclusions).toHaveBeenCalledWith('uf-1', ['auto'], [])
  })

  it('混合来源标签同时删除手工绑定并屏蔽自动来源', async () => {
    const mixed = tag({ id: 'mixed', sources: ['manual', 'filename'], automatic: true })
    api.getFileTags.mockResolvedValueOnce(ok(details([mixed]))).mockResolvedValueOnce(ok(details([])))
    const wrapper = mount(EditableFileTags, { props: { fileId: 'uf-1' }, global: globalOptions })
    await flushPromises()

    await wrapper.get('.editable-file-tags__remove').trigger('click')
    await flushPromises()

    expect(api.updateManualTags).toHaveBeenCalledWith('uf-1', [], ['mixed'])
    expect(api.updateTagExclusions).toHaveBeenCalledWith('uf-1', ['mixed'], [])
    expect(wrapper.emitted('updated')?.[0]?.[0]).toEqual(details([]))
  })

  it('部分删除失败后回载服务端状态并报告错误', async () => {
    const mixed = tag({ id: 'mixed', sources: ['manual', 'filename'], automatic: true })
    api.getFileTags.mockResolvedValueOnce(ok(details([mixed]))).mockResolvedValueOnce(ok(details([mixed])))
    api.updateManualTags.mockRejectedValue(new Error('删除手工标签失败'))
    const wrapper = mount(EditableFileTags, { props: { fileId: 'uf-1' }, global: globalOptions })
    await flushPromises()

    await wrapper.get('.editable-file-tags__remove').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('科幻')
    expect(wrapper.emitted('updated')?.[0]?.[0]).toEqual(details([mixed]))
    expect(modal.msgError).toHaveBeenCalledWith('删除手工标签失败')
  })

  it('弹出面板搜索已有标签并按分类与公开性批量添加', async () => {
    const titleCategory = { id: 'title-id', code: 'title', name: '标题', color: '#67c23a' }
    api.getFileTags.mockResolvedValueOnce(ok(details([]))).mockResolvedValueOnce(ok(details([tag()])))
    api.getEnabledTagCategories.mockResolvedValue(ok([category, titleCategory]))
    api.getTagSuggestions.mockResolvedValue(
      ok([{ id: 'movie', name: '电影', category_code: 'title', color: '#67c23a', visibility: 'inherit' }])
    )
    const wrapper = mount(EditableFileTags, { props: { fileId: 'uf-1' }, global: globalOptions })
    await flushPromises()

    await wrapper.get('.editable-file-tags__add').trigger('click')
    await flushPromises()
    const selects = wrapper.findAllComponents(ElSelect)
    expect(selects).toHaveLength(2)
    expect(selects.every(select => select.props('teleported') === false)).toBe(true)
    selects[0].vm.$emit('update:modelValue', ['电影', '  自定义  ', '自定义', ''])
    selects[1].vm.$emit('update:modelValue', 'other')
    wrapper.getComponent(ElRadioGroup).vm.$emit('update:modelValue', 'public')
    await wrapper.vm.$nextTick()
    const actions = wrapper.findAll('.action')
    await actions[actions.length - 1].trigger('click')
    await flushPromises()

    expect(api.updateManualTags).toHaveBeenCalledWith(
      'uf-1',
      [
        { name: '电影', category_id: 'title-id', visibility: 'public' },
        { name: '自定义', category_id: 'other', visibility: 'public' }
      ],
      []
    )
    expect(modal.msgSuccess).toHaveBeenCalledWith('tags.saveSuccess')
  })

  it('文件切换后忽略旧文件的延迟详情响应', async () => {
    let resolveOld: (value: unknown) => void = () => undefined
    api.getFileTags
      .mockReturnValueOnce(new Promise(resolve => (resolveOld = resolve)))
      .mockResolvedValueOnce(ok(details([tag({ id: 'new', name: '新文件标签' })], 'uf-2')))
    const wrapper = mount(EditableFileTags, {
      props: { fileId: 'uf-1', initialTags: [] },
      global: globalOptions
    })

    await wrapper.setProps({
      fileId: 'uf-2',
      initialTags: [
        { id: 'preview-2', name: '新文件预览', category_code: 'other', color: '#409eff', visibility: 'private' }
      ]
    })
    await flushPromises()
    resolveOld(ok(details([tag({ id: 'old', name: '旧文件标签' })], 'uf-1')))
    await flushPromises()

    expect(wrapper.text()).toContain('新文件标签')
    expect(wrapper.text()).not.toContain('旧文件标签')
  })
})
