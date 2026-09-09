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

  it('renames open paths, pins and recent entries at directory boundaries and persists them', () => {
    const store = useWorkspaceStore()
    store.setCurrentWorkspace(workspace('a'))
    store.openFile('Projects/Go/design.md')
    store.openFile('Projects/Go/sub/task.md')
    store.openFile('Projects/Gopher/keep.md')
    store.setActiveFile('Projects/Go/design.md')
    store.togglePin('Projects/Go/design.md', 'My design')
    store.togglePin('Projects/Go', 'Go')
    store.renameFilePaths('Projects\\Go', 'Projects/Backend', true)
    expect(store.openFiles).toEqual(['Projects/Backend/design.md', 'Projects/Backend/sub/task.md', 'Projects/Gopher/keep.md'])
    expect(store.activeFile).toBe('Projects/Backend/design.md')
    expect(store.pinnedItems).toEqual([{ path: 'Projects/Backend/design.md', title: 'My design' }, { path: 'Projects/Backend', title: 'Backend' }])
    store.renameFilePaths('Projects/Backend/design.md', 'Projects/Backend/overview.md', false)
    expect(store.recentFiles.find(item => item.path.endsWith('overview.md'))?.title).toBe('overview.md')
    setActivePinia(createPinia())
    const reloaded = useWorkspaceStore()
    reloaded.setCurrentWorkspace(workspace('a'))
    expect(reloaded.pinnedItems[0]).toEqual({ path: 'Projects/Backend/overview.md', title: 'My design' })
    expect(reloaded.recentFiles.some(item => item.path.includes('Projects/Go/'))).toBe(false)
    reloaded.setCurrentWorkspace(workspace('b'))
    expect(reloaded.pinnedItems).toEqual([])
  })
})
