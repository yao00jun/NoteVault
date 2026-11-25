import { describe, it, expect, beforeEach, vi, type Mock } from 'vitest'
import {
  setActiveEditor,
  getActiveEditor,
  getEditorSelection,
  insertAtCursor,
} from './activeEditor'

// 轻量访问层的契约：只依赖 EditorView 的「形状」，测试里用最小桩即可，
// 不需要为此加载真正的 CodeMirror（否则这个测试本身就把 CM 拖进来了）。
interface FakeEditor {
  state: {
    selection: { main: { from: number; to: number; head: number } }
    sliceDoc: (from: number, to: number) => string
    update: Mock
  }
  dispatch: (spec: unknown) => void
  focus: () => void
}

function makeEditor(doc: string, from = 0, to = 0): FakeEditor {
  const update = vi.fn((spec: unknown) => ({ spec }))
  return {
    state: {
      selection: { main: { from, to, head: to } },
      sliceDoc: vi.fn((f: number, t: number) => doc.slice(f, t)),
      update,
    },
    dispatch: vi.fn(),
    focus: vi.fn(),
  }
}

beforeEach(() => {
  setActiveEditor(null)
})

describe('activeEditor registry', () => {
  it('defaults to no active editor', () => {
    expect(getActiveEditor()).toBeNull()
  })

  it('registers and clears the active editor', () => {
    const editor = makeEditor('hello')
    setActiveEditor(editor as never)
    expect(getActiveEditor()).toBe(editor)
    setActiveEditor(null)
    expect(getActiveEditor()).toBeNull()
  })
})

describe('getEditorSelection', () => {
  it('returns the selected text of the main range', () => {
    const editor = makeEditor('hello world', 0, 5)
    setActiveEditor(editor as never)
    expect(getEditorSelection()).toBe('hello')
    expect(editor.state.sliceDoc).toHaveBeenCalledWith(0, 5)
  })

  it('returns an empty string when there is no active editor', () => {
    expect(getEditorSelection()).toBe('')
  })
})

describe('insertAtCursor', () => {
  it('inserts at the cursor head, moves the cursor after the text and refocuses', () => {
    const editor = makeEditor('abcdef', 2, 2)
    setActiveEditor(editor as never)

    expect(insertAtCursor('XY')).toBe(true)

    expect(editor.state.update).toHaveBeenCalledWith({
      changes: { from: 2, insert: 'XY' },
      selection: { anchor: 4 },
      userEvent: 'input',
    })
    expect(editor.dispatch).toHaveBeenCalledTimes(1)
    expect(editor.focus).toHaveBeenCalledTimes(1)
  })

  it('does not replace an existing selection (insert, not replace)', () => {
    // 用户选中了一段文字再点「插入」：选区内容绝不能被顶掉，
    // 插入点必须是 head，range 的 from/to 不参与。
    const editor = makeEditor('abcdef', 1, 5)
    setActiveEditor(editor as never)

    insertAtCursor('Z')

    const spec = editor.state.update.mock.calls[0][0] as { changes: { from: number } }
    expect(spec.changes.from).toBe(5)
  })

  it('reports failure instead of throwing when there is no active editor', () => {
    expect(insertAtCursor('x')).toBe(false)
  })
})

// ---------------------------------------------------------------------------
// 打包边界回归锁
//
// 这一层存在的意义是「应用外壳问有没有活动编辑器，不必背上 CodeMirror」。
// 入口静态引入 editorBridge 会让 CM 回到首屏 chunk（实测入口 612KB → 305KB，
// 一行 import 就能悄悄还回去），而功能测试完全发现不了。
//
// 源码用 Vite 的 ?raw 批量读入：tsconfig 只挂了 vite/client 类型（没有 Node 的
// fs/path 类型），而且 vitest 与构建走同一套模块解析，"谁能静态引到 editorBridge"
// 的判定标准与打包器一致。
// ---------------------------------------------------------------------------

const SOURCES = import.meta.glob('/src/**/*.{ts,vue}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

/** 读取源文件，去掉 `import type ... from`（编译期擦除，不产生运行时依赖） */
function runtimeImports(code: string): string {
  return code.replace(/\bimport\s+type\s+[\s\S]*?\s+from\s+['"][^'"]+['"]/g, '')
}

function source(relative: string): string {
  const code = SOURCES[`/src/${relative}`]
  if (code == null) throw new Error(`回归锁没读到 /src/${relative}，检查 import.meta.glob 的匹配范围`)
  return runtimeImports(code)
}

/** 外壳 = 首屏入口静态可达、却不该依赖编辑器引擎的模块 */
const SHELL_MODULES = [
  'App.vue',
  'components/layout/AICopilotDrawer.vue',
  'stores/pluginRuntime.ts',
  'plugins/activeEditor.ts',
]

const STATIC_EDITOR_BRIDGE = /from\s*['"][^'"]*editorBridge['"]/

describe('bundle boundary: shell modules must not statically import the CodeMirror bridge', () => {
  it.each(SHELL_MODULES)('%s has no static runtime import of editorBridge', relative => {
    expect(STATIC_EDITOR_BRIDGE.test(source(relative))).toBe(false)
  })

  it.each(SHELL_MODULES)('%s has no static runtime import of CodeMirror packages', relative => {
    expect(/from\s*['"](?:@codemirror\/|@lezer\/|codemirror['"])/.test(source(relative))).toBe(false)
  })

  it('the bridge itself stays out of the entry: only lazy routes may import it statically', () => {
    // 允许静态引入 editorBridge 的，只有随编辑器路由一起加载的模块
    const allowed = new Set([
      'components/editor/MarkdownEditor.vue',
      'views/EditorView.vue',
    ])
    const offenders = Object.keys(SOURCES)
      .map(path => path.replace(/^\/src\//, ''))
      .filter(rel => !rel.endsWith('.test.ts'))
      .filter(rel => rel !== 'plugins/editorBridge.ts' && rel !== 'plugins/activeEditor.ts')
      .filter(rel => STATIC_EDITOR_BRIDGE.test(runtimeImports(SOURCES[`/src/${rel}`])))
      .filter(rel => !allowed.has(rel))
    expect(offenders).toEqual([])
  })
})
