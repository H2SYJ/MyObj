import { ref, watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getTagSuggestions, type TagSuggestionScope } from '@/api/tag'
import type { CompactTag } from '@/types'

export const parseSearchTagIds = (value: unknown): string[] => {
  const raw = Array.isArray(value) ? value.join(',') : typeof value === 'string' ? value : ''
  return [
    ...new Set(
      raw
        .split(',')
        .map(item => item.trim())
        .filter(Boolean)
    )
  ]
}

export const extractSearchHistoryKeyword = (value: string): string =>
  value
    .replace(/(^|\s)#[^\s#]*/gu, '$1')
    .replace(/\s+/gu, ' ')
    .trim()

export function useFileSearchDraft(scope: Ref<TagSuggestionScope>) {
  const route = useRoute()
  const router = useRouter()
  const keyword = ref('')
  const tags = ref<CompactTag[]>([])
  let syncSerial = 0

  const syncFromRoute = async () => {
    const serial = ++syncSerial
    const isSearchPage = route.path === '/files' || route.path === '/square'
    if (!isSearchPage) {
      keyword.value = ''
      tags.value = []
      return
    }
    keyword.value = typeof route.query.search === 'string' ? route.query.search : ''
    const ids = parseSearchTagIds(route.query.tags)
    let validIDs = ids
    if (ids.length === 0) {
      tags.value = []
    } else {
      try {
        const response = await getTagSuggestions({ tagIds: ids, scope: scope.value, limit: ids.length })
        if (serial !== syncSerial) return
        if (response.code !== 200) {
          tags.value = ids.map(id => ({ id, name: id, category_code: '', color: '', visibility: '' }))
          return
        }
        const resolved = new Map((response.data || []).map(tag => [tag.id, tag]))
        tags.value = ids.map(id => resolved.get(id)).filter((tag): tag is CompactTag => Boolean(tag))
        validIDs = tags.value.map(tag => tag.id)
      } catch {
        if (serial === syncSerial) {
          tags.value = ids.map(id => ({ id, name: id, category_code: '', color: '', visibility: '' }))
        }
      }
    }

    if (
      serial === syncSerial &&
      (validIDs.join(',') !== ids.join(',') || route.query.tagMode !== undefined || route.query.tagScope !== undefined)
    ) {
      await router.replace({
        path: route.path,
        query: {
          ...route.query,
          tags: validIDs.join(',') || undefined,
          tagMode: undefined,
          tagScope: undefined
        }
      })
    }
  }

  watch(
    () => [route.path, route.query.search, route.query.tags, route.query.tagMode, route.query.tagScope, scope.value],
    () => void syncFromRoute(),
    { immediate: true }
  )

  return { keyword, tags, syncFromRoute }
}
