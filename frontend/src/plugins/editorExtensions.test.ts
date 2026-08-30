import { describe, expect, it } from 'vitest'
import { createMainThreadTransport } from './mainThreadTransport'
import { PluginRuntimeHost } from './runtime'
import { runBuiltinEditorCommand, setEditorUiHandlers } from './editorBridge'
import type { PluginStartInfo } from './types'

function info(overrides: Partial<PluginStartInfo> = {}): PluginStartInfo {
  return {
    id: 'trusted',
    name: 'Trusted',
    permissions: [],
    source: '',
    trusted: true,
    ...overrides,
  }
}

async function flush(): Promise<void> {
  for (let i = 0; i < 5; i += 1) await Promise.resolve()
  await new Promise(resolve => setTimeout(resolve, 0))
}

// ---------------------------------------------------------------------------
// 声明式编辑器扩展（P14）
//
// 插件跑在 Worker 里拿不到 DOM，所以只描述「想高亮什么」，由宿主构造真正的
// CodeMirror 对象。代价是表达力受限，换来的是：装饰一个关键词不需要完全信任。
//
// 这些测试的重心是白名单——插件传来的 class 会进 DOM、style 会进内联样式、
// 正则会影响性能，每一条都必须当作不可信输入。
// ---------------------------------------------------------------------------

describe('declarative editor extensions', () => {
  it('registers a decoration described by the plugin', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['editor.decorate'],
      source: `
        notevault.registerDecoration({
          class: 'todo-highlight',
          id: 'todo',
          pattern: 'TODO',
          style: { color: 'orange' },
        })
      `,
    }))
    await flush()

    const decorations = host.getDecorations()
    expect(decorations).toHaveLength(1)
    expect(decorations[0]).toMatchObject({
      class: 'todo-highlight',
      id: 'todo',
      pluginId: 'trusted',
      scope: 'match',
      style: { color: 'orange' },
    })
    host.removeAll()
  })

  it('strips unsafe class names and style properties', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['editor.decorate'],
      source: `
        notevault.registerDecoration({
          class: 'ok-class <script>alert(1)</script>',
          id: 'evil',
          pattern: 'TODO',
          style: {
            color: 'red',
            'background-image': 'url(http://evil.example/track.png)',
            position: 'fixed',
          },
        })
      `,
    }))
    await flush()

    const [decoration] = host.getDecorations()
    // 非法 class 片段直接剔除（保留合法的那个）
    expect(decoration.class).toBe('ok-class')
    // position 不在允许的属性里；url() 更要挡住——
    // 否则插件能用外链图片悄悄发请求，绕过 CSP 的 connect-src
    expect(decoration.style).toEqual({ color: 'red' })
    host.removeAll()
  })

  it('drops an invalid regex without affecting the rest', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['editor.decorate'],
      source: `
        notevault.registerDecoration({ id: 'bad', pattern: '([', regex: true })
        notevault.registerDecoration({ id: 'good', pattern: 'TODO' })
      `,
    }))
    await flush()

    // 非法正则只丢掉自己，不该让插件加载失败
    expect(host.getDecorations().map(item => item.id)).toEqual(['good'])
    host.removeAll()
  })

  it('registers widgets and keymaps', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['editor.decorate'],
      source: `
        notevault.registerWidget({
          class: 'tag-pill',
          id: 'tag',
          pattern: '#(\\\\w+)',
          regex: true,
          text: '标签: $1',
        })
        notevault.registerKeymap({ command: 'greet', id: 'k', key: 'Mod-Shift-h' })
      `,
    }))
    await flush()

    expect(host.getWidgets()).toEqual([
      {
        class: 'tag-pill',
        id: 'tag',
        matcher: expect.any(RegExp),
        pluginId: 'trusted',
        text: '标签: $1',
      },
    ])
    expect(host.getKeymaps()).toEqual([
      { command: 'greet', id: 'k', key: 'Mod-Shift-h', pluginId: 'trusted' },
    ])
    host.removeAll()
  })

  it('refuses editor extensions without editor.decorate', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport, {
      readinessTimeoutMs: 500,
    })

    await expect(host.load(info({
      permissions: ['ui'], // 有 ui 但没有 editor.decorate
      source: `notevault.registerDecoration({ id: 'x', pattern: 'TODO' })`,
    }))).rejects.toThrow(/Plugin stopped during startup/)

    expect(host.getDecorations()).toHaveLength(0)
    host.removeAll()
  })

  it('clears extensions when the plugin is unloaded', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['editor.decorate'],
      source: `
        notevault.registerDecoration({ id: 'todo', pattern: 'TODO' })
        notevault.registerWidget({ id: 'w', pattern: 'x', text: 'y' })
        notevault.registerKeymap({ command: 'c', id: 'k', key: 'Mod-a' })
      `,
    }))
    await flush()
    expect(host.getDecorations()).toHaveLength(1)

    // 插件禁用后，高亮不能还留在编辑器上
    await host.remove('trusted')
    expect(host.getDecorations()).toHaveLength(0)
    expect(host.getWidgets()).toHaveLength(0)
    expect(host.getKeymaps()).toHaveLength(0)
  })
})

// ---------------------------------------------------------------------------
// 工具栏按钮的三种点击形态（P14）
//
// transform 只能做「包裹选区」，但工具栏里还有两类它做不到的：
//   1. 需要宿主能力的（撤销、缩进、取色器）→ command
//   2. 需要整行操作的（标题、引用、列表）→ transform.linePrefix
// ---------------------------------------------------------------------------

describe('toolbar button declarations', () => {
  it('accepts a command-based button', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['ui'],
      source: `
        notevault.registerToolbarButton({
          command: 'editor:undo', icon: '↶', id: 'undo', title: '撤销',
        })
      `,
    }))
    await flush()

    expect(host.getToolbarButtons()).toEqual([
      { command: 'editor:undo', icon: '↶', id: 'undo', pluginId: 'trusted', title: '撤销' },
    ])
    host.removeAll()
  })

  it('accepts a line-prefix button for whole-line operations', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['ui'],
      source: `
        notevault.registerToolbarButton({
          id: 'h1', title: '一级标题', transform: { linePrefix: '# ' },
        })
      `,
    }))
    await flush()

    expect(host.getToolbarButtons()[0].transform).toEqual({ linePrefix: '# ' })
    host.removeAll()
  })

  it('notifies the plugin when a plugin-handled button is clicked', async () => {
    const clicks: unknown[] = []
    const g = globalThis as { __clicks?: unknown[] }
    g.__clicks = clicks

    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['ui'],
      source: `
        notevault.registerToolbarButton({ id: 'sort', title: '排序', handledByPlugin: true })
        notevault.onToolbarClick('sort', () => { globalThis.__clicks.push('sorted') })
      `,
    }))
    await flush()

    host.triggerToolbarButton('trusted', 'sort')
    await flush()

    expect(clicks).toEqual(['sorted'])
    host.removeAll()
  })
})

// ---------------------------------------------------------------------------
// 宿主内置命令（P14）
//
// 取色器 / 格式刷依赖组件里的隐藏 input 与响应式状态，挪不进 editorBridge，
// 所以由组件注册回调、宿主按命令名转发；段落对齐则是纯文本操作，直接做。
// ---------------------------------------------------------------------------

describe('builtin editor commands', () => {
  it('forwards UI-only commands to the registered handlers', () => {
    const calls: string[] = []
    setEditorUiHandlers({
      pickBackground: () => calls.push('bg'),
      pickColor: () => calls.push('color'),
      toggleBrush: () => calls.push('brush'),
    })

    expect(runBuiltinEditorCommand('editor:pickColor')).toBe(true)
    expect(runBuiltinEditorCommand('editor:pickBackground')).toBe(true)
    expect(runBuiltinEditorCommand('editor:brush')).toBe(true)
    expect(calls).toEqual(['color', 'bg', 'brush'])

    setEditorUiHandlers({}) // 还原，避免影响别的用例
  })

  it('reports unknown commands instead of throwing', () => {
    // 插件传来的命令名不可信：认不出的就如实说没处理，不要抛异常
    expect(runBuiltinEditorCommand('editor:doesNotExist')).toBe(false)
  })

  it('reports not-executed rather than throwing without an active editor', () => {
    // 没有打开任何文档时点到对齐按钮，不该把应用搞崩。
    // 如实返回 false（命令确实没执行），而不是抛异常或假装成功。
    expect(() => runBuiltinEditorCommand('editor:alignLeft')).not.toThrow()
    expect(runBuiltinEditorCommand('editor:alignLeft')).toBe(false)
  })
})
