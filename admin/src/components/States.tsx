// 列表的三种非正常态 + 页面级横幅 + toast。都是纯展示，不含逻辑。

import type { ErrorPresentation } from '../api/client'

export function SkeletonRows({ count = 4 }: { count?: number }) {
  return (
    <div className="rows" aria-label="加载中">
      {Array.from({ length: count }).map((_, i) => (
        <div className="skeleton-row" key={i}>
          <div className="sk" style={{ width: 4, alignSelf: 'stretch', height: 38 }} />
          <div style={{ flex: 1 }}>
            <div className="sk" style={{ height: 13, width: `${45 + i * 7}%` }} />
            <div className="sk" style={{ height: 11, width: `${30 + i * 5}%`, marginTop: 8 }} />
          </div>
          <div className="sk" style={{ height: 26, width: 78 }} />
        </div>
      ))}
    </div>
  )
}

export function EmptyState() {
  return (
    <div className="state-box">
      <div className="state-title">还没有巡检记录</div>
      <div className="state-hint">
        用手机端 AR 放置标记并提交后，这里会出现。
        <br />
        现在这个时间点，后端返回的是 <span className="mono">{'{"items":[]}'}</span>。
      </div>
    </div>
  )
}

export function ErrorState({ banner, onReload }: { banner: ErrorPresentation; onReload: () => void }) {
  return (
    <div className="state-box">
      <div className="state-title">列表没能加载出来</div>
      <div className="state-hint" style={{ maxWidth: 560, margin: '6px auto 0' }}>{banner.text}</div>
      <div style={{ marginTop: 14 }}>
        <button type="button" className="btn btn-primary" onClick={onReload}>
          重试
        </button>
      </div>
    </div>
  )
}

/** 页面顶部横幅：用于"列表已经有数据、但刚才那次操作失败了"的情况。 */
export function Banner({
  banner,
  onReload,
  onDismiss,
}: {
  banner: ErrorPresentation
  onReload: () => void
  onDismiss: () => void
}) {
  return (
    <div className="banner banner-danger">
      <span>{banner.text}</span>
      <span style={{ display: 'flex', gap: 8, flex: 'none' }}>
        {banner.needsReload ? (
          <button type="button" className="btn" style={{ padding: '3px 10px', fontSize: 12 }} onClick={onReload}>
            刷新
          </button>
        ) : null}
        <button type="button" className="btn" style={{ padding: '3px 10px', fontSize: 12 }} onClick={onDismiss}>
          知道了
        </button>
      </span>
    </div>
  )
}

export function Toast({ text }: { text: string }) {
  return (
    <div className="toast-wrap">
      <div className="toast">{text}</div>
    </div>
  )
}
