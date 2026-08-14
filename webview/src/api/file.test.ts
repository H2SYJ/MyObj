// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getThumbnail, updateThumbnail } from './file'

const network = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  putFormData: vi.fn(),
  upload: vi.fn()
}))

vi.mock('@/utils/network/request', () => network)
vi.mock('@/plugins/cache', () => ({ default: { local: { get: vi.fn() } } }))
vi.mock('@/plugins/logger', () => ({ default: { error: vi.fn() } }))

describe('文件缩略图 API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    network.putFormData.mockResolvedValue({ code: 200, message: '修改缩略图成功' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('使用用户文件 ID 和 thumbnail 字段提交 PUT 表单', async () => {
    const thumbnail = new File(['jpeg'], 'cover.jpg', { type: 'image/jpeg' })

    await updateThumbnail('uf-1', thumbnail)

    expect(network.putFormData).toHaveBeenCalledOnce()
    const [url, formData] = network.putFormData.mock.calls[0]
    expect(url).toBe('/file/thumbnail/uf-1')
    expect(formData).toBeInstanceOf(FormData)
    expect(formData.get('thumbnail')).toBe(thumbnail)
  })

  it('更新成功后为后续缩略图请求附加新的缓存版本', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      blob: () => Promise.resolve(new Blob(['jpeg'], { type: 'image/jpeg' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:thumbnail') })

    await updateThumbnail('uf-cache', new File(['jpeg'], 'cover.jpg', { type: 'image/jpeg' }))
    await getThumbnail('uf-cache')

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/file\/thumbnail\/uf-cache\?v=\d+$/)
  })
})
