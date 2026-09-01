/**
 * 搜索文本高亮 + 清洗工具
 */

/**
 * 把 Markdown 噪音字符替换为空格，便于在 snippet 中显示
 */
export function stripMarkdownNoise(text: string): string {
  if (!text) return ''
  return text
    // 标题符号
    .replace(/^[#]+\s/gm, '')
    // 强调符号（保留内容）
    .replace(/(\*{1,3}|_{1,3})([^*_]+)\1/g, '$2')
    // 行内代码
    .replace(/`([^`]+)`/g, '$1')
    // 链接 [text](url) -> text
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    // 图片 ![alt](url) -> alt
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '图片：$1')
    // Wiki 链接 [[A]] / [[A|B]] -> A
    .replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_, a, b) => b || a)
    // 任务列表
    .replace(/^\s*[-*]\s+\[[ xX]\]\s+/gm, '☐ ')
    // 列表标记
    .replace(/^\s*[-*+]\s+/gm, '')
    .replace(/^\s*\d+\.\s+/gm, '')
    // 引用
    .replace(/^>\s*/gm, '')
    // 分隔线
    .replace(/^[-*_]{3,}$/gm, '')
    // 多余空白
    .replace(/\s+/g, ' ')
    .trim()
}

/**
 * 转义 HTML 特殊字符
 */
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
}

/**
 * 转义正则特殊字符
 */
export function escapeRegex(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * 在文本中高亮所有出现的关键词（不区分大小写）
 * 返回原始转义后的 HTML，关键词包裹在 <mark> 中
 *
 * 多词查询按空格拆分后逐个高亮。
 * 原先直接用整个查询串构造正则，于是搜「缓存 过期策略」时要匹配
 * 「缓存 过期策略」这个带空格的连续串——正文里几乎不可能出现，
 * 结果一个字都高亮不出来。这与后端 P0-1 修复的多词检索是同一个坑。
 */
export function highlightText(text: string, query: string): string {
  if (!text) return ''
  const escaped = escapeHtml(text)
  const terms = query.trim().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return escaped
  // 按词长降序排列，避免短词先匹配把长词切碎（如先匹配「缓存」会破坏「缓存失效」）
  const pattern = terms
    .slice()
    .sort((a, b) => b.length - a.length)
    .map(escapeRegex)
    .join('|')
  const regex = new RegExp(`(${pattern})`, 'gi')
  return escaped.replace(regex, '<mark class="search-highlight">$1</mark>')
}

/**
 * 高亮 + 清洗（snippet 专用）
 */
export function cleanSnippet(snippet: string, query: string): string {
  return highlightText(stripMarkdownNoise(snippet), query)
}
