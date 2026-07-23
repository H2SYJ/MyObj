import 'vue-router'

export {}

declare module 'vue-router' {
  interface RouteMeta {
    mobileTitle?: string
    mobileTab?: 'files' | 'offline' | 'tasks' | 'square' | 'me'
    mobileParent?: string
    mobileSearch?: boolean
    hideMobileNav?: boolean
    settingSection?: 'profile' | 'password' | 'appearance' | 'api-key'
  }
}
