// @vitest-environment jsdom

import { flushPromises, mount, type GlobalMountOptions } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import Watch from './Watch.vue'

const api = vi.hoisted(() => ({
  createVideoPlayPrecheck: vi.fn(),
  getCinemaVideo: vi.fn(),
  getRelatedCinemaVideos: vi.fn(),
  getThumbnail: vi.fn(),
  prompt: vi.fn()
}))
const route = reactive({ params: { rootDirectoryId: '7', fileId: 'video-1' } })
const router = { push: vi.fn(), replace: vi.fn() }
const observedElements: Element[] = []

vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }))
vi.mock('@/api/cinema', () => ({
  getCinemaVideo: api.getCinemaVideo,
  getRelatedCinemaVideos: api.getRelatedCinemaVideos
}))
vi.mock('@/api/file', () => ({ getThumbnail: api.getThumbnail }))
vi.mock('@/api/video', () => ({
  createVideoPlayPrecheck: api.createVideoPlayPrecheck,
  getVideoStreamUrl: (playToken: string) => `/video/stream?play_token=${playToken}`
}))
vi.mock('@/plugins/cache', () => ({ default: { local: { get: () => 'token' } } }))
vi.mock('element-plus', () => ({ ElMessageBox: { prompt: api.prompt } }))
vi.mock('@/components/XgPlayer/index.vue', () => ({
  default: { name: 'XgPlayer', props: ['src'], template: '<div class="xg-player-stub" />' }
}))
vi.mock('@/components/EditableFileTags/index.vue', () => ({
  default: {
    name: 'EditableFileTags',
    props: ['fileId', 'initialTags'],
    emits: ['updated'],
    template: '<div class="editable-file-tags-stub" />'
  }
}))
vi.mock('./components/CinemaVideoCard.vue', () => ({
  default: { name: 'CinemaVideoCard', template: '<div class="cinema-video-card-stub" />' }
}))

class IntersectionObserverMock {
  observe(element: Element) {
    observedElements.push(element)
  }
  unobserve() {}
  disconnect() {}
}

const passthrough = (name: string) => ({ name, template: '<div><slot /></div>' })
const modal = { msgError: vi.fn() }
const globalOptions = {
  config: { globalProperties: { $modal: modal, $log: { error: vi.fn() } } },
  directives: { loading: () => undefined },
  stubs: {
    XgPlayer: { name: 'XgPlayer', props: ['src'], template: '<div class="xg-player-stub" />' },
    CinemaVideoCard: passthrough('CinemaVideoCard'),
    EditableFileTags: {
      name: 'EditableFileTags',
      props: ['fileId', 'initialTags'],
      emits: ['updated'],
      template: '<div class="editable-file-tags-stub" />'
    },
    ElEmpty: passthrough('ElEmpty'),
    ElIcon: passthrough('ElIcon'),
    VideoPlay: true,
    Loading: true,
    Lock: true
  }
} as unknown as GlobalMountOptions

describe('Cinema Watch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    observedElements.length = 0
    route.params.rootDirectoryId = '7'
    route.params.fileId = 'video-1'
    vi.stubGlobal('IntersectionObserver', IntersectionObserverMock)
    api.getCinemaVideo.mockResolvedValue({
      code: 200,
      data: {
        root: { id: 7, name: '影视库', parent_id: 1, path: '影视库' },
        video: {
          file_id: 'video-1',
          file_name: '加密影片.mp4',
          file_size: 1024,
          mime_type: 'video/mp4',
          is_enc: true,
          has_thumbnail: true,
          created_at: '2026-08-04T00:00:00Z',
          directory: { id: 7, name: '影视库', parent_id: 1, path: '影视库' },
          tags: []
        }
      }
    })
    api.getRelatedCinemaVideos.mockResolvedValue({
      code: 200,
      data: { videos: [], total: 0, page: 1, page_size: 20, has_more: false }
    })
    api.getThumbnail.mockRejectedValue(new Error('缩略图损坏'))
    api.prompt.mockResolvedValue({ value: 'secret' })
    api.createVideoPlayPrecheck.mockResolvedValue({ code: 200, data: { play_token: 'play-token' } })
  })

  it('初始只显示封面，点击后携带内存密码创建播放令牌', async () => {
    const wrapper = mount(Watch, { global: globalOptions })
    await flushPromises()

    expect(api.createVideoPlayPrecheck).not.toHaveBeenCalled()
    expect(wrapper.find('.cinema-poster').exists()).toBe(true)
    expect(observedElements.some(element => element.classList.contains('cinema-related__sentinel'))).toBe(true)

    await wrapper.get('.cinema-poster').trigger('click')
    await flushPromises()

    expect(api.prompt).toHaveBeenCalled()
    expect(api.createVideoPlayPrecheck).toHaveBeenCalledWith('video-1', 'secret')
    expect(wrapper.find('.xg-player-stub').exists()).toBe(true)
    wrapper.unmount()
  })

  it('取消密码输入时保留封面且不创建播放令牌', async () => {
    api.prompt.mockRejectedValue('cancel')
    const wrapper = mount(Watch, { global: globalOptions })
    await flushPromises()

    await wrapper.get('.cinema-poster').trigger('click')
    await flushPromises()

    expect(api.createVideoPlayPrecheck).not.toHaveBeenCalled()
    expect(wrapper.find('.cinema-poster').exists()).toBe(true)
    expect(modal.msgError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('首批相关推荐不足一屏时继续加载下一批', async () => {
    api.getRelatedCinemaVideos
      .mockResolvedValueOnce({
        code: 200,
        data: { videos: [], total: 21, page: 1, page_size: 20, has_more: true }
      })
      .mockResolvedValueOnce({
        code: 200,
        data: { videos: [], total: 21, page: 2, page_size: 20, has_more: false }
      })
    const wrapper = mount(Watch, { global: globalOptions })
    await flushPromises()

    expect(api.getRelatedCinemaVideos).toHaveBeenNthCalledWith(1, 7, 'video-1', 1, 20)
    expect(api.getRelatedCinemaVideos).toHaveBeenNthCalledWith(2, 7, 'video-1', 2, 20)
    wrapper.unmount()
  })

  it('标签更新后保留播放器并从第一页刷新相关推荐', async () => {
    const wrapper = mount(Watch, { global: globalOptions })
    await flushPromises()
    await wrapper.get('.cinema-poster').trigger('click')
    await flushPromises()
    expect(wrapper.find('.xg-player-stub').exists()).toBe(true)

    const editor = wrapper.getComponent({ name: 'EditableFileTags' })
    expect(editor.props('fileId')).toBe('video-1')
    editor.vm.$emit('updated', {
      file_id: 'video-1',
      state: 'ready',
      suppressed: [],
      tags: [
        {
          id: 'tag-1',
          name: '科幻',
          category: { id: 'title', code: 'title', name: '标题', color: '#409eff' },
          sources: ['manual'],
          visibility: 'private',
          automatic: false
        }
      ]
    })
    await flushPromises()

    expect(api.getCinemaVideo).toHaveBeenCalledTimes(1)
    expect(api.getRelatedCinemaVideos).toHaveBeenNthCalledWith(2, 7, 'video-1', 1, 20)
    expect(wrapper.find('.xg-player-stub').exists()).toBe(true)
    wrapper.unmount()
  })
})
