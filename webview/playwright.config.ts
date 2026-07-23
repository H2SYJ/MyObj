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
    { name: 'chromium-mobile', use: { ...devices['Pixel 7'], channel: 'chrome' } },
    { name: 'webkit-mobile', use: { ...devices['iPhone 13'] } }
  ],
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173/login',
    reuseExistingServer: true,
    timeout: 120_000
  }
})
