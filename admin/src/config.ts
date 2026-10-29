/**
 * 前端配置的唯一出处。
 *
 * 规矩：页面和 api 层不直接读 import.meta.env —— 只从这里拿。
 * 理由：Vite 的 import.meta.env 是**编译期替换**，值散落在各处的话，
 * 换一次地址/超时就要满仓搜；集中在这里，改配置只改环境文件。
 *
 * 环境怎么区分：
 *   .env.development → npm run dev（本地，走 vite 代理，同源请求）
 *   .env.production  → npm run build（生产，默认同源部署，由 nginx 反代 /api）
 * Vite 按 `npm run dev/build` 自动选文件，不用手改。
 */

const raw = import.meta.env

/** 读数字型环境变量，读不出或写了非法值就回退默认，不让一个错别字把页面搞挂。 */
function num(value: string | undefined, fallback: number): number {
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? n : fallback
}

export const config = {
  /**
   * 后端地址。空字符串 = 走同源 `/api/*`：
   *   dev  → vite 插件把 /api 代理到 ADMIN_PROXY_TARGET
   *   prod → 反向代理（nginx `location /api`）转到 Go
   * 只有"前后端真的分域名"时才填完整地址，例如 https://api.example.com
   */
  apiBase: (raw.VITE_API_BASE ?? '').replace(/\/+$/, ''),

  /** 单次请求超时（毫秒）。默认 8 秒；演示弱网可调到 15000。 */
  apiTimeoutMs: num(raw.VITE_API_TIMEOUT_MS, 8000),

  /** 当前构建的环境标签，显示在顶栏：DEV / PROD。防止"以为在测生产"。 */
  envLabel: raw.PROD ? 'PROD' : 'DEV',
} as const
