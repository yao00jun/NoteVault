/**
 * Front matter 解析与序列化工具（YAML 子集）。
 *
 * 设计目标：
 * - 编辑器主区只显示正文（body），front matter 不干扰阅读/写作
 * - front matter 仍保留在文件中（兼容 Obsidian / git diff / 纯文本备份）
 * - 解析时原样保留所有字段与顺序，仅 tags 提供结构化读写
 */

export interface FrontMatterField {
  /** 字段名（保持原大小写） */
  key: string
  /** 原始行（不含 key 行本身）：列表项等缩进内容原样保留 */
  extraLines: string[]
  /** tags 等列表字段的项；非列表字段为 undefined */
  listItems?: string[]
  /** 标量值（`key: value` 形式）；列表字段为 undefined */
  scalar?: string
}

export interface ParsedContent {
  /** 原始 front matter 块（含首尾 `---` 与换行），无 front matter 时为 '' */
  raw: string
  /** 解析后的字段（保持文档顺序） */
  fields: FrontMatterField[]
  /** 正文（front matter 之后的内容，原样） */
  body: string
}

const FM_RE = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/

/** 判断内容是否以 front matter 开头 */
export function hasFrontMatter(content: string): boolean {
  return FM_RE.test(content)
}

/**
 * 解析文档：拆出 front matter 与正文。
 * YAML 子集支持：
 *   - `key: value` 标量
 *   - `key:` + 缩进 `- item` 列表
 * 其他复杂结构（嵌套 map 等）按原始行保留，序列化时原样写回。
 */
export function splitFrontMatter(content: string): ParsedContent {
  const m = content.match(FM_RE)
  if (!m) {
    return { raw: '', fields: [], body: content }
  }
  const raw = m[0]
  const body = content.slice(raw.length)
  const fields = parseFields(m[1])
  return { raw, fields, body }
}

function parseFields(block: string): FrontMatterField[] {
  const fields: FrontMatterField[] = []
  const lines = block.split(/\r?\n/)
  let current: FrontMatterField | null = null

  for (const line of lines) {
    if (!line.trim()) {
      current?.extraLines.push(line)
      continue
    }
    // 缩进行（列表项或续行）归当前字段
    if (/^[\t ]/.test(line)) {
      if (current) {
        current.extraLines.push(line)
        const item = line.trim().replace(/^-\s*/, '')
        if (line.trim().startsWith('-') && item) {
          current.listItems = current.listItems || []
          current.listItems.push(item)
        }
      }
      continue
    }
    // 顶层 key 行
    const kv = line.match(/^([A-Za-z0-9_-]+)\s*:\s*(.*)$/)
    if (kv) {
      current = { key: kv[1], extraLines: [], scalar: kv[2] || undefined }
      if (!kv[2]) current.scalar = undefined
      fields.push(current)
    } else if (current) {
      current.extraLines.push(line)
    }
  }
  return fields
}

/**
 * 把解析结果序列化回文档内容。
 * tags 字段用 listItems 重写，其他字段按原样（key 行 + extraLines）写回。
 */
export function buildContent(parsed: ParsedContent, tags: string[]): string {
  if (!parsed.raw && tags.length === 0) {
    return parsed.body
  }

  const lines: string[] = ['---']
  for (const f of parsed.fields) {
    if (isTagsKey(f.key)) {
      lines.push(`${f.key}:`)
      for (const item of tags) {
        lines.push(`  - ${item}`)
      }
      // 空列表时保留 `tags: []` 语义（写空项会变成 null，这里直接省略项）
      continue
    }
    lines.push(f.scalar !== undefined ? `${f.key}: ${f.scalar}` : `${f.key}:`)
    lines.push(...f.extraLines)
  }
  // 原文档没有 tags 字段但用户添加了 tags
  if (!parsed.fields.some((f) => isTagsKey(f.key)) && tags.length > 0) {
    lines.push('tags:')
    for (const item of tags) {
      lines.push(`  - ${item}`)
    }
  }
  lines.push('---')

  const body = parsed.body.replace(/^\r?\n/, '')
  return body ? lines.join('\n') + '\n' + body : lines.join('\n') + '\n'
}

/** 提取 tags 列表（无 tags 字段返回空数组） */
export function extractTags(parsed: ParsedContent): string[] {
  const field = parsed.fields.find((f) => isTagsKey(f.key))
  return field?.listItems ? [...field.listItems] : []
}

function isTagsKey(key: string): boolean {
  return key.toLowerCase() === 'tags'
}
