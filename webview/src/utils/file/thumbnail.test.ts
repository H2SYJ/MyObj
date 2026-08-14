// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { validateThumbnailFile } from './thumbnail'

class TestImage {
  naturalWidth = 800
  naturalHeight = 600
  onload: (() => void) | null = null
  onerror: (() => void) | null = null

  set src(_value: string) {
    queueMicrotask(() => this.onload?.())
  }
}

describe('手动缩略图校验', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('拒绝非 JPEG 和超过 1 MiB 的文件', async () => {
    await expect(validateThumbnailFile(new File(['png'], 'cover.png', { type: 'image/png' }))).rejects.toThrow(
      '缩略图仅支持 JPEG 格式'
    )

    const oversized = new File([new Uint8Array(1024 * 1024 + 1)], 'cover.jpg', { type: 'image/jpeg' })
    await expect(validateThumbnailFile(oversized)).rejects.toThrow('缩略图不能超过 1 MiB')
  })

  it('接受格式、大小和尺寸符合要求的 JPEG', async () => {
    vi.stubGlobal('Image', TestImage)
    vi.stubGlobal('URL', {
      createObjectURL: vi.fn(() => 'blob:thumbnail'),
      revokeObjectURL: vi.fn()
    })

    await expect(
      validateThumbnailFile(new File(['jpeg'], 'cover.jpg', { type: 'image/jpeg' }))
    ).resolves.toBeUndefined()
  })
})
