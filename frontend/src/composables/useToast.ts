import { ref } from 'vue'

/**
 * 全局轻量通知（toast）。
 *
 * 设计取舍：
 * - **模块级单例**而非 provide/inject —— 调用方可能在任意深度、甚至是非组件模块里
 *   （如 stores、命令函数），注入链会逼着它们拿 app 实例，得不偿失。
 * - 状态放在模块作用域，任何 `useToast()` 调用共享同一份列表。
 * - 测试用 `resetToasts()` 清空，避免单例跨用例污染。
 */

export type ToastKind = 'success' | 'error' | 'warning' | 'info'

export interface Toast {
  id: number
  kind: ToastKind
  message: string
  /** 自动消失毫秒数；0 = 不自动消失，需手动关 */
  duration: number
}

/** 同屏最多保留的条数，超出时挤掉最老的，防止批量操作刷屏 */
const MAX_VISIBLE = 5

const toasts = ref<Toast[]>([])
const timers = new Map<number, ReturnType<typeof setTimeout>>()
let nextId = 1

function clearTimer(id: number) {
  const timer = timers.get(id)
  if (timer !== undefined) {
    clearTimeout(timer)
    timers.delete(id)
  }
}

function dismiss(id: number) {
  clearTimer(id)
  toasts.value = toasts.value.filter((t) => t.id !== id)
}

function push(message: string, kind: ToastKind, duration: number): number {
  const id = nextId++
  toasts.value = [...toasts.value, { id, kind, message, duration }]
  // 超出上限：先关掉最早的（含清它的定时器）
  while (toasts.value.length > MAX_VISIBLE) {
    const oldest = toasts.value[0]
    if (oldest) dismiss(oldest.id)
    else break
  }
  if (duration > 0) {
    timers.set(
      id,
      setTimeout(() => dismiss(id), duration),
    )
  }
  return id
}

/** 仅测试用：清空所有 toast 与定时器，避免模块级单例跨用例污染 */
export function resetToasts() {
  timers.forEach((timer) => clearTimeout(timer))
  timers.clear()
  toasts.value = []
  nextId = 1
}

export function useToast() {
  return {
    toasts,
    dismiss,
    /** 默认 2.6s：够看清又不打断操作 */
    success: (message: string, duration = 2600) => push(message, 'success', duration),
    /** 默认 4s：错误需要更多阅读时间 */
    error: (message: string, duration = 4000) => push(message, 'error', duration),
    /** 默认 3.2s */
    warning: (message: string, duration = 3200) => push(message, 'warning', duration),
    /** 默认 2.6s */
    info: (message: string, duration = 2600) => push(message, 'info', duration),
    /** 常驻提示（duration=0），需手动关或代码调 dismiss */
    persistent: (message: string, kind: ToastKind = 'info') => push(message, kind, 0),
  }
}
