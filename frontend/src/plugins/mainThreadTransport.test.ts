import { describe, expect, it } from 'vitest'
import { createMainThreadTransport } from './mainThreadTransport'
import { PluginRuntimeHost } from './runtime'
import type { PluginStartInfo } from './types'

// 等微任务队列排空（transport 内部用 queueMicrotask 派发，模拟 Worker 的异步语义）
async function flush(): Promise<void> {
  for (let i = 0; i < 5; i += 1) await Promise.resolve()
  await new Promise(resolve => setTimeout(resolve, 0))
}

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

describe('createMainThreadTransport', () => {
  it('starts a plugin and registers its command', async () => {
    const host = new PluginRuntimeHost(createMainThreadTransport, {
      capabilities: {
        async readFile() {
          return 'note body'
        },
      },
    })

    await host.load(info({
      permissions: ['commands', 'notifications'],
      source: `
        notevault.registerCommand({
          description: 'demo',
          id: 'ping',
          label: 'Ping',
          run: () => { notevault.notify('pong') },
        })
      `,
    }))

    expect(host.hasCommand('trusted', 'ping')).toBe(true)
    await host.runCommand('trusted', 'ping')
    host.removeAll()
  })

  it('reports a failing command instead of hanging when the handler throws', async () => {
    // 回归测试：run 同步抛错时，异常原本会在 Promise 参数求值阶段逃逸，
    // 宿主收不到 command-result，命令一直挂到超时（主进程模式下还会变成未捕获异常）。
    const host = new PluginRuntimeHost(createMainThreadTransport)

    await host.load(info({
      permissions: ['commands'],
      source: `
        notevault.registerCommand({
          id: 'boom',
          label: 'Boom',
          run: () => { throw new Error('kaboom') },
        })
      `,
    }))

    await expect(host.runCommand('trusted', 'boom')).rejects.toThrow('kaboom')
    host.removeAll()
  })

  it('does not echo host messages back to the host', async () => {
    // Worker 里 postMessage 是跨线程的：宿主发出去的消息只有插件收到，
    // 宿主自己的监听器不会收到。主线程通道必须模拟同样的隔离，
    // 否则两侧监听器混在一个集合里，彼此收到对方的消息。
    const transport = createMainThreadTransport(info())

    const seen: string[] = []
    transport.addEventListener('message', event => {
      const data = event.data as { type?: string }
      if (data?.type) seen.push(data.type)
    })

    transport.postMessage({ id: 'trusted', pluginId: 'trusted', source: '', type: 'plugin:start' })
    await flush()

    // 宿主只应看到插件回的 ready；如果通道没隔离，这里还会多出自己发的 'plugin:start'
    expect(seen).toEqual(['plugin:ready'])

    transport.terminate()
  })

  it('stops delivering messages after terminate', async () => {
    const transport = createMainThreadTransport(info())

    const seen: unknown[] = []
    transport.addEventListener('message', event => seen.push(event.data))

    transport.terminate()
    transport.postMessage({ id: 'trusted', pluginId: 'trusted', source: '', type: 'plugin:start' })
    await flush()

    expect(seen).toHaveLength(0)
  })
})
