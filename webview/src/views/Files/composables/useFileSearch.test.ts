import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSearch } from '@/composables'
import { useFileSearch } from './useFileSearch'

vi.mock('@/api/file', () => ({
  searchUserFiles: vi.fn()
}))

vi.mock('@/composables', () => ({
  useSearch: vi.fn()
}))

describe('useFileSearch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('搜索完成后加载结果中的缩略图', async () => {
    const performSearch = vi.fn().mockResolvedValue(undefined)
    const searchResults = ref([
      {
        file_id: 'user-file-1',
        file_name: '示例.mp4',
        file_size: 1024,
        mime_type: 'video/mp4',
        created_at: '2026-07-23T00:00:00Z',
        is_enc: false,
        has_thumbnail: true,
        public: false
      }
    ])

    vi.mocked(useSearch).mockReturnValue({
      searchKeyword: ref('示例'),
      isSearching: ref(false),
      searchResults,
      total: ref(1),
      currentPage: ref(1),
      pageSize: ref(20),
      performSearch,
      clearSearch: vi.fn(),
      hasSearchKeyword: ref(true)
    } as unknown as ReturnType<typeof useSearch>)

    const loadThumbnails = vi.fn().mockResolvedValue(undefined)
    const search = useFileSearch(ref('time'), ref('desc'), loadThumbnails)

    await search.performSearch('示例', 1, 20)

    expect(performSearch).toHaveBeenCalledWith('示例', 1, 20)
    expect(loadThumbnails).toHaveBeenCalledWith(searchResults.value)
  })
})
