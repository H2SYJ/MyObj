import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const readSource = (relative: string) => readFileSync(fileURLToPath(new URL(relative, import.meta.url)), 'utf8')

describe('影视模式页面结构', () => {
  it('使用独立白色背景与统一圆角卡片', () => {
    const shell = readSource('./components/CinemaShell.vue')
    const card = readSource('./components/CinemaVideoCard.vue')
    expect(shell).toContain('background: #fff')
    expect(shell).toContain('color-scheme: light')
    expect(card).toContain('border-radius: 16px')
    expect(card).toContain('background: #fff')
  })

  it('首页横向分区隐藏滚动条并限制桌面六列', () => {
    const source = readSource('./Home.vue')
    expect(source).toContain('scrollbar-width: none')
    expect(source).toContain('.cinema-section__rail::-webkit-scrollbar')
    expect(source).toContain('calc((100% - 80px) / 6)')
    expect(source).toContain('@keydown="handleRailKeydown"')
  })

  it('播放页先显示封面按钮，用户操作后才挂载播放器', () => {
    const source = readSource('./Watch.vue')
    expect(source).toContain('v-if="videoUrl"')
    expect(source).toContain('@click="startPlayback"')
    expect(source).toContain("inputType: 'password'")
  })

  it('移动端把播放器放在标题之前并保留相关视频单列', () => {
    const source = readSource('./Watch.vue')
    expect(source).toMatch(/@media \(max-width: 900px\)[\s\S]*\.cinema-player-frame\s*\{[\s\S]*order: 1/)
    expect(source).toMatch(/@media \(max-width: 900px\)[\s\S]*h1\s*\{[\s\S]*order: 2/)
  })
})
