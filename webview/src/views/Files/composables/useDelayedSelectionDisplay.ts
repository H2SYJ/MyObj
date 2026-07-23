import { onScopeDispose, ref, watch, type Ref } from 'vue'

// 浏览器无法读取系统双击间隔，这里采用 Windows 常见的 500 毫秒判定窗口。
export const SELECTION_DISPLAY_DELAY_MS = 500

export function useDelayedSelectionDisplay(selectedCount: Ref<number>, delayMs = SELECTION_DISPLAY_DELAY_MS) {
  const displayedCount = ref(0)
  let displayTimer: ReturnType<typeof setTimeout> | undefined

  const cancelTimer = () => {
    if (displayTimer) clearTimeout(displayTimer)
    displayTimer = undefined
  }

  const hideDisplay = () => {
    cancelTimer()
    displayedCount.value = 0
  }

  const scheduleDisplay = () => {
    cancelTimer()
    if (selectedCount.value === 0) {
      displayedCount.value = 0
      return
    }
    if (displayedCount.value > 0) {
      displayedCount.value = selectedCount.value
      return
    }
    displayTimer = setTimeout(() => {
      displayedCount.value = selectedCount.value
      displayTimer = undefined
    }, delayMs)
  }

  watch(
    selectedCount,
    count => {
      if (count === 0) hideDisplay()
      else if (displayedCount.value > 0) displayedCount.value = count
      else scheduleDisplay()
    },
    { flush: 'sync' }
  )

  onScopeDispose(cancelTimer)

  return {
    displayedCount,
    scheduleDisplay,
    hideDisplay
  }
}
