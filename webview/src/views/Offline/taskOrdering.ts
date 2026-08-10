type OfflineTaskOrderSource = {
  id: string
  state: number
  create_time: string
}

export function prioritizeDownloadingTasks<T extends OfflineTaskOrderSource>(tasks: readonly T[]): T[] {
  return [...tasks].sort((left, right) => {
    const statePriority = Number(right.state === 1) - Number(left.state === 1)
    return statePriority || right.create_time.localeCompare(left.create_time) || left.id.localeCompare(right.id)
  })
}
