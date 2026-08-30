// 编辑区格式工具栏按钮定义（MarkdownEditor 与 SettingsView 共用）
export interface ToolbarItem {
  id: string
  label?: string
  cls?: string
  /** 按钮名称的 i18n key（如 'toolbar.bold'） */
  i18nKey?: string
  /** 特殊按钮类型：字体颜色 / 背景色 / 格式刷 / 更多下拉 */
  type?: 'color' | 'bg' | 'brush' | 'more'
  /** 固定按钮（撤销/重做），不可在设置中隐藏 */
  fixed?: boolean
}

/** 文本处理工具（“更多”下拉内），无参数确定性操作 */
export interface TextToolItem {
  id: string
  i18nKey: string
}

export const TOOLBAR_ITEMS: ToolbarItem[] = [
  { id: 'undo', label: '↶', i18nKey: 'toolbar.undo', fixed: true },
  { id: 'redo', label: '↷', i18nKey: 'toolbar.redo', fixed: true },
  { id: 'bold', label: 'B', cls: 'tb-bold', i18nKey: 'toolbar.bold' },
  { id: 'italic', label: 'I', cls: 'tb-italic', i18nKey: 'toolbar.italic' },
  { id: 'underline', label: 'U', cls: 'tb-underline', i18nKey: 'toolbar.underline' },
  { id: 'strike', label: 'S', cls: 'tb-strike', i18nKey: 'toolbar.strike' },
  { id: 'code', label: '</>', cls: 'tb-mono', i18nKey: 'toolbar.code' },
  { id: 'color', label: 'A', cls: 'tb-color', i18nKey: 'toolbar.color', type: 'color' },
  { id: 'bg', label: 'A', cls: 'tb-bg', i18nKey: 'toolbar.bg', type: 'bg' },
  { id: 'brush', label: '🖌', i18nKey: 'toolbar.brush', type: 'brush' },
  { id: 'h1', label: 'H1', i18nKey: 'toolbar.h1' },
  { id: 'h2', label: 'H2', i18nKey: 'toolbar.h2' },
  { id: 'h3', label: 'H3', i18nKey: 'toolbar.h3' },
  { id: 'h4', label: 'H4', i18nKey: 'toolbar.h4' },
  { id: 'h5', label: 'H5', i18nKey: 'toolbar.h5' },
  { id: 'h6', label: 'H6', i18nKey: 'toolbar.h6' },
  { id: 'quote', label: '❝', i18nKey: 'toolbar.quote' },
  { id: 'ul', label: '•', i18nKey: 'toolbar.ul' },
  { id: 'ol', label: '1.', i18nKey: 'toolbar.ol' },
  { id: 'indent', label: '⇥', i18nKey: 'toolbar.indent' },
  { id: 'undent', label: '⇤', i18nKey: 'toolbar.undent' },
  { id: 'align-left', label: '⬅', i18nKey: 'toolbar.alignLeft' },
  { id: 'align-center', label: '↔', i18nKey: 'toolbar.alignCenter' },
  { id: 'align-right', label: '➡', i18nKey: 'toolbar.alignRight' },
  { id: 'align-justify', label: '☰', i18nKey: 'toolbar.alignJustify' },
  { id: 'link', label: '🔗', i18nKey: 'toolbar.link' },
  { id: 'image', label: '🖼', i18nKey: 'toolbar.image' },
  { id: 'codeblock', label: '```', cls: 'tb-mono', i18nKey: 'toolbar.codeblock' },
  { id: 'table', label: '表格', i18nKey: 'toolbar.table' },
  { id: 'hr', label: '―', i18nKey: 'toolbar.hr' },
  { id: 'more', label: '⋯', i18nKey: 'toolbar.more', type: 'more' },
]

/** 默认启用的按钮 id（撤销/重做固定显示，不在此列表内） */
export const VISIBLE_DEFAULT: string[] = TOOLBAR_ITEMS.filter(
  (i) => i.id && !i.fixed,
).map((i) => i.id as string)

/** 默认排列顺序（拖拽排序的基准） */
export const TOOLBAR_ORDER_DEFAULT: string[] = TOOLBAR_ITEMS.map((i) => i.id as string)

/** “更多”下拉内的文本处理工具（对应 Editing Toolbar 的行操作/文本处理/高级工具） */
export const TEXT_TOOLS: TextToolItem[] = [
  { id: 'removeBlankLines', i18nKey: 'toolbar.tools.removeBlankLines' },
  { id: 'insertBlankLines', i18nKey: 'toolbar.tools.insertBlankLines' },
  { id: 'splitLines', i18nKey: 'toolbar.tools.splitLines' },
  { id: 'mergeLines', i18nKey: 'toolbar.tools.mergeLines' },
  { id: 'dedupeLines', i18nKey: 'toolbar.tools.dedupeLines' },
  { id: 'sortLines', i18nKey: 'toolbar.tools.sortLines' },
  { id: 'fullHalfConvert', i18nKey: 'toolbar.tools.fullHalfConvert' },
  { id: 'numberLines', i18nKey: 'toolbar.tools.numberLines' },
  { id: 'trimLineEnds', i18nKey: 'toolbar.tools.trimLineEnds' },
  { id: 'shrinkSpaces', i18nKey: 'toolbar.tools.shrinkSpaces' },
  { id: 'removeAllWhitespace', i18nKey: 'toolbar.tools.removeAllWhitespace' },
  { id: 'listToTable', i18nKey: 'toolbar.tools.listToTable' },
  { id: 'tableToList', i18nKey: 'toolbar.tools.tableToList' },
]
