import { computed, ref, type Ref } from 'vue'

export interface MobilePageResult<T> {
  items: T[]
  total: number
}

export interface MobilePageLoader<T> {
  (page: number, pageSize: number): Promise<MobilePageResult<T>>
}

export function useMobilePagedList<T>(loader: MobilePageLoader<T>, getKey: (item: T) => string | number, pageSize = 20) {
  const items = ref<T[]>([]) as Ref<T[]>
  const page = ref(0)
  const total = ref(0)
  const loading = ref(false)
  const error = ref('')
  let requestId = 0

  const hasMore = computed(() => items.value.length < total.value || page.value === 0)

  const loadPage = async (reset = false) => {
    if (loading.value || (!reset && !hasMore.value)) return
    const currentRequest = ++requestId
    const targetPage = reset ? 1 : page.value + 1
    loading.value = true
    error.value = ''
    try {
      const result = await loader(targetPage, pageSize)
      if (currentRequest !== requestId) return
      const merged = reset ? result.items : [...items.value, ...result.items]
      const deduped = new Map<string | number, T>()
      merged.forEach(item => deduped.set(getKey(item), item))
      items.value = Array.from(deduped.values())
      total.value = result.total
      page.value = targetPage
    } catch (cause) {
      if (currentRequest !== requestId) return
      error.value = cause instanceof Error ? cause.message : '加载失败，请重试'
    } finally {
      if (currentRequest === requestId) loading.value = false
    }
  }

  const reset = async () => {
    requestId++
    items.value = []
    page.value = 0
    total.value = 0
    error.value = ''
    loading.value = false
    await loadPage(true)
  }

  const retry = () => loadPage(page.value === 0)

  return { items, page, total, loading, error, hasMore, loadMore: () => loadPage(false), reset, retry }
}
