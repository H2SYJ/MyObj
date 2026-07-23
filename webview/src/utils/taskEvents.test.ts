// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { taskEventClient } from './taskEvents'

class MockEventSource {
  static instances: MockEventSource[] = []

  readonly url: string
  readonly withCredentials: boolean
  closed = false
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  private readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()

  constructor(url: string | URL, options?: EventSourceInit) {
    this.url = String(url)
    this.withCredentials = Boolean(options?.withCredentials)
    MockEventSource.instances.push(this)
  }

  addEventListener(kind: string, listener: EventListenerOrEventListenerObject) {
    let listeners = this.listeners.get(kind)
    if (!listeners) {
      listeners = new Set()
      this.listeners.set(kind, listeners)
    }
    listeners.add(listener as (event: MessageEvent<string>) => void)
  }

  close() {
    this.closed = true
  }

  open() {
    this.onopen?.(new Event('open'))
  }

  fail() {
    this.onerror?.(new Event('error'))
  }

  emit(kind: string, data: Record<string, unknown>) {
    const event = new MessageEvent<string>(kind, { data: JSON.stringify(data) })
    this.listeners.get(kind)?.forEach(listener => listener(event))
  }
}

describe('taskEventClient', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    taskEventClient.stop()
    MockEventSource.instances.length = 0
    vi.stubGlobal('EventSource', MockEventSource)
  })

  afterEach(() => {
    taskEventClient.stop()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('每个标签页只创建一条连接并按资源ID分发事件', () => {
    const taskOne = vi.fn()
    const taskTwo = vi.fn()
    const unsubscribeOne = taskEventClient.subscribe('download.task', 'task-1', taskOne)
    const unsubscribeTwo = taskEventClient.subscribe('download.task', 'task-2', taskTwo)

    taskEventClient.start()
    taskEventClient.start()

    expect(MockEventSource.instances).toHaveLength(1)
    expect(MockEventSource.instances[0].withCredentials).toBe(true)
    expect(MockEventSource.instances[0].url).not.toContain('token=')
    MockEventSource.instances[0].open()
    MockEventSource.instances[0].emit('download.task', {
      version: 1,
      action: 'updated',
      resource_id: 'task-1',
      occurred_at: new Date().toISOString(),
      payload: { state: 1 }
    })

    expect(taskOne).toHaveBeenCalledTimes(1)
    expect(taskTwo).not.toHaveBeenCalled()
    unsubscribeOne()
    unsubscribeOne()
    unsubscribeTwo()
  })

  it('断线一秒后重连且不会发起业务REST请求', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    taskEventClient.start()
    const first = MockEventSource.instances[0]
    first.open()
    first.fail()

    expect(taskEventClient.connectionState.value).toBe('disconnected')
    expect(first.closed).toBe(true)
    await vi.advanceTimersByTimeAsync(999)
    expect(MockEventSource.instances).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(MockEventSource.instances).toHaveLength(2)
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('45秒无事件时重建连接并在sync时通知订阅者', async () => {
    const syncListener = vi.fn()
    const unsubscribe = taskEventClient.subscribe('sync', undefined, syncListener)
    taskEventClient.start()
    const first = MockEventSource.instances[0]
    first.open()
    first.emit('sync', {
      version: 1,
      action: 'sync',
      occurred_at: new Date().toISOString()
    })
    expect(syncListener).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(45_000)
    expect(first.closed).toBe(true)
    expect(taskEventClient.connectionState.value).toBe('disconnected')
    await vi.advanceTimersByTimeAsync(1000)
    expect(MockEventSource.instances).toHaveLength(2)
    unsubscribe()
  })
})
