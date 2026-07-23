import { describe, expect, it } from 'vitest'
import { usePreviewObjectUrls } from './usePreviewObjectUrls'

describe('usePreviewObjectUrls', () => {
  it('替换和关闭预览时只回收 blob URL', () => {
    const revoked: string[] = []
    const urls = usePreviewObjectUrls(url => revoked.push(url))

    urls.setPreviewUrl('image', 'blob:first')
    urls.setPreviewUrl('image', 'blob:second')
    urls.setPreviewUrl('video', '/api/video/stream')
    urls.setPreviewUrl('pdf', 'blob:pdf')
    urls.clearPreviewUrls()

    expect(revoked).toEqual(['blob:first', 'blob:second', 'blob:pdf'])
    expect(urls.imageUrl.value).toBe('')
    expect(urls.videoUrl.value).toBe('')
    expect(urls.pdfUrl.value).toBe('')
  })

  it('回收已经过期且未提交的异步结果', () => {
    const revoked: string[] = []
    const urls = usePreviewObjectUrls(url => revoked.push(url))

    urls.releaseUncommittedUrl('blob:stale')
    urls.releaseUncommittedUrl('/api/file/preview')

    expect(revoked).toEqual(['blob:stale'])
  })
})
