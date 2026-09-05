import { ref } from 'vue'

/**
 * 全局输入弹框（替代 window.prompt）。
 *
 * 与 useConfirm 同族：WebView2 会把 prompt 转成位置不可控的原生对话框，
 * 且原生 prompt 无法应用主题样式（见 useConfirm 顶部说明）。
 *
 * - Promise 风格：`const name = await promptDialog({ message, defaultValue })`
 *   确认返回输入值，取消（按钮 / Esc / 关闭）返回 null。
 * - 弹框本体由挂在 App.vue 根部的 PromptDialog.vue 渲染。
 * - 测试用 `resetPrompt()` 清状态，避免单例跨用例污染。
 */

export interface PromptOptions {
  /** 标签文案（对应原 prompt 的 message 参数） */
  message: string
  /** 预填值（对应原 prompt 的 default 参数）；弹出时全选方便直接覆盖 */
  defaultValue?: string
  placeholder?: string
  okText?: string
  cancelText?: string
}

export interface PromptState {
  visible: boolean
  message: string
  value: string
  placeholder: string
  okText: string
  cancelText: string
}

const state = ref<PromptState>({
  visible: false,
  message: '',
  value: '',
  placeholder: '',
  okText: '',
  cancelText: '',
})

let resolver: ((value: string | null) => void) | null = null

function settle(value: string | null) {
  state.value = { ...state.value, visible: false }
  resolver?.(value)
  resolver = null
}

/**
 * 弹出输入框并等待用户提交。同一时间只显示一个：
 * 若上一个还在等待，先按「取消」结算它，避免悬挂的 Promise 永远 pending。
 */
export function promptDialog(options: PromptOptions): Promise<string | null> {
  if (resolver) settle(null)
  state.value = {
    visible: true,
    message: options.message,
    value: options.defaultValue ?? '',
    placeholder: options.placeholder ?? '',
    okText: options.okText ?? '',
    cancelText: options.cancelText ?? '',
  }
  return new Promise<string | null>((resolve) => {
    resolver = resolve
  })
}

/** 仅测试用：关掉弹框并结算所有等待中的 Promise（按取消），避免跨用例污染 */
export function resetPrompt() {
  settle(null)
}

export function usePrompt() {
  return {
    state,
    /** 用户点了确定；空输入也原样返回，由调用方决定是否合法 */
    submit: () => settle(state.value.value),
    /** 用户点了取消 / Esc */
    dismiss: () => settle(null),
  }
}
