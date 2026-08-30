// NoteVault 插件 API 类型定义
//
// 用法：在插件 .js 文件顶部加一行引用，即可获得完整的类型提示与补全
//
//   /// <reference path="./notevault.d.ts" />
//
// 这是「开发者体验」里性价比最高的一环：插件作者不用读源码猜 API，
// 编辑器会直接告诉他有哪些能力、每个能力需要什么权限。

/** 插件可以在 manifest 里申请的权限 */
export type PluginPermission =
  | 'workspace.read'
  | 'workspace.write'
  | 'commands'
  | 'notifications'
  | 'ui'
  /** 声明式编辑器增强：高亮、行内小组件、快捷键 */
  | 'editor.decorate'

/** 插件请求的信任等级（manifest 的 trust 字段） */
export type PluginTrust = 'sandbox' | 'full'

/** 工作区变更事件 */
export interface WorkspaceEvent {
  /** create 新建 / modify 修改 / delete 删除 */
  type: 'create' | 'modify' | 'delete'
  /** 相对于工作区根目录的路径 */
  path: string
}

/** 命令定义 */
export interface PluginCommand {
  id: string
  label: string
  description?: string
  run: (args: Record<string, unknown>) => void | Promise<void>
}

/** 工具栏按钮的文本变换：宿主在真实编辑器上执行 */
export interface PluginTextTransform {
  prefix?: string
  suffix?: string
  insert?: string
  placeholder?: string
}

/** 工具栏按钮定义 */
export interface PluginToolbarButton {
  id: string
  title: string
  icon?: string
  tooltip?: string
  transform?: PluginTextTransform
}

/** 设置项类型 */
export type PluginSettingType = 'text' | 'toggle' | 'number'

/** 设置项定义 */
export interface PluginSettingItem {
  key: string
  label: string
  type: PluginSettingType
  default?: unknown
  description?: string
}

// ---------------------------------------------------------------------------
// 声明式编辑器扩展
//
// 插件跑在 Worker 沙箱里，拿不到 DOM，无法直接注册 CodeMirror 扩展。
// 所以只描述「想高亮什么」，由宿主构造真正的 CodeMirror 对象。
//
// 这样做的好处是：装饰一个关键词不需要用户授予「完全信任」。
// 代价是表达力受限——做不了完整的 ViewPlugin 或自定义语法模式。
// 真正需要完整 CodeMirror API 的场景，才需要声明 trust: full。
// ---------------------------------------------------------------------------

/** 单条装饰声明 */
export interface PluginDecoration {
  id: string
  /** 要匹配的文本；regex 为真时按正则解析 */
  pattern: string
  /** 是否把 pattern 当正则使用（默认否，即字面量匹配） */
  regex?: boolean
  /** 正则标志，如 'gi'。要匹配多处必须带 g */
  flags?: string
  /** 加到匹配片段上的 class。只允许字母数字、下划线、连字符，非法片段会被剔除 */
  class?: string
  /**
   * 行内样式。键名受白名单限制（不含 position / z-index 等可用来遮挡界面的属性），
   * 值里不允许出现 url()（否则能绕过 CSP 发外链请求）。
   * 不合规的项会被静默丢弃。
   */
  style?: Record<string, string>
  /** match（默认）覆盖匹配片段；line 覆盖整行 */
  scope?: 'match' | 'line'
}

/** 行内小组件声明。内容一律按纯文本渲染，不会被当作 HTML */
export interface PluginWidget {
  id: string
  pattern: string
  regex?: boolean
  flags?: string
  /** 显示的文本；支持 $1、$2 引用正则捕获组 */
  text: string
  class?: string
}

/** 快捷键绑定：按键 → 触发该插件已注册的命令 */
export interface PluginKeymap {
  id: string
  /** CM6 风格的按键，如 'Mod-Shift-h'（Mod 在 Windows 是 Ctrl、macOS 是 Cmd） */
  key: string
  /** 该插件已注册的命令 id */
  command: string
}

/** 设置面板 schema */
export interface PluginSettingsSchema {
  title?: string
  items: PluginSettingItem[]
  /** 当前值，通常从 loadData 读出后传入，否则宿主显示不出已保存的配置 */
  values: Record<string, unknown>
}

/** 一条笔记的标签信息 */
export interface TagInfo {
  name: string
  count: number
}

/** 待办事项 */
export interface TodoItem {
  id: string
  filePath: string
  fileName: string
  content: string
  lineIndex: number
  completed: boolean
  priority: string
}

/** 搜索结果 */
export interface SearchResult {
  path: string
  title: string
  snippet: string
  matchCount: number
}

/** 图谱中的一条边（反向链接） */
export interface GraphEdge {
  source: string
  target: string
}

/**
 * 注入到插件作用域的全局对象。
 *
 * 权限说明：除了 loadData / saveData / onLoad / onUnload 不需要权限外，
 * 其余 API 都要求插件在 manifest 里声明对应权限，未声明时会直接拒绝调用。
 */
export interface NoteVaultApi {
  // —— 文件读写（需要 workspace.read / workspace.write）——
  readFile(path: string): Promise<string>
  writeFile(path: string, content: string): Promise<void>

  // —— 文件与目录 ——
  /** 需要 workspace.read */
  listFiles(): Promise<string[]>
  /** 需要 workspace.write */
  createFile(path: string, content: string): Promise<unknown>
  /** 需要 workspace.write */
  deleteFile(path: string): Promise<void>
  /** 需要 workspace.write */
  renameFile(path: string, newName: string): Promise<unknown>

  // —— 元数据与检索（均需要 workspace.read）——
  getAllTags(): Promise<TagInfo[]>
  getFilesByTag(tag: string): Promise<Array<{ path: string, title: string }>>
  getAllTodos(): Promise<TodoItem[]>
  /** 返回所有指向该文档的边 */
  getBacklinks(path: string): Promise<GraphEdge[]>
  search(query: string): Promise<SearchResult[]>
  /** 解析文档头部的 YAML front matter（仅支持 key: value 平面结构） */
  getFrontmatter(path: string): Promise<Record<string, string>>

  // —— 界面与命令 ——
  /** 需要 commands */
  registerCommand(command: PluginCommand): void
  /** 需要 ui */
  registerToolbarButton(button: PluginToolbarButton): void
  /** 需要 notifications */
  notify(message: string): void

  // —— 声明式编辑器扩展（均需要 editor.decorate）——
  /** 高亮匹配的文本 */
  registerDecoration(decoration: PluginDecoration): void
  /** 用自定义小组件替换匹配文本的显示（内容按纯文本渲染） */
  registerWidget(widget: PluginWidget): void
  /** 绑定快捷键到本插件已注册的命令 */
  registerKeymap(keymap: PluginKeymap): void

  // —— 生命周期（无需权限）——
  onLoad(handler: (api: NoteVaultApi) => void): void
  onUnload(handler: () => void): void

  // —— 工作区事件（需要 workspace.read）——
  onFileChange(handler: (event: WorkspaceEvent) => void): void

  // —— 设置与私有数据 ——
  /** 声明设置面板；宿主渲染，改动时通过 onSettingsChange 回传整份值 */
  registerSettings(schema: PluginSettingsSchema): void
  onSettingsChange(handler: (values: Record<string, unknown>) => void): void
  /** 读取插件私有数据（JSON 字符串；首次运行返回空串） */
  loadData(): Promise<string>
  /** 保存插件私有数据（调用方自行 JSON.stringify） */
  saveData(data: string): Promise<void>
}

declare global {
  const notevault: NoteVaultApi
}
