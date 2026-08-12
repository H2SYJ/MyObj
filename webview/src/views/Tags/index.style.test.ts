import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compileStyle, parse } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

describe('标签云主题与响应式样式', () => {
  it('从系统变量和分类色派生亮暗主题，并支持移动端与减少动画', () => {
    const filename = fileURLToPath(new URL('./index.vue', import.meta.url))
    const source = readFileSync(filename, { encoding: 'utf-8' })
    const { descriptor } = parse(source, { filename })
    const style = descriptor.styles.find(item => item.scoped)

    expect(style).toBeDefined()
    if (!style) {
      return
    }

    const result = compileStyle({
      filename,
      id: 'data-v-tag-cloud-style',
      scoped: true,
      source: style.content
    })

    expect(result.errors).toEqual([])
    expect(result.code).toContain('color-mix(in srgb, var(--tag-color)')
    expect(result.code).toContain('html.dark .tag-cloud-item')
    expect(result.code).toMatch(/@media \(max-width: 767px\)[\s\S]*\.tag-cloud-item[^}]*min-height: 44px/)
    expect(result.code).toContain('@media (prefers-reduced-motion: reduce)')
    expect(style.content).not.toMatch(/#[0-9a-f]{3,8}\b/i)
    expect(style.content).not.toMatch(/(?:color|background(?:-color)?):\s*(?:white|black)\b/i)
  })
})
