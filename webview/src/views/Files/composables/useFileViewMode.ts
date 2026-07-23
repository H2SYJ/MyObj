import { computed, ref, type Ref } from 'vue'
import cache from '@/plugins/cache'

export type FileViewMode = 'grid' | 'list'

export interface FileViewModeStorage {
  get(key: string): string | null
  set(key: string, value: string): void
}

export function useFileViewMode(isHandheld: Ref<boolean>, storage: FileViewModeStorage = cache.local) {
  const desktopViewMode = ref<FileViewMode>(storage.get('files.viewMode') === 'list' ? 'list' : 'grid')
  const mobileViewMode = ref<FileViewMode>(storage.get('files.mobileViewMode') === 'list' ? 'list' : 'grid')
  const viewMode = computed<FileViewMode>({
    get: () => (isHandheld.value ? mobileViewMode.value : desktopViewMode.value),
    set: value => {
      if (isHandheld.value) mobileViewMode.value = value
      else desktopViewMode.value = value
    }
  })

  const setViewMode = (mode: FileViewMode) => {
    viewMode.value = mode
    storage.set(isHandheld.value ? 'files.mobileViewMode' : 'files.viewMode', mode)
  }

  return { desktopViewMode, mobileViewMode, viewMode, setViewMode }
}
