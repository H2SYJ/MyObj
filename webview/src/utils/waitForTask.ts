import { watch } from 'vue'
import { taskEventClient, type TaskEvent, type TaskEventKind } from '@/utils/taskEvents'

export type TaskEvaluation<TResult> =
  { status: 'pending' } | { status: 'success'; value: TResult } | { status: 'error'; error: Error }

export interface WaitForTaskOptions<TState extends object, TResult> {
  eventKind: TaskEventKind
  resourceId: string
  reconcile: () => Promise<Partial<TState> | null>
  evaluate: (state: Partial<TState>) => TaskEvaluation<TResult>
  timeoutMs?: number
  timeoutError?: () => Error
  onReconcileError?: (error: unknown) => void
}

const DISCONNECTED_RECONCILE_DELAYS = [1_000, 2_000, 5_000, 10_000] as const

export const waitForTaskTerminal = <TState extends object, TResult>(
  options: WaitForTaskOptions<TState, TResult>
): Promise<TResult> =>
  new Promise<TResult>((resolve, reject) => {
    let settled = false
    let reconcilePromise: Promise<Partial<TState> | null> | null = null
    let fallbackTimer: number | null = null
    let timeoutTimer: number | null = null
    let fallbackAttempt = 0
    let reconcileGeneration = 0
    let unsubscribeTask: () => void = () => {}
    let unsubscribeSync: () => void = () => {}
    let stopConnectionWatch: () => void = () => {}

    const clearFallbackTimer = () => {
      if (fallbackTimer === null) return
      window.clearTimeout(fallbackTimer)
      fallbackTimer = null
    }

    const cleanup = () => {
      reconcileGeneration += 1
      reconcilePromise = null
      clearFallbackTimer()
      if (timeoutTimer !== null) window.clearTimeout(timeoutTimer)
      timeoutTimer = null
      unsubscribeTask()
      unsubscribeSync()
      stopConnectionWatch()
    }

    const finishSuccess = (value: TResult) => {
      if (settled) return
      settled = true
      cleanup()
      resolve(value)
    }

    const finishError = (error: Error) => {
      if (settled) return
      settled = true
      cleanup()
      reject(error)
    }

    const applyState = (state: Partial<TState> | null | undefined) => {
      if (settled || !state) return
      const evaluation = options.evaluate(state)
      if (evaluation.status === 'success') finishSuccess(evaluation.value)
      else if (evaluation.status === 'error') finishError(evaluation.error)
    }

    const reconcile = () => {
      if (settled) return Promise.resolve(null)
      if (reconcilePromise) return reconcilePromise
      const generation = reconcileGeneration
      reconcilePromise = options
        .reconcile()
        .then(state => {
          if (generation === reconcileGeneration) applyState(state)
          return state
        })
        .catch(error => {
          options.onReconcileError?.(error)
          return null
        })
        .finally(() => {
          if (generation === reconcileGeneration) reconcilePromise = null
        })
      return reconcilePromise
    }

    const scheduleDisconnectedReconcile = () => {
      if (settled || fallbackTimer !== null || taskEventClient.connectionState.value !== 'disconnected') return
      const delay = DISCONNECTED_RECONCILE_DELAYS[Math.min(fallbackAttempt, DISCONNECTED_RECONCILE_DELAYS.length - 1)]
      fallbackTimer = window.setTimeout(async () => {
        fallbackTimer = null
        fallbackAttempt += 1
        await reconcile()
        scheduleDisconnectedReconcile()
      }, delay)
    }

    unsubscribeTask = taskEventClient.subscribe(options.eventKind, options.resourceId, (event: TaskEvent) => {
      applyState(event.payload as Partial<TState> | undefined)
    })
    unsubscribeSync = taskEventClient.subscribe('sync', undefined, () => {
      void reconcile()
    })
    stopConnectionWatch = watch(taskEventClient.connectionState, state => {
      if (state === 'disconnected') {
        scheduleDisconnectedReconcile()
        return
      }
      clearFallbackTimer()
      if (state === 'connected') fallbackAttempt = 0
    })

    if (options.timeoutMs !== undefined) {
      timeoutTimer = window.setTimeout(() => {
        finishError(options.timeoutError?.() || new Error('任务等待超时'))
      }, options.timeoutMs)
    }

    void reconcile()
    scheduleDisconnectedReconcile()
  })
