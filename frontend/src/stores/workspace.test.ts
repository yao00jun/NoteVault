import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useWorkspaceStore } from './workspace'
import type { Workspace } from '@/types'

const mockWorkspace: Workspace = {
  id: 'ws_1',
  name: '测试工作区',
  path: '/tmp/test-vault',
  createdAt: '2026-01-01T00:00:00Z',
  lastOpenedAt: '2026-01-01T00:00:00Z',
}

describe('useWorkspaceStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('初始状态应无工作区', () => {
    const store = useWorkspaceStore()
    expect(store.currentWorkspace).toBeNull()
    expect(store.hasWorkspace).toBe(false)
    expect(store.openFiles).toHaveLength(0)
    expect(store.activeFile).toBeNull()
    expect(store.fileTreeVersion).toBe(0)
  })

  it('setCurrentWorkspace 应设置当前工作区并更新时间', () => {
    const store = useWorkspaceStore()
    store.setCurrentWorkspace(mockWorkspace)
    expect(store.currentWorkspace).toEqual(mockWorkspace)
    expect(store.hasWorkspace).toBe(true)
    // lastOpenedAt 应被更新
    expect(store.currentWorkspace?.lastOpenedAt).not.toBe('2026-01-01T00:00:00Z')
  })

  it('setCurrentWorkspace(null) 应清空', () => {
    const store = useWorkspaceStore()
    store.setCurrentWorkspace(mockWorkspace)
    store.setCurrentWorkspace(null)
    expect(store.currentWorkspace).toBeNull()
    expect(store.hasWorkspace).toBe(false)
  })

  it('openFile 应添加文件到打开列表并设为活跃', () => {
    const store = useWorkspaceStore()
    store.openFile('notes/a.md')
    expect(store.openFiles).toEqual(['notes/a.md'])
    expect(store.activeFile).toBe('notes/a.md')
    expect(store.recentFiles).toHaveLength(1)
    expect(store.recentFiles[0].title).toBe('a.md')
  })

  it('openFile 不应重复添加同一文件', () => {
    const store = useWorkspaceStore()
    store.openFile('a.md')
    store.openFile('a.md')
    expect(store.openFiles).toHaveLength(1)
    // recentFiles 也不应重复
    expect(store.recentFiles).toHaveLength(1)
  })

  it('openFile 应将最近打开的文件排到最前', () => {
    const store = useWorkspaceStore()
    store.openFile('a.md')
    store.openFile('b.md')
    store.openFile('c.md')
    expect(store.recentFiles[0].path).toBe('c.md')
    expect(store.recentFiles[2].path).toBe('a.md')
    // 再次打开 a.md，应排到最前
    store.openFile('a.md')
    expect(store.recentFiles[0].path).toBe('a.md')
  })

  it('recentFiles 最多保留 20 条', () => {
    const store = useWorkspaceStore()
    for (let i = 0; i < 25; i++) {
      store.openFile(`file${i}.md`)
    }
    expect(store.recentFiles).toHaveLength(20)
    // 最近的应排在前面
    expect(store.recentFiles[0].path).toBe('file24.md')
  })

  it('closeFile 应从打开列表移除并切换活跃文件', () => {
    const store = useWorkspaceStore()
    store.openFile('a.md')
    store.openFile('b.md')
    store.openFile('c.md')
    // 活跃是 c.md
    expect(store.activeFile).toBe('c.md')
    // 关闭 c.md，活跃应切到 b.md
    store.closeFile('c.md')
    expect(store.openFiles).toEqual(['a.md', 'b.md'])
    expect(store.activeFile).toBe('b.md')
  })

  it('closeFile 关闭最后一个文件时 activeFile 应为 null', () => {
    const store = useWorkspaceStore()
    store.openFile('only.md')
    store.closeFile('only.md')
    expect(store.openFiles).toHaveLength(0)
    expect(store.activeFile).toBeNull()
  })

  it('closeFile 对不存在的文件应安全无操作', () => {
    const store = useWorkspaceStore()
    store.openFile('a.md')
    store.closeFile('nonexistent.md')
    expect(store.openFiles).toHaveLength(1)
  })

  it('setActiveFile 应设置活跃文件', () => {
    const store = useWorkspaceStore()
    store.openFile('a.md')
    store.openFile('b.md')
    store.setActiveFile('a.md')
    expect(store.activeFile).toBe('a.md')
    store.setActiveFile(null)
    expect(store.activeFile).toBeNull()
  })

  it('incrementFileTreeVersion 应递增版本号', () => {
    const store = useWorkspaceStore()
    expect(store.fileTreeVersion).toBe(0)
    store.incrementFileTreeVersion()
    store.incrementFileTreeVersion()
    expect(store.fileTreeVersion).toBe(2)
  })

  it('addWorkspace 应添加到工作区列表', () => {
    const store = useWorkspaceStore()
    store.addWorkspace(mockWorkspace)
    expect(store.workspaces).toHaveLength(1)
    expect(store.workspaces[0].id).toBe('ws_1')
  })
})
