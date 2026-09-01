import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  PluginService: {
    ListPlugins: vi.fn(),
  },
  FileService: {
    ReadFile: vi.fn(),
    SaveFile: vi.fn(),
  },
}))

class FakeWorker {
  static instances: Array<{ source: string, type?: string }> = []
  constructor(source: string, options: { type?: string } = {}) {
    FakeWorker.instances.push({ source, type: options.type })
  }

  addEventListener() {}
  postMessage() {}
  removeEventListener() {}
  terminate() {}
}

vi.mock('@/plugins/runtime', () => ({
  PluginRuntimeHost: class PluginRuntimeHost {
    static instances: Array<Record<string, any>> = []
    commands = [
      { description: '', id: 'hello', label: 'E2E Plugin Notify', pluginId: 'p1' },
    ]
    toolbarButtons = [
      {
        id: 'hl',
        pluginId: 'p1',
        title: '高亮',
        transform: { prefix: '==', suffix: '==', placeholder: '高亮文本' },
      },
    ]
  failedPlugins = [{ id: 'bad', name: 'Bad Plugin' }]
  loaded: Array<{ id: string }> = []
  factory: (info: { id: string }) => unknown
  options: Record<string, unknown>
    getCommands = () => this.commands.filter(command => command.pluginId !== 'removed')
    getToolbarButtons = () => this.toolbarButtons
    getFailedPlugins = () => this.failedPlugins
    emitWorkspaceEvent = vi.fn()
    async load(info: { id: string }) {
      void this.factory(info)
      this.loaded.push(info)
    }
    remove = vi.fn()
    runCommand = vi.fn(async () => undefined)

    constructor(
      factory: (info: { id: string }) => unknown,
      options: Record<string, unknown> = {},
    ) {
      this.factory = factory
      this.options = options
      PluginRuntimeHost.instances.push(this)
    }
  },
}))

vi.mock('@/plugins/workerSource', () => ({
  createWorkerSource: vi.fn((id: string) => `// fixture worker:${id}`),
}))

// E-6：文件变更改由后端 fsnotify 推送，store 会订阅 Wails 事件总线。
// 单测里没有事件总线，mock 掉并允许用例直接操纵回调。
const eventsOnUnsubscribe = vi.fn()
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => eventsOnUnsubscribe),
  },
}))

vi.mock('@/plugins/editorBridge', () => ({
  setActiveEditor: vi.fn(),
  getActiveEditor: vi.fn(() => null),
  applyTransform: vi.fn(),
}))

import { FileService, PluginService } from '@bindings/github.com/notevault/notevault/index.js'
import { Events } from '@wailsio/runtime'
import { PluginRuntimeHost } from '@/plugins/runtime'
import { createWorkerSource } from '@/plugins/workerSource'
import { applyTransform } from '@/plugins/editorBridge'
import { usePluginRuntimeStore } from './pluginRuntime'
import { useWorkspaceStore } from './workspace'

const listPlugins = vi.mocked(PluginService.ListPlugins)
const readFile = vi.mocked(FileService.ReadFile)
const saveFile = vi.mocked(FileService.SaveFile)
const applyTransformMock = vi.mocked(applyTransform)
const hostInstances = (PluginRuntimeHost as unknown as {
  instances: Array<Record<string, any>>
}).instances

const enabledPlugin = {
  enabled: true,
  filePath: '/plugins/e2e.js',
  hasError: false,
  hash: 'hash1',
  manifest: {
    id: 'p1',
    name: 'E2E Plugin',
    permissions: ['notifications'],
    version: '1.0.0',
  },
  modTime: '',
  size: 10,
  source: 'notevault.registerCommand({ id: "hello", label: "Hi" })',
} as any

beforeEach(() => {
  setActivePinia(createPinia())
  FakeWorker.instances.length = 0
  vi.stubGlobal('Blob', class {})
  vi.stubGlobal('URL', {
    createObjectURL: () => 'blob:fixture',
    revokeObjectURL: () => {},
  })
  vi.stubGlobal('Worker', FakeWorker)
  hostInstances.length = 0
  listPlugins.mockReset()
  readFile.mockReset()
  saveFile.mockReset()
  vi.mocked(Events.On).mockClear()
  eventsOnUnsubscribe.mockClear()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('usePluginRuntimeStore', () => {
  it('starts enabled plugins and exposes registered commands plus runtime failures', async () => {
    const workspaceStore = useWorkspaceStore()
    workspaceStore.setCurrentWorkspace({
      createdAt: '', id: 'ws', lastOpenedAt: '', name: 'Vault', path: '/tmp/vault',
    })
    listPlugins.mockResolvedValue([enabledPlugin])

    const store = usePluginRuntimeStore()
    await store.initialize()

    const host = hostInstances[0]
    const workerInfo = FakeWorker.instances[0]
    expect(host.loaded).toEqual([
      expect.objectContaining({ id: 'p1', permissions: ['notifications'] }),
    ])
    expect(workerInfo).toEqual({ source: 'blob:fixture', type: 'classic' })
    expect(vi.mocked(createWorkerSource)).toHaveBeenCalledWith('p1', ['notifications'])
    expect(store.commands).toEqual(host.getCommands())
    expect(store.failedPlugins).toEqual(host.getFailedPlugins())
    await store.runCommand('p1', 'hello')
    expect(host.runCommand).toHaveBeenCalledWith('p1', 'hello')
    expect(store.commandStates['p1:hello']).toBe('ok')
  })

  it('bridges sandbox capabilities to the active workspace through Wails services', async () => {
    const workspaceStore = useWorkspaceStore()
    workspaceStore.setCurrentWorkspace({
      createdAt: '', id: 'ws', lastOpenedAt: '', name: 'Vault', path: '/tmp/vault',
    })
    listPlugins.mockResolvedValue([])
    readFile.mockResolvedValue('content')
    saveFile.mockResolvedValue(undefined as never)

    const store = usePluginRuntimeStore()
    await store.initialize()
    const runtimeOptions = (hostInstances[0] as unknown as {
      options: {
        capabilities: {
          readFile: (path: string) => Promise<string>
          writeFile: (path: string, content: string) => Promise<void>
        }
        notifications?: (message: string) => void
      }
    }).options

    await expect(runtimeOptions.capabilities.readFile('notes/a.md')).resolves.toBe('content')
    await expect(runtimeOptions.capabilities.writeFile('notes/a.md', 'next')).resolves.toBeUndefined()
    expect(readFile).toHaveBeenCalledWith('/tmp/vault', 'notes/a.md')
    expect(saveFile).toHaveBeenCalledWith('/tmp/vault', 'notes/a.md', 'next')
    runtimeOptions.notifications?.('plugin ready')
    expect(store.notifications.map(item => item.message)).toContain('plugin ready')
  })

  it('exposes plugin toolbar buttons and applies the declared transform on click', async () => {
    const workspaceStore = useWorkspaceStore()
    workspaceStore.setCurrentWorkspace({
      createdAt: '', id: 'ws', lastOpenedAt: '', name: 'Vault', path: '/tmp/vault',
    })
    listPlugins.mockResolvedValue([])

    const store = usePluginRuntimeStore()
    await store.initialize()
    const host = hostInstances[0]

    expect(store.toolbarButtons).toEqual(host.getToolbarButtons())
    expect(store.toolbarButtons).toHaveLength(1)

    const button = store.toolbarButtons[0]
    store.runToolbarButton(button.pluginId, button.id)
    expect(applyTransformMock).toHaveBeenCalledWith(button.transform)
  })

  it('subscribes to backend file-change events and forwards valid ones to plugins (E-6)', async () => {
    const workspaceStore = useWorkspaceStore()
    workspaceStore.setCurrentWorkspace({
      createdAt: '', id: 'ws', lastOpenedAt: '', name: 'Vault', path: '/tmp/vault',
    })
    listPlugins.mockResolvedValue([])

    const store = usePluginRuntimeStore()
    await store.initialize()

    // 初始化时订阅了 workspace:file-changed，且只订阅一次
    const onMock = vi.mocked(Events.On)
    expect(onMock).toHaveBeenCalledTimes(1)
    expect(onMock).toHaveBeenCalledWith('workspace:file-changed', expect.any(Function))

    const host = hostInstances[0] as unknown as {
      emitWorkspaceEvent: (event: { path: string, type: string }) => void
    }
    const handler = onMock.mock.calls[0]![1] as (ev: { data: unknown } | undefined) => void

    // 合法事件：直接转发给插件（path + type 同构透传）
    handler({ data: { type: 'modify', path: 'notes/a.md' } })
    expect(host.emitWorkspaceEvent).toHaveBeenCalledWith({ type: 'modify', path: 'notes/a.md' })

    // 脏数据：缺 type / 非法 type / 缺 path 都不能转发
    handler({ data: { path: 'x.md' } })
    handler({ data: { type: 'chmod', path: 'x.md' } })
    handler({ data: { type: 'create', path: '' } })
    handler({ data: null })
    handler(undefined)
    expect(host.emitWorkspaceEvent).toHaveBeenCalledTimes(1)
  })
})
