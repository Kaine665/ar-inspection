import type { Priority } from '../types/inspection'
import { PRIORITY_LABEL } from '../types/inspection'

interface Props {
  priority: Priority
}

const CLASS: Record<Priority, string> = {
  high: 'prio-tag prio-high',
  medium: 'prio-tag prio-medium',
  low: 'prio-tag prio-low',
}

/** 优先级只展示，不可改：契约冻结点 5（PATCH 传 status 以外的字段 → 400）。 */
export default function PriorityTag({ priority }: Props) {
  return <span className={CLASS[priority]}>{PRIORITY_LABEL[priority]}</span>
}
