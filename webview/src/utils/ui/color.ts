/**
 * UI 颜色工具：标签对比文字颜色计算
 * 背景色 = 自定义颜色；文字色 = 依据背景亮度自动选黑/白
 */
import { getLightColor } from '@/utils/config/color'

const isDarkTheme = () => typeof document !== 'undefined' && document.documentElement.classList.contains('dark')

const isHex = (color: string) => /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(color)

/** 计算颜色相对亮度（0-1，线性加权近似） */
export const getLuminance = (color: string): number => {
  const value = String(color ?? '').trim()
  if (!value) {
    return 0
  }
  let r = 0
  let g = 0
  let b = 0
  if (value.startsWith('#')) {
    let hex = value.slice(1)
    if (hex.length === 3) {
      hex = hex
        .split('')
        .map(c => c + c)
        .join('')
    }
    r = parseInt(hex.slice(0, 2), 16)
    g = parseInt(hex.slice(2, 4), 16)
    b = parseInt(hex.slice(4, 6), 16)
  } else {
    const match = value.match(/rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i)
    if (!match) {
      return 0
    }
    r = Number(match[1])
    g = Number(match[2])
    b = Number(match[3])
  }
  if ([r, g, b].some(Number.isNaN)) {
    return 0
  }
  return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
}

/** 根据背景色返回可读文字色（阈值 0.6） */
export const getContrastText = (color: string, threshold = 0.6): string =>
  getLuminance(color) >= threshold ? '#111827' : '#ffffff'

/**
 * 生成 el-tag 的 CSS 变量样式：背景=自定义色（深色模式提亮 35%），文字=自动对比色。
 * 内联变量优先级最高，可压过 element-plus.css 的 html.dark .el-tag 规则。
 * 非 hex 格式（如 rgba）不做提亮，直接用原色。
 */
export const getTagStyle = (color?: string): Record<string, string> => {
  if (!color) {
    return {}
  }
  const bg = isDarkTheme() && isHex(color) ? getLightColor(color, 0.35) : color
  return {
    '--el-tag-bg-color': bg,
    '--el-tag-border-color': bg,
    '--el-tag-text-color': getContrastText(bg)
  }
}
