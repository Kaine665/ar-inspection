import { useState } from 'react'
import { clearLog, useRequestLog } from '../api/log'

/**
 * 请求日志面板 —— 这个项目"数据流转可核验"的主要抓手。
 *
 * 它只读 api/log.ts 里那份内存记录，不参与任何业务逻辑。
 * 核验方式：页面里点一下，这里立刻多一条 METHOD / 路径 / HTTP 码 / 耗时 / 请求体 / 响应体。
 * 和 DevTools Network 的差别：它显示的是"前端真正拿到的原始响应文本"，
 * 包括契约漂移时的非 JSON 文本。
 */
export default function RequestLog() {
  const entries = useRequestLog()
  const [open, setOpen] = useState(false)

  return (
    <section className="log-panel">
      <div className="log-head" onClick={() => setOpen((v) => !v)}>
        <span>
          {open ? '▾' : '▸'} 请求日志（核验用，最近 30 条）
          <span className="muted" style={{ marginLeft: 8 }}>共 {entries.length} 条</span>
        </span>
        {open && entries.length > 0 ? (
          <button
            type="button"
            className="btn"
            style={{ padding: '2px 9px', fontSize: 12 }}
            onClick={(e) => {
              e.stopPropagation()
              clearLog()
            }}
          >
            清空
          </button>
        ) : null}
      </div>

      {open ? (
        <div className="log-body">
          {entries.length === 0 ? (
            <div className="log-empty">还没有请求。刷新页面或改一条状态就会出现在这里。</div>
          ) : (
            entries.map((e) => (
              <div className="log-entry" key={e.seq}>
                <div className="log-line">
                  <span className="log-method mono">{e.method}</span>
                  <span className="log-path mono">{e.path}</span>
                  <span className={e.ok ? 'log-status-ok' : 'log-status-bad'}>
                    {e.httpStatus === null ? '无响应' : e.httpStatus}
                  </span>
                  <span className="muted">{e.durationMs}ms</span>
                  <span className="muted">{e.at}</span>
                </div>
                {e.requestBody ? (
                  <pre className="log-json">请求体 {e.requestBody}</pre>
                ) : null}
                <pre className="log-json">响应 {e.responseBody}</pre>
              </div>
            ))
          )}
        </div>
      ) : null}
    </section>
  )
}
