// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { taskEventClient } from '@/utils/taskEvents'
import { waitForTaskTerminal } from '@/utils/waitForTask'

class MockEventSource {
  static instances: MockEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners = new Map<string, Array<(event: MessageEvent<string>) => void>>()

  constructor(_url: string | URL, _options?: EventSourceInit) {
    MockEventSource.instances.push(this)
  }

  addEventListener(kind: string, handler: EventListenerOrEventListenerObject) {
    const callback = handler as (event: MessageEvent<string>) => void
    this.listeners.set(kind, [...(this.listeners.get(kind) || []), callback])
  }

  close() {}

  open() {
    this.onopen?.()
  }

  fail() {
    this.onerror?.()
  }

  emit(kind: string, payload: Record<string, unknown>) {
    const event = { data: JSON.stringify(payload) } as MessageEvent<string>
    this.listeners.get(kind)?.forEach(listener => listener(event))
  }
}

interface TestTask {
  status: 'pending' | 'completed'
}

const evaluate = (task: Partial<TestTask>) =>
  task.status === 'completed'
    ? ({ status: 'success', value: task as TestTask } as const)
    : ({ status: 'pending' } as const)

describe('waitForTaskTerminal', () => {
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

  it('SSE连接正常时只执行首次对账', async () => {
    const reconcile = vi.fn().mockResolvedValue({ status: 'pending' })
    taskEventClient.start()
    MockEventSource.instances[0].open()
    const result = waitForTaskTerminal<TestTask, TestTask>({
      eventKind: 'download.task',
      resourceId: 'task-1',
      reconcile,
      evaluate
    })
    await vi.advanceTimersByTimeAsync(20_000)
    expect(reconcile).toHaveBeenCalledTimes(1)
    MockEventSource.instances[0].emit('download.task', {
      version: 1,
      action: 'updated',
      resource_id: 'task-1',
      payload: { status: 'completed' },
      occurred_at: new Date().toISOString()
    })
    await expect(result).resolves.toMatchObject({ status: 'completed' })
  })

  it('连续重连失败后按1、2、5、10秒退避对账并保持10秒上限', async () => {
    taskEventClient.start()
    MockEventSource.instances[0].open()
    MockEventSource.instances[0].fail()
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      await vi.advanceTimersByTimeAsync(1_000)
      MockEventSource.instances[attempt].fail()
    }
    expect(taskEventClient.connectionState.value).toBe('disconnected')

    const reconcile = vi
      .fn()
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ status: 'completed' })
    const result = waitForTaskTerminal<TestTask, TestTask>({
      eventKind: 'upload.task',
      resourceId: 'task-2',
      reconcile,
      evaluate
    })
    await vi.advanceTimersByTimeAsync(0)
    expect(reconcile).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(reconcile).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(2_000)
    expect(reconcile).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(reconcile).toHaveBeenCalledTimes(4)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(reconcile).toHaveBeenCalledTimes(5)
    await vi.advanceTimersByTimeAsync(10_000)
    await expect(result).resolves.toMatchObject({ status: 'completed' })
    expect(reconcile).toHaveBeenCalledTimes(6)
    await vi.advanceTimersByTimeAsync(20_000)
    expect(reconcile).toHaveBeenCalledTimes(6)
  })

  it('连接恢复后立即取消REST退避Timer', async () => {
    taskEventClient.start()
    MockEventSource.instances[0].fail()
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      await vi.advanceTimersByTimeAsync(1_000)
      MockEventSource.instances[attempt].fail()
    }
    const reconcile = vi.fn().mockResolvedValue({ status: 'pending' })
    const result = waitForTaskTerminal<TestTask, TestTask>({
      eventKind: 'download.task',
      resourceId: 'task-recovered',
      reconcile,
      evaluate
    })
    await vi.advanceTimersByTimeAsync(0)
    taskEventClient.reconnect()
    MockEventSource.instances[4].open()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(reconcile).toHaveBeenCalledTimes(1)
    MockEventSource.instances[4].emit('download.task', {
      version: 1,
      action: 'updated',
      resource_id: 'task-recovered',
      payload: { status: 'completed' },
      occurred_at: new Date().toISOString()
    })
    await expect(result).resolves.toMatchObject({ status: 'completed' })
  })

  it('总超时不会被未完成的对账Promise阻塞', async () => {
    taskEventClient.start()
    MockEventSource.instances[0].open()
    const reconcile = vi.fn(() => new Promise<Partial<TestTask> | null>(() => {}))
    const result = waitForTaskTerminal<TestTask, TestTask>({
      eventKind: 'package.task',
      resourceId: 'task-timeout',
      reconcile,
      evaluate,
      timeoutMs: 1_000,
      timeoutError: () => new Error('测试超时')
    })
    const rejection = expect(result).rejects.toThrow('测试超时')
    await vi.advanceTimersByTimeAsync(1_000)
    await rejection
    await vi.advanceTimersByTimeAsync(20_000)
    expect(reconcile).toHaveBeenCalledTimes(1)
  })
})
