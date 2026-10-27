import type { UseHealthResult } from '../hooks/useHealth'

interface Props {
  health: UseHealthResult
}

const TEXT: Record<UseHealthResult['phase'], { label: string; color: string; hint: string }> = {
  checking: { label: '检查中…', color: 'var(--text-3)', hint: '正在请求 GET /api/health' },
  ok: { label: '服务正常', color: 'var(--st-resolved)', hint: 'GET /api/health → status=ok, db=ok' },
  db_down: { label: '服务在线 · 数据库异常', color: 'var(--st-open)', hint: 'GET /api/health → db 不是 ok，写入会失败' },
  down: { label: '未连接后端', color: 'var(--danger)', hint: 'GET /api/health 请求失败，先启动 server' },
}

/** 顶栏徽章：只表达 health，不表达列表状态。点一下立刻重查。 */
export default function HealthBadge({ health }: Props) {
  const t = TEXT[health.phase]
  return (
    <button
      type="button"
      className="chip"
      onClick={health.check}
      title={t.hint}
      style={{ cursor: 'pointer' }}
    >
      <span className="dot" style={{ background: t.color }} />
      {t.label}
      {health.data ? <span style={{ color: 'var(--text-3)' }}>· v{health.data.version}</span> : null}
    </button>
  )
}
