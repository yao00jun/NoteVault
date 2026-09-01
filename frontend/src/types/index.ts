// NoteVault 全局类型定义

export type ThemeType = 'macos' | 'winui' | 'islands-dark'

export interface Workspace {
  id: string
  name: string
  path: string
  createdAt: string
  lastOpenedAt: string
}

export interface FileNode {
  name: string
  path: string
  isDir: boolean
  ext: string
  size: number
  modified: string
  children?: FileNode[]
}

export interface Todo {
  id: number
  docId: number
  docPath: string
  content: string
  completed: boolean
  priority: 'high' | 'medium' | 'low'
  dueDate?: string
  reminderAt?: string
  createdAt: string
}

export interface Tag {
  id: number
  name: string
  color?: string
  count: number
}

export interface AISettings {
  /** OpenAI 兼容接口地址，如 https://api.openai.com/v1 或 https://api.deepseek.com/v1 */
  baseURL: string
  /** 模型名称，如 gpt-4o-mini / deepseek-chat */
  model: string
  /** API Key，仅存于本地 localStorage，不落盘到笔记 */
  apiKey: string
}

/**
 * Embedding 端点配置（P1-3 语义检索）。
 * 与 AISettings 分离：Chat/生成 与 Embedding 可能指向不同服务
 * （例如生成走云端、embedding 走本机 Ollama）。
 */
export interface EmbeddingSettings {
  /** OpenAI 兼容 embedding 接口地址，如 http://localhost:11434/v1（Ollama 本机） */
  baseURL: string
  /** embedding 模型名称，如 bge-m3 / nomic-embed-text */
  model: string
  /** API Key，本机端点（Ollama）可留空 */
  apiKey: string
}

/**
 * 重排序（Rerank）端点配置（P1-3b，可选）。
 * 在 BM25 + 向量召回之后做一遍 cross-encoder 重排，进一步修正召回顺序。
 * 未配置时检索融合退化为纯 RRF，行为与未开启语义检索一致。
 */
export interface RerankSettings {
  /** 厂商：ollama（本机 /api/rerank，免鉴权）| cohere（/v1/rerank，需 Key） */
  provider: 'ollama' | 'cohere'
  /** rerank 接口地址：ollama 填根地址 http://localhost:11434；cohere 填 https://api.cohere.ai/v1 */
  baseURL: string
  /** rerank 模型名称，如 bge-reranker-v2-m3（Ollama）/ rerank-v3.5（Cohere） */
  model: string
  /** API Key：ollama 本机可留空；cohere 必填 */
  apiKey: string
}

export interface EditorSettings {
  /** 编辑器行高 */
  lineHeight: number
  /** 预览区字体大小 */
  previewFontSize: number
}

export type ToolbarMode = 'top' | 'floating' | 'fixed'

/** 用户自定义命令：wrap=用 prefix/suffix 包裹选区；regex=对选区内文本做正则替换 */
export interface CustomCommand {
  id: string
  name: string
  type: 'wrap' | 'regex'
  prefix?: string
  suffix?: string
  pattern?: string
  replacement?: string
  flags?: string
}

export interface ToolbarSettings {
  /** 工具栏位置：top=顶部固定；floating=跟随光标浮层；fixed=底部固定 */
  mode: ToolbarMode
  /** 用户在设置中勾选显示的按钮 id（undo/redo 固定显示，不在此列表内） */
  visibleButtons: string[]
  /** 主工具栏按钮的排列顺序（id 数组），用于拖拽排序 */
  order: string[]
  /** 用户自定义命令（前缀/后缀 / 正则），出现在“更多”下拉中 */
  customCommands: CustomCommand[]
}

export interface AppSettings {
  theme: ThemeType
  language: 'zh-CN' | 'en-US'
  sidebarCollapsed: boolean
  autoSaveInterval: number
  editorMode: 'split' | 'editor' | 'preview'
  fontSize: number
  defaultWorkspace?: string
  ai: AISettings
  embedding: EmbeddingSettings
  rerank: RerankSettings
  editor: EditorSettings
  toolbar: ToolbarSettings
  reminder: {
    defaultTime: string
    doNotDisturb: {
      enabled: boolean
      start: string
      end: string
    }
    repeatOverdue: boolean
  }
  trash: {
    autoPurgeDays: number
    confirmDelete: boolean
  }
  errorReport: {
    sentryDSN: string
    enableLocalLog: boolean
  }
}

export interface SearchResult {
  path: string
  title: string
  snippet: string
  matchCount: number
  isFileMatch: boolean
}
