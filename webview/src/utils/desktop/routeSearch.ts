import type { LocationQueryRaw } from 'vue-router'
import type { DesktopSearchScope } from '@/types/desktop'

export function resolveDesktopSearchNavigation(
  currentPath: string,
  currentQuery: LocationQueryRaw,
  searchScope: DesktopSearchScope | undefined,
  rawKeyword: string
) {
  const keyword = rawKeyword.trim()
  const path = searchScope ? currentPath : '/files'
  const query: LocationQueryRaw = {
    ...(path === currentPath ? currentQuery : {}),
    search: keyword || undefined,
    page: undefined
  }
  return { path, query }
}
