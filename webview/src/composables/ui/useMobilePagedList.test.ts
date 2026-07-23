import { describe, expect, it, vi } from 'vitest'
import { useMobilePagedList } from './useMobilePagedList'

describe('useMobilePagedList', () => {
  it('追加分页并按稳定 ID 去重', async () => {
    const loader = vi.fn(async (page: number) => ({
      items: page === 1 ? [{ id: 1 }, { id: 2 }] : [{ id: 2 }, { id: 3 }],
      total: 3
    }))
    const list = useMobilePagedList(loader, item => item.id, 2)
    await list.reset()
    await list.loadMore()
    expect(list.items.value.map(item => item.id)).toEqual([1, 2, 3])
    expect(list.hasMore.value).toBe(false)
  })

  it('重置时忽略较早请求返回的数据', async () => {
    let resolveFirst!: (value: { items: { id: number }[]; total: number }) => void
    const first = new Promise<{ items: { id: number }[]; total: number }>(resolve => (resolveFirst = resolve))
    const loader = vi
      .fn()
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce({ items: [{ id: 9 }], total: 1 })
    const list = useMobilePagedList<{ id: number }>(loader, item => item.id)
    const staleRequest = list.loadMore()
    const resetRequest = list.reset()
    await resetRequest
    resolveFirst({ items: [{ id: 1 }], total: 1 })
    await staleRequest
    expect(list.items.value).toEqual([{ id: 9 }])
  })

  it('失败后保留错误并可重试', async () => {
    const loader = vi.fn().mockRejectedValueOnce(new Error('网络异常')).mockResolvedValueOnce({ items: [{ id: 1 }], total: 1 })
    const list = useMobilePagedList<{ id: number }>(loader, item => item.id)
    await list.reset()
    expect(list.error.value).toBe('网络异常')
    await list.retry()
    expect(list.error.value).toBe('')
    expect(list.items.value).toEqual([{ id: 1 }])
  })
})
