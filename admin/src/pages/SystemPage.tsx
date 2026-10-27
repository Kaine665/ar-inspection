import { useState } from 'react'
import type { UseHealthResult } from '../hooks/useHealth'

interface Props {
  health: UseHealthResult
}

const ENDPOINTS = [
  {
    method: 'GET',
    path: '/api/health',
    use: '服务活着吗、数据库通吗',
    who: '手机端自检 · 评审验证 · 顶栏徽章',
    res: '200 {"status":"ok","db":"ok","version":"0.1.0"}',
  },
  {
    method: 'POST',
    path: '/api/inspections',
    use: '新建一条问题（id / 时间 / 初始状态由后端生成）',
    who: '手机端提交（管理端不调）',
    res: '201 + 整条记录',
  },
  {
    method: 'GET',
    path: '/api/inspections',
    use: '取全部问题，后端固定按 createdAt 倒序',
    who: '管理端打开页面 / 刷新',
    res: '200 {"items":[…]}',
  },
  {
    method: 'PATCH',
    path: '/api/inspections/:id',
    use: '只改 status 一个字段',
    who: '管理端点状态下拉',
    res: '200 + 更新后整条',
  },
]

const ERRORS = [
  { code: 'invalid_argument', http: '400', when: '字段格式不对（标题空 / 优先级不认识 / 坐标非有限数）', here: '行内红字' },
  { code: 'not_found', http: '404', when: '改一条不存在的 id', here: '顶部横幅 + 刷新' },
  { code: 'invalid_transition', http: '409', when: '状态流转不被允许（除 resolved → open 外的回退）', here: '行内红字 + 控件回滚' },
  { code: 'internal', http: '500', when: '后端自己出错', here: '顶部横幅 + 重试' },
]

const SMOKE = [
  'curl.exe http://127.0.0.1:8080/api/health',
  'curl.exe http://127.0.0.1:8080/api/inspections',
  'curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/<id> -H "Content-Type: application/json" -d "{\\"status\\":\\"in_progress\\"}"',
  'curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/<id> -H "Content-Type: application/json" -d "{\\"title\\":\\"x\\"}"',
  'curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/insp_999 -H "Content-Type: application/json" -d "{\\"status\\":\\"resolved\\"}"',
]

function CopyLine({ text }: { text: string }) {
  const [done, setDone] = useState(false)
  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'flex-start' }}>
      <pre className="code" style={{ flex: 1, margin: 0 }}>
        {text}
      </pre>
      <button
        type="button"
        className="btn"
        style={{ padding: '3px 9px', fontSize: 12, flex: 'none' }}
        onClick={() => {
          void navigator.clipboard.writeText(text).then(() => {
            setDone(true)
            window.setTimeout(() => setDone(false), 1400)
          })
        }}
      >
        {done ? '已复制' : '复制'}
      </button>
    </div>
  )
}

/**
 * 契约与状态页。作用只有一个：演示和排障时不用切终端就能说清
 * "后端是什么、契约是什么、健康与否"。它不持有任何业务状态。
 */
export default function SystemPage({ health }: Props) {
  const h = health.data
  return (
    <>
      <div className="page-head" style={{ marginBottom: 16 }}>
        <div>
          <h1 className="page-title">契约与状态</h1>
          <p className="page-sub">这一页不产生任何写操作，只读 health 与契约文本</p>
        </div>
        <button type="button" className="btn" onClick={health.check}>
          重新检查 health
        </button>
      </div>

      <div className="card">
        <h2 className="card-title">GET /api/health</h2>
        <div className="kv">
          <div>
            <div className="field-label">status</div>
            <div className="mono">{h?.status ?? '—'}</div>
          </div>
          <div>
            <div className="field-label">db</div>
            <div className="mono">{h?.db ?? '—'}</div>
          </div>
          <div>
            <div className="field-label">version</div>
            <div className="mono">{h?.version ?? '—'}</div>
          </div>
          <div>
            <div className="field-label">最后检查</div>
            <div className="mono">{health.lastChecked ?? '—'}</div>
          </div>
        </div>
        {health.phase === 'down' ? (
          <div className="field-note" style={{ color: 'var(--danger)' }}>
            请求失败：后端没起，或者 admin 的代理目标不对（admin/vite.config.ts）。
          </div>
        ) : null}
        {health.phase === 'db_down' ? (
          <div className="field-note" style={{ color: 'var(--st-open)' }}>
            服务在，但 db 不是 ok —— 这时列表可能读得出来、写会失败。
          </div>
        ) : null}
      </div>

      <div className="card">
        <h2 className="card-title">四个端点（契约 6.1）</h2>
        <table className="plain">
          <thead>
            <tr>
              <th>方法 + 路径</th>
              <th>干什么</th>
              <th>谁在用</th>
              <th>成功响应</th>
            </tr>
          </thead>
          <tbody>
            {ENDPOINTS.map((e) => (
              <tr key={e.method + e.path}>
                <td className="mono">
                  {e.method} {e.path}
                </td>
                <td>{e.use}</td>
                <td>{e.who}</td>
                <td className="mono">{e.res}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="muted" style={{ marginTop: 10 }}>
          只有四个，因为 v1 的业务只有四件事。没有 GET /:id（没有详情页）、没有 DELETE（BR-10
          记录永不物理删除）、没有登录权限（v1 不做）。
        </div>
      </div>

      <div className="card">
        <h2 className="card-title">四个错误码与它们在这页面上的落点（契约 6.3）</h2>
        <table className="plain">
          <thead>
            <tr>
              <th>code</th>
              <th>HTTP</th>
              <th>什么情况</th>
              <th>管理端显示在</th>
            </tr>
          </thead>
          <tbody>
            {ERRORS.map((e) => (
              <tr key={e.code}>
                <td className="mono">{e.code}</td>
                <td className="mono">{e.http}</td>
                <td>{e.when}</td>
                <td>{e.here}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="card">
        <h2 className="card-title">冒烟命令（可直接复制，改 &lt;id&gt; 即可）</h2>
        {SMOKE.map((c) => (
          <div key={c} style={{ marginBottom: 8 }}>
            <CopyLine text={c} />
          </div>
        ))}
        <div className="muted">
          第 4 条应返回 400 invalid_argument（PATCH 传了 status 以外的字段），第 5 条应返回 404 not_found。
        </div>
      </div>

      <div className="card">
        <h2 className="card-title">这一版明确不做</h2>
        <div className="muted" style={{ lineHeight: 1.9 }}>
          登录与权限 · 编辑标题/优先级/坐标 · 删除记录 · 分页 · 搜索 · 统计报表 · 截图上传（契约 6.7 已预留）
        </div>
      </div>
    </>
  )
}
