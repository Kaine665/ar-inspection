import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 前端所有请求都走同源 /api/*，由 dev server 代理到 Go 后端。
// 这样浏览器不看 CORS 头（Go 那边没配 CORS 也能跑），真机连后端时改成
// VITE_API_BASE=http://<手机热点IP>:8080 即可，代码一行不用动。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Windows 上编辑器"原子保存"会先写一个隐藏的 *.tmpdir/*.tmp 再改名，
    // vite 的 watcher 一旦抓到它就会 EBUSY 崩溃（本机实测过）。
    // 这几条 ignore 是必须的，不是洁癖。
    watch: {
      ignored: ['**/.*.tmpdir/**', '**/*.tmp', '**/node_modules/**', '**/dist/**'],
    },
    proxy: {
      '/api': {
        target: process.env.ADMIN_PROXY_TARGET ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
