import { getFileList, getThumbnail } from '@/api/file'
import { useI18n } from '@/composables'
import type { FileItem, FileListResponse } from '@/types'
import cache from '@/plugins/cache'

export type FileSortBy = 'name' | 'size' | 'time'
export type FileSortOrder = 'asc' | 'desc'

const SORT_BY_KEY = 'files.sortBy'
const SORT_ORDER_KEY = 'files.sortOrder'

export function useFileList() {
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const router = useRouter()
  const route = useRoute()
  const { t } = useI18n()

  const fileListData = ref<FileListResponse>({
    breadcrumbs: [],
    current_directory_id: 0,
    folders: [],
    files: [],
    total: 0,
    page: 1,
    page_size: 20
  })

  const currentPage = ref(1)
  const pageSize = ref(20)
  const currentDirectoryId = ref(0)
  const thumbnailCache = ref<Map<string, string>>(new Map())
  const loadingThumbnailIds = new Set<string>()
  const thumbnailRequestVersions = new Map<string, number>()
  const loading = ref(false)
  const cachedSortBy = cache.local.get(SORT_BY_KEY)
  const cachedSortOrder = cache.local.get(SORT_ORDER_KEY)
  const sortBy = ref<FileSortBy>(
    ['name', 'size', 'time'].includes(cachedSortBy || '') ? (cachedSortBy as FileSortBy) : 'time'
  )
  const sortOrder = ref<FileSortOrder>(cachedSortOrder === 'asc' ? 'asc' : 'desc')

  const breadcrumbs = computed(() => fileListData.value.breadcrumbs)

  const formatBreadcrumbName = (name: string): string => {
    if (!name) {
      return ''
    }
    if (name === 'home' || name === '') {
      return t('files.home')
    }
    return name
  }

  const loadThumbnails = async (files: FileItem[]) => {
    const thumbnailPromises = files
      .filter(
        file => file.has_thumbnail && !thumbnailCache.value.has(file.file_id) && !loadingThumbnailIds.has(file.file_id)
      )
      .map(async file => {
        const requestVersion = thumbnailRequestVersions.get(file.file_id) || 0
        thumbnailRequestVersions.set(file.file_id, requestVersion)
        loadingThumbnailIds.add(file.file_id)
        try {
          const blobUrl = await getThumbnail(file.file_id)
          if (blobUrl && thumbnailRequestVersions.get(file.file_id) === requestVersion) {
            thumbnailCache.value.set(file.file_id, blobUrl)
          } else if (blobUrl) {
            URL.revokeObjectURL(blobUrl)
          }
        } catch (error) {
          // 缩略图加载失败不影响主流程
          proxy?.$log.warn(t('files.thumbnailLoadFailed') + `: ${file.file_id}`, error)
        } finally {
          if (thumbnailRequestVersions.get(file.file_id) === requestVersion) {
            loadingThumbnailIds.delete(file.file_id)
          }
        }
      })

    await Promise.all(thumbnailPromises)
  }

  const loadFileList = async (append = false) => {
    loading.value = true
    try {
      const res = await getFileList({
        directory_id: currentDirectoryId.value,
        sortBy: sortBy.value,
        sortOrder: sortOrder.value,
        page: currentPage.value,
        pageSize: pageSize.value
      })

      if (res.code === 200) {
        fileListData.value = append
          ? {
              ...res.data,
              breadcrumbs: fileListData.value.breadcrumbs,
              folders: [...fileListData.value.folders, ...res.data.folders],
              files: [...fileListData.value.files, ...res.data.files]
            }
          : res.data

        currentDirectoryId.value = res.data.current_directory_id

        // 不等待缩略图加载完成，后台并发加载
        loadThumbnails(res.data.files).catch(() => {
          // 静默处理错误
        })
      } else {
        proxy?.$modal.msgError(res.message || t('files.loadFailed'))
      }
    } catch (error) {
      proxy?.$modal.msgError(t('files.loadFileListFailed'))
      proxy?.$log.error(error)
    } finally {
      loading.value = false
    }
  }

  const navigateToDirectory = (directoryId: number) => {
    router.push({
      path: route.path,
      query: {
        directoryId: String(directoryId)
      }
    })
    // 注意：不需要手动调用 loadFileList，watch 会自动处理
  }

  const getThumbnailUrl = (fileId: string) => thumbnailCache.value.get(fileId) || ''

  const refreshThumbnail = async (fileId: string) => {
    const requestVersion = (thumbnailRequestVersions.get(fileId) || 0) + 1
    thumbnailRequestVersions.set(fileId, requestVersion)
    loadingThumbnailIds.add(fileId)
    const cachedUrl = thumbnailCache.value.get(fileId)
    if (cachedUrl) {
      URL.revokeObjectURL(cachedUrl)
      thumbnailCache.value.delete(fileId)
    }

    try {
      const blobUrl = await getThumbnail(fileId, true)
      if (blobUrl && thumbnailRequestVersions.get(fileId) === requestVersion) {
        thumbnailCache.value.set(fileId, blobUrl)
      } else if (blobUrl) {
        URL.revokeObjectURL(blobUrl)
      }
    } finally {
      if (thumbnailRequestVersions.get(fileId) === requestVersion) {
        loadingThumbnailIds.delete(fileId)
      }
    }
  }

  const handlePageChange = (page: number) => {
    currentPage.value = page
    loadFileList()
  }

  const handleSizeChange = (size: number) => {
    pageSize.value = size
    currentPage.value = 1
    loadFileList()
  }

  const setSorting = (nextSortBy: FileSortBy, nextSortOrder: FileSortOrder = sortOrder.value) => {
    sortBy.value = nextSortBy
    sortOrder.value = nextSortOrder
    cache.local.set(SORT_BY_KEY, nextSortBy)
    cache.local.set(SORT_ORDER_KEY, nextSortOrder)
  }

  let initialized = false

  // 监听路由变化，支持首次加载及浏览器前进/后退
  watch(
    () => route.query.directoryId,
    rawDirectoryId => {
      const parsed = typeof rawDirectoryId === 'string' ? Number(rawDirectoryId) : 0
      const directoryId = Number.isInteger(parsed) && parsed > 0 ? parsed : 0
      const pathChanged = currentDirectoryId.value !== directoryId

      if (pathChanged) {
        currentDirectoryId.value = directoryId
        currentPage.value = 1
      }

      if (!initialized || pathChanged) {
        initialized = true
        loadFileList()
      }
    },
    { immediate: true } // 立即执行一次，处理初始加载
  )

  return {
    fileListData,
    currentPage,
    pageSize,
    currentPath: currentDirectoryId,
    breadcrumbs,
    formatBreadcrumbName,
    loadFileList,
    navigateToPath: navigateToDirectory,
    getThumbnailUrl,
    refreshThumbnail,
    loadThumbnails,
    handlePageChange,
    handleSizeChange,
    loading,
    sortBy,
    sortOrder,
    setSorting
  }
}
