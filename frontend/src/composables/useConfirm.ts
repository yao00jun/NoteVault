import { ref } from 'vue'

/**
 * 全局确认弹框（替代 window.confirm）。
 *
 * 为什么不用 window.confirm：WebView2 会把它转成原生对话框，
 * 弹在窗口右上角、位置不可控、样式与应用主题脱节（见 TitleBar 退出确认的先例）。
 *
 * 设计取舍与 useToast 一致：
 * - **模块级单例**而非 provide/inject —— 调用方可能在任意深度的组件里。
 * - Promise 风格 API：`await confirmDialog({ message })` 返回布尔，
 *   调用侧把原来的 `if (!confirm(msg)) return` 平移成
 *   `if (!(await confirmDialog({ message: msg }))) return` 即可。
 * - 弹框本体由挂在 App.vue 根部的 ConfirmDialog.vue 渲染（纯展示层）。
 * - 测试用 `resetConfirm()` 清状态，避免单例跨用例污染。
 */

export interface ConfirmOptions {
  /** 正文（对应原 confirm 的 message 参数） */
  message: string
  /** 标题；不传则不渲染标题行 */
  title?: string
  /** 确认按钮文案；不传走 i18n 的 common.confirm */
  confirmText?: string
  /** 取消按钮文案；不传走 i18n 的 common.cancel */
  cancelText?: string
  /** 危险操作（删除/清空/永久删除）：确认按钮渲染为红色 */
  danger?: boolean
  /**
   * 可选的第三按钮（介于取消与确认之间），用于「二选一」场景，
   * 如删除分组：[取消] [只删分组框(alt)] [连卡片一起删(confirm)]。
   * 点击返回 'alt'。
   */
  altText?: string
}

export type ConfirmResult = 'confirm' | 'alt' | 'dismiss'

export interface ConfirmState {
  visible: boolean
  title: string
  message: string
  confirmText: string
  cancelText: string
  danger: boolean
  altText: string
}

const state = ref<ConfirmState>({
  visible: false,
  title: '',
  message: '',
  confirmText: '',
  cancelText: '',
  danger: false,
  altText: '',
})

let resolver: ((result: ConfirmResult) => void) | null = null

function settle(result: ConfirmResult) {
  state.value = { ...state.value, visible: false }
  resolver?.(result)
  resolver = null
}

/**
 * 弹出确认框并等待用户选择。同一时间只显示一个：
 * 若上一个还在等待，先按「取消」结算它，避免悬挂的 Promise 永远 pending。
 */
/** 三值结果：confirm=主确认（红色/强调按钮），alt=可选的中间选项，dismiss=取消/Esc/遮罩 */
export function chooseDialog(options: ConfirmOptions): Promise<ConfirmResult> {
  if (resolver) settle('dismiss')
  state.value = {
    visible: true,
    title: options.title ?? '',
    message: options.message,
    confirmText: options.confirmText ?? '',
    cancelText: options.cancelText ?? '',
    danger: options.danger ?? false,
    altText: options.altText ?? '',
  }
  return new Promise<ConfirmResult>((resolve) => {
    resolver = resolve
  })
}

/** 布尔快捷方式：等价于 chooseDialog 后判断是否点了主确认按钮 */
export async function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  return (await chooseDialog(options)) === 'confirm'
}

/** 仅测试用：关掉弹框并结算所有等待中的 Promise（按取消），避免跨用例污染 */
export function resetConfirm() {
  settle('dismiss')
}

export function useConfirm() {
  return {
    state,
    /** 用户点了主确认按钮 */
    accept: () => settle('confirm'),
    /** 用户点了第三按钮（altText） */
    chooseAlt: () => settle('alt'),
    /** 用户点了取消 / 遮罩 / Esc */
    dismiss: () => settle('dismiss'),
  }
}
