import type { TagCloudItem } from '@/api/tag'

export const sortTagCloudItems = (items: TagCloudItem[]): TagCloudItem[] =>
  [...items].sort(
    (left, right) =>
      right.file_count - left.file_count || left.name.localeCompare(right.name) || left.id.localeCompare(right.id)
  )

export const tagCloudFontSize = (count: number, minCount: number, maxCount: number, handheld = false): number => {
  const minSize = 14
  const maxSize = handheld ? 28 : 34
  if (maxCount <= minCount) {
    return (minSize + maxSize) / 2
  }
  const minValue = Math.log1p(Math.max(0, minCount))
  const maxValue = Math.log1p(Math.max(0, maxCount))
  const ratio = (Math.log1p(Math.max(0, count)) - minValue) / (maxValue - minValue)
  return Math.round((minSize + ratio * (maxSize - minSize)) * 10) / 10
}

export const tagCloudSizeClass = (fontSize: number): string => {
  if (fontSize >= 27) {
    return 'is-large'
  }
  if (fontSize >= 20) {
    return 'is-medium'
  }
  return 'is-small'
}
