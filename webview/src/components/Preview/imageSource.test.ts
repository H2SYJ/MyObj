import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

describe('Preview 图片来源', () => {
  it('预览图片时加载原文件而不是缩略图', () => {
    const filename = fileURLToPath(new URL('./index.vue', import.meta.url))
    const source = readFileSync(filename, { encoding: 'utf-8' })
    const imageBranch = source.match(/case 'image':[\s\S]*?break/)?.[0] || ''

    expect(imageBranch).toContain("commitPreviewUrl('image', await getFilePreviewUrl(fileId), requestVersion)")
    expect(imageBranch).not.toContain('THUMBNAIL')
    expect(imageBranch).not.toContain('getThumbnail')
  })
})
