import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const readSource = (relativePath: string) => {
  const filename = fileURLToPath(new URL(relativePath, import.meta.url))
  return readFileSync(filename, { encoding: 'utf-8' })
}

const removeWhitespace = (source: string) => source.replace(/\s+/g, '')

const fullscreenExclusions =
  ':not(.xgplayer-rotate-fullscreen):not(.xgplayer-is-fullscreen):not(.xgplayer-is-cssfullscreen)'

describe('XgPlayer 全屏样式', () => {
  it('普通播放器布局不覆盖 xgplayer 的三种全屏状态', () => {
    const playerSource = readSource('./index.vue')
    const rendererSource = readSource('../Preview/renderers/MediaPreviewRenderer.vue')

    expect(removeWhitespace(playerSource)).toContain(`.xg-player-wrapper${fullscreenExclusions}`)
    expect(removeWhitespace(rendererSource)).toContain(`.preview-video-xgplayer${fullscreenExclusions}`)
    expect(playerSource).not.toMatch(/^\s*\.xg-player-wrapper\s*\{/m)
    expect(rendererSource).not.toMatch(/^\s*\.preview-video-xgplayer\s*\{/m)
  })

  it('预览弹窗挂载到 body 且不再重复定义播放器根布局', () => {
    const previewSource = readSource('../Preview/index.vue')

    expect(previewSource).toMatch(/<el-dialog[\s\S]*?append-to-body/)
    expect(previewSource).not.toMatch(/^\s*\.preview-video-xgplayer(?:\s|:)/m)
  })
})
