// @vitest-environment jsdom

import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import XgPlayer from './index.vue'

interface MockPlayerInstance {
  config: Record<string, unknown>
  handlers: Map<string, (...args: unknown[]) => void>
  switchURL: ReturnType<typeof vi.fn>
  destroy: ReturnType<typeof vi.fn>
}

const xgplayerMock = vi.hoisted(() => ({
  instances: [] as MockPlayerInstance[]
}))

vi.mock('xgplayer', () => {
  class MockPlayer {
    config: Record<string, unknown>
    handlers = new Map<string, (...args: unknown[]) => void>()
    switchURL = vi.fn().mockResolvedValue(undefined)
    destroy = vi.fn()
    play = vi.fn()
    pause = vi.fn()
    replay = vi.fn()

    constructor(config: Record<string, unknown>) {
      this.config = config
      xgplayerMock.instances.push(this)
    }

    on(event: string, handler: (...args: unknown[]) => void) {
      this.handlers.set(event, handler)
    }
  }

  return {
    default: MockPlayer,
    Events: {
      READY: 'ready',
      ERROR: 'error',
      PLAY: 'play',
      PAUSE: 'pause',
      ENDED: 'ended'
    }
  }
})

vi.mock('@/composables/core/useI18n', () => ({
  useI18n: () => ({
    locale: { value: 'zh-CN' },
    t: (key: string) => key
  })
}))

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width
  })
}

describe('XgPlayer', () => {
  beforeEach(() => {
    xgplayerMock.instances.length = 0
    setViewportWidth(1280)
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false })
    })
  })

  it('手机端开启旋转全屏并传递播放配置', async () => {
    setViewportWidth(390)
    const wrapper = mount(XgPlayer, {
      props: {
        src: '/api/video/mobile.mp4',
        autoplay: true,
        loop: true
      }
    })

    await flushPromises()

    expect(xgplayerMock.instances).toHaveLength(1)
    expect(xgplayerMock.instances[0].config).toMatchObject({
      url: '/api/video/mobile.mp4',
      autoplay: true,
      loop: true,
      playsinline: true,
      videoFillMode: 'contain',
      fullscreen: {
        rotateFullscreen: true
      }
    })

    wrapper.unmount()
  })

  it('桌面端关闭旋转全屏并使用浏览器原生全屏', async () => {
    const wrapper = mount(XgPlayer, {
      props: {
        src: '/api/video/desktop.mp4'
      }
    })

    await flushPromises()

    expect(xgplayerMock.instances[0].config.fullscreen).toEqual({
      rotateFullscreen: false
    })

    wrapper.unmount()
  })

  it('转发播放器事件、切换视频地址并在卸载时销毁实例', async () => {
    const wrapper = mount(XgPlayer, {
      props: {
        src: '/api/video/first.mp4'
      }
    })

    await flushPromises()
    const player = xgplayerMock.instances[0]

    player.handlers.get('ready')?.()
    player.handlers.get('play')?.()
    player.handlers.get('pause')?.()
    player.handlers.get('ended')?.()
    player.handlers.get('error')?.(new Error('加载失败'))

    expect(wrapper.emitted('ready')).toHaveLength(1)
    expect(wrapper.emitted('play')).toHaveLength(1)
    expect(wrapper.emitted('pause')).toHaveLength(1)
    expect(wrapper.emitted('ended')).toHaveLength(1)
    expect(wrapper.emitted('error')).toEqual([['preview.video.loadFailed']])

    await wrapper.setProps({ src: '/api/video/second.mp4' })
    expect(player.switchURL).toHaveBeenCalledWith('/api/video/second.mp4')

    wrapper.unmount()
    expect(player.destroy).toHaveBeenCalledOnce()
  })
})
