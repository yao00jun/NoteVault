import { defineStore } from 'pinia'
import { Events } from '@wailsio/runtime'
import { ref, onScopeDispose } from 'vue'
import {
  FileService,
  GraphService,
  PluginService,
  SearchService,
  TagService,
  TodoService,
} from '@bindings/github.com/notevault/notevault/index.js'
import type { FileNode } from '@bindings/github.com/notevault/notevault/models.js'
import { PluginRuntimeHost } from '@/plugins/runtime'
import { createMainThreadTransport } from '@/plugins/mainThreadTransport'
import { createWorkerSource } from '@/plugins/workerSource'
import {
  applyLinePrefix,
  applyTransform,
  getEditorSelection,
  replaceEditorSelection,
  runBuiltinEditorCommand,
  setEditorUiHandlers,
  type EditorUiHandlers,
} from '@/plugins/editorBridge'
import type {
  CompiledDecoration,
  CompiledWidget,
  PluginKeymap,
  PluginPermission,
  PluginStartInfo,
  PluginTransport,
  RegisteredPluginCommand,
  RegisteredPluginSettings,
  RegisteredPluginToolbarButton,
  WorkspaceEvent,
} from '@/plugins/types'
import { useWorkspaceStore } from './workspace'

export interface PluginNotification {
  id: number
  kind: 'error' | 'info'
  message: string
  pluginId?: string
}

const knownPermissions = new Set<PluginPermission>([
  'workspace.read',
  'workspace.write',
  'commands',
  'notifications',
  'ui',
])

export const usePluginRuntimeStore = defineStore('plugin-runtime', () => {
  // 执行通道按信任等级分流：
  //   - 未授权        → Worker 沙箱，网络/本地存储/嵌套 Worker 全部禁用；
  //   - 已授权 full   → 主进程上下文，可访问界面与完整能力，外联由 CSP 的
  //                     connect-src / img-src 兜底（见 internal/security/csp.go）。
  // 授权状态是逐插件、且与源码哈希绑定的，插件一更新授权就失效。
  const host = new PluginRuntimeHost(
    info => (info.trusted ? createMainThreadTransport(info) : createWorkerTransport(info)),
    {
      // 这些能力后端早已具备，这里只是把它们桥接给插件（#28）。
      // 全部走同一套 capability 协议，插件侧不需要知道自己跑在 Worker 还是主进程。
      capabilities: {
        async readFile(relativePath) {
          return await FileService.ReadFile(requireWorkspacePath(), relativePath)
        },
        async writeFile(relativePath, content) {
          const result = await FileService.SaveFile(requireWorkspacePath(), relativePath, content)
          return result
        },
        async listFiles() {
          const tree = (await FileService.GetFileTree(requireWorkspacePath())) ?? []
          return flattenFileTree(tree)
        },
        async createFile(relativePath, content) {
          const result = await FileService.CreateFile(requireWorkspacePath(), relativePath, content)
          return result
        },
        async deleteFile(relativePath) {
          await FileService.DeleteFile(requireWorkspacePath(), relativePath)
        },
        async renameFile(relativePath, newName) {
          const result = await FileService.RenameFile(requireWorkspacePath(), relativePath, newName)
          return result
        },
        async getAllTags() {
          return await TagService.GetAllTags(requireWorkspacePath())
        },
        async getFilesByTag(tag) {
          return await TagService.GetFilesByTag(requireWorkspacePath(), tag)
        },
        async getAllTodos() {
          return await TodoService.GetAllTodos(requireWorkspacePath())
        },
        async getBacklinks(relativePath) {
          const graph = await GraphService.GetGraph(requireWorkspacePath())
          // 只返回指向该文档的来源路径，插件拿到的是最常用的一跳反向链接
          return (graph?.edges ?? []).filter(edge => edge?.target === relativePath)
        },
        async search(query) {
          return await SearchService.Search(requireWorkspacePath(), query)
        },
        async getFrontmatter(relativePath) {
          const source = await FileService.ReadFile(requireWorkspacePath(), relativePath)
          return parseFrontmatter(source)
        },
        // 插件私有数据（#29）：存在应用数据目录，不属于工作区笔记，
        // 且由后端按插件 ID 隔离（含 ID 合法性校验，防路径穿越）。
        async loadPluginData(pluginId) {
          return await PluginService.LoadPluginData(pluginId)
        },
        async savePluginData(pluginId, data) {
          await PluginService.SavePluginData(pluginId, data)
        },
        // 编辑器（P14）：插件拿不到 DOM，读写选区由宿主在真实编辑器上执行
        getSelection() {
          return getEditorSelection()
        },
        replaceSelection(text) {
          replaceEditorSelection(text)
        },
      },
      notifications(message) {
        pushNotification('info', message)
      },
      onEvent({ level, message, pluginId }) {
        pushNotification(level, message, pluginId)
      },
    },
  )

  const activeIds = ref<string[]>([])
  // 记录每个已激活插件当前实际使用的执行通道（true=主进程 full-trust）。
  // 用户授权或撤销后，插件必须重启才能切换通道，靠这个来检测变化。
  const activeTrust = ref<Record<string, boolean>>({})
  // 加载插件时用的源码哈希（P15 热重载）：下次比对发现变了就重启该插件
  const activeHash = ref<Record<string, string>>({})
  // 插件设置 schema（#29），按 pluginId 索引
  const pluginSettings = ref<Record<string, RegisteredPluginSettings>>({})
  // 声明式编辑器扩展（P14）：插件声明、宿主校验后的结果，由编辑器组件消费。
  // 编辑器需要在运行时增删这些扩展，所以走 Compartment 热替换。
  const editorDecorations = ref<CompiledDecoration[]>([])
  const editorWidgets = ref<CompiledWidget[]>([])
  const editorKeymaps = ref<PluginKeymap[]>([])
  const commands = ref<RegisteredPluginCommand[]>([])
  const toolbarButtons = ref<RegisteredPluginToolbarButton[]>([])
  const commandStates = ref<Record<string, 'failed' | 'loading' | 'ok'>>({})
  const failedPlugins = ref<Array<{ id: string, name: string }>>([])
  const initialized = ref(false)
  const loading = ref(false)
  const notifications = ref<PluginNotification[]>([])
  const runtimeError = ref('')

  function requireWorkspacePath(): string {
    const workspaceStore = useWorkspaceStore()
    if (!workspaceStore.currentWorkspace?.path) {
      throw new Error('没有可用的工作区')
    }
    return workspaceStore.currentWorkspace.path
  }

  let notificationSequence = 0
  function pushNotification(
    kind: PluginNotification['kind'],
    message: string,
    pluginId?: string,
  ): void {
    notificationSequence += 1
    notifications.value.push({
      id: notificationSequence,
      kind,
      message,
      pluginId,
    })
    while (notifications.value.length > 5) notifications.value.shift()
  }

  function dismissNotification(id: number): void {
    notifications.value = notifications.value.filter(item => item.id !== id)
  }

  function parsePermissions(values?: string[] | null): PluginPermission[] | null {
    if (!values || values.length === 0) return []
    for (const value of values) {
      if (!knownPermissions.has(value as PluginPermission)) return null
    }
    return values as PluginPermission[]
  }

  function syncRuntimeState(): void {
    commands.value = host.getCommands()
    toolbarButtons.value = host.getToolbarButtons()
    failedPlugins.value = host.getFailedPlugins()

    // 插件设置（#29）：schema 由插件声明，宿主只负责渲染与回传
    const settings: Record<string, RegisteredPluginSettings> = {}
    for (const id of activeIds.value) {
      const schema = host.getPluginSettings(id)
      if (schema) settings[id] = schema
    }
    pluginSettings.value = settings

    // 编辑器扩展是全局的（不按插件分组），每次同步整体替换
    editorDecorations.value = host.getDecorations()
    editorWidgets.value = host.getWidgets()
    editorKeymaps.value = host.getKeymaps()
  }

  function updateSetting(pluginId: string, key: string, value: unknown): void {
    host.updatePluginSetting(pluginId, key, value)
    const schema = host.getPluginSettings(pluginId)
    if (schema) pluginSettings.value = { ...pluginSettings.value, [pluginId]: { ...schema } }
  }

  // ---------------------------------------------------------------------------
  // 工作区文件变更（E-6）：由后端 fsnotify 实时推送（workspace:file-changed），
  // 替换旧方案"前端轮询文件树 + 快照比对"——不再有 3 秒延迟，也不再每次
  // 遍历整棵树重算修改时间。事件 payload 是 { type, path }：
  // type ∈ create / modify / delete，path 为相对工作区根目录的 "/" 分隔路径，
  // 与 WorkspaceEvent 完全同构，直接转发给声明了 workspace.read 的插件。
  // ---------------------------------------------------------------------------
  let stopFileEventSubscription: (() => void) | null = null

  function startFileEventSubscription(): void {
    if (stopFileEventSubscription) return
    try {
      stopFileEventSubscription = Events.On('workspace:file-changed', (ev) => {
        // 后端单参数 Emit → data 就是事件对象本身；这里仍做结构校验，
        // 防御未来 payload 变更或传输层异常把脏数据喂给插件。
        const data = ev?.data as { path?: unknown, type?: unknown } | null | undefined
        if (!data) return
        const { path, type } = data
        if (typeof path !== 'string' || path === '') return
        if (type !== 'create' && type !== 'modify' && type !== 'delete') return
        host.emitWorkspaceEvent({ path, type })
      })
    } catch (error) {
      // 没有事件总线的环境（单测 / 非 Wails 预览）静默降级：插件只是收不到推送
      console.warn('[pluginRuntime] 文件变更事件订阅失败:', error)
    }
  }

  // 热重载（P15）：定期检查插件源码是否变化，变了就重启该插件。
  // 改插件文件时不必再手点刷新——开发者体验里很关键的一环。
  const HOT_RELOAD_INTERVAL_MS = 3000
  let hotReloadTimer: ReturnType<typeof setInterval> | null = null

  function startHotReload(): void {
    if (hotReloadTimer) return
    hotReloadTimer = setInterval(() => {
      void refreshPlugins(true) // quiet：不翻转 loading，避免界面每几秒闪一下
    }, HOT_RELOAD_INTERVAL_MS)
  }

  function stopHotReload(): void {
    if (!hotReloadTimer) return
    clearInterval(hotReloadTimer)
    hotReloadTimer = null
  }

  // 订阅与定时器必须在 store 作用域销毁时清掉，否则会一直空转
  onScopeDispose(() => {
    stopFileEventSubscription?.()
    stopFileEventSubscription = null
    stopHotReload()
  })

  // quiet=true 用于后台热重载：不翻转 loading，避免每几秒闪一次界面
  async function refreshPlugins(quiet = false): Promise<void> {
    if (!quiet) loading.value = true
    runtimeError.value = ''
    try {
      const plugins = (await PluginService.ListPlugins()) ?? []
      const enabledIds = new Set<string>()
      const startInfos: PluginStartInfo[] = []

      for (const plugin of plugins) {
        if (!plugin.enabled || plugin.hasError) continue
        const permissions = parsePermissions(plugin.manifest.permissions)
        if (!permissions) continue

        const id = plugin.manifest.id
        enabledIds.add(id)

        const trusted = plugin.trustGranted === true
        const active = activeIds.value.includes(id)
        // 三种情况需要重启：
        //   1. 授权状态变了 → 执行通道要在 Worker 沙箱与主进程之间切换，
        //      沿用旧实例的话，撤销授权后插件仍跑在主进程，降级不生效；
        //   2. 源码哈希变了 → 热重载（P15），改完插件文件不必手点刷新；
        //   3. 两者都没变且已激活 → 什么都不做。
        const unchanged = active
          && activeTrust.value[id] === trusted
          && activeHash.value[id] === plugin.hash
        if (unchanged) continue

        if (active) {
          await host.remove(id) // 给插件执行 onunload 的机会
          activeIds.value = activeIds.value.filter(x => x !== id)
        }
        startInfos.push({
          id,
          name: plugin.manifest.name,
          permissions,
          source: plugin.source,
          trusted,
        })
        // 记下加载时用的源码哈希，作为下次判断「插件是否被改过」的基线（热重载）
        activeHash.value[id] = plugin.hash
      }

      for (const previousId of [...activeIds.value]) {
        if (!enabledIds.has(previousId)) {
          await host.remove(previousId)
          activeIds.value = activeIds.value.filter(id => id !== previousId)
          delete activeTrust.value[previousId]
        }
      }

      for (const info of startInfos) {
        try {
          await host.load(info)
          if (!activeIds.value.includes(info.id)) activeIds.value.push(info.id)
          activeTrust.value[info.id] = info.trusted === true
        } catch (error) {
          runtimeError.value = error instanceof Error ? error.message : String(error)
        }
      }
      syncRuntimeState()
    } catch (error) {
      runtimeError.value = error instanceof Error ? error.message : String(error)
    } finally {
      // 文件变更由后端 fsnotify 推送（E-6），这里只负责订阅一次
      startFileEventSubscription()
      startHotReload()
      if (!quiet) loading.value = false
      initialized.value = true
    }
  }

  async function initialize(): Promise<void> {
    if (initialized.value) return
    await refreshPlugins()
  }

  async function runCommand(pluginId: string, commandId: string): Promise<void> {
    const key = `${pluginId}:${commandId}`
    commandStates.value[key] = 'loading'
    try {
      await host.runCommand(pluginId, commandId)
      commandStates.value[key] = 'ok'
    } catch (error) {
      commandStates.value[key] = 'failed'
      pushNotification(
        'error',
        error instanceof Error ? error.message : String(error),
        pluginId,
      )
      throw error
    }
  }

  function runToolbarButton(pluginId: string, buttonId: string): void {
    const button = host.getToolbarButtons().find(
      b => b.pluginId === pluginId && b.id === buttonId,
    )
    if (!button) return

    // 三种点击行为，按优先级：
    //   1. command        → 宿主内置命令（撤销、缩进、取色器这类，插件碰不到）
    //   2. handledByPlugin → 通知插件自己处理（读选区 → 计算 → 写回）
    //   3. transform      → 宿主在编辑器上执行文本变换
    if (button.command) {
      runEditorCommand(button.command)
      return
    }
    if (button.handledByPlugin) {
      host.triggerToolbarButton(pluginId, buttonId)
      return
    }
    // 行级（标题/引用/列表）与选区级（加粗/斜体）走不同的实现，
    // transform 协议用 linePrefix 是否存在来区分
    const transform = button.transform ?? {}
    if (transform.linePrefix) {
      applyLinePrefix(transform.linePrefix)
      return
    }
    applyTransform(transform)
  }

  /**
   * 执行宿主内置的编辑器命令（P14）。
   * 这些命令需要碰编辑器的历史栈或打开宿主 UI，插件在沙箱里做不到，
   * 所以由宿主提供固定集合，插件只能通过 command 名字引用。
   */
  function runEditorCommand(command: string): void {
    runBuiltinEditorCommand(command)
  }

  /** 供编辑器组件注册取色器 / 格式刷这类依赖组件状态的 UI 操作 */
  function registerEditorUiHandlers(handlers: EditorUiHandlers): void {
    setEditorUiHandlers(handlers)
  }

  return {
    activeIds,
    clearNotification: dismissNotification,
    commandStates,
    commands,
    dismissNotification,
    editorDecorations,
    editorKeymaps,
    editorWidgets,
    failedPlugins,
    initialized,
    initialize,
    loading,
    notifications,
    pluginSettings,
    refreshPlugins,
    registerEditorUiHandlers,
    runCommand,
    runToolbarButton,
    runtimeError,
    syncRuntimeState,
    toolbarButtons,
    updateSetting,
  }
})

// ---------------------------------------------------------------------------
// 工作区变更事件（#27 → E-6 改造）
//
// 事件源已从「前端比对文件树快照」迁到「后端 fsnotify 实时推送」，
// 订阅逻辑见本文件 startFileEventSubscription。这里只剩文件树的
// 展平工具（插件 listFiles 能力仍在用）。
// ---------------------------------------------------------------------------

// flattenFileTree 把文件树展平成路径列表。
// 插件拿扁平列表比递归树好处理——遍历、过滤、聚合都是最常见的用法。
// 绑定类型里数组元素可空（Go 切片的 nil 元素），这里统一跳过
function flattenFileTree(nodes: Array<FileNode | null>): string[] {
  const out: string[] = []
  const walk = (list: Array<FileNode | null>): void => {
    for (const node of list) {
      if (!node) continue
      if (node.isDir) walk(node.children ?? [])
      else out.push(node.path)
    }
  }
  walk(nodes)
  return out
}

// parseFrontmatter 解析 Markdown 头部的 YAML front matter。
// 刻意只支持 `key: value` 这种平面结构，不做完整 YAML 解析
// （嵌套、数组、多行值都超出插件元数据的常见需求，也让实现更容易保证安全）。
function parseFrontmatter(source: string): Record<string, string> {
  const match = /^---\r?\n([\s\S]*?)\r?\n---/.exec(source)
  if (!match) return {}
  const out: Record<string, string> = {}
  for (const line of match[1].split(/\r?\n/)) {
    const index = line.indexOf(':')
    if (index <= 0) continue
    const key = line.slice(0, index).trim()
    if (!key) continue
    out[key] = line.slice(index + 1).trim().replace(/^["'`]|["'`]$/g, '')
  }
  return out
}

function createWorkerTransport(info: PluginStartInfo): PluginTransport {
  const source = createWorkerSource(info.id, info.permissions)
  const blobUrl = URL.createObjectURL(new Blob([source], { type: 'text/javascript' }))
  const worker = new Worker(blobUrl, {
    name: `notevault-plugin-${info.id}`,
    type: 'classic',
  })
  URL.revokeObjectURL(blobUrl)

  const listeners = new Map<
    string,
    Map<(event: { data?: unknown }) => void, (event: Event) => void>
  >()

  return {
    addEventListener(type, listener) {
      const wrapped = event => listener(event as { data?: unknown })
      const byListener = listeners.get(type) ?? new Map()
      byListener.set(listener, wrapped)
      listeners.set(type, byListener)
      worker.addEventListener(type, wrapped)
    },
    postMessage(data) {
      worker.postMessage(data)
    },
    removeEventListener(type, listener) {
      const byListener = listeners.get(type)
      if (!byListener) return
      const wrapped = byListener.get(listener)
      if (!wrapped) return
      byListener.delete(listener)
      worker.removeEventListener(type, wrapped)
    },
    terminate() {
      worker.terminate()
    },
  }
}
