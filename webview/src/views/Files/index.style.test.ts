import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compileStyle, parse } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

describe('Files 页面工作区结构', () => {
  it('复用 WorkspacePage 管理页面各区域', () => {
    const filename = fileURLToPath(new URL('./index.vue', import.meta.url))
    const source = readFileSync(filename, { encoding: 'utf-8' })
    const { descriptor } = parse(source, { filename })
    const style = descriptor.styles.find(item => item.scoped)
    const template = descriptor.template?.content || ''

    expect(source).toContain("import WorkspacePage from '@/components/WorkspacePage/index.vue'")
    expect(template).toMatch(/<WorkspacePage :title="t\('route\.files'\)">/)
    expect(template).toContain('#actions')
    expect(template).toContain('#toolbar')
    expect(template).toContain('class="file-workspace-breadcrumb"')
    expect(template).toContain('name="file-selection-toolbar"')
    expect(template).toContain('class="file-selection-count"')
    expect(template).toContain('#footer')
    expect(template).toContain('#floating')
    expect(template).toContain('#overlays')

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
    expect(result.code).toContain('.files-page')
    expect(result.code).toContain('.file-content-area')
    expect(result.code).toContain('.file-selection-toolbar-enter-active')
    expect(result.code).toContain('@media (prefers-reduced-motion: reduce)')
    expect(result.code).not.toContain('.desktop-shell')
  })
})
