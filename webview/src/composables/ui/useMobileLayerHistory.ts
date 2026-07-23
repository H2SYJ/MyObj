import { nextTick, onBeforeUnmount, onMounted, watch, type Ref } from 'vue'

const MOBILE_LAYER_STATE = '__myobjMobileLayer'

export function useMobileLayerHistory(open: Ref<boolean>, key: string, enabled: Ref<boolean> | boolean = true) {
  let historyEntryActive = false
  let closingFromPopState = false

  const handlePopState = () => {
    if (!historyEntryActive || !open.value) return
    closingFromPopState = true
    historyEntryActive = false
    open.value = false
    nextTick(() => {
      closingFromPopState = false
    })
  }

  watch(open, value => {
    if (typeof window === 'undefined') return
    if (value) {
      const isEnabled = typeof enabled === 'boolean' ? enabled : enabled.value
      if (!isEnabled) return
      if (window.history.state?.[MOBILE_LAYER_STATE] !== key) {
        window.history.pushState({ ...window.history.state, [MOBILE_LAYER_STATE]: key }, '')
      }
      historyEntryActive = true
      return
    }

    if (!closingFromPopState && historyEntryActive && window.history.state?.[MOBILE_LAYER_STATE] === key) {
      historyEntryActive = false
      window.history.back()
    }
  })

  onMounted(() => window.addEventListener('popstate', handlePopState))
  onBeforeUnmount(() => {
    window.removeEventListener('popstate', handlePopState)
    if (historyEntryActive && window.history.state?.[MOBILE_LAYER_STATE] === key) {
      const nextState = { ...window.history.state }
      delete nextState[MOBILE_LAYER_STATE]
      window.history.replaceState(nextState, '')
    }
  })
}
