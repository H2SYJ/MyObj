import { getFileList, getThumbnail } from '@/api/file'
import { useI18n } from '@/composables'
import type { FileListResponse } from '@/types'
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
  const loading = ref(false)
  const cachedSortBy = cache.local.get(SORT_BY_KEY)
  const cachedSortOrder = cache.local.get(SORT_ORDER_KEY)
  const sortBy = ref<FileSortBy>(
    ['name', 'size', 'time'].includes(cachedSortBy || '') ? (cachedSortBy as FileSortBy) : 'time'
  )
  const sortOrder = ref<FileSortOrder>(cachedSortOrder === 'asc' ? 'asc' : 'desc')

  const breadcrumbs = computed(() => fileListData.value.breadcrumbs)

  const formatBreadcrumbName = (name: string): string => {
    if (!name) return ''
    if (name === 'home' || name === '') {
      return t('files.home')
    }
    return name
  }

  const loadFileList = async () => {
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
        fileListData.value = res.data

        currentDirectoryId.value = res.data.current_directory_id

        // 使用 Promise.all 并发加载缩略图
        const thumbnailPromises = res.data.files
          .filter((file: any) => file.has_thumbnail && !thumbnailCache.value.has(file.file_id))
          .map(async (file: any) => {
            try {
              const blobUrl = await getThumbnail(file.file_id)
              if (blobUrl) {
                thumbnailCache.value.set(file.file_id, blobUrl)
              }
            } catch (error) {
              // 缩略图加载失败不影响主流程
              proxy?.$log.warn(t('files.thumbnailLoadFailed') + `: ${file.file_id}`, error)
            }
          })

        // 不等待缩略图加载完成，后台加载
        Promise.all(thumbnailPromises).catch(() => {
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

  const getThumbnailUrl = (fileId: string) => {
    return thumbnailCache.value.get(fileId) || ''
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
    handlePageChange,
    handleSizeChange,
    loading,
    sortBy,
    sortOrder,
    setSorting
  }
}
