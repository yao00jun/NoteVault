import { describe, expect, it } from 'vitest'
import { createMainThreadTransport } from './mainThreadTransport'
import { PluginRuntimeHost, type PluginCapabilities } from './runtime'
import type { PluginStartInfo } from './types'

// 用主进程通道跑：node 测试环境里没有真正的 Worker
function hostWith(caps: PluginCapabilities): PluginRuntimeHost {
  return new PluginRuntimeHost(createMainThreadTransport, { capabilities: caps })
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

describe('plugin file & metadata capabilities', () => {
  it('routes listFiles / search / tags through the host', async () => {
    const calls: string[] = []
    const host = hostWith({
      async getAllTags() {
        calls.push('tags')
        return [{ name: 'note' }]
      },
      async listFiles() {
        calls.push('list')
        return ['a.md', 'b.md']
      },
      async search(query) {
        calls.push(`search:${query}`)
        return [{ path: 'a.md' }]
      },
    })

    await host.load(info({
      permissions: ['commands', 'notifications', 'workspace.read'],
      source: `
        notevault.registerCommand({
          id: 'probe',
          label: 'Probe',
          run: async () => {
            const files = await notevault.listFiles()
            await notevault.search('hello')
            const tags = await notevault.getAllTags()
            notevault.notify(files.join(',') + '|' + tags.length)
          },
        })
      `,
    }))

    await host.runCommand('trusted', 'probe')
    expect(calls).toEqual(['list', 'search:hello', 'tags'])
    host.removeAll()
  })

  it('refuses write capabilities when workspace.write was not declared', async () => {
    const host = hostWith({
      async deleteFile() {
        throw new Error('deleteFile must not be reached without permission')
      },
    })

    await host.load(info({
      permissions: ['commands', 'workspace.read'], // 故意不给 workspace.write
      source: `
        notevault.registerCommand({
          id: 'del',
          label: 'Del',
          run: async () => { await notevault.deleteFile('a.md') },
        })
      `,
    }))

    // 沙箱侧就会先拦下来，根本不会打到宿主
    await expect(host.runCommand('trusted', 'del')).rejects.toThrow(/permission is required/)
    host.removeAll()
  })

  it('reports capability unavailable instead of failing the plugin load', async () => {
    // 宿主什么能力都不提供：插件仍能加载，只在真正调用那个能力时报错。
    // 这样宿主逐步补齐能力时，老插件不会因为「用到了还没实现的能力」而整个挂掉。
    const host = hostWith({})

    await host.load(info({
      permissions: ['commands', 'workspace.read'],
      source: `
        notevault.registerCommand({
          id: 'ls',
          label: 'Ls',
          run: async () => { await notevault.listFiles() },
        })
      `,
    }))

    expect(host.hasCommand('trusted', 'ls')).toBe(true)
    await expect(host.runCommand('trusted', 'ls')).rejects.toThrow(/capability unavailable/)
    host.removeAll()
  })

  it('passes semantic argument names rather than reusing a generic field', async () => {
    // 改协议时最容易出错的地方：所有能力都塞进同一个 path 字段。
    // 这里守住 tags.files 用 tag、search.query 用 query。
    const seen: Record<string, string> = {}
    const host = hostWith({
      async getFilesByTag(tag) {
        seen.tag = tag
        return []
      },
      async search(query) {
        seen.query = query
        return []
      },
    })

    await host.load(info({
      permissions: ['commands', 'workspace.read'],
      source: `
        notevault.registerCommand({
          id: 'args',
          label: 'Args',
          run: async () => {
            await notevault.getFilesByTag('mytag')
            await notevault.search('myquery')
          },
        })
      `,
    }))

    await host.runCommand('trusted', 'args')
    expect(seen).toEqual({ query: 'myquery', tag: 'mytag' })
    host.removeAll()
  })
})
