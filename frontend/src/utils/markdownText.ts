/**
 * Markdown 文本的纯函数工具：标题 slug、CSS 选择器转义、wiki 链接拆解。
 *
 * 为什么集中在这里：slugifyHeading 与 cssEscape 原先在 EditorView.vue 与
 * MarkdownPreview.vue 各有一份逐字相同的实现，而它们的输出直接决定
 * 「点 [[note#标题]] 能不能滚到那个标题」——补了一处忘了另一处就是
 * 「点了没反应」，且这种不一致很难在测试外被发现。
 * 现在两份视图共用本文件，改动只有一处。
 *
 * 注意：wiki 链接拆解与后端 internal/service/graphservice.go 的
 * parseWikiLinkTarget 是同一口径，**两边必须同步改**（后端负责建边，
 * 前端负责跳转，口径不一致会出现「图上有边但点不过去」）。
 */

/**
 * 把标题文本转成 DOM id（GitHub 风格 slugify）。
 * marked 默认不生成 heading id，渲染后由 DOM 操作补。
 */
export function slugifyHeading(text: string): string {
  return text
    .trim()
    .toLowerCase()
    // 去掉所有标点（保留字母、数字、CJK、空格、横线）
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
}

/**
 * CSS.escape 的兜底包装。
 * 旧 WebView（Wails 依赖的系统 WebView2/WebKitGTK 版本较老）里 CSS 可能未定义，
 * 直接调用会抛，此时退化为只转义引号与反斜杠。
 */
export function cssEscape(s: string): string {
  try {
    return CSS.escape(s)
  } catch {
    return s.replace(/["\\]/g, '\\$&')
  }
}

/**
 * wiki 链接 [[...]] 匹配正则。
 *
 * 带 g 标志，属于「有状态」正则对象：用 matchAll 遍历是安全的
 * （规范要求 matchAll 内部克隆正则，不共享 lastIndex），
 * 但不要用 re.test() / re.exec() 在多次调用间复用同一实例。
 */
export const WIKI_LINK_RE = /\[\[([^\]\n]+)\]\]/g

export interface WikiLinkParts {
  file: string
  anchor: string
  block: string
  alias: string
  /** [[...]] 内部串原文，用于「目标不存在时按原名创建」等回退场景 */
  raw: string
}

/**
 * 把 [[...]] 内部串拆成 { file, anchor, block, alias } 四元组。
 *
 * 顺序约定（与 Obsidian 一致）：
 *   1. 先剥别名 |xxx（别名内可能含 #、^ 字符，先剥避免误解析）
 *   2. 再剥块 ID ^xxx（取最后一个 ^，块 ID 仅在末尾）
 *   3. 最后剥锚点 #xxx（取第一个 #，标题可能含 # 但锚点引用取整段）
 *   4. 剩下的就是 file
 *
 * 例：
 *   "note#标题^blk|别名" → file=note, anchor=标题, block=blk, alias=别名
 *   "#标题"             → file="", anchor=标题, block="", alias=""
 *   "^blk"              → file="", anchor="", block=blk, alias=""
 */
export function parseWikiLinkTarget(target: string): WikiLinkParts {
  let rest = target
  let alias = ''
  let block = ''
  let anchor = ''

  const barIdx = rest.indexOf('|')
  if (barIdx >= 0) {
    alias = rest.slice(barIdx + 1).trim()
    rest = rest.slice(0, barIdx).trim()
  }
  const caretIdx = rest.lastIndexOf('^')
  if (caretIdx >= 0) {
    block = rest.slice(caretIdx + 1).trim()
    rest = rest.slice(0, caretIdx).trim()
  }
  const hashIdx = rest.indexOf('#')
  if (hashIdx >= 0) {
    anchor = rest.slice(hashIdx + 1).trim()
    rest = rest.slice(0, hashIdx).trim()
  }

  return { file: rest.trim(), anchor, block, alias, raw: target }
}

/** 只取 wiki 链接的文件部分（出链、反向链接只需要它）。 */
export function parseWikiLinkFile(target: string): string {
  return parseWikiLinkTarget(target).file
}
