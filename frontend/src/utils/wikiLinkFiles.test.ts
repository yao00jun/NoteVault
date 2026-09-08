import { describe, expect, it } from 'vitest'
import type { FileNode } from '@/components/editor/FileTree.vue'
import { findFileByName } from './wikiLinkFiles'

const file = (path: string): FileNode => ({ path, name: path.split('/').at(-1)!, fullPath: `/vault/${path}`, isDir: false })
const source = file('Projects/商城/排查复盘.md')
const chapter = file('Learning/Go/排查复盘.md')
const tree: FileNode[] = [
  { path: 'Learning', name: 'Learning', fullPath: '/vault/Learning', isDir: true, children: [chapter] },
  { path: 'Projects', name: 'Projects', fullPath: '/vault/Projects', isDir: true, children: [source] },
]

describe('wiki link file resolution', () => {
  it('resolves both directions of a distilled knowledge link by full workspace path', () => {
    expect(findFileByName(tree, source.path)).toBe(source)
    expect(findFileByName(tree, chapter.path)).toBe(chapter)
  })

  it('supports extensionless full paths and Windows separators and case aliases', () => {
    expect(findFileByName(tree, 'Projects/商城/排查复盘')).toBe(source)
    expect(findFileByName(tree, '.\\learning\\go\\排查复盘.MD')).toBe(chapter)
  })

  it('preserves simple Markdown links and skips a directory sharing the note name', () => {
    const note = file('Resources/Guide.MARKDOWN')
    const folder = { ...file('Guide'), isDir: true }
    expect(findFileByName([folder, note], 'Guide')).toBe(note)
    expect(findFileByName(tree, '排查复盘')).toBe(chapter)
    expect(findFileByName(tree, '排查复盘.md')).toBe(chapter)
  })

  it('never redirects a missing qualified path to a same-named file elsewhere', () => {
    expect(findFileByName(tree, 'Projects/其他/排查复盘.md')).toBeNull()
    expect(findFileByName(tree, '../Projects/商城/排查复盘.md')).toBeNull()
  })

  it('prefers exact case when case-distinct files are present', () => {
    const upper = file('Learning/面试宝典/Go.md'), lower = file('Learning/面试宝典/go.md')
    expect(findFileByName([upper, lower], lower.path)).toBe(lower)
    expect(findFileByName([upper, lower], upper.path)).toBe(upper)
  })
})
