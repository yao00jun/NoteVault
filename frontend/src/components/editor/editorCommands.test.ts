// @vitest-environment jsdom
import { describe, expect, it, beforeEach } from 'vitest'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import {
  alignLines,
  applyFormat,
  applyTableCommand,
  insertBlock,
  insertCallout,
  insertTable,
  insertText,
  prefixLines,
  setHeading,
  wrapSelection,
} from './editorCommands'

let host: HTMLElement
let view: EditorView

function createView(doc: string): EditorView {
  return new EditorView({
    state: EditorState.create({ doc, extensions: [] }),
    parent: host,
  })
}

/** 把 [from, to) 设为唯一选区 */
function select(v: EditorView, from: number, to: number): void {
  v.dispatch({ selection: { anchor: from, head: to } })
}

function docText(v: EditorView): string {
  return v.state.doc.toString()
}

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  view = createView('hello world')
})

describe('wrapSelection', () => {
  it('有选区时包裹并把选区留在内容上', () => {
    select(view, 0, 5)
    wrapSelection(view, '**')
    expect(docText(view)).toBe('**hello** world')
    expect(view.state.selection.main).toMatchObject({ from: 2, to: 7 })
  })

  it('无选区时插入成对包裹符并落在中间', () => {
    wrapSelection(view, '`')
    expect(docText(view)).toBe('``hello world')
    expect(view.state.selection.main.from).toBe(1)
  })

  it('前后缀不同时各自正确（链接）', () => {
    select(view, 0, 5)
    wrapSelection(view, '[', '](url)')
    expect(docText(view)).toBe('[hello](url) world')
  })
})

describe('setHeading', () => {
  it('给行设置标题级别', () => {
    setHeading(view, 2)
    expect(docText(view)).toBe('## hello world')
  })

  it('同级别再次执行则取消标题', () => {
    setHeading(view, 1)
    setHeading(view, 1)
    expect(docText(view)).toBe('hello world')
  })

  it('已有其它级别时替换为新级别', () => {
    view = createView('### title')
    setHeading(view, 1)
    expect(docText(view)).toBe('# title')
  })
})

describe('prefixLines', () => {
  it('无前缀时添加', () => {
    prefixLines(view, '> ')
    expect(docText(view)).toBe('> hello world')
  })

  it('已有前缀时移除（toggle）', () => {
    prefixLines(view, '- ')
    prefixLines(view, '- ')
    expect(docText(view)).toBe('hello world')
  })
})

describe('alignLines', () => {
  it('包裹对齐标签', () => {
    alignLines(view, 'center')
    expect(docText(view)).toBe('<center>hello world</center>')
  })

  it('相同对齐再次执行则取消', () => {
    alignLines(view, 'center')
    alignLines(view, 'center')
    expect(docText(view)).toBe('hello world')
  })

  it('不同对齐直接替换', () => {
    alignLines(view, 'center')
    alignLines(view, 'right')
    expect(docText(view)).toBe('<p align="right">hello world</p>')
  })
})

describe('insertBlock / insertText', () => {
  it('insertBlock 无选区时在光标处插入占位代码块', () => {
    insertBlock(view, '```\n', '\n```\n')
    expect(docText(view)).toBe('```\n代码\n```\nhello world')
  })

  it('insertText 在光标处插入', () => {
    select(view, 5, 5)
    insertText(view, '\n---\n')
    expect(docText(view)).toBe('hello\n---\n world')
  })
})

describe('applyFormat', () => {
  it('table 命令插入三列表格', () => {
    applyFormat(view, 'table')
    expect(docText(view)).toContain('| 列1 | 列2 | 列3 |')
  })

  it('bold 命令等价于 wrapSelection(**)', () => {
    select(view, 0, 5)
    applyFormat(view, 'bold')
    expect(docText(view)).toBe('**hello** world')
  })

  it('hr 命令插入分割线', () => {
    applyFormat(view, 'hr')
    expect(docText(view)).toBe('\n---\nhello world')
  })

  it('未知命令不改动文档', () => {
    applyFormat(view, 'nonexistent')
    expect(docText(view)).toBe('hello world')
  })
})

describe('insertCallout', () => {
  it('无选区时插入占位模板', () => {
    insertCallout(view, 'note')
    expect(docText(view)).toBe('> [!note]\n> 在此输入内容\nhello world')
  })

  it('类型默认 note 且大小写归一', () => {
    insertCallout(view, 'WARNING')
    expect(docText(view)).toContain('> [!warning]')
  })

  it('有选区时把选区逐行加 > 前缀作为 callout 内容', () => {
    select(view, 0, 'hello world'.length)
    insertCallout(view, 'tip')
    expect(docText(view)).toBe('> [!tip]\n> hello world\n')
    expect(view.state.selection.main.from).toBe('> [!tip]\n> hello world\n'.length)
  })

  it('多行选区逐行加前缀', () => {
    const v = createView('line1\nline2')
    select(v, 0, v.state.doc.length)
    insertCallout(v, 'info')
    expect(docText(v)).toBe('> [!info]\n> line1\n> line2\n')
  })
})

describe('applyTableCommand', () => {
  const tableDoc = '| a | b |\n| --- | --- |\n| 1 | 2 |\n'
  function tableWithCursor(row: number, col: number): EditorView {
    const v = createView(tableDoc)
    const line = v.state.doc.line(row)
    const text = line.text
    let count = 0
    let pos = line.from
    for (let i = 0; i < text.length; i++) {
      if (text[i] === '|') {
        count++
        if (count === col + 1) {
          pos = line.from + i + 1
          break
        }
      }
    }
    v.dispatch({ selection: { anchor: pos } })
    return v
  }

  it('不在表格内时 table-add-row 不改变文档', () => {
    const v = createView('普通文本')
    v.dispatch({ selection: { anchor: 2 } })
    applyTableCommand(v, 'table-add-row')
    expect(docText(v)).toBe('普通文本')
  })

  it('table-insert 插入 3x3 空表', () => {
    const v = createView('x')
    v.dispatch({ selection: { anchor: 0 } })
    applyTableCommand(v, 'table-insert')
    expect(docText(v)).toContain('| 列1 | 列2 | 列3 |')
    expect(docText(v)).toContain('| --- | --- | --- |')
  })

  it('table-add-row 在光标数据行后插入空行', () => {
    const v = tableWithCursor(3, 0)
    applyTableCommand(v, 'table-add-row')
    const lines = docText(v).split('\n')
    expect(lines[0]).toBe('| a | b |')
    expect(lines[1]).toBe('| --- | --- |')
    expect(lines[2]).toBe('| 1 | 2 |')
    expect(lines[3]).toBe('|  |  |')
  })

  it('table-del-col 删除光标所在列', () => {
    const v = tableWithCursor(3, 0)
    applyTableCommand(v, 'table-del-col')
    const lines = docText(v).split('\n')
    expect(lines[0]).toBe('| b |')
    expect(lines[2]).toBe('| 2 |')
  })

  it('table-align-center 设置分隔行对齐标记', () => {
    const v = tableWithCursor(3, 1)
    applyTableCommand(v, 'table-align-center')
    const lines = docText(v).split('\n')
    expect(lines[1]).toBe('| --- | :--: |')
  })

  it('table-del-row 不删表头/分隔行', () => {
    const v = tableWithCursor(1, 0)
    applyTableCommand(v, 'table-del-row')
    expect(docText(v).split('\n')[0]).toBe('| a | b |')
  })
})
