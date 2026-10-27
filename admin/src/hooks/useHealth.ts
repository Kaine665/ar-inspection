// 顶栏那个"服务正常/未连接"徽章的数据来源。
// 它是独立的旁路：health 挂了不影响列表，列表挂了也不影响它。
// 这对应契约里 health 端点的定位——只回答"服务活着吗、库通吗"。

import { useCallback, useEffect, useRef, useState } from 'react'
import { getHealth } from '../api/client'
import type { HealthResponse } from '../types/inspection'

export type HealthPhase = 'checking' | 'ok' | 'db_down' | 'down'

export interface UseHealthResult {
  phase: HealthPhase
  data: HealthResponse | null
  lastChecked: string | null
  check: () => void
}

const POLL_MS = 10_000

export function useHealth(): UseHealthResult {
  const [phase, setPhase] = useState<HealthPhase>('checking')
  const [data, setData] = useState<HealthResponse | null>(null)
  const [lastChecked, setLastChecked] = useState<string | null>(null)
  const [tick, setTick] = useState(0)
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const check = useCallback(() => setTick((n) => n + 1), [])

  useEffect(() => {
    let cancelled = false
    getHealth()
      .then((h) => {
        if (cancelled || !alive.current) return
        setData(h)
        setPhase(h.db === 'ok' ? 'ok' : 'db_down')
        setLastChecked(new Date().toLocaleTimeString('zh-CN', { hour12: false }))
      })
      .catch(() => {
        if (cancelled || !alive.current) return
        setData(null)
        setPhase('down')
        setLastChecked(new Date().toLocaleTimeString('zh-CN', { hour12: false }))
      })
    return () => {
      cancelled = true
    }
  }, [tick])

  useEffect(() => {
    const t = window.setInterval(() => setTick((n) => n + 1), POLL_MS)
    return () => window.clearInterval(t)
  }, [])

  return { phase, data, lastChecked, check }
}
