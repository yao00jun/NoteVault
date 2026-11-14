import type { DistillRequest } from '@/api/workbench'
import { i18n } from '@/i18n'
import { splitFrontMatter } from './frontmatter'
import { normalizeNotePath } from './navigation'

export interface DistillationSource {
  workspacePath: string
  path: string
  title: string
  content: string
}

export interface DistillationDraft {
  title: string
  summary: string
  question: string
  answer: string
}

const hasControlCharacter = (text: string) => [...text].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)

const safeNotePath = (filePath: string): string | null => {
  const path = normalizeNotePath(filePath)
  if (path.split('/').some(part => !part || part === '.' || part === '..') || hasControlCharacter(path) || path.includes(':')) return null
  return path
}

export function isProjectMarkdown(filePath: string): boolean {
  const path = safeNotePath(filePath)
  return !!path && /^Projects\/[^/]+\/.+\.(md|markdown)$/i.test(path)
}

/** Inbox/<name>.md 也是蒸馏来源（沉淀时需在窗口内指定关联项目）。 */
export function isInboxMarkdown(filePath: string): boolean {
  const path = safeNotePath(filePath)
  return !!path && /^Inbox\/[^/]+\.(md|markdown)$/i.test(path)
}

export function isDistillableSource(filePath: string): boolean {
  return isProjectMarkdown(filePath) || isInboxMarkdown(filePath)
}

export function distillTitleStem(title: string): string {
  return title.trim().replace(/\.(md|markdown)$/i, '').trim()
}

export function distillNoteTitle(name: string, content: string): string {
  const body = splitFrontMatter(content).body
  const heading = body.match(/^#\s+(.+?)\s*#*\s*$/m)?.[1]
  return distillTitleStem(heading || name).replace(/[<>:"/\\|?*#[\]]/g, ' ').replace(/\s+/g, ' ').trim() || i18n.global.t('workbench.distill.fallbackTitle')
}

/** Mirrors basic filename rules for preview; the backend remains authoritative. */
export function distillNameError(value: string): string {
  if (!value.trim()) return i18n.global.t('workbench.distill.nameRequired')
  if (value === '.' || value === '..' || /[<>:"/\\|?*#[\]]/.test(value) || hasControlCharacter(value) || /[. ]$/.test(value)) return i18n.global.t('workbench.distill.nameInvalid')
  if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(value)) return i18n.global.t('workbench.distill.reservedSystemName')
  return ''
}

export function distillationPaths(request: DistillRequest): string[] {
  const folder = normalizeNotePath(request.targetFolder)
  const paths = [normalizeNotePath(request.sourceFile), `${folder}/${distillTitleStem(request.targetTitle)}.md`]
  if (request.targetMode === 'book') paths.push(`${folder}/book.md`)
  return paths
}

/** Keep answer sections beneath the SRS question while retaining code verbatim. */
export function interviewAnswerBody(markdown: string): string {
  let fence: { character: string; length: number } | null = null
  return markdown.split('\n').map(line => {
    const boundary = line.replace(/\r$/, '').match(/^ {0,3}(`{3,}|~{3,})(.*)$/)
    if (fence) {
      if (boundary && boundary[1]![0] === fence.character && boundary[1]!.length >= fence.length && !boundary[2]!.trim()) fence = null
      return line
    }
    if (boundary) {
      fence = { character: boundary[1]![0]!, length: boundary[1]!.length }
      return line
    }
    return line.replace(/^( {0,3})#{1,3}([ \t]+)/, '$1####$2')
  }).join('\n')
}

/** AI 草稿支持单对象或数组；缺必填字段的条目被丢弃，全部无效时抛错。 */
export function parseDistillationDrafts(raw: string, mode: DistillRequest['targetMode']): DistillationDraft[] {
  const text = raw.trim().replace(/^```(?:json)?\s*\n?([\s\S]*?)\n?```$/i, '$1').trim()
  const invalid = () => new Error(i18n.global.t('workbench.distill.draftInvalid'))
  let data: unknown
  try { data = JSON.parse(text) }
  catch { throw invalid() }
  if (!data || typeof data !== 'object') throw invalid()
  const required = mode === 'book'
    ? (record: Record<string, unknown>) => typeof record.summary === 'string' && !!record.summary.trim()
    : (record: Record<string, unknown>) => typeof record.question === 'string' && !!record.question.trim() && typeof record.answer === 'string' && !!record.answer.trim()
  const entries = Array.isArray(data) ? data : [data]
  const drafts = entries.flatMap(entry => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return []
    const field = (key: string) => typeof entry[key] === 'string' ? (entry[key] as string).trim() : ''
    const draft = { title: field('title'), summary: field('summary'), question: field('question'), answer: field('answer') }
    return required(entry) ? [draft] : []
  })
  if (!drafts.length) throw invalid()
  return drafts
}
