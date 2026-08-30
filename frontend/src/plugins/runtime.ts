import type {
  CompiledDecoration,
  CompiledWidget,
  PluginKeymap,
  PluginPermission,
  PluginStartInfo,
  PluginTransport,
  PluginWorkerMessage,
  RegisteredPluginCommand,
  RegisteredPluginSettings,
  RegisteredPluginToolbarButton,
  WorkspaceEvent,
} from './types'

/**
 * 宿主提供给插件的能力实现。
 *
 * 每一项都是可选的：宿主没提供的能力，插件调用时只会得到
 * "capability unavailable"，而不是让整个插件加载失败——
 * 这样能力可以按需逐步补齐，老插件也不会因为宿主升级而挂掉。
 */
export interface PluginCapabilities {
  // 基础读写
  readFile?: (relativePath: string) => Promise<string>
  writeFile?: (relativePath: string, content: string) => Promise<void>
  // 文件与目录
  listFiles?: () => Promise<string[]>
  createFile?: (relativePath: string, content: string) => Promise<unknown>
  deleteFile?: (relativePath: string) => Promise<void>
  renameFile?: (relativePath: string, newName: string) => Promise<unknown>
  // 元数据与检索
  getAllTags?: () => Promise<unknown>
  getFilesByTag?: (tag: string) => Promise<unknown>
  getAllTodos?: () => Promise<unknown>
  getBacklinks?: (relativePath: string) => Promise<unknown>
  search?: (query: string) => Promise<unknown>
  getFrontmatter?: (relativePath: string) => Promise<unknown>
  // 插件私有数据（#29）：按插件 ID 隔离，读写的是应用数据目录而不是工作区
  loadPluginData?: (pluginId: string) => Promise<string>
  savePluginData?: (pluginId: string, data: string) => Promise<void>
  // 编辑器（P14）：插件拿不到 DOM，读写选区都必须由宿主在真实编辑器上执行
  getSelection?: () => string
  replaceSelection?: (text: string) => void
}

export interface PluginRuntimeOptions {
  readinessTimeoutMs?: number
  executionTimeoutMs?: number
  // 卸载时给插件执行 onunload 的清理窗口（毫秒），超时即强制终止
  unloadTimeoutMs?: number
  notifications?: (message: string) => void
  onEvent?: (event: { level: 'error' | 'info', message: string, pluginId?: string }) => void
  capabilities?: PluginCapabilities
}

interface LoadedPlugin {
  id: string
  name: string
  permissions: PluginPermission[]
  transport: PluginTransport
  ready: boolean
}

interface PendingCall {
  resolve: () => void
  reject: (error: Error) => void
}

// capability 方法表：插件能调用什么、需要哪个权限、由宿主的哪个实现来提供。
//
// 表驱动而非 switch：新增一个能力只需要在表里加一项 + 在 PluginCapabilities 上加一个可选实现，
// 不用改分发逻辑。宿主没实现的项会在插件调用时返回 "capability unavailable"，
// 而不是让插件加载失败。
interface CapabilitySpec {
  // 需要的权限；缺省表示不需要任何权限。
  // 例如插件私有数据——数据按插件 ID 隔离，插件只能访问自己的那一份，
  // 再要求 workspace 权限反而是多余的。
  permission?: PluginPermission
  invoke: (
    caps: PluginCapabilities,
    args: Record<string, unknown>,
    pluginId: string,
  ) => Promise<unknown>
}

const str = (value: unknown): string => (typeof value === 'string' ? value : '')

const CAPABILITIES: Record<string, CapabilitySpec> = {
  // —— 基础读写 ——
  'workspace.read': {
    permission: 'workspace.read',
    async invoke(caps, args) {
      if (!caps.readFile) throw new Error('capability unavailable')
      return await caps.readFile(str(args.path))
    },
  },
  'workspace.write': {
    permission: 'workspace.write',
    async invoke(caps, args) {
      if (!caps.writeFile) throw new Error('capability unavailable')
      await caps.writeFile(str(args.path), str(args.content))
      return undefined
    },
  },

  // —— 文件与目录 ——
  'workspace.list': {
    permission: 'workspace.read',
    async invoke(caps) {
      if (!caps.listFiles) throw new Error('capability unavailable')
      return await caps.listFiles()
    },
  },
  'workspace.create': {
    permission: 'workspace.write',
    async invoke(caps, args) {
      if (!caps.createFile) throw new Error('capability unavailable')
      return await caps.createFile(str(args.path), str(args.content))
    },
  },
  'workspace.delete': {
    permission: 'workspace.write',
    async invoke(caps, args) {
      if (!caps.deleteFile) throw new Error('capability unavailable')
      await caps.deleteFile(str(args.path))
      return undefined
    },
  },
  'workspace.rename': {
    permission: 'workspace.write',
    async invoke(caps, args) {
      if (!caps.renameFile) throw new Error('capability unavailable')
      return await caps.renameFile(str(args.path), str(args.newName))
    },
  },

  // —— 元数据与检索 ——
  'tags.all': {
    permission: 'workspace.read',
    async invoke(caps) {
      if (!caps.getAllTags) throw new Error('capability unavailable')
      return await caps.getAllTags()
    },
  },
  'tags.files': {
    permission: 'workspace.read',
    async invoke(caps, args) {
      if (!caps.getFilesByTag) throw new Error('capability unavailable')
      return await caps.getFilesByTag(str(args.tag))
    },
  },
  'todos.all': {
    permission: 'workspace.read',
    async invoke(caps) {
      if (!caps.getAllTodos) throw new Error('capability unavailable')
      return await caps.getAllTodos()
    },
  },
  'graph.backlinks': {
    permission: 'workspace.read',
    async invoke(caps, args) {
      if (!caps.getBacklinks) throw new Error('capability unavailable')
      return await caps.getBacklinks(str(args.path))
    },
  },
  'search.query': {
    permission: 'workspace.read',
    async invoke(caps, args) {
      if (!caps.search) throw new Error('capability unavailable')
      return await caps.search(str(args.query))
    },
  },
  'meta.frontmatter': {
    permission: 'workspace.read',
    async invoke(caps, args) {
      if (!caps.getFrontmatter) throw new Error('capability unavailable')
      return await caps.getFrontmatter(str(args.path))
    },
  },

  // —— 插件私有数据（#29）——
  // 不需要 workspace 权限：数据按插件 ID 隔离，插件只能读写自己的那一份。
  'plugin.data.load': {
    async invoke(caps, _args, pluginId) {
      if (!caps.loadPluginData) throw new Error('capability unavailable')
      return await caps.loadPluginData(pluginId)
    },
  },
  'plugin.data.save': {
    async invoke(caps, args, pluginId) {
      if (!caps.savePluginData) throw new Error('capability unavailable')
      await caps.savePluginData(pluginId, str(args.data))
      return undefined
    },
  },

  // —— 编辑器（P14）——
  // 需要 ui 权限：这是在改动用户正在编辑的内容。
  'editor.selection': {
    permission: 'ui',
    async invoke(caps) {
      if (!caps.getSelection) throw new Error('capability unavailable')
      return caps.getSelection()
    },
  },
  'editor.replace': {
    permission: 'ui',
    async invoke(caps, args) {
      if (!caps.replaceSelection) throw new Error('capability unavailable')
      caps.replaceSelection(str(args.text))
      return undefined
    },
  },
}

// ---------------------------------------------------------------------------
// 声明式编辑器扩展的白名单校验（P14）
//
// 插件传来的每一个字段都不可信：
//   - class 会进 DOM 的 className；
//   - style 会进内联样式，不加限制就能用 position:fixed 盖住整个界面做钓鱼；
//   - 正则由插件提供，灾难性回溯能把编辑器卡死。
// 所以这里逐项收紧，宁可丢弃也不放行。
// ---------------------------------------------------------------------------

/** 单个插件最多能注册的编辑器扩展总数，防止刷量拖垮编辑器 */
const MAX_EDITOR_EXTENSIONS_PER_PLUGIN = 50

const SAFE_CLASS_NAME = /^[A-Za-z0-9_-]+$/

/** 允许的样式属性。刻意不含 position / z-index / fixed 之类可用来遮挡界面的属性 */
const ALLOWED_STYLE_PROPS = new Set([
  'background',
  'background-color',
  'border',
  'border-left',
  'border-radius',
  'color',
  'font-size',
  'font-style',
  'font-weight',
  'opacity',
  'padding',
  'text-decoration',
  'text-shadow',
])

function sanitizeClass(raw: unknown): string | undefined {
  if (typeof raw !== 'string') return undefined
  const safe = raw.split(/\s+/).filter(part => part && SAFE_CLASS_NAME.test(part))
  return safe.length > 0 ? safe.join(' ') : undefined
}

function sanitizeStyle(raw: unknown): Record<string, string> | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const out: Record<string, string> = {}
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (typeof value !== 'string') continue
    if (!ALLOWED_STYLE_PROPS.has(key)) continue
    // 拒绝 url()：否则插件能用一张外链图片把请求发出去，绕过 CSP 的 connect-src。
    // 也拒绝 ; " '，否则能在值里截断并注入额外的 CSS 属性。
    if (/[<>;"']|url\(/i.test(value)) continue
    out[key] = value
  }
  return Object.keys(out).length > 0 ? out : undefined
}

/** 正则标志只放行这几个，且强制带 g（否则只会匹配第一处） */
function sanitizeFlags(raw: unknown): string {
  const cleaned = (typeof raw === 'string' ? raw : '').replace(/[^gimsuy]/g, '')
  return cleaned.includes('g') ? cleaned : `${cleaned}g`
}

/** 构建匹配器；正则非法时返回 null（直接丢弃该装饰，而不是让插件加载失败） */
function buildMatcher(
  pattern: string,
  useRegex: boolean,
  flags: unknown,
): RegExp | string | null {
  if (!useRegex) return pattern
  try {
    return new RegExp(pattern, sanitizeFlags(flags))
  } catch {
    return null
  }
}

export class PluginRuntimeHost {
  private readonly plugins = new Map<string, LoadedPlugin>()
  private readonly registeredCommands = new Map<string, RegisteredPluginCommand>()
  private readonly registeredToolbarButtons = new Map<string, RegisteredPluginToolbarButton>()
  private readonly registeredSettings = new Map<string, RegisteredPluginSettings>()
  // 声明式编辑器扩展（P14），key 统一为 `${pluginId}:${id}`
  private readonly decorations = new Map<string, CompiledDecoration>()
  private readonly widgets = new Map<string, CompiledWidget>()
  private readonly keymaps = new Map<string, PluginKeymap>()
  private readonly pendingCalls = new Map<string, PendingCall>()
  private readonly activeCommands = new Map<string, string>()
  private readonly messageHandlers = new Map<string, (event: { data?: unknown }) => void>()
  private readonly failedPlugins: Array<{ id: string, name: string }> = []
  private readonly events = new Map<string, Array<{ level: 'error' | 'info', message: string }>>()
  private currentPluginId = ''

  constructor(
    private readonly factory: (info: PluginStartInfo) => PluginTransport,
    private readonly options: PluginRuntimeOptions = {},
  ) {}

  async load(info: PluginStartInfo): Promise<void> {
    this.stop(info.id)

    const plugin: LoadedPlugin = {
      id: info.id,
      name: info.name,
      permissions: info.permissions,
      transport: this.factory(info),
      ready: false,
    }
    const handler = event => this.handleMessage(info.id, event)
    this.currentPluginId = info.id
    this.messageHandlers.set(info.id, handler)
    plugin.transport.addEventListener('message', handler)
    this.plugins.set(info.id, plugin)
    plugin.transport.postMessage({
      id: info.id,
      permissions: info.permissions,
      source: info.source,
      type: 'plugin:start',
    })

    try {
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => {
          reject(new Error(`${info.name} startup timeout`))
        }, this.options.readinessTimeoutMs ?? 5000)

        this.pendingCalls.set(`ready:${info.id}`, {
          resolve: () => {
            clearTimeout(timer)
            resolve()
          },
          reject: (error: Error) => {
            clearTimeout(timer)
            reject(error)
          },
        })
      })
      plugin.ready = true
    } catch (error) {
      this.markFailed(info, error instanceof Error ? error.message : String(error))
      throw error
    }
  }

  getCommands(): RegisteredPluginCommand[] {
    return [...this.registeredCommands.values()]
  }

  getToolbarButtons(): RegisteredPluginToolbarButton[] {
    return [...this.registeredToolbarButtons.values()]
  }

  getPluginSettings(pluginId: string): RegisteredPluginSettings | undefined {
    return this.registeredSettings.get(pluginId)
  }

  /**
   * 更新一个设置项，并把整份值回传给插件（#29）。
   *
   * 宿主只负责渲染与收集，持久化交给插件自己（saveData）——
   * 值放在插件侧，宿主就不需要额外维护一份存储格式。
   */
  updatePluginSetting(pluginId: string, key: string, value: unknown): void {
    const settings = this.registeredSettings.get(pluginId)
    const plugin = this.plugins.get(pluginId)
    if (!settings || !plugin) return
    settings.values = { ...settings.values, [key]: value }
    plugin.transport.postMessage({
      id: pluginId,
      pluginId,
      type: 'plugin:settings-change',
      values: settings.values,
    })
  }

  /**
   * 通知插件「它的某个工具栏按钮被点了」（P14）。
   *
   * 只发通知、不等结果——按钮点击是 UI 事件，
   * 不该被插件里的异步逻辑卡住。
   */
  triggerToolbarButton(pluginId: string, buttonId: string): void {
    const plugin = this.plugins.get(pluginId)
    if (!plugin) return
    plugin.transport.postMessage({
      buttonId,
      id: pluginId,
      pluginId,
      type: 'plugin:toolbar-click',
    })
  }

  getEvents(pluginId: string): Array<{ level: 'error' | 'info', message: string }> {
    return [...(this.events.get(pluginId) ?? [])]
  }

  getFailedPlugins(): Array<{ id: string, name: string }> {
    return [...this.failedPlugins]
  }

  hasCommand(pluginId: string, commandId: string): boolean {
    return this.registeredCommands.has(`${pluginId}:${commandId}`)
  }

  /**
   * 优雅卸载：先给插件执行 onunload 的机会，再强制终止（#26）。
   *
   * 之前是直接 terminate——插件没机会注销监听器或保存状态。
   * 这里保留一个有限的清理窗口，超时就强杀：
   * 卸载流程不能被单个坏插件（死循环、不响应）卡住。
   */
  async remove(id: string): Promise<void> {
    await this.requestUnload(id)
    this.stop(id)
  }

  /**
   * 把工作区变更广播给插件（#27）。
   *
   * 只发给声明了 workspace.read 的插件：事件里带文件路径，
   * 没有读权限的插件不该借此拿到整个库的结构。
   */
  emitWorkspaceEvent(event: WorkspaceEvent): void {
    for (const plugin of this.plugins.values()) {
      if (!this.hasPermission(plugin, 'workspace.read')) continue
      plugin.transport.postMessage({
        event,
        id: plugin.id,
        pluginId: plugin.id,
        type: 'plugin:event',
      })
    }
  }

  // —— 声明式编辑器扩展（P14）——
  // 返回副本：这些对象会被编辑器组件直接拿去用，不能让外部改动内部状态

  getDecorations(): CompiledDecoration[] {
    return [...this.decorations.values()]
  }

  getWidgets(): CompiledWidget[] {
    return [...this.widgets.values()]
  }

  getKeymaps(): PluginKeymap[] {
    return [...this.keymaps.values()]
  }

  private countEditorExtensions(pluginId: string): number {
    let count = 0
    for (const key of this.decorations.keys()) if (key.startsWith(`${pluginId}:`)) count += 1
    for (const key of this.widgets.keys()) if (key.startsWith(`${pluginId}:`)) count += 1
    for (const key of this.keymaps.keys()) if (key.startsWith(`${pluginId}:`)) count += 1
    return count
  }

  private async requestUnload(id: string): Promise<void> {
    const plugin = this.plugins.get(id)
    if (!plugin) return

    plugin.transport.postMessage({ id, pluginId: id, type: 'plugin:unload' })

    const timeout = this.options.unloadTimeoutMs ?? 1000
    await new Promise<void>(resolve => {
      const timer = setTimeout(resolve, timeout)
      // 无论插件是否响应都要继续：超时与正常回调走同一个出口
      this.pendingCalls.set(`unload:${id}`, {
        resolve: () => {
          clearTimeout(timer)
          resolve()
        },
        reject: () => {
          clearTimeout(timer)
          resolve()
        },
      })
    })
    this.pendingCalls.delete(`unload:${id}`)
  }

  removeAll(): void {
    for (const id of [...this.plugins.keys()]) this.stop(id)
  }

  runCommand(pluginId: string, commandId: string): Promise<void> {
    const plugin = this.plugins.get(pluginId)
    if (!plugin || !plugin.ready || !this.hasCommand(pluginId, commandId)) {
      return Promise.reject(new Error('Plugin command is unavailable'))
    }

    const key = `${pluginId}:${commandId}`
    return new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.rejectCall(key, new Error('Plugin command timed out'))
        this.activeCommands.delete(pluginId)
        reject(new Error('Plugin command timed out'))
      }, this.options.executionTimeoutMs ?? 5000)

      this.pendingCalls.set(key, {
        resolve: () => {
          clearTimeout(timer)
          resolve()
        },
        reject: (error: Error) => {
          clearTimeout(timer)
          reject(error)
        },
      })
      this.activeCommands.set(pluginId, key)
      plugin.transport.postMessage({
        commandId,
        id: pluginId,
        type: 'plugin:command-run',
      })
    })
  }

  private stop(id: string): void {
    const plugin = this.plugins.get(id)
    if (plugin) {
      const handler = this.messageHandlers.get(id)
      if (handler) plugin.transport.removeEventListener('message', handler)
      this.messageHandlers.delete(id)
      plugin.transport.terminate()
      this.plugins.delete(id)
      this.events.delete(id)
    }

    for (const [key, command] of [...this.registeredCommands.entries()]) {
      if (command.pluginId === id) this.registeredCommands.delete(key)
    }
    for (const [key, button] of [...this.registeredToolbarButtons.entries()]) {
      if (button.pluginId === id) this.registeredToolbarButtons.delete(key)
    }
    this.registeredSettings.delete(id)
    // 编辑器扩展也要清掉，否则插件禁用后高亮还留在编辑器上
    for (const key of [...this.decorations.keys()]) {
      if (key.startsWith(`${id}:`)) this.decorations.delete(key)
    }
    for (const key of [...this.widgets.keys()]) {
      if (key.startsWith(`${id}:`)) this.widgets.delete(key)
    }
    for (const key of [...this.keymaps.keys()]) {
      if (key.startsWith(`${id}:`)) this.keymaps.delete(key)
    }
    this.rejectCall(`ready:${id}`, new Error('Plugin stopped during startup'))
    const activeKey = this.activeCommands.get(id)
    if (activeKey) {
      this.rejectCall(activeKey, new Error('Plugin stopped while running a command'))
      this.activeCommands.delete(id)
    }
    if (this.currentPluginId === id) this.currentPluginId = ''
  }

  private rejectCall(key: string, error: Error): void {
    const pending = this.pendingCalls.get(key)
    if (!pending) return
    this.pendingCalls.delete(key)
    pending.reject(error)
  }

  private async handleMessage(pluginId: string, rawEvent: { data?: unknown }): Promise<void> {
    const data = rawEvent.data as PluginWorkerMessage | undefined
    if (!data || typeof data.type !== 'string') return

    const plugin = this.plugins.get(pluginId)
    if (!plugin) return

    switch (data.type) {
      case 'plugin:ready': {
        this.pendingCalls.get(`ready:${pluginId}`)?.resolve()
        this.pendingCalls.delete(`ready:${pluginId}`)
        break
      }
      case 'plugin:command-register': {
        const command = data.command
        if (!command?.id || !command.label || !this.hasPermission(plugin, 'commands')) return
        this.registeredCommands.set(`${pluginId}:${command.id}`, {
          description: command.description || '',
          id: command.id,
          label: command.label,
          pluginId,
        })
        break
      }
      case 'plugin:toolbar-register': {
        const button = data.button
        if (!button?.id || !button.title || !this.hasPermission(plugin, 'ui')) return
        this.registeredToolbarButtons.set(`${pluginId}:${button.id}`, {
          ...button,
          pluginId,
        })
        break
      }
      case 'plugin:notification': {
        const message = data.message ?? data.error
        if (typeof message === 'string' && this.hasPermission(plugin, 'notifications')) {
          this.options.notifications?.(message)
        }
        break
      }
      case 'plugin:settings-register': {
        const settings = data.settings
        if (!settings || !Array.isArray(settings.items)) return
        this.registeredSettings.set(pluginId, {
          items: settings.items,
          pluginId,
          title: settings.title || '',
          values: settings.values ?? {},
        })
        break
      }
      // —— 声明式编辑器扩展（P14）——
      // 权限在沙箱侧已经查过一次，这里必须再查：
      // 消息来自插件，宿主不能假设它遵守了任何约定。
      case 'plugin:decoration-register': {
        if (!this.hasPermission(plugin, 'editor.decorate')) return
        const raw = data.decoration
        if (!raw || typeof raw.pattern !== 'string' || !raw.pattern) return
        const matcher = buildMatcher(raw.pattern, raw.regex === true, raw.flags)
        if (!matcher) return
        if (this.countEditorExtensions(pluginId) >= MAX_EDITOR_EXTENSIONS_PER_PLUGIN) return
        this.decorations.set(`${pluginId}:${raw.id}`, {
          class: sanitizeClass(raw.class),
          id: String(raw.id ?? ''),
          matcher,
          pluginId,
          scope: raw.scope === 'line' ? 'line' : 'match',
          style: sanitizeStyle(raw.style),
        })
        break
      }
      case 'plugin:widget-register': {
        if (!this.hasPermission(plugin, 'editor.decorate')) return
        const raw = data.widget
        if (!raw || typeof raw.pattern !== 'string' || !raw.pattern) return
        const matcher = buildMatcher(raw.pattern, raw.regex === true, raw.flags)
        if (!matcher) return
        if (this.countEditorExtensions(pluginId) >= MAX_EDITOR_EXTENSIONS_PER_PLUGIN) return
        this.widgets.set(`${pluginId}:${raw.id}`, {
          class: sanitizeClass(raw.class),
          id: String(raw.id ?? ''),
          matcher,
          pluginId,
          text: String(raw.text ?? ''),
        })
        break
      }
      case 'plugin:keymap-register': {
        if (!this.hasPermission(plugin, 'editor.decorate')) return
        const raw = data.keymap
        if (!raw || typeof raw.key !== 'string' || !raw.key) return
        if (typeof raw.command !== 'string' || !raw.command) return
        if (this.countEditorExtensions(pluginId) >= MAX_EDITOR_EXTENSIONS_PER_PLUGIN) return
        this.keymaps.set(`${pluginId}:${raw.id}`, {
          command: raw.command,
          id: String(raw.id ?? ''),
          key: raw.key,
          pluginId,
        })
        break
      }
      case 'plugin:unloaded': {
        this.pendingCalls.get(`unload:${pluginId}`)?.resolve()
        this.pendingCalls.delete(`unload:${pluginId}`)
        break
      }
      case 'plugin:capability-request': {
        await this.handleCapabilityRequest(plugin, data)
        break
      }
      case 'plugin:command-result': {
        const commandKey = this.activeCommands.get(pluginId)
        if (!commandKey) return
        const pending = this.pendingCalls.get(commandKey)
        if (!pending) return

        this.pendingCalls.delete(commandKey)
        this.activeCommands.delete(pluginId)
        if (data.status === 'completed') pending.resolve()
        else pending.reject(new Error(data.error || 'Plugin command failed'))
        break
      }
      case 'plugin:error': {
        this.markFailed({ id: plugin.id, name: plugin.name }, data.error || `${plugin.name} crashed`)
        break
      }
    }
  }

  private async handleCapabilityRequest(
    plugin: LoadedPlugin,
    data: {
      method?: PluginWorkerMessage['method']
      args?: Record<string, unknown>
    },
  ): Promise<void> {
    const requestId = String(data.args?.requestId ?? '')
    const respond = (
      ok: boolean,
      value?: unknown,
      error?: string,
    ) => ({
      error,
      ok,
      pluginId: plugin.id,
      requestId,
      type: 'plugin:capability-response' as const,
      value,
    })

    const method = data.method
    const spec = method ? CAPABILITIES[method] : undefined
    if (!spec) {
      plugin.transport.postMessage(respond(false, undefined, 'unsupported capability'))
      return
    }

    if (spec.permission && !this.hasPermission(plugin, spec.permission)) {
      plugin.transport.postMessage(respond(false, undefined, 'permission denied'))
      const commandKey = this.activeCommands.get(plugin.id)
      if (commandKey) {
        this.rejectCall(commandKey, new Error('permission denied'))
        this.activeCommands.delete(plugin.id)
      }
      return
    }

    try {
      const value = await spec.invoke(this.options.capabilities ?? {}, data.args ?? {}, plugin.id)
      plugin.transport.postMessage(respond(true, value))
    } catch (error) {
      plugin.transport.postMessage(
        respond(false, undefined, error instanceof Error ? error.message : String(error)),
      )
    }
  }

  private hasPermission(plugin: LoadedPlugin, permission: PluginPermission): boolean {
    return plugin.permissions.includes(permission)
  }

  private recordEvent(
    info: Pick<PluginStartInfo, 'id'>,
    level: 'error' | 'info',
    message: string,
  ): void {
    const list = this.events.get(info.id) ?? []
    list.push({ level, message })
    this.events.set(info.id, list)
  }

  private markFailed(info: Pick<PluginStartInfo, 'id' | 'name'>, message: string): void {
    if (!this.failedPlugins.some(item => item.id === info.id && item.name === info.name)) {
      this.failedPlugins.push({ ...info })
    }
    this.recordEvent(info, 'error', message)
    this.stop(info.id)
    this.options.onEvent?.({ level: 'error', message, pluginId: info.id })
  }
}
