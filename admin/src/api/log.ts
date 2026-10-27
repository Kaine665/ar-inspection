// 请求日志：把每一次真实的网络往返留在内存里，页面底部可见。
//
// 存在的唯一理由：让"数据流转"能被人肉核验。不打开 DevTools 也能看到
// 发了什么、回了什么、花了多久、失败是哪种失败。
// 它是开发期观察工具，不参与业务逻辑——删掉这个文件，业务照跑。

import { useSyncExternalStore } from 'react'

export interface RequestLogEntry {
  seq: number
  at: string
  method: string
  path: string
  requestBody?: string
  httpStatus: number | null
  ok: boolean
  durationMs: number
  responseBody: string
}

const MAX = 30
let entries: RequestLogEntry[] = []
let seq = 0
const listeners = new Set<() => void>()

function emit() {
  for (const l of listeners) l()
}

export function pushLog(entry: Omit<RequestLogEntry, 'seq' | 'at'>) {
  seq += 1
  entries = [{ ...entry, seq, at: new Date().toLocaleTimeString('zh-CN', { hour12: false }) }, ...entries].slice(0, MAX)
  emit()
}

export function clearLog() {
  entries = []
  emit()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useRequestLog(): RequestLogEntry[] {
  return useSyncExternalStore(subscribe, () => entries, () => entries)
}
