// ============================================================================
// editorCommands —— MarkdownEditor 的纯 CodeMirror 6 编辑命令。
//
// 全部函数只依赖传入的 EditorView，不碰 Vue 响应式状态：
//   - MarkdownEditor.vue 与 EditorToolbar.vue 共用这一层；
//   - 单元测试可以直接构造 EditorView 驱动，无需挂载组件。
// 只读保护由调用方负责（命令层不做 readonly 判断）。
// ============================================================================

import { EditorSelection } from '@codemirror/state'
import { undo, redo, indentMore, indentLess } from '@codemirror/commands'
import type { EditorView } from '@codemirror/view'

/** 用 before/after 包裹选区；无选区时插入占位并把光标置于中间 */
export function wrapSelection(view: EditorView, before: string, after: string = before): void {
  const { state } = view
  const spec = state.changeByRange((range) => {
    const selected = state.sliceDoc(range.from, range.to)
    if (selected) {
      return {
        changes: { from: range.from, to: range.to, insert: before + selected + after },
        range: EditorSelection.range(range.from + before.length, range.from + before.length + selected.length),
      }
    }
    const insert = before + after
    return {
      changes: { from: range.from, insert },
      range: EditorSelection.cursor(range.from + before.length),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
}

/** 给选区覆盖的每一行设置/切换标题级别 */
export function setHeading(view: EditorView, level: number): void {
  const { state } = view
  const changes: { from: number; to: number; insert: string }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let l = startLine; l <= endLine; l++) {
      if (seen.has(l)) continue
      seen.add(l)
      const line = state.doc.line(l)
      const text = line.text
      const m = text.match(/^(#{1,6}\s+)/)
      let insert: string
      if (m && m[0].length - 1 === level) {
        insert = text.slice(m[0].length)
      } else {
        insert = '#'.repeat(level) + ' ' + text.replace(/^#{1,6}\s+/, '')
      }
      changes.push({ from: line.from, to: line.to, insert })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
}

/** 给选区覆盖的每一行加/去行前缀（引用、列表） */
export function prefixLines(view: EditorView, prefix: string): void {
  const { state } = view
  const changes: { from: number; to: number; insert: string }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let l = startLine; l <= endLine; l++) {
      if (seen.has(l)) continue
      seen.add(l)
      const line = state.doc.line(l)
      const text = line.text
      const insert = text.startsWith(prefix) ? text.slice(prefix.length) : prefix + text
      changes.push({ from: line.from, to: line.to, insert })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
}

/** 给选区覆盖的每一行施加/取消对齐（HTML 包裹，复刻 Obsidian 08-11） */
export function alignLines(view: EditorView, align: 'left' | 'center' | 'right' | 'justify'): void {
  const { state } = view
  const wrapText = (s: string) =>
    align === 'center' ? `<center>${s}</center>` : `<p align="${align}">${s}</p>`
  const changes: { from: number; to: number; insert: string }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let l = startLine; l <= endLine; l++) {
      if (seen.has(l)) continue
      seen.add(l)
      const line = state.doc.line(l)
      const text = line.text
      const inner = unwrapAlign(text)
      let insert: string
      if (inner !== null && isAlignTag(text, align)) {
        insert = inner // 已是该对齐 → 取消
      } else if (inner !== null) {
        insert = wrapText(inner) // 是其它对齐 → 换成当前
      } else {
        insert = wrapText(text)
      }
      changes.push({ from: line.from, to: line.to, insert })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
}

function unwrapAlign(text: string): string | null {
  let m = text.match(/^<p align="(?:left|center|right|justify)">(.*)<\/p>$/s)
  if (m) return m[1]
  m = text.match(/^<center>(.*)<\/center>$/s)
  if (m) return m[1]
  return null
}

function isAlignTag(text: string, align: string): boolean {
  if (align === 'center') return /^<center>[\s\S]*<\/center>$/.test(text)
  return new RegExp(`^<p align="${align}">[\\s\\S]*</p>$`).test(text)
}

/** 用 before/after 包裹选区为块（代码块等），无选区用占位 */
export function insertBlock(view: EditorView, before: string, after: string): void {
  const { state } = view
  const spec = state.changeByRange((range) => {
    const sel = state.sliceDoc(range.from, range.to)
    const inner = sel || '代码'
    const text = before + inner + after
    return {
      changes: { from: range.from, to: range.to, insert: text },
      range: EditorSelection.range(range.from + before.length, range.from + before.length + inner.length),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
}

/** 在光标处插入纯文本（分割线、表格等） */
export function insertText(view: EditorView, text: string): void {
  const { state } = view
  const spec = state.changeByRange((range) => ({
    changes: { from: range.from, insert: text },
    range: EditorSelection.cursor(range.from + text.length),
  }))
  view.dispatch({ ...spec, userEvent: 'input' })
}

/** 在光标处插入 Callout 块（Obsidian 语法 `> [!type]`）。
 * 有选区时把选区逐行加 `> ` 前缀作为 callout 内容；无选区插入占位模板。 */
export function insertCallout(view: EditorView, type: string): void {
  const { state } = view
  const typeName = (type || 'note').toLowerCase()
  const spec = state.changeByRange((range) => {
    const sel = state.sliceDoc(range.from, range.to)
    const body = sel ? sel : '在此输入内容'
    const bodyLines = body.split('\n').map((l) => '> ' + l).join('\n')
    const text = `> [!${typeName}]\n${bodyLines}\n`
    return {
      changes: { from: range.from, to: range.to, insert: text },
      range: EditorSelection.range(range.from + text.length, range.from + text.length),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
  view.focus()
}

/** 工具栏命令分发：id 与 toolbarButtons.ts 的 TOOLBAR_ITEMS 一一对应 */
export function applyFormat(view: EditorView, id: string): void {
  switch (id) {
    case 'undo': undo(view); break
    case 'redo': redo(view); break
    case 'bold': wrapSelection(view, '**'); break
    case 'italic': wrapSelection(view, '*'); break
    case 'underline': wrapSelection(view, '<u>', '</u>'); break
    case 'strike': wrapSelection(view, '~~'); break
    case 'code': wrapSelection(view, '`'); break
    case 'h1': setHeading(view, 1); break
    case 'h2': setHeading(view, 2); break
    case 'h3': setHeading(view, 3); break
    case 'h4': setHeading(view, 4); break
    case 'h5': setHeading(view, 5); break
    case 'h6': setHeading(view, 6); break
    case 'quote': prefixLines(view, '> '); break
    case 'ul': prefixLines(view, '- '); break
    case 'ol': prefixLines(view, '1. '); break
    case 'indent': indentMore(view); break
    case 'undent': indentLess(view); break
    case 'align-left': alignLines(view, 'left'); break
    case 'align-center': alignLines(view, 'center'); break
    case 'align-right': alignLines(view, 'right'); break
    case 'align-justify': alignLines(view, 'justify'); break
    case 'link': wrapSelection(view, '[', '](url)'); break
    case 'image': wrapSelection(view, '![', '](url)'); break
    case 'codeblock': insertBlock(view, '```\n', '\n```\n'); break
    case 'table':
      insertText(view, '| 列1 | 列2 | 列3 |\n| --- | --- | --- |\n| 单元格 | 单元格 | 单元格 |\n')
      break
    case 'hr': insertText(view, '\n---\n'); break
    // 表格编辑（P2-6）：光标不在表格内时，table-insert 插入空表，其余无操作
    case 'table-insert':
    case 'table-add-row':
    case 'table-del-row':
    case 'table-add-col':
    case 'table-del-col':
    case 'table-align-left':
    case 'table-align-center':
    case 'table-align-right':
      applyTableCommand(view, id)
      break
  }
  if (id !== 'undo' && id !== 'redo' && id !== 'indent' && id !== 'undent') {
    view.focus()
  }
}

// ===== 表格编辑（P2-6）=====
// Markdown 表格：
//   | 头1 | 头2 |
//   | --- | --- |   ← 分隔行，决定列数；对齐用 :--: / :--- / ---:
//   | a   | b   |

/** 解析一行表格为单元格数组（去首尾 | 并 trim） */
function parseTableRow(line: string): string[] {
  const t = line.trim()
  if (!t.startsWith('|')) return []
  const inner = t.endsWith('|') ? t.slice(1, -1) : t.slice(1)
  return inner.split('|').map((c) => c.trim())
}

/** 是否分隔行（仅由 | - : 空格组成） */
function isSeparatorRow(line: string): boolean {
  return /^\|[\s:|-]+\|$/.test(line.trim())
}

interface TableInfo {
  startLine: number
  endLine: number
  rows: string[][] // 含表头、分隔行与数据行
}

/** 从光标所在行向外扩，找到所在的完整表格块 */
function findTableAt(doc: { line(n: number): { text: string }; lines: number }, lineNumber: number): TableInfo | null {
  if (!doc.line(lineNumber).text.trim().startsWith('|')) return null
  let start = lineNumber
  let end = lineNumber
  while (start > 1 && doc.line(start - 1).text.trim().startsWith('|')) start--
  while (end < doc.lines && doc.line(end + 1).text.trim().startsWith('|')) end++
  const rows: string[][] = []
  for (let l = start; l <= end; l++) rows.push(parseTableRow(doc.line(l).text))
  return { startLine: start, endLine: end, rows }
}

/** 光标在行内的列索引（按 | 计数；落在首列边框上时归零） */
function columnIndexAt(lineText: string, offset: number): number {
  const before = lineText.slice(0, offset)
  const pipes = (before.match(/\|/g) || []).length
  return Math.max(0, pipes - 1)
}

/** 单元格序列化为表格行文本 */
function serializeRow(cells: string[], isSeparator: boolean): string {
  const body = cells
    .map((c) => (isSeparator ? (isAlignmentToken(c) ? c : '---') : c))
    .join(' | ')
  return `| ${body} |`
}

function isAlignmentToken(c: string): boolean {
  return /^:?-+:?$/.test(c)
}

/** 把表格替换回文档（按行覆盖） */
function replaceTable(view: EditorView, table: TableInfo, newRows: string[][]): void {
  const { state } = view
  const from = state.doc.line(table.startLine).from
  const to = state.doc.line(table.endLine).to
  const text = newRows
    .map((r, i) => serializeRow(r, i === 1 && newRows.length > 1))
    .join('\n')
  view.dispatch({
    changes: { from, to, insert: text },
    userEvent: 'input',
  })
}

/** 插入 3×3 空表到光标处 */
export function insertTable(view: EditorView): void {
  const text = '| 列1 | 列2 | 列3 |\n| --- | --- | --- |\n|  |  |  |\n'
  const { state } = view
  const spec = state.changeByRange((range) => ({
    changes: { from: range.from, insert: text },
    range: EditorSelection.cursor(range.from + text.length),
  }))
  view.dispatch({ ...spec, userEvent: 'input' })
}

/**
 * 在光标所在表格上做行/列/对齐操作。
 * 不在表格内且非 table-insert 时直接返回（无操作）。
 */
export function applyTableCommand(view: EditorView, id: string): void {
  const { state } = view
  const head = state.selection.main.head
  const curLine = state.doc.lineAt(head)
  const table = findTableAt(state.doc, curLine.number)

  if (id === 'table-insert') {
    insertTable(view)
    return
  }
  if (!table || table.rows.length < 2) return // 至少要有表头+分隔行

  const cols = table.rows[0].length
  const rowIdx = curLine.number - table.startLine
  const colIdx = columnIndexAt(curLine.text, head - curLine.from)

  const newRows = table.rows.map((r) => [...r])
  switch (id) {
    case 'table-add-row': {
      const blank = Array<string>(cols).fill('')
      // 表头/分隔行之后插入；否则插在光标行之后
      const insertAt = rowIdx <= 1 ? 2 : rowIdx + 1
      newRows.splice(insertAt, 0, blank)
      break
    }
    case 'table-del-row': {
      if (rowIdx <= 1) return // 表头/分隔行不可删
      newRows.splice(rowIdx, 1)
      break
    }
    case 'table-add-col': {
      for (const r of newRows) r.splice(colIdx + 1, 0, '')
      break
    }
    case 'table-del-col': {
      if (cols <= 1) return
      for (const r of newRows) r.splice(colIdx, 1)
      break
    }
    case 'table-align-left':
    case 'table-align-center':
    case 'table-align-right': {
      const token = id === 'table-align-left' ? ':---' : id === 'table-align-center' ? ':--:' : '---:'
      if (newRows.length > 1) newRows[1][colIdx] = token
      break
    }
    default:
      return
  }
  replaceTable(view, table, newRows)
}
