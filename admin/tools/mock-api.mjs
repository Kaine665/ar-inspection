// ============================================================
// admin 线专用的临时 mock 后端（不是交付物，不替代 server 线）。
//
// 存在理由：Go 那边还没起来时，admin 的数据流转也能被完整核验。
// 它严格照 docs/api.md 第六节实现"四个端点 + 四个错误码"，
// 这样等真后端接上时，前端不需要改一行。
//
// 用法：
//   node tools/mock-api.mjs                 # 127.0.0.1:8080，正常返回
//   MOCK_LATENCY_MS=1500 node tools/mock-api.mjs   # 人为变慢，看"不做乐观更新"
//   MOCK_FAIL=409 node tools/mock-api.mjs   # 让 PATCH 永远 409 invalid_transition
//   MOCK_FAIL=404 / 500 / bad_shape / nonjson
//
// ⚠ 一处契约没冻结的地方（需要和 server 线对齐）：
//   6.6 第 2 条只说了 "resolved → open 允许、其它回退 409"，
//   但没说 open 能不能直接跳到 resolved（跳过"处理中"）。
//   这里暂时按"允许跳级"实现，等 server 定下来再统一。
// ============================================================

import { createServer } from 'node:http'

const PORT = Number(process.env.MOCK_PORT ?? 8080)
const LATENCY = Number(process.env.MOCK_LATENCY_MS ?? 0)
const FAIL = process.env.MOCK_FAIL ?? ''

let seq = 120
const now = Date.now()

const db = [
  {
    id: 'insp_000123',
    title: 'B1 入口墙面破损',
    description: '左侧墙体存在裂缝，长约 40cm，位于距地 50cm 处。',
    priority: 'high',
    status: 'open',
    position: { x: 0.42, y: -1.3, z: 1.26 },
    createdAt: new Date(now - 3 * 60_000).toISOString(),
    updatedAt: new Date(now - 3 * 60_000).toISOString(),
  },
  {
    id: 'insp_000122',
    title: '3F 配电间门锁松动',
    description: '门把手晃动，需换锁芯',
    priority: 'medium',
    status: 'in_progress',
    position: { x: -2.1, y: -1.05, z: 4.33 },
    createdAt: new Date(now - 2 * 3600_000).toISOString(),
    updatedAt: new Date(now - 40 * 60_000).toISOString(),
  },
  {
    id: 'insp_000121',
    title: 'B2 地面标识线磨损',
    description: '停车位编号看不清',
    priority: 'low',
    status: 'resolved',
    position: { x: 5.02, y: -1.28, z: -0.87 },
    createdAt: new Date(now - 26 * 3600_000).toISOString(),
    updatedAt: new Date(now - 20 * 3600_000).toISOString(),
  },
]

const PRIORITIES = ['low', 'medium', 'high']
const STATUSES = ['open', 'in_progress', 'resolved']

function send(res, status, body, headers = {}) {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  res.writeHead(status, {
    'Content-Type': typeof body === 'string' ? 'text/plain; charset=utf-8' : 'application/json; charset=utf-8',
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET,POST,PATCH,OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type',
    ...headers,
  })
  res.end(text)
  console.log(`  → ${status} ${text.slice(0, 120)}${text.length > 120 ? '…' : ''}`)
}

function fail(res, httpStatus, code, message) {
  send(res, httpStatus, { error: { code, message } })
}

/** 单向前进 + resolved 可重开；同一状态幂等 200。 */
function transitionAllowed(from, to) {
  if (from === to) return true
  if (to === 'resolved') return true // 前进（含 open→resolved 跳级，契约未定义，见文件头说明）
  if (to === 'in_progress') return from === 'open'
  if (to === 'open') return from === 'resolved' // 只有"复发重开"这一种回退
  return false
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url ?? '/', `http://${req.headers.host}`)
  const path = url.pathname
  console.log(`${req.method} ${path}`)

  if (req.method === 'OPTIONS') {
    send(res, 204, '')
    return
  }

  if (LATENCY > 0) await new Promise((r) => setTimeout(r, LATENCY))

  // ---- GET /api/health ----
  if (req.method === 'GET' && path === '/api/health') {
    if (FAIL === 'nonjson') {
      send(res, 200, 'this is not json at all')
      return
    }
    send(res, 200, { status: 'ok', db: FAIL === 'db_down' ? 'error' : 'ok', version: '0.1.0' })
    return
  }

  // ---- GET /api/inspections ----
  if (req.method === 'GET' && path === '/api/inspections') {
    if (FAIL === 'nonjson') {
      send(res, 405, 'Method Not Allowed')
      return
    }
    const items = [...db].sort((a, b) => (a.createdAt < b.createdAt ? 1 : -1))
    if (FAIL === 'bad_shape') {
      // 故意把契约打歪：status 变成未知值，priority 缺失
      const broken = items.map((it, i) =>
        i === 0 ? { ...it, status: 'pending_review', priority: undefined } : it,
      )
      send(res, 200, { items: broken })
      return
    }
    send(res, 200, { items })
    return
  }

  // ---- POST /api/inspections ----
  if (req.method === 'POST' && path === '/api/inspections') {
    const body = await readJson(req)
    if (body === null) {
      fail(res, 400, 'invalid_argument', '请求体不是合法 JSON')
      return
    }
    const title = typeof body.title === 'string' ? body.title.trim() : ''
    if (title.length === 0) return fail(res, 400, 'invalid_argument', '标题不能为空')
    if (title.length > 100) return fail(res, 400, 'invalid_argument', '标题最多 100 字')
    const description = typeof body.description === 'string' ? body.description : ''
    if (description.length > 500) return fail(res, 400, 'invalid_argument', '描述最多 500 字')
    if (!PRIORITIES.includes(body.priority)) {
      return fail(res, 400, 'invalid_argument', '优先级必须是 low、medium 或 high')
    }
    const pos = body.position
    if (!pos || typeof pos !== 'object') return fail(res, 400, 'invalid_argument', '缺少位置坐标')
    for (const k of ['x', 'y', 'z']) {
      if (typeof pos[k] !== 'number' || !Number.isFinite(pos[k])) {
        return fail(res, 400, 'invalid_argument', '坐标不是有效数字')
      }
    }
    seq += 1
    const stamp = new Date().toISOString()
    const record = {
      id: `insp_${String(seq).padStart(6, '0')}`,
      title,
      description,
      priority: body.priority,
      status: 'open',
      position: { x: +pos.x.toFixed(3), y: +pos.y.toFixed(3), z: +pos.z.toFixed(3) },
      createdAt: stamp,
      updatedAt: stamp,
    }
    db.push(record)
    send(res, 201, record)
    return
  }

  // ---- PATCH /api/inspections/:id ----
  if (req.method === 'PATCH' && path.startsWith('/api/inspections/')) {
    const id = decodeURIComponent(path.slice('/api/inspections/'.length))
    const record = db.find((it) => it.id === id)

    if (FAIL === '500') return fail(res, 500, 'internal', '服务异常，请重试')
    if (FAIL === '404') return fail(res, 404, 'not_found', '记录不存在')
    if (!record) return fail(res, 404, 'not_found', '记录不存在')

    const body = await readJson(req)
    if (body === null) return fail(res, 400, 'invalid_argument', '请求体不是合法 JSON')

    const keys = Object.keys(body)
    if (keys.some((k) => k !== 'status')) {
      return fail(res, 400, 'invalid_argument', '只允许修改 status 字段')
    }
    if (!STATUSES.includes(body.status)) {
      return fail(res, 400, 'invalid_argument', '状态必须是 open、in_progress 或 resolved')
    }
    if (FAIL === '409') return fail(res, 409, 'invalid_transition', '已解决的问题不能重新处理')
    if (!transitionAllowed(record.status, body.status)) {
      return fail(res, 409, 'invalid_transition', `不能从 ${record.status} 改成 ${body.status}`)
    }

    record.status = body.status // 幂等：同状态也走这里，返回 200
    record.updatedAt = new Date().toISOString()
    send(res, 200, record)
    return
  }

  if (FAIL === 'nonjson') {
    send(res, 405, 'Method Not Allowed')
    return
  }
  fail(res, 404, 'not_found', '没有这个端点')
})

function readJson(req) {
  return new Promise((resolve) => {
    let raw = ''
    req.on('data', (c) => {
      raw += c
    })
    req.on('end', () => {
      if (raw.length === 0) return resolve({})
      try {
        resolve(JSON.parse(raw))
      } catch {
        resolve(null)
      }
    })
  })
}

server.listen(PORT, '127.0.0.1', () => {
  console.log(`mock api on http://127.0.0.1:${PORT}  (latency=${LATENCY}ms, fail='${FAIL || 'none'}')`)
})
