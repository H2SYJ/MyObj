import { expect, test, type Page } from '@playwright/test'

const userInfo = {
  id: 1,
  user_name: 'mobile-admin',
  name: '移动端管理员',
  email: 'admin@example.com',
  phone: '13800000000',
  group_id: 1,
  space: 107374182400,
  free_space: 85899345920
}

const cinemaVideo = (id: number) => ({
  file_id: `cinema-video-${id}`,
  file_name: `移动影视视频 ${id}.mp4`,
  file_size: 1048576,
  mime_type: 'video/mp4',
  is_enc: false,
  has_thumbnail: false,
  created_at: '2026-08-04T00:00:00Z',
  directory: { id: 7, name: '移动影视库', parent_id: 0, path: '移动影视库' },
  tags: []
})

const mockApi = async (page: Page) => {
  await page.route('**/dev-api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/dev-api/, '')
    let data: unknown = {}

    if (path === '/user/info') data = userInfo
    else if (path === '/cinema/7/home') {
      data = {
        root: { id: 7, name: '移动影视库', parent_id: 0, path: '移动影视库' },
        sections: Array.from({ length: 4 }, (_, sectionIndex) => ({
          directory: {
            id: 7 + sectionIndex,
            name: sectionIndex === 0 ? '移动影视库' : `移动影视分区 ${sectionIndex}`,
            parent_id: sectionIndex === 0 ? 0 : 7,
            path: sectionIndex === 0 ? '移动影视库' : `移动影视库/移动影视分区 ${sectionIndex}`
          },
          videos: Array.from({ length: 6 }, (_, index) => cinemaVideo(index + 1)),
          total: 6,
          has_more: false
        })),
        total: 4,
        page: 1,
        page_size: Number(url.searchParams.get('page_size') || 20),
        has_more: false
      }
    } else if (/^\/cinema\/7\/videos\/cinema-video-\d+$/.test(path)) {
      const id = Number(path.split('-').at(-1))
      data = {
        root: { id: 7, name: '移动影视库', parent_id: 0, path: '移动影视库' },
        video: cinemaVideo(id)
      }
    } else if (/^\/cinema\/7\/videos\/cinema-video-\d+\/related$/.test(path)) {
      data = {
        videos: [cinemaVideo(2), cinemaVideo(3)],
        total: 2,
        page: 1,
        page_size: 20,
        has_more: false
      }
    } else if (path === '/file/list') {
      const pageNumber = Number(url.searchParams.get('page') || 1)
      const start = (pageNumber - 1) * 20
      const files = Array.from({ length: Math.max(0, Math.min(20, 25 - start)) }, (_, index) => {
        const id = start + index + 1
        return {
          file_id: `file-${id}`,
          file_name: `移动文件 ${id}.txt`,
          file_size: id,
          mime_type: 'text/plain',
          is_enc: false,
          has_thumbnail: false,
          public: false,
          created_at: '2026-07-23T00:00:00Z',
          tags:
            id === 1
              ? [
                  { id: 'tag-4k', name: '4K', category_code: 'resolution', color: '#409eff' },
                  { id: 'tag-movie', name: '电影', category_code: 'title', color: '#67c23a' },
                  { id: 'tag-cn', name: '国语', category_code: 'language', color: '#e6a23c' }
                ]
              : []
        }
      })
      data = {
        breadcrumbs: [],
        current_directory_id: 0,
        folders: [],
        files,
        total: 25,
        page: pageNumber,
        page_size: 20
      }
    } else if (path === '/file/tags/suggestions') {
      data = [
        { id: 'tag-4k', name: '4K', category_code: 'resolution', color: '#409eff', visibility: 'inherit' },
        { id: 'tag-movie', name: '电影', category_code: 'title', color: '#67c23a', visibility: 'inherit' }
      ]
    } else if (path === '/file/search/user') data = { files: [], total: 0, page: 1, page_size: 20 }
    else if (path === '/file/search/public') data = { files: [], total: 0 }
    else if (path === '/file/public/list') data = { files: [], total: 0 }
    else if (path === '/download/list') data = { tasks: [], total: 0, page: 1, page_size: 20 }
    else if (path === '/file/upload/taskList' || path === '/file/upload/uncompleted') data = []
    else if (path === '/file/upload/expired') data = []
    else if (path === '/share/list') {
      data = [
        {
          id: 1,
          file_id: 'mobile-share-file',
          file_name: '移动端分享文件.pdf',
          token: 'mobile-share-token',
          password_hash: '',
          download_count: 1,
          expires_at: '2026-12-31 23:59:59',
          created_at: '2026-07-25 10:00:00'
        }
      ]
    } else if (path === '/recycled/list') data = { items: [], total: 0 }
    else if (path === '/subscription/list') data = { subscriptions: [], total: 0 }
    else if (path === '/subscription/plugins') data = []
    else if (path === '/admin/plugin/list') data = []
    else if (path === '/admin/plugin/audit') data = { items: [] }
    else if (path === '/share/info') {
      data = {
        token: 'public-token',
        file_name: '公开资料.pdf',
        file_size: 1024,
        mime_type: 'application/pdf',
        has_password: false,
        is_expired: false,
        download_count: 2,
        expires_at: null
      }
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, message: 'ok', data })
    })
  })
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(info => {
    localStorage.setItem('token', 'e2e-token')
    localStorage.setItem('userInfo', JSON.stringify(info))
    localStorage.setItem('locale', 'zh-CN')
  }, userInfo)
  await mockApi(page)
})

test('影视播放页在移动端按播放器、标题、相关推荐单列排列', async ({ page }) => {
  await page.goto('/cinema/7/watch/cinema-video-1')
  await expect(page.locator('.cinema-brand')).toContainText('移动影视库')
  await expect(page.locator('.mobile-bottom-nav')).toHaveCount(0)
  expect(await page.locator('.cinema-shell').evaluate(element => getComputedStyle(element).backgroundColor)).toBe(
    'rgb(255, 255, 255)'
  )
  await expect(page.locator('.cinema-poster')).toBeVisible()

  const playerBox = await page.locator('.cinema-player-frame').boundingBox()
  const titleBox = await page.locator('.cinema-watch h1').boundingBox()
  const relatedBox = await page.locator('.cinema-related').boundingBox()
  expect(playerBox).not.toBeNull()
  expect(titleBox).not.toBeNull()
  expect(relatedBox).not.toBeNull()
  expect(playerBox!.y).toBeLessThan(titleBox!.y)
  expect(titleBox!.y).toBeLessThan(relatedBox!.y)
  expect(await page.locator('.cinema-watch').evaluate(element => getComputedStyle(element).display)).toBe('block')
  expect(await page.locator('.cinema-related').evaluate(element => getComputedStyle(element).borderRadius)).toBe('16px')
})

test('影视首页隐藏滚动条并保留横向和纵向滚动', async ({ page }) => {
  await page.goto('/cinema/7')
  const shell = page.locator('.cinema-shell')
  const rail = page.locator('.cinema-section__rail').first()

  await expect(rail.locator('.cinema-video-card')).toHaveCount(6)
  expect(await rail.evaluate(element => getComputedStyle(element).scrollbarWidth)).toBe('none')
  expect(await rail.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await rail.evaluate(element => element.scrollTo({ left: 180 }))
  await expect.poll(() => rail.evaluate(element => element.scrollLeft)).toBeGreaterThan(0)

  expect(await shell.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true)
  await shell.evaluate(element => element.scrollTo({ top: 120 }))
  await expect.poll(() => shell.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
})

test('五个主标签、无限滚动与二级页返回', async ({ page }) => {
  await page.goto('/files')
  const nav = page.locator('.mobile-bottom-nav')
  await expect(nav).toBeVisible()
  await expect(nav.locator('a')).toHaveCount(5)
  await expect(page.locator('.file-card')).toHaveCount(20)
  await page.locator('.mobile-infinite-list .sentinel').scrollIntoViewIfNeeded()
  await expect(page.locator('.file-card')).toHaveCount(25)
  const fabBox = await page.locator('.page-fab').boundingBox()
  const meTabBox = await nav.locator('a[href="/me"]').boundingBox()
  expect(fabBox).not.toBeNull()
  expect(meTabBox).not.toBeNull()
  expect(fabBox!.y + fabBox!.height).toBeLessThanOrEqual(meTabBox!.y)

  for (const [label, path] of [
    ['离线', '/offline'],
    ['任务', '/tasks'],
    ['广场', '/square'],
    ['我的', '/me']
  ] as const) {
    await nav.getByText(label, { exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${path}$`))
  }

  await page.goto('/settings')
  await expect(page.locator('.mobile-bottom-nav')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '返回' })).toBeVisible()
  await page.getByRole('button', { name: '返回' }).click()
  await expect(page).toHaveURL(/\/me$/)
})

test('订阅全屏层、浏览器返回与管理中心', async ({ page }) => {
  await page.goto('/subscriptions')
  await page.getByRole('button', { name: '新建订阅' }).click()
  await expect(page.locator('.el-dialog.is-fullscreen')).toBeVisible()
  await page.goBack()
  await expect(page.locator('.el-dialog.is-fullscreen')).toBeHidden()
  await expect(page).toHaveURL(/\/subscriptions$/)

  await page.goto('/admin')
  await expect(page.locator('.admin-hub-item')).toHaveCount(7)
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await expect(page.locator('html')).toHaveClass(/dark/)
})

test('移动端显示单标签摘要并通过搜索层提交标签', async ({ page }) => {
  const searchRequests: URL[] = []
  page.on('request', request => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/file/search/user')) searchRequests.push(url)
  })
  await page.goto('/files')
  const firstFile = page.locator('.file-card').first()
  await expect(firstFile.locator('.file-tags')).toContainText('4K')
  await expect(firstFile.locator('.file-tags')).toContainText('+2')

  await page.getByRole('button', { name: '搜索' }).first().click()
  const searchLayer = page.locator('.full-screen-layer')
  const search = searchLayer.locator('input')
  await search.fill('#4')
  await expect(searchLayer.getByRole('option', { name: /#4K/ })).toBeVisible()
  await searchLayer.getByRole('option', { name: /#4K/ }).click()
  await expect(searchLayer.locator('.file-search-input__tag')).toContainText('#4K')
  await expect(page).not.toHaveURL(/tags=/)

  await search.fill('#电')
  await expect(searchLayer.getByRole('option', { name: /#电影/ })).toBeVisible()
  await searchLayer.getByRole('option', { name: /#电影/ }).click()
  await expect(searchLayer.locator('.file-search-input__tag')).toHaveCount(2)
  await expect(page).not.toHaveURL(/tags=/)

  await searchLayer.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page).toHaveURL(/tags=tag-4k,tag-movie/)
  await expect.poll(() => searchRequests.length).toBeGreaterThan(0)
  const request = searchRequests[searchRequests.length - 1]
  expect(request.searchParams.get('tag_ids')).toBe('tag-4k,tag-movie')
  expect(request.searchParams.has('tag_mode')).toBe(false)
  expect(request.searchParams.has('directory_id')).toBe(false)
})

test('公开分享页适配手机安全区', async ({ page }) => {
  await page.goto('/share/public-token')
  await expect(page.getByText('公开资料.pdf')).toBeVisible()
  await expect(page.getByRole('button', { name: /下载/ })).toBeVisible()
  await expect(page.locator('.share-download-page')).toHaveCSS(
    'min-height',
    `${await page.evaluate(() => window.innerHeight)}px`
  )
})

test('表格卡片选择后立即显示底部批量操作栏', async ({ page }) => {
  await page.goto('/shares')
  const checkbox = page.locator('.mobile-share-item .mobile-checkbox').first()
  await expect(checkbox).toBeVisible()
  await checkbox.click()

  const actions = page.locator('.table-selection-actions--floating')
  await expect(actions).toBeVisible()
  const position = await actions.boundingBox()
  const viewport = page.viewportSize()
  expect(position).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(position!.y + position!.height).toBeLessThanOrEqual(viewport!.height - 70)

  await actions.locator('[data-test="selection-clear"]').click()
  await expect(actions).toHaveCount(0)
})
