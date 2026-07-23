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

const mockApi = async (page: Page) => {
  await page.route('**/dev-api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/dev-api/, '')
    let data: unknown = {}

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
            has_thumbnail: false,
            public: false,
            created_at: '2026-07-23T00:00:00Z'
          }
        ],
        total: 2,
        page: 1,
        page_size: 20
      }
    } else if (path === '/file/search/user') {
      data = { files: [], total: 0 }
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
      data = { tasks: [], total: 0, page: 1, page_size: 20 }
    } else if (path === '/file/upload/taskList' || path === '/file/upload/uncompleted') {
      data = []
    } else if (path === '/file/upload/expired') {
      data = []
    } else if (path === '/share/list') {
      data = []
    } else if (path === '/recycled/list') {
      data = { items: [], total: 0 }
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

test('固定侧栏、路由搜索恢复与无横向滚动', async ({ page }) => {
  await page.goto('/files')
  await expect(page.locator('.desktop-shell')).toBeVisible()
  await expect(page.locator('.desktop-sidebar')).toBeVisible()
  await expect(page.locator('.desktop-header')).toBeVisible()
  await expect(page.locator('.mobile-bottom-nav')).toHaveCount(0)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
  await expect(page).toHaveScreenshot('desktop-files-shell.png', { fullPage: true })

  const search = page.locator('.desktop-header__search input')
  await search.fill('验收报告')
  await search.press('Enter')
  await expect(page).toHaveURL(/\/files\?.*search=%E9%AA%8C%E6%94%B6%E6%8A%A5%E5%91%8A/)
  await page.reload()
  await expect(search).toHaveValue('验收报告')

  await search.fill('')
  await search.press('Enter')
  await expect(page.locator('.file-grid')).toBeVisible()
  await page.locator('.page-toolbar__secondary .el-button-group button').nth(1).click()
  await expect(page.locator('.file-list')).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-files-list.png', { fullPage: true })
})

test('任务查询参数、账户概览和设置分区可直接访问', async ({ page }, testInfo) => {
  await page.goto('/tasks?tab=download')
  await expect(page.locator('.task-tabs')).toBeVisible()
  await expect(page).toHaveScreenshot('desktop-tasks.png', { fullPage: true })
  await page.locator('.task-tabs .el-tabs__item').first().click()
  await expect(page).toHaveURL(/\/tasks\?tab=upload/)

  await page.goto('/me')
  await expect(page.locator('.account-overview')).toBeVisible()

  const isEnglish = testInfo.project.name === 'chromium-desktop-1024'
  await page.goto('/subscriptions')
  await expect(page.locator('.subscriptions-page h2')).toHaveText(isEnglish ? 'Subscriptions' : '订阅管理')

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
