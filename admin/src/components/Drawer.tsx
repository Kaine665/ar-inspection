import type { Inspection, Status } from '../types/inspection'
import { STATUS_COLOR, STATUS_LABEL } from '../types/inspection'
import PriorityTag from './PriorityTag'
import StatusSelect from './StatusSelect'
import { formatAbsolute, formatCoords, formatRelative } from '../utils/time'

interface Props {
  item: Inspection
  pending: boolean
  error: string | null
  onChangeStatus: (id: string, next: Status) => void
  onClose: () => void
}

/**
 * 详情抽屉。不是第三个页面——契约里没有 GET /api/inspections/:id，
 * 所以这里显示的每一条都来自列表那一次响应，点开不需要再发请求。
 * 好处是永远不会出现"详情页 404 但列表里还有"的分裂。
 */
export default function Drawer({ item, pending, error, onChangeStatus, onClose }: Props) {
  return (
    <>
      <div className="drawer-overlay" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-label="问题详情">
        <div className="drawer-head">
          <div style={{ minWidth: 0 }}>
            <span className="muted mono">{item.id}</span>
            <h2 className="drawer-title">{item.title}</h2>
          </div>
          <button type="button" className="close-btn" onClick={onClose} aria-label="关闭">
            ×
          </button>
        </div>

        <div className="field">
          <div className="field-label">优先级</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 9 }}>
            <PriorityTag priority={item.priority} />
            <span className="muted">{item.priority}</span>
          </div>
        </div>

        <div className="field">
          <div className="field-label">处理状态</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 9 }}>
            <span className="dot" style={{ background: STATUS_COLOR[item.status] }} />
            <span style={{ fontSize: 13 }}>{STATUS_LABEL[item.status]}</span>
            <StatusSelect
              value={item.status}
              pending={pending}
              onPick={(next) => onChangeStatus(item.id, next)}
            />
          </div>
          {pending ? <div className="field-note">正在提交，等后端返回…</div> : null}
          {error ? <div className="row-error" style={{ marginTop: 5 }}>{error}</div> : null}
        </div>

        <div className="field">
          <div className="field-label">描述（巡检员填，可空）</div>
          <div className="field-value">{item.description.length > 0 ? item.description : '（未填写）'}</div>
        </div>

        <div className="field">
          <div className="field-label">位置坐标（单位：米）</div>
          <div className="field-value coords">{formatCoords(item.position)}</div>
          <div className="field-note">
            原点 = 手机启动 AR 时的位置，y 为负表示地面在下方（−1.0 ~ −1.5 属正常，不是 bug）。
            坐标只是现场快照，换人、重启、换角度都不复现——所以真正找地方要靠描述。
          </div>
        </div>

        <div className="field">
          <div className="field-label">时间</div>
          <div className="field-value">
            创建 {formatAbsolute(item.createdAt)}（{formatRelative(item.createdAt)}）
            <br />
            最后更新 {formatAbsolute(item.updatedAt)}（{formatRelative(item.updatedAt)}）
          </div>
        </div>

      </aside>
    </>
  )
}
