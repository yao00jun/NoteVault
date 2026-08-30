import { afterEach, describe, expect, it, vi } from 'vitest'
import { createWorkerSource } from './workerSource'

interface ScopeFixture {
  messages: unknown[]
  receive: (data: unknown) => void
  terminated?: boolean
  [key: string]: unknown
}

let scope: ScopeFixture | null = null

function installScope(
  permissions: Array<'workspace.read' | 'workspace.write' | 'commands' | 'notifications' | 'ui'> =
    ['workspace.read', 'commands', 'notifications'],
): ScopeFixture & {
  notevault: Record<string, (...args: unknown[]) => unknown>
} {
  const messages: unknown[] = []
  const listeners = new Map<string, Array<(event: { data?: unknown }) => void>>()
  scope = {
    messages,
    get terminated() {
      return this.__terminated === true
    },
    set terminated(value: boolean) {
      this.__terminated = value
    },
    receive(data: unknown) {
      for (const listener of [...listeners.get('message') ?? []]) listener({ data })
    },
    __terminated: false,
    addEventListener(type: string, listener: (event: { data?: unknown }) => void) {
      const list = listeners.get(type) ?? []
      list.push(listener)
      listeners.set(type, list)
    },
    removeEventListener(type: string, listener: (event: { data?: unknown }) => void) {
      const list = listeners.get(type) ?? []
      const index = list.indexOf(listener)
      if (index >= 0) list.splice(index, 1)
    },
    postMessage(message: unknown) {
      messages.push(message)
    },
  }

  vi.stubGlobal('self', scope)
  ;(0, eval)(createWorkerSource('test-plugin', permissions))
  return scope as ScopeFixture & {
    notevault: Record<string, (...args: unknown[]) => unknown>
  }
}

// 等待 Promise 链排空。
// 用宏任务（setTimeout）收尾，而不是固定次数的 await Promise.resolve()：
// 依赖精确的微任务 tick 数非常脆弱——运行时里多加一层 .then 就会让断言失效。
async function flush(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await new Promise(resolve => setTimeout(resolve, 0))
}

describe('createWorkerSource', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    scope = null
  })

  it('builds an isolated bootstrap for the exact plugin identity and permissions', () => {
    const source = createWorkerSource('fixture', ['workspace.read', 'notifications'])
    expect(source).toContain('"fixture"')
    expect(source).toContain('["workspace.read","notifications"]')
    expect(source).toContain("'fetch'")
    expect(source).toContain("'localStorage'")
    expect(source).toContain("'workspace.read'")
    expect(source).toContain("'workspace.write'")
  })

  it('blocks nested Worker constructors so a plugin cannot escape the sandbox', () => {
    const installed = installScope()
    // 逃逸路径：new Worker(blobUrl) 创建的内层 Worker 是全新的全局作用域，
    // 不继承沙箱对 fetch / importScripts 的改写，等于绕过了全部隔离。
    // 因此这里验证的是「访问即抛错」，而不只是源码里出现这个字符串。
    for (const name of ['Worker', 'SharedWorker']) {
      expect(() => installed[name]).toThrow(/disabled in plugin sandbox/)
    }
  })

  it('actually blocks storage accessors, not just mentions them', () => {
    const installed = installScope()
    // 回归测试：这几项原本是单独写的 defineProperty，重命名 helper 时漏改，
    // getter 引用了未定义标识符、异常被 try/catch 吞掉——结果「源码里写着禁用了，
    // 实际属性根本没定义」。所以这里必须验证访问时真的抛错。
    for (const name of ['localStorage', 'sessionStorage', 'indexedDB']) {
      expect(() => installed[name]).toThrow(/disabled in plugin sandbox/)
    }
  })

  it('omits sandbox restrictions entirely in trusted mode', () => {
    const sandboxSource = createWorkerSource('p', [], false)
    const trustedSource = createWorkerSource('p', [], true)
    expect(sandboxSource).toContain('denyBlocked')
    // 受信任模式不生成任何禁用逻辑。刻意不生成「包在 if 里的死代码」，
    // 那种结构可被绕过，直接不生成更好审。
    expect(trustedSource).not.toContain('denyBlocked')
    expect(trustedSource).not.toContain('disabled in plugin sandbox')
  })

  it('evaluates the plugin before reporting readiness and returns command status', async () => {
    const installed = installScope()
    expect(installed.messages.some(message => (message as { type?: string }).type === 'plugin:ready')).toBe(false)

    installed.receive({
      id: 'test-plugin',
      source: `notevault.registerCommand({
        description: 'Show fixture notification',
        id: 'hello',
        label: 'Hello',
        run: () => { notevault.notify('Hi') },
      })`,
      type: 'plugin:start',
    })
    await flush()
    expect(installed.messages).toContainEqual(expect.objectContaining({
      id: 'test-plugin',
      type: 'plugin:command-register',
    }))
    expect(installed.messages.at(-1)).toMatchObject({ id: 'test-plugin', type: 'plugin:ready' })

    installed.receive({ commandId: 'hello', id: 'test-plugin', type: 'plugin:command-run' })
    await flush()
    expect(installed.messages).toContainEqual(expect.objectContaining({ type: 'plugin:notification' }))
    expect(installed.messages.at(-1)).toMatchObject({
      pluginId: 'test-plugin',
      status: 'completed',
      type: 'plugin:command-result',
    })
  })

  it('routes declared workspace reads through request ids', async () => {
    const installed = installScope()
    installed.receive({
      id: 'test-plugin',
      source: `notevault.registerCommand({
        id: 'read',
        label: 'Read',
        run: async () => { notevault.notify(await notevault.readFile('notes/a.md')) },
      })`,
      type: 'plugin:start',
    })
    await flush()

    installed.receive({ commandId: 'read', id: 'test-plugin', type: 'plugin:command-run' })
    await flush()
    const request = installed.messages.at(-1) as {
      args?: { path?: string, requestId?: string }
      method?: string
    }
    expect(request.method).toBe('workspace.read')
    expect(request.args?.path).toBe('notes/a.md')

    installed.receive({
      ok: true,
      requestId: request.args?.requestId,
      type: 'plugin:capability-response',
      value: 'file content',
    })
    await flush()
    expect(installed.messages.at(-1)).toMatchObject({
      pluginId: 'test-plugin',
      status: 'completed',
      type: 'plugin:command-result',
    })
    expect(installed.messages.some(item => (item as { type?: string }).type === 'plugin:notification'
      && (item as { message?: string }).message === 'file content')).toBe(true)
  })

  it('rejects undeclared capability requests locally without asking the host', async () => {
    const installed = installScope()
    installed.receive({
      id: 'test-plugin',
      source: `notevault.registerCommand({
        id: 'write',
        label: 'Write',
        run: async () => await notevault.writeFile('a.md', 'content'),
      })`,
      type: 'plugin:start',
    })
    await flush()
    const before = installed.messages.length

    installed.receive({ commandId: 'write', id: 'test-plugin', type: 'plugin:command-run' })
    await flush()
    expect(installed.messages.length - before).not.toBe(0)
    expect(installed.messages.filter(item => (item as { type?: string }).type === 'plugin:capability-request'))
      .toEqual([])
    expect(installed.messages.at(-1)).toMatchObject({
      error: 'workspace.write permission is required',
      status: 'failed',
      type: 'plugin:command-result',
    })
  })

  it('emits a toolbar-register message when ui permission is granted', async () => {
    const installed = installScope(['ui'])
    installed.receive({
      id: 'test-plugin',
      source: `notevault.registerToolbarButton({
        id: 'hl',
        title: '高亮',
        icon: '==',
        transform: { prefix: '==', suffix: '==', placeholder: '高亮文本' },
      })`,
      type: 'plugin:start',
    })
    await flush()
    expect(installed.messages).toContainEqual(expect.objectContaining({
      button: {
        id: 'hl',
        title: '高亮',
        icon: '==',
        transform: { prefix: '==', suffix: '==', placeholder: '高亮文本' },
      },
      type: 'plugin:toolbar-register',
    }))
  })

  it('rejects toolbar registration without ui permission', async () => {
    const installed = installScope(['commands'])
    installed.receive({
      id: 'test-plugin',
      source: `notevault.registerToolbarButton({ id: 'hl', title: '高亮' })`,
      type: 'plugin:start',
    })
    await flush()
    expect(installed.messages.some(item => (item as { type?: string }).type === 'plugin:toolbar-register'))
      .toBe(false)
  })

  it('emits plugin:error when toolbar registration lacks ui permission', async () => {
    // 缺权限时不能把整插件拖挂——只发 plugin:error 消息，
    // 插件继续运行、其他按钮/装饰仍可注册。
    // 旧实现是 throw，会导致 plugin:start 整体失败、整插件标记为 failed。
    const installed = installScope(['commands'])
    installed.receive({
      id: 'test-plugin',
      source: `notevault.registerToolbarButton({ id: 'hl', title: '高亮' })`,
      type: 'plugin:start',
    })
    await flush()
    const errors = installed.messages.filter(
      item => (item as { type?: string }).type === 'plugin:error',
    )
    expect(errors.length).toBeGreaterThan(0)
  })
})
