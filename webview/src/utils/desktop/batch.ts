import type { BatchOperationResult } from '@/types/desktop'

export function failedItemIDs(result: BatchOperationResult) {
  return new Set(result.failed_items.map(item => item.item_id))
}

export function retainBatchFailures<T>(items: T[], result: BatchOperationResult, getID: (item: T) => string) {
  const failed = failedItemIDs(result)
  return items.filter(item => failed.has(getID(item)))
}
