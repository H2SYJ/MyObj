// @vitest-environment jsdom

import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CinemaVideoCard from './CinemaVideoCard.vue'

const getThumbnail = vi.hoisted(() => vi.fn())
vi.mock('@/api/file', () => ({ getThumbnail }))
vi.mock('@element-plus/icons-vue', () => ({
  Lock: { template: '<span />' },
  VideoPlay: { template: '<span class="video-placeholder" />' }
}))

const video = {
  file_id: 'video-1',
  file_name: '封面损坏.mp4',
  file_size: 100,
  mime_type: 'video/mp4',
  is_enc: false,
  has_thumbnail: true,
  created_at: '2026-08-04T00:00:00Z',
  directory: { id: 1, name: '影视库', parent_id: 0, path: '影视库' },
  tags: []
}

describe('CinemaVideoCard', () => {
  it('缩略图加载失败时保留视频占位图', async () => {
    getThumbnail.mockRejectedValue(new Error('缩略图不可用'))
    const wrapper = mount(CinemaVideoCard, {
      props: { video },
      global: { stubs: { ElIcon: { template: '<span class="placeholder"><slot /></span>' }, VideoPlay: true } }
    })
    await flushPromises()

    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('.placeholder').exists()).toBe(true)
    wrapper.unmount()
  })
})
