import { onBeforeUnmount } from 'vue'

export interface LatestRequestTicket {
  signal: AbortSignal
  isCurrent: () => boolean
}

export function createLatestRequestGate() {
  let sequence = 0
  let controller: AbortController | null = null
  let disposed = false

  const begin = (): LatestRequestTicket => {
    controller?.abort()
    controller = new AbortController()
    const currentController = controller
    const requestSequence = ++sequence
    if (disposed) currentController.abort()
    return {
      signal: currentController.signal,
      isCurrent: () => !disposed && requestSequence === sequence && !currentController.signal.aborted
    }
  }

  const cancel = () => {
    sequence++
    controller?.abort()
    controller = null
  }

  const dispose = () => {
    disposed = true
    cancel()
  }

  return { begin, cancel, dispose }
}

export function useLatestRequest() {
  const gate = createLatestRequestGate()
  onBeforeUnmount(gate.dispose)
  return gate
}
