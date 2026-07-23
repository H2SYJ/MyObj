import { effectScope, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SELECTION_DISPLAY_DELAY_MS, useDelayedSelectionDisplay } from './useDelayedSelectionDisplay'

afterEach(() => {
  vi.useRealTimers()
})

describe('useDelayedSelectionDisplay', () => {
  it('双击判定窗口结束后才显示选中数量', () => {
    vi.useFakeTimers()
    const scope = effectScope()
    const selectedCount = ref(0)
    const selectionDisplay = scope.run(() => useDelayedSelectionDisplay(selectedCount))!

    selectedCount.value = 1
    expect(selectionDisplay.displayedCount.value).toBe(0)

    vi.advanceTimersByTime(SELECTION_DISPLAY_DELAY_MS - 1)
    expect(selectionDisplay.displayedCount.value).toBe(0)

    vi.advanceTimersByTime(1)
    expect(selectionDisplay.displayedCount.value).toBe(1)
    scope.stop()
  })

  it('显示后立即同步后续选中数量', () => {
    vi.useFakeTimers()
    const scope = effectScope()
    const selectedCount = ref(0)
    const selectionDisplay = scope.run(() => useDelayedSelectionDisplay(selectedCount))!

    selectedCount.value = 1
    vi.advanceTimersByTime(SELECTION_DISPLAY_DELAY_MS)
    selectedCount.value = 2

    expect(selectionDisplay.displayedCount.value).toBe(2)
    scope.stop()
  })

  it('双击打开时取消待显示状态', () => {
    vi.useFakeTimers()
    const scope = effectScope()
    const selectedCount = ref(0)
    const selectionDisplay = scope.run(() => useDelayedSelectionDisplay(selectedCount))!

    selectedCount.value = 1
    selectionDisplay.hideDisplay()
    vi.advanceTimersByTime(SELECTION_DISPLAY_DELAY_MS)

    expect(selectionDisplay.displayedCount.value).toBe(0)

    selectionDisplay.scheduleDisplay()
    vi.advanceTimersByTime(SELECTION_DISPLAY_DELAY_MS)
    expect(selectionDisplay.displayedCount.value).toBe(1)
    scope.stop()
  })

  it('取消选择时清理待显示计时器', () => {
    vi.useFakeTimers()
    const scope = effectScope()
    const selectedCount = ref(0)
    const selectionDisplay = scope.run(() => useDelayedSelectionDisplay(selectedCount))!

    selectedCount.value = 1
    selectedCount.value = 0
    vi.advanceTimersByTime(SELECTION_DISPLAY_DELAY_MS)

    expect(selectionDisplay.displayedCount.value).toBe(0)
    scope.stop()
  })
})
