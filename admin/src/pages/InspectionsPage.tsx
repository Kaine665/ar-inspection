import { useEffect, useMemo, useState } from 'react'
import { useInspections } from '../hooks/useInspections'
import type { Inspection, Priority, Status } from '../types/inspection'
import { STATUS_LABEL } from '../types/inspection'
import PriorityTag from '../components/PriorityTag'
import StatusSelect from '../components/StatusSelect'
import Drawer from '../components/Drawer'
import { Banner, EmptyState, ErrorState, SkeletonRows, Toast } from '../components/States'
import { formatCoords, formatRelative } from '../utils/time'

type Filter = 'all' | Status

const PRIO_BAR: Record<Priority, string> = {
  high: 'var(--prio-high)',
  medium: 'var(--prio-medium)',
  low: 'var(--prio-low)',
}

const FILTERS: Filter[] = ['all', 'open', 'in_progress', 'resolved']

/**
 * 主页面。数据只有两个来源，都写在下面这几行里：
 *   useInspections() → GET /api/inspections
 *   下拉 onPick      → list.changeStatus() → PATCH /api/inspections/:id
 * 页面上显示的每一条记录，都是某次 GET 或 PATCH 响应里的对象，没有本地拼的。
 */
export default function InspectionsPage() {
  const list = useInspections()
  const [filter, setFilter] = useState<Filter>('all')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [lastSynced, setLastSynced] = useState<string | null>(null)

  useEffect(() => {
    if (list.items !== null) {
      setLastSynced(new Date().toLocaleTimeString('zh-CN', { hour12: false }))
    }
  }, [list.items])

  const items = list.items
  const counts = useMemo(() => {
    const base = { all: 0, open: 0, in_progress: 0, resolved: 0 } as Record<Filter, number>
    for (const it of items ?? []) {
      base.all += 1
      base[it.status] += 1
    }
    return base
  }, [items])

  const visible = useMemo(() => {
    if (items === null) return []
    return filter === 'all' ? items : items.filter((it) => it.status === filter)
  }, [items, filter])

  const selected: Inspection | null = useMemo(
    () => (items === null || selectedId === null ? null : items.find((it) => it.id === selectedId) ?? null),
    [items, selectedId],
  )

  return (
    <>
      <div className="page-head">
        <div>
          <h1 className="page-title">问题列表</h1>
          <p className="page-sub">
            今天要处理什么 · 按创建时间倒序（排序由后端固定，前端不再排）
          </p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span className="muted">{lastSynced ? `最后同步 ${lastSynced}` : '尚未同步'}</span>
          <button type="button" className="btn" onClick={list.reload} disabled={list.phase === 'loading'}>
            刷新
          </button>
        </div>
      </div>

      <div className="stats">
        {FILTERS.map((f) => (
          <div className="stat" key={f}>
            <div className="stat-label">{f === 'all' ? '全部' : STATUS_LABEL[f]}</div>
            <div className="stat-value">{counts[f]}</div>
          </div>
        ))}
      </div>

      <div className="toolbar">
        <div className="tabs">
          {FILTERS.map((f) => (
            <button
              type="button"
              key={f}
              className="tab"
              aria-pressed={filter === f}
              onClick={() => setFilter(f)}
            >
              {f === 'all' ? '全部' : STATUS_LABEL[f]}
              <span className="tab-count">{counts[f]}</span>
            </button>
          ))}
        </div>
        <span className="muted">过滤只发生在前端，不额外请求后端（契约不支持 query 参数）</span>
      </div>

      {list.banner !== null && items !== null ? (
        <Banner banner={list.banner} onReload={list.reload} onDismiss={list.dismissBanner} />
      ) : null}

      {list.phase === 'loading' && items === null ? <SkeletonRows /> : null}

      {list.phase === 'error' && items === null && list.banner !== null ? (
        <ErrorState banner={list.banner} onReload={list.reload} />
      ) : null}

      {list.phase === 'ready' && items !== null && items.length === 0 ? <EmptyState /> : null}

      {items !== null && items.length > 0 ? (
        <div className="rows">
          {visible.length === 0 ? (
            <div className="state-box">
              <div className="state-title">这个状态下现在没有记录</div>
              <div className="state-hint">
                共 {items.length} 条，只是都不在当前筛选里。切回「全部」。
              </div>
            </div>
          ) : (
            visible.map((it) => (
              <div
                className="row"
                key={it.id}
                role="button"
                tabIndex={0}
                data-selected={selectedId === it.id}
                onClick={() => setSelectedId(it.id)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    setSelectedId(it.id)
                  }
                }}
              >
                <span className="row-bar" style={{ background: PRIO_BAR[it.priority] }} />
                <span className="row-main">
                  <span className="row-title-line">
                    <PriorityTag priority={it.priority} />
                    <span className="row-title">{it.title}</span>
                  </span>
                  <span className="row-meta">
                    {it.description.length > 0 ? it.description : '（无描述）'}
                    {' · '}
                    {formatCoords(it.position)}
                  </span>
                  {list.rowError !== null && list.rowError.id === it.id ? (
                    <span className="row-error">{list.rowError.text}</span>
                  ) : null}
                </span>
                <span className="row-side">
                  <StatusSelect
                    value={it.status}
                    pending={list.pendingId === it.id}
                    onPick={(next) => list.changeStatus(it.id, next)}
                  />
                  <span className="row-time">{formatRelative(it.createdAt)}</span>
                  {list.pendingId === it.id ? <span className="row-pending">提交中…</span> : null}
                </span>
              </div>
            ))
          )}
        </div>
      ) : null}

      {selected !== null ? (
        <Drawer
          item={selected}
          pending={list.pendingId === selected.id}
          error={list.rowError !== null && list.rowError.id === selected.id ? list.rowError.text : null}
          onChangeStatus={list.changeStatus}
          onClose={() => setSelectedId(null)}
        />
      ) : null}

      {list.toast !== null ? <Toast text={list.toast} /> : null}
    </>
  )
}
