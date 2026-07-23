import { ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useFileViewMode } from './useFileViewMode'

describe('useFileViewMode', () => {
  it('手机和桌面视图偏好独立保存', () => {
    const values = new Map<string, string>([
      ['files.viewMode', 'list'],
      ['files.mobileViewMode', 'grid']
    ])
    const storage = {
      get: (key: string) => values.get(key) || null,
      set: (key: string, value: string) => values.set(key, value)
    }
    const handheld = ref(true)
    const state = useFileViewMode(handheld, storage)
    expect(state.viewMode.value).toBe('grid')
    state.setViewMode('list')
    handheld.value = false
    expect(state.viewMode.value).toBe('list')
    state.setViewMode('grid')
    expect(values.get('files.mobileViewMode')).toBe('list')
    expect(values.get('files.viewMode')).toBe('grid')
  })
})
