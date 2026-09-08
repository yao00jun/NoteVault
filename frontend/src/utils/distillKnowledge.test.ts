import { describe, expect, it } from 'vitest'
import { distillNoteTitle, distillationPaths, interviewAnswerBody, isProjectMarkdown, parseDistillationDraft } from './distillKnowledge'

describe('project knowledge distillation', () => {
  it('only exposes distillation for project Markdown documents', () => {
    expect(isProjectMarkdown('Projects/物流/排查.md')).toBe(true)
    expect(isProjectMarkdown('Projects\\A\\design\\Guide.markdown')).toBe(true)
    for (const path of ['Learning/Go/笔记.md', 'Projects/empty.md', 'Projects/A/chart.canvas', '../Projects/A/note.md', 'Projects/A/../../Inbox/note.md']) {
      expect(isProjectMarkdown(path)).toBe(false)
    }
  })

  it('takes a usable title from the note body without leaking frontmatter into a filename', () => {
    expect(distillNoteTitle('debug.md', '---\ntitle: Metadata\n---\n# Kafka: 消息积压？\n\nText')).toBe('Kafka 消息积压？')
    expect(distillNoteTitle('排查记录.markdown', '没有一级标题')).toBe('排查记录')
  })

  it('reserves the source, chapter and book metadata together for a book write', () => {
    expect(distillationPaths({ sourceFile: 'Projects/A/source.md', targetMode: 'book', targetFolder: 'Learning/Go', targetTitle: '并发.md', summary: 'text', question: '', answer: '' })).toEqual([
      'Projects/A/source.md', 'Learning/Go/并发.md', 'Learning/Go/book.md',
    ])
  })

  it('reserves the shared interview topic so an open draft cannot overwrite appended cards', () => {
    expect(distillationPaths({ sourceFile: 'Projects/A/source.md', targetMode: 'interview', targetFolder: 'Learning/面试宝典', targetTitle: 'Go.markdown', summary: '', question: 'Q', answer: 'A' })).toEqual([
      'Projects/A/source.md', 'Learning/面试宝典/Go.md',
    ])
  })

  it('accepts structured AI drafts but ignores model-supplied paths and commands', () => {
    expect(parseDistillationDraft('```json\n{"title":"并发复盘","summary":"限制 Worker 数量","targetFolder":"../outside"}\n```', 'book')).toEqual({ title: '并发复盘', summary: '限制 Worker 数量', question: '', answer: '' })
    expect(parseDistillationDraft('{"question":"如何排查？","answer":"使用 pprof"}', 'interview')).toMatchObject({ question: '如何排查？', answer: '使用 pprof' })
  })

  it('rejects malformed or empty AI drafts instead of replacing a manual summary', () => {
    expect(() => parseDistillationDraft('Here is a summary', 'book')).toThrow('草稿')
    expect(() => parseDistillationDraft('{"summary":[]}', 'book')).toThrow('草稿')
    expect(() => parseDistillationDraft('{"question":"Q","answer":" "}', 'interview')).toThrow('草稿')
  })

  it('nests interview answer headings inside the SRS card without changing fenced examples', () => {
    expect(interviewAnswerBody('# Debug\n\n## Diagnosis\nRead metrics\n\n```sh\n# A shell comment\n```\n\n### Prevention\nBound workers')).toBe('#### Debug\n\n#### Diagnosis\nRead metrics\n\n```sh\n# A shell comment\n```\n\n#### Prevention\nBound workers')
  })

  it.each(['```', '~~~'])('preserves CRLF code examples within %s fences while nesting answer sections', (fence) => {
    const lines = ['## Diagnosis', `${fence}c`, '# include <stdio.h>', fence, '### Prevention', 'Bound workers']
    const expected = ['#### Diagnosis', `${fence}c`, '# include <stdio.h>', fence, '#### Prevention', 'Bound workers']
    expect(interviewAnswerBody(lines.join('\r\n'))).toBe(expected.join('\r\n'))
  })
})
