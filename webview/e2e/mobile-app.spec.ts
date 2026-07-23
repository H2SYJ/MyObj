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

const mockApi = async (page: Page) => {
  await page.route('**/dev-api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/dev-api/, '')
    let data: unknown = {}

    if (path === '/user/info') data = userInfo
    else if (path === '/file/list') {
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
          created_at: '2026-07-23T00:00:00Z'
        }
      })
      data = { breadcrumbs: [], current_directory_id: 0, folders: [], files, total: 25, page: pageNumber, page_size: 20 }
    } else if (path === '/file/search/public') data = { files: [], total: 0 }
    else if (path === '/file/public/list') data = { files: [], total: 0 }
    else if (path === '/download/list') data = { tasks: [], total: 0, page: 1, page_size: 20 }
    else if (path === '/file/upload/taskList' || path === '/file/upload/uncompleted') data = []
    else if (path === '/file/upload/expired') data = []
    else if (path === '/share/list') data = { items: [], total: 0 }
    else if (path === '/recycled/list') data = { items: [], total: 0 }
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

    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 200, message: 'ok', data }) })
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

test('五个主标签、无限滚动与二级页返回', async ({ page }) => {
  await page.goto('/files')
  const nav = page.locator('.mobile-bottom-nav')
  await expect(nav).toBeVisible()
  await expect(nav.locator('a')).toHaveCount(5)
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
    if (path === '/square') {
      const viewButtons = page.locator('.toolbar-actions .el-button')
      await expect(viewButtons).toHaveCount(2)
      await expect(viewButtons.nth(0)).toBeVisible()
      await expect(viewButtons.nth(1)).toBeVisible()
    }
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
  await expect(page.locator('.admin-hub-item')).toHaveCount(6)
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await expect(page.locator('html')).toHaveClass(/dark/)
})

test('公开分享页适配手机安全区', async ({ page }) => {
  await page.goto('/share/public-token')
  await expect(page.getByText('公开资料.pdf')).toBeVisible()
  await expect(page.getByRole('button', { name: /下载/ })).toBeVisible()
  await expect(page.locator('.share-download-page')).toHaveCSS('min-height', `${await page.evaluate(() => window.innerHeight)}px`)
})
