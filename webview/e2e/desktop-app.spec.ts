import { expect, test, type Page } from '@playwright/test'

const userInfo = {
  id: 'desktop-admin',
  user_name: 'desktop-admin',
  name: '桌面端管理员',
  email: 'admin@example.com',
  phone: '13800000000',
  group_id: 1,
  space: 107374182400,
  free_space: 85899345920,
  state: 0
}

const offlineTask = {
  id: 'offline-task-1',
  url: 'https://example.com/video.mp4',
  file_name: '离线下载示例.mp4',
  file_size: 1048576,
  downloaded_size: 524288,
  progress: 50,
  speed: 1024,
  type: 0,
  type_text: 'HTTP',
  state: 1,
  state_text: '下载中',
  save_path: '/',
  support_range: true,
  enable_encryption: false,
  requires_password: false,
  has_request_headers: false,
  requires_headers: false,
  error_msg: '',
  file_id: '',
  create_time: '2026-07-25 10:00:00',
  update_time: '2026-07-25 10:01:00',
  finish_time: ''
}

const cinemaVideo = (id: number) => ({
  file_id: `cinema-video-${id}`,
  file_name: `影视验收视频 ${id}.mp4`,
  file_size: 1048576,
  mime_type: 'video/mp4',
  is_enc: false,
  has_thumbnail: false,
  created_at: `2026-08-04T0${Math.min(id, 9)}:00:00Z`,
  directory: { id: 7, name: '影视库', parent_id: 0, path: '影视库' },
  tags: []
})

const mockApi = async (page: Page) => {
  await page.route('**/dev-api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/dev-api/, '')
    let data: unknown = {}

    if (path.startsWith('/file/thumbnail/')) {
      await route.fulfill({
        status: 200,
        contentType: 'image/svg+xml',
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="90"><rect width="160" height="90" fill="#2563eb"/><path d="M0 72l42-34 30 24 28-20 60 48H0z" fill="#93c5fd"/></svg>'
      })
      return
    }

    if (path === '/user/info') {
      data = userInfo
    } else if (path === '/user/sysInfo') {
      data = { is_first_use: false, allow_register: true }
    } else if (path === '/file/list') {
      data = {
        breadcrumbs: [],
        current_directory_id: 0,
        folders: [{ id: 1, name: '项目资料', parent_id: 0, created_at: '2026-07-23T00:00:00Z' }],
        files: [
          {
            file_id: 'desktop-file-1',
            file_name: '桌面验收报告.pdf',
            file_size: 2048,
            mime_type: 'application/pdf',
            is_enc: false,
            has_thumbnail: true,
            public: false,
            created_at: '2026-07-23T00:00:00Z'
          }
        ],
        total: 2,
        page: 1,
        page_size: 20
      }
    } else if (path === '/cinema/7/home') {
      data = {
        root: { id: 7, name: '影视库', parent_id: 0, path: '影视库' },
        sections: [
          {
            directory: { id: 7, name: '影视库', parent_id: 0, path: '影视库' },
            videos: Array.from({ length: 6 }, (_, index) => cinemaVideo(index + 1)),
            total: 8,
            has_more: true
          }
        ],
        total: 1,
        page: 1,
        page_size: Number(url.searchParams.get('page_size') || 20),
        has_more: false
      }
    } else if (path === '/cinema/7/folders/7/videos') {
      data = {
        root: { id: 7, name: '影视库', parent_id: 0, path: '影视库' },
        directory: { id: 7, name: '影视库', parent_id: 0, path: '影视库' },
        videos: Array.from({ length: 8 }, (_, index) => cinemaVideo(index + 1)),
        total: 8,
        page: 1,
        page_size: 24,
        has_more: false
      }
    } else if (/^\/cinema\/7\/videos\/cinema-video-\d+$/.test(path)) {
      const id = Number(path.split('-').at(-1))
      data = { root: { id: 7, name: '影视库', parent_id: 0, path: '影视库' }, video: cinemaVideo(id) }
    } else if (/^\/cinema\/7\/videos\/cinema-video-\d+\/related$/.test(path)) {
      data = {
        videos: [cinemaVideo(6), cinemaVideo(5)],
        total: 2,
        page: 1,
        page_size: 20,
        has_more: false
      }
    } else if (path === '/file/search/user') {
      data = { files: [], total: 0 }
    } else if (path === '/file/tags/suggestions') {
      data = [
        {
          id: 'tag-4k',
          name: '4K',
          category_code: 'resolution',
          color: '#409eff',
          visibility: 'inherit'
        },
        {
          id: 'tag-movie',
          name: '电影',
          category_code: 'title',
          color: '#67c23a',
          visibility: 'inherit'
        }
      ]
    } else if (path === '/file/search/public' || path === '/file/public/list') {
      data = { files: [], total: 0 }
    } else if (path === '/admin/user/list') {
      data = {
        users: [{ ...userInfo, group_name: '管理员', created_at: '2026-07-23 08:00:00' }],
        total: 1,
        page: 1,
        page_size: 20
      }
    } else if (path === '/admin/group/list') {
      data = {
        groups: [{ id: 1, name: '管理员', group_default: 1, space: 0, created_at: '2026-07-23 08:00:00' }],
        total: 1
      }
    } else if (path === '/admin/plugin/list') {
      data = []
    } else if (path === '/admin/plugin/audit') {
      data = { items: [], total: 0 }
    } else if (path === '/download/list') {
      const tasks = url.searchParams.has('types') ? [offlineTask] : []
      data = { tasks, total: tasks.length, page: 1, page_size: 20 }
    } else if (path === '/file/upload/taskList' || path === '/file/upload/uncompleted') {
      data = []
    } else if (path === '/file/upload/expired') {
      data = [
        {
          id: 'expired-task-1',
          file_name: '过期上传任务.zip',
          file_size: 2048,
          chunk_size: 1024,
          total_chunks: 2,
          uploaded_chunks: 1,
          progress: 50,
          status: 'expired',
          is_enc: false,
          directory_id: 0,
          create_time: '2026-07-20 10:00:00',
          update_time: '2026-07-20 10:01:00',
          expire_time: '2026-07-21 10:00:00'
        }
      ]
    } else if (path === '/share/list') {
      data = [
        {
          id: 1,
          file_id: 'shared-file-1',
          file_name: '分享验收文件.pdf',
          token: 'desktop-share-token',
          password_hash: '',
          download_count: 3,
          expires_at: '2026-12-31 23:59:59',
          created_at: '2026-07-25 10:00:00'
        }
      ]
    } else if (path === '/recycled/list') {
      data = {
        items: [
          {
            recycled_id: 'recycled-1',
            item_type: 'file',
            item_name: '回收站验收文件.txt',
            item_count: 0,
            file_id: 'recycled-file-1',
            file_name: '回收站验收文件.txt',
            file_size: 1024,
            mime_type: 'text/plain',
            is_enc: false,
            has_thumbnail: false,
            deleted_at: '2026-07-24 10:00:00'
          }
        ],
        total: 1,
        page: 1,
        pageSize: 20
      }
    } else if (path === '/admin/power/list') {
      data = {
        powers: [
          {
            id: 1,
            name: '验收权限',
            description: '用于验证批量操作栏',
            characteristic: 'acceptance.read',
            created_at: '2026-07-25 10:00:00'
          }
        ],
        total: 1
      }
    } else if (path === '/subscription/list') {
      data = { subscriptions: [], total: 0 }
    } else if (path === '/subscription/plugins') {
      data = []
    } else if (path === '/share/info') {
      data = {
        file_id: 'public-file',
        file_name: '公开资料.pdf',
        file_size: 1024,
        mime_type: 'application/pdf',
        has_password: false,
        is_expired: false,
        download_count: 2,
        expires_at: '2026-12-31 23:59:59'
      }
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 200, message: 'ok', data })
    })
  })
}

test.beforeEach(async ({ page }, testInfo) => {
  const preferences = {
    locale: testInfo.project.name === 'chromium-desktop-1024' ? 'en-US' : 'zh-CN',
    theme: testInfo.project.name === 'chromium-desktop-1440' ? 'light' : 'dark'
  }
  await page.addInitScript(
    ({ info, preferences: initialPreferences }) => {
      localStorage.setItem('token', 'desktop-e2e-token')
      localStorage.setItem('userInfo', JSON.stringify(info))
      localStorage.setItem('locale', initialPreferences.locale)
      localStorage.setItem('theme', initialPreferences.theme)
    },
    { info: userInfo, preferences }
  )
  await mockApi(page)
})

test('影视模式桌面布局、隐藏横向滚动条和路由前进后退', async ({ page }, testInfo) => {
  await page.goto('/cinema/7')
  await expect(page.locator('.cinema-brand')).toContainText('影视库')
  await expect(page.locator('.layout-sidebar')).toHaveCount(0)
  expect(await page.locator('.cinema-shell').evaluate(element => getComputedStyle(element).backgroundColor)).toBe(
    'rgb(255, 255, 255)'
  )
  const rail = page.locator('.cinema-section__rail')
  await expect(rail.locator('.cinema-video-card')).toHaveCount(6)
  expect(await rail.evaluate(element => getComputedStyle(element).scrollbarWidth)).toBe('none')
  expect(
    await rail
      .locator('.cinema-video-card')
      .first()
      .evaluate(element => getComputedStyle(element).borderRadius)
  ).toBe('16px')

  await rail.locator('.cinema-video-card').first().click()
  await expect(page).toHaveURL(/\/cinema\/7\/watch\/cinema-video-1$/)
  await expect(page.locator('.cinema-watch h1')).toContainText('影视验收视频 1')
  const watchDisplay = await page.locator('.cinema-watch').evaluate(element => getComputedStyle(element).display)
  if (testInfo.project.name === 'chromium-desktop-1440' || testInfo.project.name === 'chromium-desktop-1024') {
    expect(watchDisplay).toBe('grid')
    expect(
      await page.locator('.cinema-watch').evaluate(element => getComputedStyle(element).gridTemplateColumns)
    ).toContain('340px')
  } else {
    expect(watchDisplay).toBe('block')
  }

  await page.goBack()
  await expect(page).toHaveURL(/\/cinema\/7$/)
  await page.goForward()
  await expect(page).toHaveURL(/\/cinema\/7\/watch\/cinema-video-1$/)
})

test('固定侧栏、路由搜索恢复与无横向滚动', async ({ page }) => {
  await page.goto('/files')
  await expect(page.locator('.desktop-shell')).toBeVisible()
  await expect(page.locator('.desktop-sidebar')).toBeVisible()
  await expect(page.locator('.desktop-header')).toBeVisible()
  await expect(page.locator('.mobile-bottom-nav')).toHaveCount(0)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
  await expect(page).toHaveScreenshot('desktop-files-shell.png', { fullPage: true, maxDiffPixelRatio: 0.03 })

  const search = page.locator('.desktop-header__search input')
  await search.fill('验收报告')
  await search.press('Enter')
  await expect(page).toHaveURL(/\/files\?.*search=%E9%AA%8C%E6%94%B6%E6%8A%A5%E5%91%8A/)
  await page.reload()
  await expect(search).toHaveValue('验收报告')

  await search.fill('')
  await search.press('Enter')
  await expect(page.locator('.file-grid')).toBeVisible()
  await page.locator('.file-view-toolbar .el-button-group button').nth(1).click()
  await expect(page.locator('.file-list')).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-files-list.png', { fullPage: true, maxDiffPixelRatio: 0.03 })
})

test('桌面搜索栏通过井号选择标签并在提交后跨目录任一匹配', async ({ page }) => {
  const searchRequests: URL[] = []
  page.on('request', request => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/file/search/user')) searchRequests.push(url)
  })
  await page.goto('/files?directoryId=12')
  const search = page.locator('.desktop-header__search input')
  await search.fill('#4')
  await expect(page.getByRole('option', { name: /#4K/ })).toBeVisible()
  await page.getByRole('option', { name: /#4K/ }).click()
  await expect(page.locator('.desktop-header__search .file-search-input__tag')).toContainText('#4K')
  await expect(page).not.toHaveURL(/tags=/)

  await search.fill('#电')
  await expect(page.getByRole('option', { name: /#电影/ })).toBeVisible()
  await page.getByRole('option', { name: /#电影/ }).click()
  await expect(page.locator('.desktop-header__search .file-search-input__tag')).toHaveCount(2)
  await expect(page).not.toHaveURL(/tags=/)

  await search.fill('报告')
  await search.press('Enter')
  await expect(page).toHaveURL(/tags=tag-4k,tag-movie/)
  await expect(page).toHaveURL(/search=%E6%8A%A5%E5%91%8A/)
  await expect.poll(() => searchRequests.length).toBeGreaterThan(0)
  const request = searchRequests[searchRequests.length - 1]
  expect(request.searchParams.get('tag_ids')).toBe('tag-4k,tag-movie')
  expect(request.searchParams.has('tag_mode')).toBe(false)
  expect(request.searchParams.has('directory_id')).toBe(false)

  await page.reload()
  await expect(page.locator('.desktop-header__search .file-search-input__tag')).toHaveCount(2)
  await expect(page.locator('.desktop-header__search .file-search-input__tag')).toContainText(['#4K', '#电影'])
  await page.locator('.desktop-header__search .file-search-input__clear').click()
  await expect(page).not.toHaveURL(/(?:search|tags)=/)
})

test('任务查询参数、账户概览和设置分区可直接访问', async ({ page }, testInfo) => {
  await page.goto('/tasks?tab=download')
  const taskTabs = page.getByRole('tablist', { name: /传输列表|Transfer List/i })
  await expect(taskTabs).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-tasks.png', { fullPage: true })
  await taskTabs.getByRole('tab').first().click()
  await expect(page).toHaveURL(/\/tasks\?tab=upload/)

  await page.goto('/me')
  await expect(page.locator('.account-overview')).toBeVisible()

  const isEnglish = testInfo.project.name === 'chromium-desktop-1024'
  await page.goto('/subscriptions')
  await expect(page.getByRole('heading', { name: isEnglish ? 'Subscriptions' : '订阅管理', exact: true })).toBeVisible()

  await page.goto('/settings/appearance')
  await expect(page.locator('.desktop-settings')).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-settings.png', { fullPage: true, maxDiffPixels: 50 })

  await page.goto('/admin')
  await expect(page.locator('.admin-workspace')).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-admin.png', { fullPage: true })

  await page.goto('/admin/plugins')
  await expect(page.locator('.plugin-center')).toBeVisible()
  await expect(page.locator('.plugin-center .toolbar button').first()).toContainText(
    isEnglish ? 'Install Plugin' : '安装插件'
  )

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
})

test('登录与公开分享使用独立品牌壳层', async ({ page, context }, testInfo) => {
  await page.goto('/share/public-token')
  await expect(page.locator('.public-shell')).toBeVisible()
  await expect(page.getByText('公开资料.pdf')).toBeVisible()
  await expect(page.locator('.action-section button')).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-share-download.png', { fullPage: true })

  const loginPage = await context.newPage()
  const preferences = {
    locale: testInfo.project.name === 'chromium-desktop-1024' ? 'en-US' : 'zh-CN',
    theme: testInfo.project.name === 'chromium-desktop-1440' ? 'light' : 'dark'
  }
  await loginPage.addInitScript(initialPreferences => {
    localStorage.clear()
    localStorage.setItem('locale', initialPreferences.locale)
    localStorage.setItem('theme', initialPreferences.theme)
  }, preferences)
  await mockApi(loginPage)
  await loginPage.goto('/login')
  await expect(loginPage.locator('.public-shell')).toBeVisible()
  await expect(loginPage.getByText('MyObj')).toBeVisible()
  await expect(loginPage).toHaveScreenshot('desktop-login.png', { fullPage: true })
  await loginPage.close()
})

test('表格选择操作立即切换并保持主题样式', async ({ page }, testInfo) => {
  const verifySelectionActions = async (path: string, checkboxSelector: string) => {
    await page.goto(path)
    const checkbox = page.locator(checkboxSelector).first()
    await expect(checkbox).toBeVisible()
    await checkbox.click()

    const actions = page.locator('.table-selection-actions--inline').first()
    await expect(actions).toBeVisible()
    await actions.locator('[data-test="selection-clear"]').click()
    await expect(page.locator('.table-selection-actions--inline')).toHaveCount(0)
  }

  await page.goto('/shares')
  const headerCell = page.locator('.shares-table th.el-table__cell').first()
  await expect(headerCell).toBeVisible()
  const headerBackground = await headerCell.evaluate(element => getComputedStyle(element).backgroundColor)
  expect(headerBackground).toBe(
    testInfo.project.name === 'chromium-desktop-1440' ? 'rgb(255, 255, 255)' : 'rgba(0, 0, 0, 0)'
  )

  await verifySelectionActions('/shares', '.shares-table .el-table__body-wrapper .el-checkbox')
  await verifySelectionActions('/trash', '.trash-table .el-table__body-wrapper .el-checkbox')
  await verifySelectionActions(
    '/offline',
    '.offline-table .el-table__body-wrapper .el-checkbox:visible, .mobile-task-list .task-checkbox:visible'
  )
  if (testInfo.project.name !== 'chromium-desktop-1440') {
    const statusTag = page.locator('.el-tag--primary:visible').first()
    await expect(statusTag).toBeVisible()
    const tagColors = await statusTag.evaluate(element => {
      const style = getComputedStyle(element)
      return { background: style.backgroundColor, border: style.borderColor, color: style.color }
    })
    expect(tagColors).toEqual({
      background: 'rgba(59, 130, 246, 0.08)',
      border: 'rgba(96, 165, 250, 0.32)',
      color: 'rgb(147, 197, 253)'
    })
  }
  await verifySelectionActions('/admin/permissions', '.admin-table .el-table__body-wrapper .el-checkbox')

  await page.goto('/tasks?tab=upload')
  await page.getByRole('button', { name: /过期|Expired/i }).click()
  const expiredCheckbox = page.locator(
    '.expired-tasks-table .el-table__body-wrapper .el-checkbox:visible, .expired-tasks-dialog .mobile-task-list .task-checkbox:visible'
  )
  await expect(expiredCheckbox.first()).toBeVisible()
  await expiredCheckbox.first().click()
  await expect(page.locator('.expired-tasks-dialog .table-selection-actions--inline')).toBeVisible()
})

test('文件宫格使用 300px 卡片和完整 16:9 缩略图', async ({ page }) => {
  await page.goto('/files')

  const card = page.locator('.file-card').filter({ hasText: '桌面验收报告.pdf' })
  const preview = card.locator('.file-preview')
  const thumbnail = preview.locator('.thumbnail-image')
  await expect(thumbnail).toBeVisible()

  const metrics = await card.evaluate(element => {
    const previewElement = element.querySelector<HTMLElement>('.file-preview')!
    const image = element.querySelector<HTMLImageElement>('.thumbnail-image')!
    const cardRect = element.getBoundingClientRect()
    const previewRect = previewElement.getBoundingClientRect()
    return {
      cardWidth: cardRect.width,
      previewRatio: previewRect.width / previewRect.height,
      objectFit: getComputedStyle(image).objectFit
    }
  })

  expect(metrics.cardWidth).toBeGreaterThanOrEqual(300)
  expect(metrics.previewRatio).toBeCloseTo(16 / 9, 1)
  expect(metrics.objectFit).toBe('contain')
})
