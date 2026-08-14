import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

describe('文件广场搜索结果右键菜单', () => {
  it('网格、表格和移动列表复用公开文件菜单', () => {
    const source = readFileSync(fileURLToPath(new URL('./index.vue', import.meta.url)), { encoding: 'utf-8' })

    expect(source).toContain('@contextmenu.prevent="openPublicFileContextMenu(file, $event)"')
    expect(source).toContain('@row-contextmenu="handleRowContextMenu"')
    expect(source).toContain('<FileContextMenu')
    expect(source).toContain("{ key: 'preview', label: t('files.preview'), icon: View }")
    expect(source).toContain("{ key: 'download', label: t('files.download'), icon: Download }")
  })
})
