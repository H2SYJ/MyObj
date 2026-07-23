import { describe, expect, it } from 'vitest'
import { retainBatchFailures } from './batch'

describe('retainBatchFailures', () => {
  it('部分成功后仅保留失败项及其原因对应的选择', () => {
    const selected = [{ id: 1 }, { id: 2 }, { id: 3 }]
    const retained = retainBatchFailures(
      selected,
      {
        total_count: 3,
        success_count: 2,
        failed_count: 1,
        failed_items: [{ item_id: '2', reason: '无权限' }]
      },
      item => String(item.id)
    )
    expect(retained).toEqual([{ id: 2 }])
  })
})
