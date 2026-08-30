/**
 * 插件可申请的权限。
 *
 * 与后端白名单保持一致（internal/plugin/pluginservice.go 的 allowedPluginPermissions），
 * 新增权限必须两边同时加，否则插件加载会直接报「未知权限」。
 */
export type PluginPermission =
  | 'workspace.read'
  | 'workspace.write'
  | 'commands'
  | 'notifications'
  | 'ui'
  // 声明式编辑器增强（P14）。刻意与 ui 分开：
  // 「往编辑器里加高亮」和「注册工具栏按钮 / 改用户选区」能力等级不同，
  // 合并会让授权提示变得含糊，用户反而不敢点允许。
  | 'editor.decorate'

export interface PluginManifestPermissions {
  id: string
  name?: string
  version?: string
  permissions?: string[] | null
}

export interface PluginStartInfo {
  id: string
  name: string
  source: string
  permissions: PluginPermission[]
  /**
   * trusted 该插件是否已获得 full-trust 授权。
   * true → 在主进程上下文运行（可访问界面与完整能力，受 CSP 约束外联）；
   * false → 在 Worker 沙箱内运行（网络/存储/嵌套 Worker 全部禁用）。
   *
   * 设为可选且默认 false，与后端「未声明即最低权限」保持一致：
   * 漏传不会导致权限提升，最坏情况是插件被限制在沙箱里。
   */
  trusted?: boolean
}

/**
 * 工作区变更事件（#27）。
 *
 * 由宿主比对文件树快照产生（见 stores/pluginRuntime.ts）。
 * 刻意只给 type 和 path——不夹带文件内容，避免事件通道变成绕过权限的读取口子。
 */
export interface WorkspaceEvent {
  type: 'create' | 'modify' | 'delete'
  path: string
}

// ---------------------------------------------------------------------------
// 插件设置（#29）
//
// 插件跑在 Worker 里拿不到 DOM，无法自己画设置界面。
// 所以走「声明式 schema + 宿主渲染」：插件只描述有哪些设置项，
// 宿主据此渲染，用户改动时再把整份值回传给插件，由插件自己存（saveData）。
// ---------------------------------------------------------------------------

export type PluginSettingType = 'text' | 'toggle' | 'number'

export interface PluginSettingItem {
  key: string
  label: string
  type: PluginSettingType
  default?: unknown
  description?: string
}

export interface PluginSettingsSchema {
  title?: string
  items: PluginSettingItem[]
  /** 当前值。插件从 loadData 读出来后一并交给宿主，否则宿主显示不出已保存的配置 */
  values: Record<string, unknown>
}

export interface RegisteredPluginSettings extends PluginSettingsSchema {
  pluginId: string
}

// ---------------------------------------------------------------------------
// 声明式编辑器扩展（P14）
//
// 插件跑在 Worker 里拿不到 DOM，无法直接注册 CodeMirror 6 的 extension
// （ViewPlugin / DecorationSet 这些对象也跨不了 postMessage）。
// 所以走「插件声明 + 宿主构建」：插件只描述「想高亮什么、想插什么小组件、绑什么快捷键」，
// 宿主据此构造真正的 CM6 对象。
//
// 这条路线的工业先例是 VS Code：扩展进程完全隔离，编辑器装饰全部走声明式 RPC，
// 生态照样繁荣。它换来的是——装饰一个关键词不需要用户授予完全信任。
// ---------------------------------------------------------------------------

/** 单条装饰声明 */
export interface PluginDecoration {
  id: string
  /** 要匹配的文本；regex 为真时按正则解析 */
  pattern: string
  /** 是否把 pattern 当正则使用（默认否，即字面量匹配） */
  regex?: boolean
  /** 正则标志，如 'gi'。注意：要匹配多处必须带 g */
  flags?: string
  /** 加到匹配片段上的 class（只允许字母数字、下划线、连字符、空格） */
  class?: string
  /** 行内样式；键名受宿主白名单限制，危险属性会被丢弃 */
  style?: Record<string, string>
  /** match（默认）只覆盖匹配片段；line 覆盖整行 */
  scope?: 'match' | 'line'
  pluginId: string
}

/** 行内小组件声明。宿主只渲染纯文本，绝不解析 HTML */
export interface PluginWidget {
  id: string
  pattern: string
  regex?: boolean
  flags?: string
  /** 显示的文本；支持 $1、$2 引用正则捕获组 */
  text: string
  class?: string
  pluginId: string
}

/** 快捷键绑定：按键 → 触发该插件已注册的命令 */
export interface PluginKeymap {
  id: string
  /** CM6 风格的按键，如 'Mod-Shift-h'。Mod 在 Windows 上是 Ctrl、macOS 上是 Cmd */
  key: string
  /** 该插件已注册的命令 id */
  command: string
  pluginId: string
}

/**
 * 宿主校验并编译后的装饰。
 *
 * 与声明式的 PluginDecoration 分开，是为了让「不可信输入」和「已校验输入」
 * 在类型上就区分开：拿不到 Compiled 形态就说明还没过白名单。
 */
export interface CompiledDecoration {
  id: string
  pluginId: string
  /** 字面量字符串，或已通过合法性校验的正则 */
  matcher: RegExp | string
  class?: string
  style?: Record<string, string>
  scope: 'match' | 'line'
}

/** 宿主校验并编译后的行内小组件 */
export interface CompiledWidget {
  id: string
  pluginId: string
  matcher: RegExp | string
  /** 已展开 $1/$2 之前仍是模板，渲染时才按捕获组替换 */
  text: string
  class?: string
}

export interface RegisteredPluginCommand {
  pluginId: string
  id: string
  label: string
  description?: string
}

/** 文本变换描述：插件声明的工具栏按钮如何改选中文本（宿主在真实编辑器上执行） */
export interface PluginTextTransform {
  /** 在选区两端包裹（加粗、斜体这类） */
  prefix?: string
  suffix?: string
  /** 直接插入（水平线、表格模板这类） */
  insert?: string
  /** 无选区时插入并选中的占位文本 */
  placeholder?: string
  /**
   * 行级操作：在选中行的行首插入前缀（标题、引用、列表这类）。
   *
   * 与 prefix 的区别在于作用范围——prefix 只包住选区文本，
   * linePrefix 作用于整行，这正是 H1 / 引用 / 列表需要的语义。
   *
   * 宿主会先剥掉该行已有的行级前缀再插入；
   * 若剥掉后剩下的前缀与要插入的相同，则表示「取消」（相当于开关）。
   */
  linePrefix?: string
}

/** 插件注册的工具栏按钮定义 */
export interface PluginToolbarButton {
  id: string
  title: string
  icon?: string
  tooltip?: string
  /**
   * 文本变换：点击时由宿主在真实编辑器上执行。
   * 适合加粗、标题、引用这类「包裹 / 插入」操作。
   */
  transform?: PluginTextTransform
  /**
   * 宿主内置命令名：点击时执行该命令，而不是做文本变换。
   * 适合撤销、缩进、打开取色器这类需要宿主能力的操作——
   * 插件碰不到编辑器的历史栈，也开不了取色器。
   */
  command?: string
  /**
   * 由插件自己异步处理点击（配合 notevault.onToolbarClick）。
   * 适合「读选区 → 计算 → 写回」这类操作，例如行排序、全角半角转换。
   */
  handledByPlugin?: boolean
}

/** 宿主内置的编辑器命令（供按钮的 command 字段引用） */
export type EditorCommandId =
  | 'editor:undo'
  | 'editor:redo'
  | 'editor:indent'
  | 'editor:undent'
  | 'editor:pickColor'
  | 'editor:pickBackground'
  | 'editor:brush'
  | 'editor:alignLeft'
  | 'editor:alignCenter'
  | 'editor:alignRight'
  | 'editor:alignJustify'

/** 运行时已注册的插件工具栏按钮（带 pluginId 命名空间） */
export interface RegisteredPluginToolbarButton extends PluginToolbarButton {
  pluginId: string
}

export interface PluginCapabilityResponse {
  requestId: string
  ok: boolean
  value?: unknown
  error?: string
}

export interface PluginWorkerMessage {
  type: string
  id?: string
  pluginId?: string
  message?: string
  command?: Omit<RegisteredPluginCommand, 'pluginId'>
  status?: 'completed' | 'failed'
  error?: string
  // capability 方法名。取值见 runtime.ts 的 CAPABILITIES 表——
  // 这里用 string 而不是联合类型，是为了让「宿主新增能力」不必回头改协议类型。
  method?: string
  args?: Record<string, unknown>
  button?: PluginToolbarButton
  // 设置（#29）：插件声明的 schema，以及宿主回传的整份设置值
  settings?: PluginSettingsSchema
  values?: Record<string, unknown>
  // 声明式编辑器扩展（P14）。
  // 刻意不写成具体的 PluginDecoration 等类型——这里拿到的是插件传来的原始数据，
  // 每一个字段都必须当作不可信输入处理，具体结构由宿主校验后再落成 Compiled 形态。
  decoration?: Record<string, unknown>
  widget?: Record<string, unknown>
  keymap?: Record<string, unknown>
  // 工具栏按钮被点击（仅 handledByPlugin 的按钮会收到）
  buttonId?: string
}

export interface PluginTransport {
  postMessage(data: unknown): void
  addEventListener(
    type: string,
    listener: (event: { data?: unknown }) => void,
  ): void
  removeEventListener(
    type: string,
    listener: (event: { data?: unknown }) => void,
  ): void
  terminate(): void
}
