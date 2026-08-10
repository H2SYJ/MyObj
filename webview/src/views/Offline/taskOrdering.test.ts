import { describe, expect, it } from 'vitest'
import { prioritizeDownloadingTasks } from './taskOrdering'

describe('prioritizeDownloadingTasks', () => {
  it('将下载中任务排在其他状态之前并保持组内创建时间倒序', () => {
    const tasks = [
      { id: 'finished', state: 3, create_time: '2026-08-10 10:00:00' },
      { id: 'downloading-old', state: 1, create_time: '2026-08-10 08:00:00' },
      { id: 'queued', state: 0, create_time: '2026-08-10 09:00:00' },
      { id: 'downloading-new', state: 1, create_time: '2026-08-10 09:30:00' }
    ]

    expect(prioritizeDownloadingTasks(tasks).map(task => task.id)).toEqual([
      'downloading-new',
      'downloading-old',
      'finished',
      'queued'
    ])
    expect(tasks.map(task => task.id)).toEqual(['finished', 'downloading-old', 'queued', 'downloading-new'])
  })
})
