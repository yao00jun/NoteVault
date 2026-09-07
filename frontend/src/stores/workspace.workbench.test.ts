// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWorkspaceStore } from './workspace'

const workspace = (id: string) => ({ id, name: id, path: `/vault/${id}`, createdAt: '', lastOpenedAt: '' })

describe('workspace-scoped workbench navigation', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('limits pins to eight and preserves keyboard/drag reorder across restarts', () => {
    const store = useWorkspaceStore()
    store.setCurrentWorkspace(workspace('a'))
    for (let n = 0; n < 8; n++) expect(store.togglePin(`note${n}.md`)).toBe(true)
    expect(store.togglePin('ninth.md')).toBe(false)
    store.movePin('note0.md', 7)
    expect(store.pinnedItems.at(-1)?.path).toBe('note0.md')
    setActivePinia(createPinia())
    const reloaded = useWorkspaceStore()
    reloaded.setCurrentWorkspace(workspace('a'))
    expect(reloaded.pinnedItems.map(x => x.path)).toEqual(store.pinnedItems.map(x => x.path))
  })

  it('isolates pins, tabs and recent files when switching workspace', () => {
    const store = useWorkspaceStore()
    store.setCurrentWorkspace(workspace('a'))
    store.togglePin('a.md')
    store.openFile('a.md')
    store.setCurrentWorkspace(workspace('b'))
    expect(store.pinnedItems).toEqual([])
    expect(store.recentFiles).toEqual([])
    expect(store.openFiles).toEqual([])
    expect(store.activeFile).toBeNull()
    store.togglePin('b.md')
    store.openFile('b.md')
    store.setCurrentWorkspace(workspace('a'))
    expect(store.pinnedItems.map(x => x.path)).toEqual(['a.md'])
    expect(store.recentFiles.map(x => x.path)).toEqual(['a.md'])
  })

  it('normalizes Windows file paths for pin de-duplication and recent titles', () => {
    const store = useWorkspaceStore()
    store.setCurrentWorkspace(workspace('a'))
    store.togglePin('Projects\\商城\\project.md')
    expect(store.isPinned('Projects/商城/project.md')).toBe(true)
    store.openFile('Projects\\商城\\project.md')
    expect(store.recentFiles[0]).toMatchObject({ path: 'Projects/商城/project.md', title: 'project.md' })
  })
})
