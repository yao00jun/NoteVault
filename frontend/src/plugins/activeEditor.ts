import type { EditorView } from '@codemirror/view'

// ---------------------------------------------------------------------------
// 活动编辑器访问层（轻量，运行时零 CodeMirror 依赖）
//
// 这一层只有「当前活动编辑器是谁」的注册表，外加两个不碰 CM 对象模型的读写。
// 它存在的唯一理由是**边界**：应用外壳（App.vue、AI 抽屉、插件 store）在
// 首屏就要问「有没有活动编辑器」，如果从 editorBridge 拿，就会把整套
// CodeMirror（压缩后约 300KB）拖进入口 chunk——而编辑器本身是懒加载路由，
// 打开「今天」页根本用不上它。
//
// 规则：这里只允许 `import type` 自 @codemirror/*。需要 EditorSelection /
// Decoration / 命令的地方一律去 editorBridge（重操作），由调用方按需加载。
// 选区更新用 TransactionSpec 的 `{ anchor }` 字面量，等价于 EditorSelection.cursor。
// ---------------------------------------------------------------------------

let activeEditor: EditorView | null = null

/** 由 MarkdownEditor 在创建/销毁时注册；插件与外壳只读 */
export function setActiveEditor(view: EditorView | null): void {
  activeEditor = view
}

export function getActiveEditor(): EditorView | null {
  return activeEditor
}

/**
 * 读取当前选区文本（P14）。
 * 没有活动编辑器时返回空串——插件应当能容忍这个情况，
 * 而不是假设一定有编辑器打开着。
 */
export function getEditorSelection(): string {
  const view = activeEditor
  if (!view) return ''
  const { from, to } = view.state.selection.main
  return view.state.sliceDoc(from, to)
}

/**
 * 在当前光标处插入文本，不替换已有选区（AI Copilot「插入当前笔记」专用，
 * AI-COPILOT-FLOATING-ORB 蓝图 Step 3）。
 * 与 replaceEditorSelection 的区别：后者会吞掉选区——用户复制了一段文字
 * 再点「插入」时，绝不能把选区内容顶掉。
 * 返回是否成功（没有活动编辑器时为 false，由调用方降级提示）。
 */
export function insertAtCursor(text: string): boolean {
  const view = activeEditor
  if (!view) return false
  const { state } = view
  const pos = state.selection.main.head
  view.dispatch(
    state.update({
      changes: { from: pos, insert: text },
      selection: { anchor: pos + text.length },
      userEvent: 'input',
    }),
  )
  view.focus()
  return true
}

/**
 * 按需加载重型编辑器桥（变换、对齐、内置命令、插件装饰）。
 * 返回的是同一个模块实例——编辑器打开时它必然已在模块缓存里，这里只是
 * 让「外壳不需要编辑器」的场景不为此付出首屏体积。
 */
export function loadEditorBridge(): Promise<typeof import('./editorBridge')> {
  return import('./editorBridge')
}
