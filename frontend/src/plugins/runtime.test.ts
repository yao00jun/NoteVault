import { describe, expect, it, vi } from 'vitest'
import { PluginRuntimeHost } from './runtime'
import type { PluginTransport } from './types'

class FakeTransport implements PluginTransport {
  private listeners = new Map<string, Array<(event: { data?: unknown }) => void>>()
  readonly sent: unknown[] = []
  terminated = false

  addEventListener(type: string, listener: (event: { data?: unknown }) => void) {
    const list = this.listeners.get(type) ?? []
    list.push(listener)
    this.listeners.set(type, list)
  }

  removeEventListener(type: string, listener: (event: { data?: unknown }) => void) {
    const list = this.listeners.get(type) ?? []
    this.listeners.set(type, list.filter(item => item !== listener))
  }

  postMessage(data: unknown) {
    this.sent.push(data)
  }

  terminate() {
    this.terminated = true
  }

  receive(data: unknown) {
    for (const listener of this.listeners.get('message') ?? []) {
      listener({ data })
    }
  }
}

const scriptSource = 'notevault.registerCommand({ id: "hello", label: "Hello" })'

describe('PluginRuntimeHost', () => {
  it('starts a plugin, accepts commands, and executes one', async () => {
    const worker = new FakeTransport()
    const onError = vi.fn()
    const host = new PluginRuntimeHost(() => worker, { readinessTimeoutMs: 1000, onEvent: onError })

    const loading = host.load({ id: 'p1', name: 'P1', source: scriptSource, permissions: ['commands'] })

    worker.receive({
      pluginId: 'p1',
      type: 'plugin:ready',
    })
    worker.receive({
      command: { description: '', id: 'hello', label: 'Hello' },
      pluginId: 'p1',
      type: 'plugin:command-register',
    })
    await loading
    expect(worker.sent[0]).toMatchObject({
      id: 'p1',
      permissions: ['commands'],
      source: scriptSource,
      type: 'plugin:start',
    })

    expect(host.getCommands()).toEqual([
      { description: '', id: 'hello', label: 'Hello', pluginId: 'p1' },
    ])
    expect(host.getFailedPlugins()).toEqual([])

    const running = host.runCommand('p1', 'hello')
    expect(worker.sent.at(-1)).toMatchObject({ commandId: 'hello', id: 'p1', type: 'plugin:command-run' })
    worker.receive({ id: 'p1', status: 'completed', type: 'plugin:command-result' })
    await expect(running).resolves.toBeUndefined()
    expect(onError).not.toHaveBeenCalled()
  })

  it('rejects a capability the plugin did not declare', async () => {
    const worker = new FakeTransport()
    let notified = false
    const host = new PluginRuntimeHost(
      () => worker,
      { notifications: message => { notified = message === 'Hi'; }, readinessTimeoutMs: 100 },
    )
    const loading = host.load({
      id: 'p2',
      name: 'P2',
      source: '',
      permissions: ['commands', 'notifications'],
    })
    worker.receive({ type: 'plugin:ready', pluginId: 'p2' })
    worker.receive({
      command: { description: '', id: 'read', label: 'Read' },
      pluginId: 'p2',
      type: 'plugin:command-register',
    })
    await loading
    expect(host.hasCommand('p2', 'read')).toBe(true)

    const pending = host.runCommand('p2', 'read')
    await Promise.resolve()
    worker.receive({ id: 'req-1', method: 'workspace.read', type: 'plugin:capability-request' })
    await expect(pending).rejects.toThrow('permission denied')
    expect(notified).toBe(false)
  })

  it('marks a plugin failed when startup times out', async () => {
    vi.useFakeTimers()
    try {
      const host = new PluginRuntimeHost(() => new FakeTransport(), { readinessTimeoutMs: 10 })
      const loading = host.load({ id: 'slow', name: 'Slow', source: '', permissions: ['commands'] })
      const assertion = expect(loading).rejects.toThrow('startup timeout')
      await vi.advanceTimersByTimeAsync(11)
      await assertion
      expect(host.getFailedPlugins()).toEqual([
        expect.objectContaining({ id: 'slow', name: 'Slow' }),
      ])
    } finally {
      vi.useRealTimers()
    }
  })

  it('registers a plugin toolbar button when ui permission is granted', async () => {
    const worker = new FakeTransport()
    const host = new PluginRuntimeHost(() => worker, { readinessTimeoutMs: 1000 })
    const loading = host.load({
      id: 'tb', name: 'TB', source: '', permissions: ['ui'],
    })
    worker.receive({ pluginId: 'tb', type: 'plugin:ready' })
    worker.receive({
      button: { id: 'hl', title: '高亮', transform: { prefix: '==', suffix: '==' } },
      pluginId: 'tb',
      type: 'plugin:toolbar-register',
    })
    await loading
    expect(host.getToolbarButtons()).toEqual([
      { id: 'hl', pluginId: 'tb', title: '高亮', transform: { prefix: '==', suffix: '==' } },
    ])
  })

  it('ignores toolbar registration without ui permission', async () => {
    const worker = new FakeTransport()
    const host = new PluginRuntimeHost(() => worker, { readinessTimeoutMs: 1000 })
    const loading = host.load({
      id: 'tb2', name: 'TB2', source: '', permissions: ['commands'],
    })
    worker.receive({ pluginId: 'tb2', type: 'plugin:ready' })
    worker.receive({
      button: { id: 'x', title: 'X' },
      pluginId: 'tb2',
      type: 'plugin:toolbar-register',
    })
    await loading
    expect(host.getToolbarButtons()).toEqual([])
  })

  it('clears toolbar buttons when the plugin is stopped', async () => {
    const worker = new FakeTransport()
    // unloadTimeoutMs 调小：这个 fake worker 不会响应 plugin:unload，
    // 否则本用例每次都要等满 1 秒的默认卸载窗口
    const host = new PluginRuntimeHost(() => worker, {
      readinessTimeoutMs: 1000,
      unloadTimeoutMs: 50,
    })
    const loading = host.load({
      id: 'tb3', name: 'TB3', source: '', permissions: ['ui'],
    })
    worker.receive({ pluginId: 'tb3', type: 'plugin:ready' })
    worker.receive({
      button: { id: 'hl', title: '高亮' },
      pluginId: 'tb3',
      type: 'plugin:toolbar-register',
    })
    await loading
    expect(host.getToolbarButtons()).toHaveLength(1)

    // remove 现在是异步的：会先给插件一个执行 onunload 的窗口再终止
    await host.remove('tb3')
    expect(host.getToolbarButtons()).toEqual([])
  })
})
