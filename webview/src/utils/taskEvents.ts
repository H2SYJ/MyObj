import { readonly, ref, type DeepReadonly, type Ref } from 'vue'
import { API_BASE_URL } from '@/config/api'
import logger from '@/plugins/logger'

export type TaskEventKind = 'sync' | 'download.task' | 'upload.task' | 'package.task' | 'heartbeat'
export type TaskEventConnectionState = 'stopped' | 'connecting' | 'connected' | 'disconnected'

export interface TaskEvent<T extends Record<string, unknown> = Record<string, unknown>> {
  event_id?: number
  version: number
  action: string
  resource_id?: string
  terminal?: boolean
  payload?: T
  occurred_at: string
}

type TaskEventListener = {
  resourceId?: string
  handler: (event: TaskEvent) => void
}

const EVENT_KINDS: TaskEventKind[] = ['sync', 'download.task', 'upload.task', 'package.task', 'heartbeat']
const RECONNECT_DELAY = 1000
const MAX_RECONNECT_ATTEMPTS = 3
const WATCHDOG_TIMEOUT = 45_000

class TaskEventClient {
  private source: EventSource | null = null
  private reconnectTimer: number | null = null
  private watchdogTimer: number | null = null
  private reconnectAttempts = 0
  private stopped = true
  private readonly state = ref<TaskEventConnectionState>('stopped')
  private readonly listeners = new Map<TaskEventKind, Set<TaskEventListener>>()

  readonly connectionState: DeepReadonly<Ref<TaskEventConnectionState>> = readonly(this.state)

  start() {
    if (!this.stopped) return
    this.stopped = false
    this.connect()
  }

  stop() {
    this.stopped = true
    this.reconnectAttempts = 0
    this.clearReconnectTimer()
    this.clearWatchdog()
    this.closeSource()
    this.state.value = 'stopped'
  }

  reconnect() {
    if (this.stopped) return
    this.reconnectAttempts = 0
    this.clearReconnectTimer()
    this.clearWatchdog()
    this.closeSource()
    this.state.value = 'connecting'
    this.connect()
  }

  subscribe(kind: TaskEventKind, resourceId: string | undefined, handler: (event: TaskEvent) => void) {
    const listener: TaskEventListener = { resourceId, handler }
    let listeners = this.listeners.get(kind)
    if (!listeners) {
      listeners = new Set()
      this.listeners.set(kind, listeners)
    }
    listeners.add(listener)

    let subscribed = true
    return () => {
      if (!subscribed) return
      subscribed = false
      listeners?.delete(listener)
      if (listeners?.size === 0) this.listeners.delete(kind)
    }
  }

  private connect() {
    if (this.stopped || this.source) return
    this.state.value = 'connecting'
    const source = new EventSource(`${API_BASE_URL}/events`, { withCredentials: true })
    this.source = source

    EVENT_KINDS.forEach(kind => {
      source.addEventListener(kind, rawEvent => this.handleEvent(source, kind, rawEvent as MessageEvent<string>))
    })

    source.onopen = () => {
      if (this.source !== source) return
      this.reconnectAttempts = 0
      this.state.value = 'connected'
      this.touchWatchdog(source)
    }
    source.onerror = () => {
      if (this.source !== source) return
      this.closeSource()
      this.clearWatchdog()
      if (this.stopped) return
      this.retryOrDisconnect()
    }
  }

  private handleEvent(source: EventSource, kind: TaskEventKind, rawEvent: MessageEvent<string>) {
    if (this.source !== source) return
    this.touchWatchdog(source)
    let event: TaskEvent
    try {
      event = JSON.parse(rawEvent.data) as TaskEvent
    } catch (error) {
      logger.warn('忽略无法解析的任务实时事件', { kind, error })
      return
    }
    if (event.version !== 1) {
      logger.warn('忽略不支持的任务实时事件版本', { kind, version: event.version })
      return
    }
    const listeners = Array.from(this.listeners.get(kind) || [])
    listeners.forEach(listener => {
      if (listener.resourceId && listener.resourceId !== event.resource_id) return
      try {
        listener.handler(event)
      } catch (error) {
        logger.error('任务实时事件监听器执行失败', { kind, resourceId: event.resource_id, error })
      }
    })
  }

  private touchWatchdog(source: EventSource) {
    this.clearWatchdog()
    this.watchdogTimer = window.setTimeout(() => {
      if (this.source !== source || this.stopped) return
      this.closeSource()
      this.retryOrDisconnect()
    }, WATCHDOG_TIMEOUT)
  }

  private retryOrDisconnect() {
    if (this.reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
      this.state.value = 'disconnected'
      return
    }
    this.state.value = 'connecting'
    this.scheduleReconnect()
  }

  private scheduleReconnect() {
    if (this.stopped || this.reconnectTimer !== null) return
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.reconnectAttempts += 1
      this.connect()
    }, RECONNECT_DELAY)
  }

  private closeSource() {
    const source = this.source
    this.source = null
    source?.close()
  }

  private clearReconnectTimer() {
    if (this.reconnectTimer === null) return
    window.clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  private clearWatchdog() {
    if (this.watchdogTimer === null) return
    window.clearTimeout(this.watchdogTimer)
    this.watchdogTimer = null
  }
}

export const taskEventClient = new TaskEventClient()
