import process from 'node:process'
import { defineConfig, devices } from '@playwright/test'

const PREVIEW_PORT = '4173'

/**
 * E2E tests run against the production build served by `vite preview`.
 * Without local browsers, use `npm run e2e:docker` (official Playwright image).
 */
export default defineConfig({
  testDir: './e2e',
  // Generous limits: the CI host also builds the backend images while the suite runs.
  timeout: 60_000,
  expect: { timeout: 10_000 },
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: `http://localhost:${PREVIEW_PORT}`,
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npm run build-only && npm run preview -- --port ${PREVIEW_PORT} --strictPort`,
    port: Number(PREVIEW_PORT),
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
})
