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

  it('首页使用 340px 卡片横向滚动，其他桌面列表自适应四列', () => {
    const source = readSource('./Home.vue')
    const folder = readSource('./FolderVideos.vue')
    const watch = readSource('./Watch.vue')
    const shell = readSource('./components/CinemaShell.vue')
    expect(source).toContain('scrollbar-width: none')
    expect(source).toContain('.cinema-section__rail::-webkit-scrollbar')
    expect(shell).toContain('--cinema-home-card-width: 340px')
    expect(source).toContain('grid-auto-columns: var(--cinema-home-card-width, 340px)')
    expect(folder).toContain('grid-template-columns: repeat(4, minmax(0, 1fr))')
    expect(watch).toContain('grid-template-columns: repeat(4, minmax(0, 1fr))')
    expect(source).toContain('@keydown="handleRailKeydown"')
    expect(source).toContain('@wheel="handleRailWheel"')
    expect(source).toContain('touch-action: pan-x pan-y')
    expect(source).toContain('-webkit-overflow-scrolling: touch')
    expect(shell).toContain('overflow-y: auto')
    expect(shell).toContain('.cinema-shell::-webkit-scrollbar')
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

  it('桌面播放页标题和播放器居中排列，相关推荐位于下方网格', () => {
    const source = readSource('./Watch.vue')
    expect(source).toMatch(/\.cinema-watch\s*\{\s*display: block/)
    expect(source).toMatch(/\.cinema-watch__main\s*\{[\s\S]*width: 100%/)
    expect(source).toMatch(/\.cinema-player-frame\s*\{[\s\S]*width: 100%/)
    expect(source).toContain('margin: 0 auto')
    expect(source).toContain('class="cinema-related__grid"')
    expect(source).toMatch(/\.cinema-related__grid\s*\{[\s\S]*grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/)
  })

  it('播放页使用通用内联标签编辑器', () => {
    const source = readSource('./Watch.vue')
    expect(source).toContain('<EditableFileTags')
    expect(source).toContain(':file-id="video.file_id"')
    expect(source).toContain('@updated="handleTagsUpdated"')
  })
})
