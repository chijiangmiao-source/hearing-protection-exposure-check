import { defineConfig } from '@playwright/test'

// 端到端：真实 Go API（:8080）+ 真实 Vite 页面（:5173，/api 代理到 Go），
// 不使用任何 mock 或固定结果。
const goBin = process.env.GO_BIN ?? 'go'

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: process.env.WEB_BASE_URL ?? 'http://localhost:5173',
    trace: 'retain-on-failure'
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        launchOptions: {
          args: ['--no-sandbox', '--disable-dev-shm-usage']
        }
      }
    }
  ],
  webServer: [
    {
      command: `${goBin} run .`,
      cwd: '../api',
      port: 8080,
      env: { API_PORT: '8080' },
      reuseExistingServer: true,
      timeout: 60_000
    },
    {
      command: 'npm run dev -- --port 5173',
      port: 5173,
      reuseExistingServer: true,
      timeout: 60_000
    }
  ]
})
