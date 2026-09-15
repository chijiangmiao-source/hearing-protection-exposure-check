import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 开发环境把 /api 与 /health 代理到 Go API；生产环境由容器内的 nginx 完成同一代理。
export default defineConfig({
  plugins: [vue()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.VITE_API_TARGET ?? 'http://localhost:8080',
        changeOrigin: true
      },
      '/health': {
        target: process.env.VITE_API_TARGET ?? 'http://localhost:8080',
        changeOrigin: true
      }
    }
  },
  test: {
    globals: true,
    environment: 'jsdom',
    include: ['test/**/*.test.ts'],
    exclude: ['node_modules', 'dist', 'e2e/**']
  }
})
