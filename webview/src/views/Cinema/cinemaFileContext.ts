import type { InjectionKey } from 'vue'
import type { CinemaVideo } from '@/api/cinema'

export type OpenCinemaFileContextMenu = (video: CinemaVideo, event: MouseEvent | KeyboardEvent) => void

export const cinemaFileContextMenuKey: InjectionKey<OpenCinemaFileContextMenu> = Symbol('cinema-file-context-menu')
