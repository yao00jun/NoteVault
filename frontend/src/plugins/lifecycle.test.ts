import { describe, expect, it } from 'vitest'
import { createMainThreadTransport } from './mainThreadTransport'
import { PluginRuntimeHost } from './runtime'
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

const globals = globalThis as { __lifecycle?: unknown[] }

async function flush(): Promise<void> {
  for (let i = 0; i < 5; i += 1) await Promise.resolve()
  await new Promise(resolve => setTimeout(resolve, 0))
}

describe('plugin lifecycle', () => {
  it('delivers workspace events to subscribed plugins', async () => {
    const events: unknown[] = []
    globals.__lifecycle = events

    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      permissions: ['workspace.read'],
      source: `
        notevault.onFileChange(event => { globalThis.__lifecycle.push(event) })
      `,
    }))

    host.emitWorkspaceEvent({ path: 'a.md', type: 'create' })
    await flush()

    expect(events).toEqual([{ path: 'a.md', type: 'create' }])
    host.removeAll()
  })

  it('withholds workspace events from plugins without workspace.read', async () => {
    // 事件里带文件路径，没有读权限的插件不该借此拿到库的结构。
    // 订阅入口就会拒绝，插件连 handler 都注册不上。
    const host = new PluginRuntimeHost(createMainThreadTransport, {
      readinessTimeoutMs: 500,
    })

    // 插件一启动就抛错，宿主会立刻标记失败并卸载它
    // （reject 消息来自 stop 而不是启动超时——这比干等超时更合理）
    await expect(host.load(info({
      permissions: ['commands'], // 故意不给 workspace.read
      source: 'notevault.onFileChange(() => {})',
    }))).rejects.toThrow(/Plugin stopped during startup/)

    expect(host.getFailedPlugins().map(item => item.id)).toContain('trusted')
    host.removeAll()
  })

  it('round-trips plugin settings between the host and the plugin', async () => {
    const seen: unknown[] = []
    globals.__lifecycle = seen

    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      source: `
        notevault.registerSettings({
          title: 'Demo',
          items: [{ key: 'apiKey', label: 'API Key', type: 'text', default: '' }],
          values: { apiKey: 'initial' },
        })
        notevault.onSettingsChange(values => { globalThis.__lifecycle.push(values) })
      `,
    }))

    // 宿主拿到 schema 才能渲染设置界面
    const schema = host.getPluginSettings('trusted')
    expect(schema?.title).toBe('Demo')
    expect(schema?.items).toHaveLength(1)
    expect(schema?.values).toEqual({ apiKey: 'initial' })

    // 用户在宿主界面改值 → 整份值回传给插件，由插件自己持久化
    host.updatePluginSetting('trusted', 'apiKey', 'changed')
    await flush()

    expect(seen).toEqual([{ apiKey: 'changed' }])
    host.removeAll()
  })

  it('runs onload on start and onunload before teardown', async () => {
    const events: string[] = []
    globals.__lifecycle = events

    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      source: `
        notevault.onLoad(() => { globalThis.__lifecycle.push('load') })
        notevault.onUnload(() => { globalThis.__lifecycle.push('unload') })
      `,
    }))

    expect(events).toEqual(['load'])

    // 卸载前必须给插件清理的机会——以前是直接 terminate，插件没有任何回调时机
    await host.remove('trusted')
    expect(events).toEqual(['load', 'unload'])
  })

  it('also accepts an exported { onload, onunload } object', async () => {
    const events: string[] = []
    globals.__lifecycle = events

    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      // 注意必须显式 return：new Function 不会自动返回最后一个表达式的值。
      // 这也是为什么更推荐 notevault.onLoad/onUnload——不容易漏。
      source: `
        return {
          onload: () => { globalThis.__lifecycle.push('load') },
          onunload: () => { globalThis.__lifecycle.push('unload') },
        }
      `,
    }))

    expect(events).toEqual(['load'])
    await host.remove('trusted')
    expect(events).toEqual(['load', 'unload'])
  })

  it('keeps running the remaining handlers when one onunload throws', async () => {
    const events: string[] = []
    globals.__lifecycle = events

    const host = new PluginRuntimeHost(createMainThreadTransport)
    await host.load(info({
      source: `
        notevault.onUnload(() => { throw new Error('boom') })
        notevault.onUnload(() => { globalThis.__lifecycle.push('second') })
      `,
    }))

    // 一个清理回调抛错不能连坐——否则后面的资源清理会被整段跳过
    await host.remove('trusted')
    expect(events).toEqual(['second'])
  })
})
