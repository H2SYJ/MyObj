// @vitest-environment jsdom

import { flushPromises, mount, type GlobalMountOptions } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TagDictionary from './TagDictionary.vue'

const api = vi.hoisted(() => ({
  getEnabledTagCategories: vi.fn(),
  getPersonalTagDictionary: vi.fn(),
  previewPersonalTagDictionary: vi.fn(),
  updatePersonalTagDictionary: vi.fn()
}))

vi.mock('@/api/tag', () => api)
vi.mock('@/composables', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const passthrough = (name: string) => ({ name, template: '<div><slot /></div>' })
const ElButton = {
  name: 'ElButton',
  emits: ['click'],
  template: '<button class="action" @click="$emit(\'click\')"><slot /></button>'
}

const mountDictionary = () =>
  mount(TagDictionary, {
    global: {
      config: {
        globalProperties: {
          $modal: { msgError: vi.fn(), msgSuccess: vi.fn() }
        }
      },
      directives: { loading: () => undefined },
      stubs: {
        ElButton,
        ElAlert: passthrough('ElAlert'),
        ElTable: passthrough('ElTable'),
        ElTableColumn: { name: 'ElTableColumn', template: '<div></div>' },
        ElSelect: passthrough('ElSelect'),
        ElOption: passthrough('ElOption'),
        ElInput: passthrough('ElInput'),
        ElInputNumber: passthrough('ElInputNumber'),
        ElSwitch: passthrough('ElSwitch'),
        FileTags: passthrough('FileTags')
      }
    } as unknown as GlobalMountOptions
  })

describe('TagDictionary', () => {
  it('保存个人词典后展示自动重建任务', async () => {
    const rules = [
      {
        id: 'rule-1',
        type: 'word',
        target_field: 'basename',
        pattern: '流浪地球',
        replacement: '',
        category_id: 'title',
        priority: 10,
        weight: 1,
        enabled: true
      }
    ]
    api.getPersonalTagDictionary.mockResolvedValue({ code: 200, data: { rules } })
    api.getEnabledTagCategories.mockResolvedValue({
      code: 200,
      data: [{ id: 'title', code: 'title', name: '标题', color: '#409eff' }]
    })
    api.updatePersonalTagDictionary.mockResolvedValue({
      code: 200,
      data: { rule_set: { rules }, rebuild_job: { id: 'job-1', total: 12 } }
    })
    const wrapper = mountDictionary()
    await flushPromises()

    const save = wrapper.findAll('.action').find(item => item.text() === 'common.save')
    await save!.trigger('click')
    await flushPromises()

    expect(api.updatePersonalTagDictionary).toHaveBeenCalledWith([expect.objectContaining({ pattern: '流浪地球' })])
    expect(wrapper.text()).toContain('settings.tagDictionary.rebuildQueued')
  })

  it('预览时按行提交文件名样例', async () => {
    api.getPersonalTagDictionary.mockResolvedValue({ code: 200, data: { rules: [] } })
    api.getEnabledTagCategories.mockResolvedValue({ code: 200, data: [] })
    api.previewPersonalTagDictionary.mockResolvedValue({ code: 200, data: [] })
    const wrapper = mountDictionary()
    await flushPromises()

    const preview = wrapper.findAll('.action').find(item => item.text() === 'tags.preview')
    await preview!.trigger('click')
    await flushPromises()

    expect(api.previewPersonalTagDictionary).toHaveBeenCalledWith(
      ['流浪地球2.2023.2160p.WEB-DL.H265.国语.mkv', '三体.S01E08.1080p.中英字幕.mp4'],
      []
    )
  })
})
