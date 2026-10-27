// 时间显示口径：列表里给人看"多久以前"，抽屉里两个都给。
// 契约里 createdAt / updatedAt 是 RFC3339 的 UTC 串，这里统一转本地时区。

export function formatRelative(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const diff = Date.now() - t
  if (diff < 0) return '刚刚'
  const min = Math.floor(diff / 60_000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const hour = Math.floor(min / 60)
  if (hour < 24) return `${hour} 小时前`
  const day = Math.floor(hour / 24)
  if (day < 7) return `${day} 天前`
  return new Date(t).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })
}

export function formatAbsolute(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}

/** 坐标保留 3 位小数（契约 6.2：单位米，保留 3 位）。 */
export function formatCoord(n: number): string {
  return n.toFixed(3)
}

export function formatCoords(p: { x: number; y: number; z: number }): string {
  return `x ${formatCoord(p.x)} / y ${formatCoord(p.y)} / z ${formatCoord(p.z)} m`
}
