// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest'
import type { SearchResponse } from '@/api/file'
import { useSearch } from './useSearch'

interface SearchItem {
  name: string
}

const response = (name: string): SearchResponse<SearchItem> => ({
  code: 200,
  message: '搜索成功',
  data: {
    files: [{ name }],
    total: 1
  }
})

describe('useSearch', () => {
  it('连续搜索时只接受最后一次请求的结果', async () => {
    let resolvePrevious: (value: SearchResponse<SearchItem>) => void = () => undefined
    let resolveLatest: (value: SearchResponse<SearchItem>) => void = () => undefined
    const searchApi = vi
      .fn()
      .mockImplementationOnce(() => new Promise<SearchResponse<SearchItem>>(resolve => (resolvePrevious = resolve)))
      .mockImplementationOnce(() => new Promise<SearchResponse<SearchItem>>(resolve => (resolveLatest = resolve)))
    const search = useSearch(searchApi, files => files, undefined, false)

    const previousRequest = search.performSearch('上次搜索')
    const latestRequest = search.performSearch('本次搜索')
    resolveLatest(response('本次结果'))
    await latestRequest

    expect(search.searchResults.value).toEqual([{ name: '本次结果' }])
    expect(search.isSearching.value).toBe(false)

    resolvePrevious(response('上次结果'))
    await previousRequest

    expect(search.searchResults.value).toEqual([{ name: '本次结果' }])
    expect(search.total.value).toBe(1)
  })

  it('清空结果后忽略仍在执行的搜索请求', async () => {
    let resolveRequest: (value: SearchResponse<SearchItem>) => void = () => undefined
    const searchApi = vi.fn(() => new Promise<SearchResponse<SearchItem>>(resolve => (resolveRequest = resolve)))
    const search = useSearch(searchApi, files => files, undefined, false)

    const request = search.performSearch('待清空搜索')
    search.clearSearchResults()
    resolveRequest(response('过期结果'))
    await request

    expect(search.searchResults.value).toEqual([])
    expect(search.total.value).toBe(0)
    expect(search.isSearching.value).toBe(false)
  })
})
