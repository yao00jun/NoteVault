import type { DistillRequest } from '@/api/workbench'
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

export function isProjectMarkdown(filePath: string): boolean {
  const path = normalizeNotePath(filePath)
  return /^Projects\/[^/]+\/.+\.(md|markdown)$/i.test(path)
    && !path.split('/').some(part => !part || part === '.' || part === '..')
    && !hasControlCharacter(path) && !path.includes(':')
}

export function distillTitleStem(title: string): string {
  return title.trim().replace(/\.(md|markdown)$/i, '').trim()
}

export function distillNoteTitle(name: string, content: string): string {
  const body = splitFrontMatter(content).body
  const heading = body.match(/^#\s+(.+?)\s*#*\s*$/m)?.[1]
  return distillTitleStem(heading || name).replace(/[<>:"/\\|?*#[\]]/g, ' ').replace(/\s+/g, ' ').trim() || '实战复盘'
}

/** Mirrors basic filename rules for preview; the backend remains authoritative. */
export function distillNameError(value: string): string {
  if (!value.trim()) return '请填写名称'
  if (value === '.' || value === '..' || /[<>:"/\\|?*#[\]]/.test(value) || hasControlCharacter(value) || /[. ]$/.test(value)) return '名称不能包含路径、特殊符号或末尾空格/句点'
  if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(value)) return '请更换系统保留的文件名'
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

export function parseDistillationDraft(raw: string, mode: DistillRequest['targetMode']): DistillationDraft {
  const text = raw.trim().replace(/^```(?:json)?\s*\n?([\s\S]*?)\n?```$/i, '$1').trim()
  const invalid = () => new Error('AI 草稿格式不完整，请重试或直接编辑表单')
  let data: Record<string, unknown>
  try { data = JSON.parse(text) }
  catch { throw invalid() }
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw invalid()
  const field = (key: string) => typeof data[key] === 'string' ? (data[key] as string).trim() : ''
  const draft = { title: field('title'), summary: field('summary'), question: field('question'), answer: field('answer') }
  if (mode === 'book' ? !draft.summary : !draft.question || !draft.answer) throw invalid()
  return draft
}
