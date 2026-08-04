import 'vue-router'
import type { DesktopRouteMeta } from './desktop'

export {}

declare module 'vue-router' {
  interface RouteMeta extends DesktopRouteMeta {
    title?: string
    i18nKey?: string
    requiresAuth?: boolean
    requiresAdmin?: boolean
    mobileTitle?: string
    mobileTab?: 'files' | 'offline' | 'tasks' | 'square' | 'me'
    mobileParent?: string
    mobileSearch?: boolean
    hideMobileNav?: boolean
    settingSection?: 'profile' | 'password' | 'appearance' | 'api-key' | 'tag-dictionary'
  }
}
