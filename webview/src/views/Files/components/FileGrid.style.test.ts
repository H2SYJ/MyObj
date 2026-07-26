import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compileStyle, parse } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

describe('FileGrid 宫格缩略图布局', () => {
  it('桌面使用 300px 卡片和 16:9 完整缩略图', () => {
    const filename = fileURLToPath(new URL('./FileGrid.vue', import.meta.url))
    const source = readFileSync(filename, { encoding: 'utf-8' })
    const { descriptor } = parse(source, { filename })
    const style = descriptor.styles.find(item => item.scoped)

    expect(style).toBeDefined()
    if (!style) {
      return
    }

    const result = compileStyle({
      filename,
      id: 'data-v-file-grid-style',
      scoped: true,
      source: style.content
    })

    expect(result.errors).toEqual([])
    expect(result.code).toContain('minmax(300px, 1fr)')
    expect(result.code).toContain('aspect-ratio: 16 / 9')
    expect(result.code).toContain('object-fit: contain')
    expect(result.code).toContain('@media (max-width: 767px)')
    expect(result.code).toContain('minmax(160px, 1fr)')
    expect(result.code).toContain('minmax(132px, 1fr)')
  })
})
