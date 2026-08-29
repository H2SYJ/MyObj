import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const css = readFileSync(fileURLToPath(new URL('./element-plus.css', import.meta.url)), { encoding: 'utf-8' })

describe('Element Plus 全局主题样式', () => {
  it('仅在浅色主题将表格表头统一为白色', () => {
    expect(css).toContain('html:not(.dark) .el-table {')
    expect(css).toContain('--el-table-header-bg-color: #ffffff;')
    expect(css).toMatch(/html:not\(\.dark\) \.el-table th\.el-table__cell\s*{[^}]*background: #ffffff !important;/s)
    expect(css).toMatch(/html\.dark \.el-table th\.el-table__cell\s*{[^}]*background: transparent !important;/s)
  })

  it('为深色主题提供完整的柔和 Tag 色板', () => {
    for (const value of [
      '59, 130, 246',
      '96, 165, 250',
      '#93c5fd',
      '16, 185, 129',
      '52, 211, 153',
      '#6ee7b7',
      '245, 158, 11',
      '251, 191, 36',
      '#fcd34d',
      '239, 68, 68',
      '248, 113, 113',
      '#fca5a5',
      '148, 163, 184',
      '#cbd5e1'
    ]) {
      expect(css).toContain(value)
    }

    expect(css).toMatch(/html\.dark \.el-tag\.el-tag--plain\s*{[^}]*--myobj-tag-bg-alpha: 0\.08;/s)
    expect(css).toMatch(/html\.dark \.el-tag\.el-tag--dark\s*{[^}]*--myobj-tag-bg-alpha: 0\.32;/s)
  })

  it('为业务标签提供由源色派生的浅底彩字配色（亮暗自适应）', () => {
    expect(css).toMatch(
      /\.myobj-tag\s*{[^}]*--myobj-tag-color: var\(--el-color-primary\);[\s\S]*?--el-tag-bg-color: color-mix\(in srgb, var\(--myobj-tag-color\) 12%, transparent\);/s
    )
    expect(css).toMatch(
      /\.myobj-tag\s*{[^}]*--el-tag-border-color: color-mix\(in srgb, var\(--myobj-tag-color\) 28%, transparent\);/s
    )
    expect(css).toMatch(
      /html\.dark \.myobj-tag\s*{[^}]*--el-tag-bg-color: color-mix\(in srgb, var\(--myobj-tag-color\) 20%, transparent\);/s
    )
    expect(css).toMatch(
      /html\.dark \.myobj-tag\s*{[^}]*--el-tag-border-color: color-mix\(in srgb, var\(--myobj-tag-color\) 38%, transparent\);/s
    )
    expect(css).toMatch(
      /html\.dark \.myobj-tag\s*{[^}]*--el-tag-text-color: color-mix\(in srgb, var\(--myobj-tag-color\) 60%, rgb\(255 255 255\)\);/s
    )
  })
})
