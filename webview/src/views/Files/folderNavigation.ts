import type { FolderItem } from '@/types'

export const cinemaRouteForFolder = (folder: FolderItem): string | undefined =>
  folder.cinema_mode ? `/cinema/${folder.id}` : undefined
