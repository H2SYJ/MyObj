import { describe, expect, it } from 'vitest'
import { createLatestRequestGate } from './useLatestRequest'

describe('createLatestRequestGate', () => {
  it('开始新请求时取消旧请求并阻止旧响应回写', () => {
    const gate = createLatestRequestGate()
    const first = gate.begin()
    const second = gate.begin()

    expect(first.signal.aborted).toBe(true)
    expect(first.isCurrent()).toBe(false)
    expect(second.signal.aborted).toBe(false)
    expect(second.isCurrent()).toBe(true)
  })

  it('卸载后取消当前请求且不允许再提交结果', () => {
    const gate = createLatestRequestGate()
    const request = gate.begin()
    gate.dispose()

    expect(request.signal.aborted).toBe(true)
    expect(request.isCurrent()).toBe(false)
    expect(gate.begin().isCurrent()).toBe(false)
  })
})
