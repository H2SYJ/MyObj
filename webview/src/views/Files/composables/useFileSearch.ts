import { computed, ref, type Ref } from 'vue'
import { searchUserFiles, type SearchFileItem } from '@/api/file'
import { useSearch } from '@/composables'
import type { FileListResponse, FileItem } from '@/types'
import type { FileSortBy, FileSortOrder } from './useFileList'

/**
 * 文件搜索 composable（用户文件）
 */
export function useFileSearch(
  sortBy: Ref<FileSortBy>,
  sortOrder: Ref<FileSortOrder>,
  loadThumbnails: (files: FileItem[]) => Promise<void>,
  tagIds: Ref<string[]> = ref([])
) {
  // 结果转换函数：将后端返回的文件转换为 FileItem 格式
  const transformResult = (files: SearchFileItem[]): FileItem[] =>
    files.map(file => ({
      file_id: file.uf_id || file.id,
      file_name: file.file_name || file.name,
      file_size: file.size,
      mime_type: file.mime,
      created_at: file.created_at,
      is_enc: file.is_enc,
      has_thumbnail: Boolean(file.thumbnail_img),
      public: file.public,
      tags: file.tags || [],
      tag_state: file.tag_state
    }))

  const search = useSearch<FileItem, SearchFileItem>(
    params => searchUserFiles({ ...params, sortBy: sortBy.value, sortOrder: sortOrder.value }),
    transformResult,
    undefined,
    true,
    () => ({
      tag_ids: tagIds.value.join(',') || undefined
    })
  )

  // 将搜索结果包装为 FileListResponse 格式（兼容现有代码）
  const searchResults = computed<FileListResponse>(() => ({
    breadcrumbs: [],
    current_directory_id: 0,
    folders: [],
    files: search.searchResults.value,
    total: search.total.value,
    page: search.currentPage.value,
    page_size: search.pageSize.value
  }))

  const performSearch = async (...args: Parameters<typeof search.performSearch>) => {
    await search.performSearch(...args)
    // 搜索结果复用“我的文件”的缩略图缓存，不阻塞结果展示
    loadThumbnails(search.searchResults.value).catch(() => {
      // 单个缩略图加载失败不影响搜索结果
    })
  }

  return {
    searchKeyword: search.searchKeyword,
    isSearching: search.isSearching,
    searchResults,
    performSearch,
    clearSearch: search.clearSearch,
    hasSearchKeyword: search.hasSearchKeyword,
    clearSearchResults: search.clearSearchResults,
    hasActiveSearch: computed(() => search.hasSearchKeyword.value || tagIds.value.length > 0)
  }
}
