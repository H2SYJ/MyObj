import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 8_000 },
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure'
  },
  projects: [
    {
      name: 'chromium-desktop-1440',
      testMatch: '**/desktop-*.spec.ts',
      use: {
        browserName: 'chromium',
        channel: 'chrome',
        viewport: { width: 1440, height: 900 },
        colorScheme: 'light',
        locale: 'zh-CN'
      }
    },
    {
      name: 'chromium-desktop-1024',
      testMatch: '**/desktop-*.spec.ts',
      use: {
        browserName: 'chromium',
        channel: 'chrome',
        viewport: { width: 1024, height: 768 },
        colorScheme: 'dark',
        locale: 'en-US'
      }
    },
    {
      name: 'chromium-desktop-768',
      testMatch: '**/desktop-*.spec.ts',
      use: {
        browserName: 'chromium',
        channel: 'chrome',
        viewport: { width: 768, height: 1024 },
        colorScheme: 'dark',
        locale: 'zh-CN'
      }
    },
    { name: 'chromium-mobile', testMatch: '**/mobile-*.spec.ts', use: { ...devices['Pixel 7'], channel: 'chrome' } },
    { name: 'webkit-mobile', testMatch: '**/mobile-*.spec.ts', use: { ...devices['iPhone 13'] } }
  ],
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173/login',
    reuseExistingServer: true,
    timeout: 120_000
  }
})
