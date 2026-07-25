import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const readSource = (relativePath: string) =>
  readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), { encoding: 'utf-8' })

describe('表格批量操作栏接入', () => {
  const pageSources = [
    '../../views/Shares/index.vue',
    '../../views/Trash/index.vue',
    '../../views/Offline/index.vue',
    '../../views/Admin/Permissions/index.vue',
    '../../views/Tasks/components/ExpiredTasksDialog.vue'
  ].map(readSource)

  it('五处选择入口同时提供顶部和移动端操作模式', () => {
    for (const source of pageSources) {
      expect(source).toContain('TableSelectionActions')
      expect(source).toContain('mode="floating"')
      expect(source).toContain('@clear=')
      expect(source).not.toContain('useDelayedSelectionDisplay')
    }
  })

  it('离线下载的种子文件选择仍保留原表单流程', () => {
    const offline = pageSources[2]

    expect(offline).toContain('class="file-selection-section"')
    expect(offline).toContain('class="selection-header"')
    expect(offline).toContain('handleStartTorrentDownload')
  })
})
