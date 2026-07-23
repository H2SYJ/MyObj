export interface MobileNavItem {
  key: string
  label: string
  icon: string
  path: string
}

export interface MobileSheetAction {
  key: string
  label: string
  icon?: string
  tone?: 'default' | 'primary' | 'danger'
  disabled?: boolean
}
