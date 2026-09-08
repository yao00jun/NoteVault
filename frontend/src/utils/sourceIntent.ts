import type { CollectionKind, SourceImportOpenRequest } from '@/api/sourceImport'

function validURL(source: string): boolean {
  try {
    const url = new URL(source)
    return /^https?:$/.test(url.protocol) && !!url.hostname && !url.username && !url.password
  } catch {
    return false
  }
}

function absolutePath(source: string): boolean {
  return /^(?:[a-z]:[\\/]|\\\\[^\\/]+[\\/]|\/[^/])/i.test(source)
    && !source.split(/[\\/]/).includes('..')
    && !/[\r\n\0<>|]/.test(source)
}

function explicitSourceCommand(frame: string): boolean {
  // Match the entire imperative, not a list of known question words. Anything
  // outside this small grammar stays in chat, including unfamiliar suffixes.
  // Sources are placeholders here, so their path/query text is never grammar.
  const englishKind = '(?:(?:a|an|the) )?(?:project|book|topic)'
  const englishAction = '(?:create|import|initialize|initialise|init|set up)'
  const englishBody = `(?:${englishAction} (?:${englishKind} (?:from|using) <source>|<source>(?: (?:as|into|to) ${englishKind})?)|use <source> to ${englishAction} ${englishKind})`
  const english = frame.trim().replace(/\s+/g, ' ')
  if (new RegExp(`^(?:(?:help me|i want (?:you )?to) )?${englishBody}[.!。！]*$`, 'i').test(english)
    || new RegExp(`^(?:(?:can|could|would) you(?: please)?|please) ${englishBody}[?？.!。！]*$`, 'i').test(english)) return true

  const chineseKind = '(?:一个|一本)?(?:项目|图书|书籍|书本|书|专题|主题)'
  const chineseAction = '(?:导入|初始化|新建|创建|建立|建)'
  const sourceFirst = `(?:把|将|用|从|根据)?<source>${chineseAction}(?:成|为|到|作为)?${chineseKind}`
  const actionFirst = `${chineseAction}<source>(?:(?:成|为|到|作为)${chineseKind})?`
  const kindFirst = `${chineseAction}${chineseKind}(?:从|使用)?<source>`
  const chineseBody = `(?:${sourceFirst}|${actionFirst}|${kindFirst})`
  const chinese = frame.replace(/\s+/g, '')
  return new RegExp(`^${chineseBody}[.!。！]*$`).test(chinese)
    || new RegExp(`^(?:请(?:帮我)?|帮我|麻烦(?:你)?|可以帮我)${chineseBody}(?:[，,]?(?:吗|好吗|可以吗))?[?？.!。！]*$`).test(chinese)
}

/** Only explicit user commands enter intake; source contents are never interpreted here. */
export function parseSourceIntent(raw: string): SourceImportOpenRequest | null {
  const text = raw.trim()
  if (!text || /[\r\n\0]/.test(text) || text.includes('```')) return null

  const sources: { source: string; start: number; end: number; quoted: boolean }[] = []
  let remainder = text
  const quotes = /"([^"\n]+)"|'([^'\n]+)'|“([^”\n]+)”|「([^」\n]+)」|`([^`\n]+)`/g
  for (const match of text.matchAll(quotes)) {
    const source = match.slice(1).find(value => value !== undefined)!.trim()
    if (/^https?:/i.test(source) || absolutePath(source)) {
      if (!validURL(source) && !absolutePath(source)) return null
      sources.push({ source, start: match.index, end: match.index + match[0].length, quoted: true })
    }
    remainder = remainder.slice(0, match.index) + ' '.repeat(match[0].length) + remainder.slice(match.index + match[0].length)
  }
  const tokens = /https?:\/\/[^\s<>"'`]+|[a-z]:[\\/][^\s<>"'`]+|\\\\[^\\/\s]+[\\/][^\s<>"'`]+|\/[^\s<>"'`]+/gi
  for (const match of remainder.matchAll(tokens)) {
    if (match.index > 0 && /[a-z0-9:./\\~_-]/i.test(remainder[match.index - 1]!)) return null
    const source = match[0].replace(/[。，,;；!?！？)）]+$/, '')
    // A clause attached with Chinese punctuation is not an unambiguous URL/path.
    // A source that genuinely includes these characters must be quoted.
    if (/[，。；！？]/.test(source)) return null
    if (!validURL(source) && !absolutePath(source)) return null
    // Keep stripped sentence punctuation in the command context. In particular,
    // a question mark following an unquoted URL must not disappear with the URL.
    sources.push({ source, start: match.index, end: match.index + source.length, quoted: false })
  }
  if (sources.length !== 1) return null
  const candidate = sources[0]!
  const context = (text.slice(0, candidate.start) + ' ' + text.slice(candidate.end)).trim()
  const frame = text.slice(0, candidate.start) + '<source>' + text.slice(candidate.end)
  if (!explicitSourceCommand(frame)) return null

  if (!candidate.quoted && !validURL(candidate.source)) {
    const tail = text.slice(candidate.end).trim()
    // An unquoted path with a space is ambiguous. Never silently import its first word.
    if (tail && !/^(?:为|成|到|作为|初始|创建|新建|导入|建立|建一个|[，。！？,!?]|as\b|into\b|to\b)/i.test(tail)) return null
  }

  const kinds: CollectionKind[] = []
  if (/项目|\bproject\b/i.test(context)) kinds.push('project')
  if (/书|\bbook\b/i.test(context)) kinds.push('book')
  if (/专题|主题|\btopic\b/i.test(context)) kinds.push('topic')
  if (kinds.length > 1) return null
  const kind = kinds[0]
  const isURL = validURL(candidate.source)
  const localFile = !isURL && /\.(?:md|markdown|txt|pdf|docx|epub|zip|html?)$/i.test(candidate.source)
  return {
    ...(kind ? { kind } : {}),
    sourceType: isURL ? 'url' : localFile ? 'files' : 'folder',
    source: candidate.source,
    instruction: text,
    autoStart: !!kind && !localFile,
  }
}
