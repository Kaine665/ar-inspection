import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 前端所有请求都走同源 /api/*，由 dev server 代理到 Go 后端 ——
// 浏览器因此看不到跨域问题，后端在生产也可以干脆不开 CORS。
//
// 这里读的是 **node 侧**环境变量（不带 VITE_ 前缀，不会打进浏览器包）：
//   ADMIN_PROXY_TARGET  后端地址，默认 http://127.0.0.1:8080（见 .env.development）
//   ADMIN_PORT          开发服务器端口，默认 5173
export default defineConfig({
  plugins: [react()],
  server: {
    port: Number(process.env.ADMIN_PORT ?? 5173),
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
