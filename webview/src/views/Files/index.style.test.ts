import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compileStyle, parse } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

describe('Files 页面桌面样式', () => {
  it('不会把页面内边距和卡片样式编译到桌面壳层', () => {
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
      id: 'data-v-files-style',
      scoped: true,
      source: style.content
    })

    expect(result.errors).toEqual([])
    expect(result.code).toContain('.desktop-shell .files-page')
    expect(result.code).toContain('.desktop-shell .file-content-area')
    expect(result.code).not.toMatch(/\.desktop-shell\s*\{\s*padding:/)
    expect(result.code).not.toMatch(/\.desktop-shell\s*\{\s*border:/)
  })
})
