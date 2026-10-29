// 这三个文件里的类型 = docs/api.md 第六节的逐条翻译。
// 改契约 → 先改 docs/api.md → 再改这里 → 再改代码。顺序不能反。

export type Priority = 'low' | 'medium' | 'high'
export type Status = 'open' | 'in_progress' | 'resolved'

export interface Position {
  x: number
  y: number
  z: number
}

/** 一条巡检记录。字段与契约 6.2 一一对应，不多不少。 */
export interface Inspection {
  id: string
  title: string
  description: string
  priority: Priority
  status: Status
  position: Position
  createdAt: string
  updatedAt: string
}

/** GET /api/inspections 的响应。包一层 items 是因为 Unity 的 JsonUtility
 *  反序列化不了顶层数组（契约 6.1 的硬约束，不是为了好看）。 */
export interface InspectionListResponse {
  items: Inspection[]
}

/** GET /api/health 的响应。 */
export interface HealthResponse {
  status: string
  db: string
  version: string
}

/** 后端业务错误码，只有这五个（见 docs/api.md 第 3 节）。 */
export type ServerErrorCode =
  | 'invalid_argument'
  | 'not_found'
  | 'method_not_allowed'
  | 'invalid_transition'
  | 'internal'

/** 前端口径的错误码：后端五个 + 两个只可能由前端自己产生的。 */
export type ClientErrorCode = 'network' | 'bad_response'

export type AppErrorCode = ServerErrorCode | ClientErrorCode

/** 后端统一错误体。 */
export interface ApiErrorBody {
  error: { code: ServerErrorCode; message: string }
}

export const STATUS_LABEL: Record<Status, string> = {
  open: '待处理',
  in_progress: '处理中',
  resolved: '已解决',
}

export const PRIORITY_LABEL: Record<Priority, string> = {
  low: '低',
  medium: '中',
  high: '高',
}

/** 状态配色（只用于圆点，不表达优先级）。 */
export const STATUS_COLOR: Record<Status, string> = {
  open: 'var(--st-open)',
  in_progress: 'var(--st-progress)',
  resolved: 'var(--st-resolved)',
}
