import {
  Decoration,
  EditorView,
  ViewPlugin,
  WidgetType,
  keymap,
  type DecorationSet,
  type ViewUpdate,
} from '@codemirror/view'
import { EditorSelection, RangeSetBuilder, type Extension } from '@codemirror/state'
import { indentLess, indentMore, redo, undo } from '@codemirror/commands'
import type {
  CompiledDecoration,
  CompiledWidget,
  PluginKeymap,
  PluginTextTransform,
} from './types'

// 当前活动编辑器视图（由 MarkdownEditor 在创建/销毁时注册）。
// 插件运行在 Worker 沙箱里，不能直接碰 DOM，因此“按钮点击→改选中文本”
// 这一步必须由宿主在真实编辑器上执行——这就是 editorBridge 的职责。
let activeEditor: EditorView | null = null

export function setActiveEditor(view: EditorView | null): void {
  activeEditor = view
}

export function getActiveEditor(): EditorView | null {
  return activeEditor
}

// ---------------------------------------------------------------------------
// 宿主内置编辑器命令（P14）
//
// 有些工具栏按钮需要碰编辑器的历史栈或打开宿主 UI——插件在沙箱里做不到。
// 所以宿主提供一组固定命令，插件只能通过命令名引用，不能自己实现。
// ---------------------------------------------------------------------------

/**
 * 依赖 Vue 组件的 UI 操作（取色器、格式刷）。
 * 这些逻辑留在组件里，由组件注册进来，editorBridge 只负责转发。
 */
export interface EditorUiHandlers {
  pickColor?: () => void
  pickBackground?: () => void
  toggleBrush?: () => void
}

let uiHandlers: EditorUiHandlers = {}

export function setEditorUiHandlers(handlers: EditorUiHandlers): void {
  uiHandlers = handlers
}

// ---------------------------------------------------------------------------
// 段落对齐（整行包裹 HTML 标签）
// ---------------------------------------------------------------------------

type AlignMode = 'left' | 'center' | 'right' | 'justify'

function unwrapAlign(text: string): string | null {
  const paragraph = text.match(/^<p align="(?:left|center|right|justify)">(.*)<\/p>$/s)
  if (paragraph) return paragraph[1]
  const center = text.match(/^<center>(.*)<\/center>$/s)
  if (center) return center[1]
  return null
}

function isAlignTag(text: string, align: AlignMode): boolean {
  if (align === 'center') return /^<center>[\s\S]*<\/center>$/.test(text)
  return new RegExp(`^<p align="${align}">[\\s\\S]*</p>$`).test(text)
}

/**
 * 段落对齐：已是该对齐 → 取消；是其它对齐 → 替换；否则包裹。
 * 用 HTML 标签是因为 Markdown 没有原生对齐语法，这与 Obsidian 的做法一致。
 */
export function applyAlign(align: AlignMode): void {
  const view = activeEditor
  if (!view) return
  const { state } = view
  const wrapText = (text: string) =>
    align === 'center' ? `<center>${text}</center>` : `<p align="${align}">${text}</p>`

  const changes: { from: number, insert: string, to: number }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let lineNumber = startLine; lineNumber <= endLine; lineNumber += 1) {
      if (seen.has(lineNumber)) continue
      seen.add(lineNumber)
      const line = state.doc.line(lineNumber)
      const inner = unwrapAlign(line.text)
      let insert: string
      if (inner !== null && isAlignTag(line.text, align)) {
        insert = inner // 已是该对齐 → 取消
      } else if (inner !== null) {
        insert = wrapText(inner) // 是其它对齐 → 换成当前
      } else {
        insert = wrapText(line.text)
      }
      changes.push({ from: line.from, insert, to: line.to })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
  view.focus()
}

/**
 * 执行一个宿主内置命令。返回是否已处理——
 * 未识别的命令名由调用方决定如何处理（目前静默忽略）。
 */
export function runBuiltinEditorCommand(command: string): boolean {
  // 取色器与格式刷依赖组件状态，但没有活动编辑器时也不该崩
  if (command === 'editor:pickColor') {
    uiHandlers.pickColor?.()
    return true
  }
  if (command === 'editor:pickBackground') {
    uiHandlers.pickBackground?.()
    return true
  }
  if (command === 'editor:brush') {
    uiHandlers.toggleBrush?.()
    return true
  }

  const view = activeEditor
  if (!view) return false

  switch (command) {
    case 'editor:undo':
      undo(view)
      return true
    case 'editor:redo':
      redo(view)
      return true
    case 'editor:indent':
      indentMore(view)
      return true
    case 'editor:undent':
      indentLess(view)
      return true
    case 'editor:alignLeft':
      applyAlign('left')
      return true
    case 'editor:alignCenter':
      applyAlign('center')
      return true
    case 'editor:alignRight':
      applyAlign('right')
      return true
    case 'editor:alignJustify':
      applyAlign('justify')
      return true
    default:
      return false
  }
}

/** 行级前缀：标题 #、引用 >、无序列表 -、有序列表 1. 等 */
const LINE_PREFIX_PATTERN = /^(#{1,6}\s+|>\s?|[-*+]\s+|\d+\.\s+)/

/**
 * 在选中行的行首插入前缀（P14）——标题、引用、列表这类整行操作。
 *
 * 已有相同前缀 → 取消；已有其它前缀 → 替换。
 * 这样 H1 按钮点两次会回到普通文本，符合编辑器工具栏的常规行为。
 */
export function applyLinePrefix(prefix: string): void {
  const view = activeEditor
  if (!view || !prefix) return
  const { state } = view
  const changes: { from: number, insert: string, to: number }[] = []
  const seen = new Set<number>()

  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let lineNumber = startLine; lineNumber <= endLine; lineNumber += 1) {
      if (seen.has(lineNumber)) continue
      seen.add(lineNumber)
      const line = state.doc.line(lineNumber)
      const match = LINE_PREFIX_PATTERN.exec(line.text)
      const existing = match ? match[0] : ''
      const rest = line.text.slice(existing.length)
      // 已是该前缀 → 取消；否则替换成新的
      const insert = existing === prefix ? rest : prefix + rest
      changes.push({ from: line.from, insert, to: line.to })
    }
  }

  if (changes.length > 0) view.dispatch({ changes })
  view.focus()
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
 * 替换当前选区（P14）；无选区时等价于在光标处插入。
 * 与 applyTransform 的区别：那个只能做「包裹」，这个允许插件
 * 自己算出替换后的文本（大小写转换、格式化、模板渲染等）。
 */
export function replaceEditorSelection(text: string): void {
  const view = activeEditor
  if (!view) return
  const { state } = view
  view.dispatch(
    state.changeByRange(range => ({
      changes: { from: range.from, insert: text, to: range.to },
      range: EditorSelection.cursor(range.from + text.length),
    })),
  )
  view.focus()
}

/**
 * 应用插件声明的文本变换到当前选区/光标（宿主在真实编辑器上执行）。
 * UI 注册协议的落点：插件只能声明 transform，宿主拿到按钮点击后在这里真正改文本。
 */
export function applyTransform(transform: PluginTextTransform): void {
  const view = activeEditor
  if (!view) return
  const { state } = view

  // 直接插入模式：在光标处插入文本（无需选区）
  if (transform.insert != null) {
    const text = transform.insert
    view.dispatch(
      state.changeByRange((range) => ({
        changes: { from: range.from, insert: text },
        range: EditorSelection.cursor(range.from + text.length),
      })),
    )
    view.focus()
    return
  }

  // 包裹模式：prefix/suffix 包裹选区；无选区则插入占位并选中占位
  const prefix = transform.prefix ?? ''
  const suffix = transform.suffix ?? prefix
  const placeholder = transform.placeholder ?? ''

  const spec = state.changeByRange((range) => {
    const selected = state.sliceDoc(range.from, range.to)
    if (selected) {
      return {
        changes: { from: range.from, to: range.to, insert: prefix + selected + suffix },
        range: EditorSelection.range(
          range.from + prefix.length,
          range.from + prefix.length + selected.length,
        ),
      }
    }
    const insert = prefix + placeholder + suffix
    return {
      changes: { from: range.from, insert },
      range: EditorSelection.range(
        range.from + prefix.length,
        range.from + prefix.length + placeholder.length,
      ),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
  view.focus()
}

// ---------------------------------------------------------------------------
// 行内格式快捷键（B / I / K / \）
//
// 编辑器「组件级」CM6 keymap 与 App.vue「全局」快捷键共用同一入口，
// 保证两处行为完全一致、不出现两套实现漂移。
// bold/italic/code 是纯包裹式变换，复用 applyTransform；
// link 需要把 url 段选中以便直接键入，单独处理。
// ---------------------------------------------------------------------------

export type InlineFormat = 'bold' | 'italic' | 'code' | 'link'

/** 对当前选区/光标施加行内 Markdown 格式 */
export function applyInlineFormat(format: InlineFormat): void {
  const view = activeEditor
  if (!view) return
  if (format === 'link') {
    applyLink()
    return
  }
  const spec: PluginTextTransform =
    format === 'bold'
      ? { prefix: '**', suffix: '**', placeholder: '粗体文本' }
      : format === 'italic'
        ? { prefix: '*', suffix: '*', placeholder: '斜体文本' }
        : { prefix: '`', suffix: '`', placeholder: '代码' }
  applyTransform(spec)
}

/** 插入 Markdown 链接：[选中文本](url) 或 [链接文本](url)，并把 url 段选中便于直接键入 */
function applyLink(): void {
  const view = activeEditor
  if (!view) return
  const { state } = view
  const spec = state.changeByRange((range) => {
    const selected = state.sliceDoc(range.from, range.to)
    if (selected) {
      const insert = `[${selected}](url)`
      // 选中文本的结束位置 + '](' 的长度 (=3) 即 url 起点
      const urlStart = range.from + selected.length + 3
      return {
        changes: { from: range.from, to: range.to, insert },
        range: EditorSelection.range(urlStart, urlStart + 3),
      }
    }
    const insert = '[链接文本](url)'
    const urlStart = range.from + '链接文本'.length + 3
    return {
      changes: { from: range.from, insert },
      range: EditorSelection.range(urlStart, urlStart + 3),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
  view.focus()
}

// ---------------------------------------------------------------------------
// 声明式编辑器扩展（P14）
//
// 插件只描述「想高亮什么、想插什么小组件」，真正的 CodeMirror 对象在这里构造。
// 这样插件不需要 full-trust 也能做编辑器增强——
// 代价是表达力受限（做不了完整 ViewPlugin、语法模式），
// 换来的是「装饰一个关键词」不必让用户授予完全信任。
// ---------------------------------------------------------------------------

/**
 * 遍历文本中所有匹配。
 *
 * 两个坑：正则每次都要重建（复用 RegExp 实例的 lastIndex 是有状态的）；
 * 零宽匹配必须手动推进 lastIndex，否则 a* 这类可能匹配到空串的正则会死循环。
 */
function forEachMatch(
  text: string,
  matcher: RegExp | string,
  callback: (from: number, to: number, match: RegExpMatchArray | null) => void,
): void {
  if (typeof matcher === 'string') {
    if (!matcher) return
    let index = text.indexOf(matcher)
    while (index !== -1) {
      callback(index, index + matcher.length, null)
      index = text.indexOf(matcher, index + matcher.length)
    }
    return
  }

  const flags = matcher.flags.includes('g') ? matcher.flags : `${matcher.flags}g`
  const regex = new RegExp(matcher.source, flags)
  let match: RegExpExecArray | null
  while ((match = regex.exec(text)) !== null) {
    if (match[0] === '') {
      regex.lastIndex += 1 // 零宽匹配：不推进就永远出不去
      continue
    }
    callback(match.index, match.index + match[0].length, match as RegExpMatchArray)
  }
}

/** 把 $1 / $2 展开为正则捕获组（字面量匹配时原样返回） */
function expandTemplate(text: string, match: RegExpMatchArray | null): string {
  if (!match) return text
  return text.replace(/\$(\d)/g, (whole, index: string) => match[Number(index)] ?? whole)
}

/** 内联样式。键名和值都已在宿主侧过白名单，这里只负责拼串 */
function styleToString(style: Record<string, string> | undefined): string | undefined {
  if (!style) return undefined
  const entries = Object.entries(style)
  return entries.length === 0 ? undefined : entries.map(([k, v]) => `${k}: ${v}`).join('; ')
}

class PluginWidgetType extends WidgetType {
  constructor(
    readonly text: string,
    readonly className: string,
  ) {
    super()
  }

  eq(other: PluginWidgetType): boolean {
    return other.text === this.text && other.className === this.className
  }

  toDOM(): HTMLElement {
    const span = document.createElement('span')
    span.className = `cm-plugin-widget ${this.className}`.trim()
    // 用 textContent 而不是 innerHTML：插件给的内容一律当纯文本，
    // 这里绝不能引入 HTML 注入面
    span.textContent = this.text
    return span
  }

  ignoreEvent(): boolean {
    return false
  }
}

interface PendingMark {
  from: number
  to: number
  decoration: Decoration
}

function collectMarks(
  view: EditorView,
  decorations: CompiledDecoration[],
  widgets: CompiledWidget[],
): PendingMark[] {
  const text = view.state.doc.toString()
  const marks: PendingMark[] = []

  for (const item of decorations) {
    forEachMatch(text, item.matcher, (from, to) => {
      let start = from
      let end = to
      if (item.scope === 'line') {
        const line = view.state.doc.lineAt(from)
        start = line.from
        end = line.to
      }
      if (start >= end) return
      const style = styleToString(item.style)
      marks.push({
        decoration: Decoration.mark({
          attributes: style ? { style } : undefined,
          class: item.class,
        }),
        from: start,
        to: end,
      })
    })
  }

  for (const widget of widgets) {
    forEachMatch(text, widget.matcher, (from, to, match) => {
      if (from >= to) return
      marks.push({
        decoration: Decoration.replace({
          widget: new PluginWidgetType(expandTemplate(widget.text, match), widget.class ?? ''),
        }),
        from,
        to,
      })
    })
  }

  return marks
}

/**
 * 把插件声明的装饰与小组件编译成 CodeMirror extension。
 *
 * 装饰集合变化时（插件注册/卸载/热重载）必须重建并 reconfigure——
 * ViewPlugin 的闭包捕获的是当前这份声明快照。
 */
export function buildPluginDecorationExtension(
  decorations: CompiledDecoration[],
  widgets: CompiledWidget[],
): Extension {
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet

      constructor(view: EditorView) {
        this.decorations = build(view)
      }

      update(update: ViewUpdate): void {
        if (update.docChanged || update.viewportChanged) {
          this.decorations = build(update.view)
        }
      }
    },
    { decorations: plugin => plugin.decorations },
  )

  function build(view: EditorView): DecorationSet {
    const marks = collectMarks(view, decorations, widgets)
      .sort((a, b) => a.from - b.from || a.to - b.to)
    const builder = new RangeSetBuilder<Decoration>()
    let previousTo = -1
    for (const mark of marks) {
      // RangeSetBuilder 要求严格升序。重叠区间直接跳过而不是报错——
      // 多个插件装饰同一段文本是很常见的事，不该把编辑器拖挂
      if (mark.from < previousTo) continue
      builder.add(mark.from, mark.to, mark.decoration)
      previousTo = mark.to
    }
    return builder.finish()
  }
}

/**
 * 把插件声明的快捷键编译成 CodeMirror keymap。
 *
 * 命令的实际执行交给调用方——这里不该知道插件运行时的细节。
 */
export function buildPluginKeymapExtension(
  keymaps: PluginKeymap[],
  runCommand: (pluginId: string, commandId: string) => void,
): Extension {
  if (keymaps.length === 0) return []
  return keymap.of(
    keymaps.map(item => ({
      key: item.key,
      run: () => {
        runCommand(item.pluginId, item.command)
        return true
      },
    })),
  )
}
