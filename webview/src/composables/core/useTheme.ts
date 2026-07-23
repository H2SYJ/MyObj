import { toggleCssDarkMode } from '@/utils/config/theme'

export type Theme = 'light' | 'dark' | 'auto'

const theme = ref<Theme>('auto')
const isDark = ref(false)
const grayscale = ref(false)
const colourWeakness = ref(false)
let initialized = false
let mediaQuery: MediaQueryList | null = null

const applyAuxiliaryModes = () => {
  const filters: string[] = []
  if (grayscale.value) filters.push('grayscale(100%)')
  if (colourWeakness.value) filters.push('invert(80%)')
  document.documentElement.style.filter = filters.join(' ')
}

const applyTheme = () => {
  const root = document.documentElement
  root.classList.add('theme-transitioning')
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  isDark.value = theme.value === 'auto' ? prefersDark : theme.value === 'dark'
  toggleCssDarkMode(isDark.value)
  window.setTimeout(() => root.classList.remove('theme-transitioning'), 300)
}

const loadTheme = () => {
  const savedTheme = localStorage.getItem('theme') as Theme | null
  theme.value = savedTheme && ['light', 'dark', 'auto'].includes(savedTheme) ? savedTheme : 'auto'
  grayscale.value = localStorage.getItem('grayscale') === 'true'
  colourWeakness.value = localStorage.getItem('colourWeakness') === 'true'
  applyTheme()
  applyAuxiliaryModes()
}

/** PC 与移动端共享的核心主题能力；废弃的自定义颜色与预设配置不再读取。 */
export function useTheme() {
  const setTheme = (value: Theme) => {
    theme.value = value
    localStorage.setItem('theme', value)
    applyTheme()
  }

  const toggleTheme = () => {
    if (theme.value === 'auto') setTheme(isDark.value ? 'light' : 'dark')
    else setTheme(theme.value === 'light' ? 'dark' : 'light')
  }

  const setGrayscale = (value: boolean) => {
    grayscale.value = value
    localStorage.setItem('grayscale', String(value))
    applyAuxiliaryModes()
  }

  const setColourWeakness = (value: boolean) => {
    colourWeakness.value = value
    localStorage.setItem('colourWeakness', String(value))
    applyAuxiliaryModes()
  }

  onMounted(() => {
    if (initialized) return
    initialized = true
    loadTheme()
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    mediaQuery.addEventListener('change', applyTheme)
  })

  return {
    theme: readonly(theme),
    isDark: readonly(isDark),
    grayscale: readonly(grayscale),
    colourWeakness: readonly(colourWeakness),
    toggleTheme,
    setTheme,
    applyTheme,
    setGrayscale,
    setColourWeakness
  }
}
