import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get, putFormData } from './request'

vi.mock('@/config/api', () => ({ API_BASE_URL: '/dev-api' }))
vi.mock('@/plugins/cache', () => ({
  default: {
    local: {
      get: vi.fn(() => '')
    }
  }
}))

describe('GET 请求参数序列化', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ code: 200, message: 'ok', data: [] })
    })
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('忽略 undefined 和 null，同时保留其他假值参数', async () => {
    await get('/file/tags/suggestions', {
      keyword: 'jenkins',
      tag_ids: undefined,
      cursor: null,
      scope: 'user',
      limit: 50,
      page: 0,
      enabled: false,
      empty: ''
    })

    const requestURL = String(fetchMock.mock.calls[0][0])
    expect(requestURL).toBe(
      '/dev-api/file/tags/suggestions?keyword=jenkins&scope=user&limit=50&page=0&enabled=false&empty='
    )
    expect(requestURL).not.toContain('undefined')
    expect(requestURL).not.toContain('null')
  })

  it('PUT 表单保留 FormData 并由浏览器生成 Content-Type boundary', async () => {
    const formData = new FormData()
    formData.append('thumbnail', new Blob(['jpeg'], { type: 'image/jpeg' }), 'cover.jpg')

    await putFormData('/file/thumbnail/uf-1', formData)

    const options = fetchMock.mock.calls[0][1] as RequestInit
    expect(options.method).toBe('PUT')
    expect(options.body).toBe(formData)
    expect(options.headers).not.toHaveProperty('Content-Type')
  })
})
