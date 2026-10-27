import type { Status } from '../types/inspection'
import { STATUS_LABEL } from '../types/inspection'

interface Props {
  value: Status
  pending: boolean
  onPick: (next: Status) => void
}

const ORDER: Status[] = ['open', 'in_progress', 'resolved']

/**
 * 状态控件：一个 select，三个值。
 *
 * 两个刻意的设计：
 * 1. 不做乐观更新——onPick 只往上抛意图，等后端返回后父组件用响应覆盖。
 *    所以在飞的过程中 select 显示的还是旧值（pending 时禁用）。
 * 2. 不本地做"能不能这么改"的判断。resolved → open 允许、其它回退 409，
 *    这个规则住在后端；前端要是自己拦，后端改了规则前端就骗人。
 */
export default function StatusSelect({ value, pending, onPick }: Props) {
  return (
    <select
      className="select"
      value={value}
      disabled={pending}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => onPick(e.target.value as Status)}
      aria-label="修改处理状态"
    >
      {ORDER.map((s) => (
        <option key={s} value={s}>
          {STATUS_LABEL[s]}
        </option>
      ))}
    </select>
  )
}
