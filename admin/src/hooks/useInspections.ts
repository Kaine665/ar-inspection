// 列表状态机。整个 admin 的数据流转就集中在这个文件 + api/client.ts 里，
// 别的地方只拿结果、不碰网络。想核验数据怎么走，读这两个文件就够。

import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, describeError, listInspections, patchStatus } from '../api/client'
import type { ErrorPresentation } from '../api/client'
import type { Inspection, Status } from '../types/inspection'
import { STATUS_LABEL } from '../types/inspection'

export type ListPhase = 'loading' | 'error' | 'ready'

export interface UseInspectionsResult {
  phase: ListPhase
  /** phase === 'ready' 时才有值；空数组代表"后端说现在一条都没有" */
  items: Inspection[] | null
  /** 页面顶部横幅（网络错 / 404 / 500 / 契约漂移） */
  banner: ErrorPresentation | null
  /** 正在改的那一行（同一时刻只允许一个在飞，简单且够用） */
  pendingId: string | null
  /** 行内错误（409 invalid_transition / 400 invalid_argument） */
  rowError: { id: string; text: string } | null
  toast: string | null
  reload: () => void
  changeStatus: (id: string, next: Status) => void
  dismissBanner: () => void
  dismissToast: () => void
}

/**
 * 三段数据流转，全部在这里：
 *
 *   进入页面 → listInspections() → GET /api/inspections → setItems(items)
 *   点状态下拉 → patchStatus(id, next) → PATCH /api/inspections/:id
 *              → 成功：用后端返回的整条替换本地那一条（后端是唯一真源）
 *              → 失败：本地一条都不改，按 code 决定错误显示在哪
 */
export function useInspections(): UseInspectionsResult {
  const [phase, setPhase] = useState<ListPhase>('loading')
  const [items, setItems] = useState<Inspection[] | null>(null)
  const [banner, setBanner] = useState<ErrorPresentation | null>(null)
  const [pendingId, setPendingId] = useState<string | null>(null)
  const [rowError, setRowError] = useState<{ id: string; text: string } | null>(null)
  const [toast, setToast] = useState<string | null>(null)

  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const reload = useCallback(() => {
    setPhase('loading')
    setBanner(null)
    setRowError(null)

    listInspections()
      .then((list) => {
        if (!alive.current) return
        setItems(list)
        setPhase('ready')
      })
      .catch((err: unknown) => {
        if (!alive.current) return
        // 失败时保留旧数据不清空，但页面会显示错误横幅 + 重试
        setBanner(describeError(err))
        setPhase('error')
      })
  }, [])

  useEffect(() => {
    reload()
  }, [reload])

  const changeStatus = useCallback(
    (id: string, next: Status) => {
      if (pendingId !== null) return // 一次只飞一个，避免同一条记录被并发改
      setPendingId(id)
      setRowError(null)

      patchStatus(id, next)
        .then((updated) => {
          if (!alive.current) return
          // 关键：用后端返回的整条覆盖，而不是本地自己拼一个 status——
          // updatedAt 只有后端知道，本地拼等于自己造数据（A5 明令禁止）
          setItems((prev) => (prev === null ? prev : prev.map((it) => (it.id === updated.id ? updated : it))))
          setToast(`已改为「${STATUS_LABEL[updated.status]}」`)
        })
        .catch((err: unknown) => {
          if (!alive.current) return
          const shown = describeError(err)
          if (shown.scope === 'row') {
            setRowError({ id, text: shown.text })
          } else {
            // 404 / 500 / 网络错 / 契约漂移：记录可能已不存在或服务挂了，
            // 这时候行内提示不够，必须升到页面级
            setBanner(shown)
            if (err instanceof ApiError && err.code === 'not_found') setPhase('error')
          }
        })
        .finally(() => {
          if (!alive.current) return
          setPendingId(null)
        })
    },
    [pendingId],
  )

  const dismissBanner = useCallback(() => setBanner(null), [])
  const dismissToast = useCallback(() => setToast(null), [])

  // toast 自动消失，不打断任何操作
  useEffect(() => {
    if (toast === null) return
    const t = window.setTimeout(() => setToast(null), 2400)
    return () => window.clearTimeout(t)
  }, [toast])

  return { phase, items, banner, pendingId, rowError, toast, reload, changeStatus, dismissBanner, dismissToast }
}
