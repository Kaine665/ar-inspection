// ============================================================
// 全站唯一的网络出口。三个函数 = 契约里的三个端点（health 除外不写死）。
// 页面和 hook 里不允许再出现 fetch —— 想加请求，只能加在这个文件里。
//
// 这个文件干四件事：
//   1. 发请求（带超时）
//   2. 把 HTTP 状态码 + 响应体翻译成一个 ApiError（错误归一，A4）
//   3. 校验响应形状是否仍然符合契约（漂了就说漂了，别静默出错）
//   4. 把每次往返写进请求日志（可核验）
// ============================================================

import { pushLog } from './log'
import type {
  ApiErrorBody,
  AppErrorCode,
  HealthResponse,
  Inspection,
  Priority,
  ServerErrorCode,
  Status,
} from '../types/inspection'

/** 默认走同源 /api/*（dev 由 vite 代理到 Go）。要直连就把 VITE_API_BASE 设成
 *  http://127.0.0.1:8080 —— 此时后端必须自带 CORS 头。 */
const API_BASE: string = import.meta.env.VITE_API_BASE ?? ''
const TIMEOUT_MS = 8000

const SERVER_CODES: readonly ServerErrorCode[] = ['invalid_argument', 'not_found', 'invalid_transition', 'internal']
const PRIORITIES: readonly Priority[] = ['low', 'medium', 'high']
const STATUSES: readonly Status[] = ['open', 'in_progress', 'resolved']

export class ApiError extends Error {
  readonly code: AppErrorCode
  readonly httpStatus: number | null

  constructor(code: AppErrorCode, message: string, httpStatus: number | null) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.httpStatus = httpStatus
  }
}

// ---------------------------------------------------------------
// 底层：一次请求
// ---------------------------------------------------------------
async function call(method: string, path: string, body?: unknown): Promise<unknown> {
  const requestBody = body === undefined ? undefined : JSON.stringify(body)
  const started = performance.now()
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), TIMEOUT_MS)

  let res: Response
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      headers: requestBody === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: requestBody,
      signal: controller.signal,
    })
  } catch (e) {
    const durationMs = Math.round(performance.now() - started)
    const aborted = e instanceof DOMException && e.name === 'AbortError'
    pushLog({
      method,
      path,
      requestBody,
      httpStatus: null,
      ok: false,
      durationMs,
      responseBody: aborted ? `(超时 ${TIMEOUT_MS}ms，已中断)` : `(网络错误) ${String(e)}`,
    })
    throw new ApiError('network', aborted ? `请求超时（${TIMEOUT_MS}ms）` : '连不上后端服务', null)
  } finally {
    window.clearTimeout(timer)
  }

  const text = await res.text()
  const durationMs = Math.round(performance.now() - started)
  let parsed: unknown = null
  if (text.length > 0) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = null
    }
  }

  pushLog({
    method,
    path,
    requestBody,
    httpStatus: res.status,
    ok: res.ok,
    durationMs,
    responseBody: text.length > 0 ? text : '(空响应体)',
  })

  if (!res.ok) {
    const err = extractError(parsed)
    if (err) throw new ApiError(err.code, err.message, res.status)

    // 走到这里说明后端没按契约返回错误体。三种常见情况分开处理：
    // ① dev 代理连不上 Go（vite 返回 500 + "ECONNREFUSED …"）→ 其实是网络错
    // ② dev 代理连接失败但响应体是空的（vite 有时就这样）→ 同样是网络错
    // ③ 真的返回了纯文本（如 Go 默认的 405）→ 契约之外的响应
    const empty5xx = res.status >= 500 && text.trim().length === 0
    if (/ECONNREFUSED|ECONNRESET|proxy error|socket hang up/i.test(text) || empty5xx) {
      throw new ApiError('network', '连不上后端服务（代理没拿到任何响应）', res.status)
    }
    const hint = text.replace(/\s+/g, ' ').trim().slice(0, 120)
    throw new ApiError(
      'bad_response',
      `后端返回了契约之外的错误响应（HTTP ${res.status}）${hint ? '：' + hint : ''}`,
      res.status,
    )
  }

  if (parsed === null) {
    throw new ApiError('bad_response', '响应不是合法 JSON', res.status)
  }
  return parsed
}

function extractError(parsed: unknown): ApiErrorBody['error'] | null {
  if (typeof parsed !== 'object' || parsed === null) return null
  const box = (parsed as Record<string, unknown>).error
  if (typeof box !== 'object' || box === null) return null
  const { code, message } = box as Record<string, unknown>
  if (typeof code !== 'string' || !SERVER_CODES.includes(code as ServerErrorCode)) return null
  if (typeof message !== 'string') return null
  return { code: code as ServerErrorCode, message }
}

// ---------------------------------------------------------------
// 形状校验：契约漂移要当场喊出来，而不是渲染出一堆 undefined
// ---------------------------------------------------------------
function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

function parseInspection(raw: unknown): Inspection {
  if (typeof raw !== 'object' || raw === null) throw new ApiError('bad_response', '列表里出现了非对象元素', null)
  const o = raw as Record<string, unknown>
  const position = o.position as Record<string, unknown> | undefined

  if (typeof o.id !== 'string' || typeof o.title !== 'string') {
    throw new ApiError('bad_response', '记录缺少 id 或 title', null)
  }
  if (!PRIORITIES.includes(o.priority as Priority)) {
    throw new ApiError('bad_response', `记录的 priority 不是契约值：${String(o.priority)}`, null)
  }
  if (!STATUSES.includes(o.status as Status)) {
    throw new ApiError('bad_response', `记录的 status 不是契约值：${String(o.status)}`, null)
  }
  if (!position || !isFiniteNumber(position.x) || !isFiniteNumber(position.y) || !isFiniteNumber(position.z)) {
    throw new ApiError('bad_response', '记录的 position 不是三个有限数', null)
  }
  if (typeof o.createdAt !== 'string' || typeof o.updatedAt !== 'string') {
    throw new ApiError('bad_response', '记录缺少 createdAt / updatedAt', null)
  }

  return {
    id: o.id,
    title: o.title,
    // 契约里 description 可空缺省为 ""，这里容错成空串而不是报错
    description: typeof o.description === 'string' ? o.description : '',
    priority: o.priority as Priority,
    status: o.status as Status,
    position: { x: position.x, y: position.y, z: position.z },
    createdAt: o.createdAt,
    updatedAt: o.updatedAt,
  }
}

// ---------------------------------------------------------------
// 对外三个端点
// ---------------------------------------------------------------

/** GET /api/health —— 服务活着吗、库通吗。不参与列表数据，只喂顶栏徽章。 */
export async function getHealth(): Promise<HealthResponse> {
  const parsed = await call('GET', '/api/health')
  const o = parsed as Record<string, unknown>
  if (typeof o.status !== 'string' || typeof o.db !== 'string' || typeof o.version !== 'string') {
    throw new ApiError('bad_response', 'health 响应缺少 status / db / version', null)
  }
  return { status: o.status, db: o.db, version: o.version }
}

/** GET /api/inspections —— 管理端唯一的数据来源。每次进页面都重拉，不缓存。 */
export async function listInspections(): Promise<Inspection[]> {
  const parsed = await call('GET', '/api/inspections')
  const items = (parsed as Record<string, unknown>).items
  if (!Array.isArray(items)) {
    throw new ApiError('bad_response', '列表响应缺少 items 数组（契约 6.1）', null)
  }
  return items.map(parseInspection)
}

/** PATCH /api/inspections/:id —— 只改 status 一个字段。
 *  body 里绝不多带字段：契约 6.6 第 5 条规定传其它字段直接 400。 */
export async function patchStatus(id: string, status: Status): Promise<Inspection> {
  const parsed = await call('PATCH', `/api/inspections/${encodeURIComponent(id)}`, { status })
  return parseInspection(parsed)
}

// ---------------------------------------------------------------
// 错误归一（A4）：网络错 / 4xx / 5xx 全部翻成人话，并决定"显示在哪"
// scope = 'banner' → 页面顶部横幅（用户站着也得看见、且要刷新）
// scope = 'row'    → 就显示在那一行下面（用户自己刚点的，改一下就行）
// ---------------------------------------------------------------
export interface ErrorPresentation {
  scope: 'banner' | 'row'
  text: string
  /** banner 上要不要给"刷新"按钮 */
  needsReload: boolean
}

export function describeError(err: unknown): ErrorPresentation {
  const e = err instanceof ApiError ? err : new ApiError('internal', String(err), null)
  switch (e.code) {
    case 'network':
      return {
        scope: 'banner',
        text: `${e.message}。确认 Go 服务已启动（默认 127.0.0.1:8080），或检查 admin/vite.config.ts 里的代理目标。`,
        needsReload: true,
      }
    case 'bad_response':
      return {
        scope: 'banner',
        text: `${e.message} 打开下方「请求日志」看原始响应——这条一般意味着三端契约不一致了。`,
        needsReload: true,
      }
    case 'not_found':
      return { scope: 'banner', text: `${e.message}。请刷新列表。`, needsReload: true }
    case 'internal':
      return { scope: 'banner', text: '服务异常，请重试。', needsReload: true }
    case 'invalid_argument':
      return { scope: 'row', text: e.message, needsReload: false }
    case 'invalid_transition':
      return { scope: 'row', text: e.message, needsReload: false }
    default:
      return { scope: 'banner', text: e.message, needsReload: true }
  }
}
