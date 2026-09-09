// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, onUnmounted, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import type { CancellablePromise } from '@wailsio/runtime'
import { ArchiveService, FileService, TrashService } from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useEditorDraft, type EditorTab } from './useEditorDraft'
import { useEditorFileOperations } from './useEditorFileOperations'

const mocks = vi.hoisted(() => ({ prompt: vi.fn(), confirm: vi.fn(), error: vi.fn(), warning: vi.fn(), success: vi.fn() }))
vi.mock('./usePrompt', () => ({ promptDialog: mocks.prompt }))
vi.mock('./useConfirm', () => ({ confirmDialog: mocks.confirm }))
vi.mock('./useToast', () => ({ useToast: () => ({ error: mocks.error, warning: mocks.warning, success: mocks.success }) }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => () => {}) } }))
vi.mock('@/api', () => ({
  FileService: { ReadFile: vi.fn(), SaveFile: vi.fn(), CreateFile: vi.fn(), CreateFolder: vi.fn(), RenameFile: vi.fn(), DeleteFile: vi.fn() },
  ArchiveService: { ArchiveFile: vi.fn() }, TrashService: { MoveToTrash: vi.fn() },
}))

enableAutoUnmount(afterEach)
beforeEach(() => { vi.resetAllMocks(); localStorage.clear(); mocks.confirm.mockResolvedValue(true) })
afterEach(() => { vi.useRealTimers() })

function file(path: string, isDir = false) { return { name: path.split('/').pop()!, path, fullPath: `C:/vault/${path}`, isDir } }
const backendPromise = <T>(promise: Promise<T>) => promise as CancellablePromise<T>
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

async function setup() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useWorkspaceStore()
  store.setCurrentWorkspace({ id: 'a', name: 'a', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: ['/editor', '/vault'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push('/editor?file=Projects/Go/Tasks.md')
  const tabs = ref<EditorTab[]>([
    { path: 'Projects/Go/Tasks.md', name: 'Tasks.md', content: 'latest draft', isDirty: true, lastSavedAt: '' },
    { path: 'Projects/Go/nested/design.md', name: 'design.md', content: 'nested draft', isDirty: true, lastSavedAt: '' },
    { path: 'Projects/Gopher/keep.md', name: 'keep.md', content: 'unrelated', isDirty: false, lastSavedAt: '' },
  ])
  tabs.value.forEach(tab => store.openFile(tab.path))
  store.setActiveFile(tabs.value[0]!.path)
  store.togglePin(tabs.value[0]!.path)
  const activeTabIndex = ref(0)
  const focusFolder = ref<string | null>('Projects/Go')
  const disk = new Map(tabs.value.map(tab => [tab.path, tab.isDirty ? 'old content' : tab.content]))
  vi.mocked(FileService.SaveFile).mockImplementation((_ws, path, content) => backendPromise(Promise.resolve().then(() => { disk.set(path, content) })))
  vi.mocked(FileService.ReadFile).mockImplementation((_ws, path) => backendPromise(Promise.resolve().then(() => {
    if (!disk.has(path)) throw new Error('missing file')
    return disk.get(path)!
  })))
  vi.mocked(FileService.RenameFile).mockImplementation((_ws, old, name) => backendPromise(Promise.resolve().then(() => {
    const path = old.slice(0, old.lastIndexOf('/') + 1) + name
    for (const [key, content] of [...disk]) if (key === old || key.startsWith(old + '/')) {
      disk.delete(key)
      disk.set(path + key.slice(old.length), content)
    }
    return file(path, !old.endsWith('.md'))
  })))
  const refresh = vi.fn(async () => undefined)
  const openFile = vi.fn(async () => undefined)
  let operations!: ReturnType<typeof useEditorFileOperations>
  let draft!: ReturnType<typeof useEditorDraft>
  const wrapper = mount(defineComponent({ setup() {
    const workspacePath = computed(() => store.currentWorkspace?.path)
    draft = useEditorDraft({ tabs, activeTabIndex, workspacePath, autoSaveInterval: () => 250, findTabIndex: path => tabs.value.findIndex(tab => tab.path === path) })
    operations = useEditorFileOperations({ tabs, activeTabIndex, workspacePath, focusFolder, withExternalFileChanges: draft.withExternalFileChanges, refresh, openFile })
    onUnmounted(draft.dispose)
    return () => null
  } }), { global: { plugins: [pinia, router, i18n] } })
  return { store, router, tabs, activeTabIndex, focusFolder, disk, draft, operations, refresh, openFile, wrapper }
}

describe('editor file operations', () => {
  it('creates folders at the selected nested parent and expands the created path', async () => {
    const { operations, focusFolder, refresh } = await setup()
    mocks.prompt.mockResolvedValue('Examples')
    vi.mocked(FileService.CreateFolder).mockResolvedValue(file('Learning/Go/Examples', true))
    await operations.handleNewFolder('Learning/Go')
    expect(FileService.CreateFolder).toHaveBeenCalledWith('C:/vault', 'Learning/Go/Examples')
    expect(focusFolder.value).toBe('Learning/Go/Examples')
    expect(refresh).toHaveBeenCalledOnce()
  })

  it('creates documents through the same prompt and opens the returned Markdown path', async () => {
    const { operations, openFile } = await setup()
    mocks.prompt.mockResolvedValue('Notes.md')
    const node = file('Learning/Go/Notes.md')
    vi.mocked(FileService.CreateFile).mockResolvedValue(node)
    await operations.handleNewFile('Learning/Go')
    expect(FileService.CreateFile).toHaveBeenCalledWith('C:/vault', node.path, '# Notes\n\n')
    expect(openFile).toHaveBeenCalledWith(node)
  })

  it('flushes dirty folder descendants and remaps tabs, route, pins and recents together', async () => {
    const { operations, tabs, router, store, focusFolder, disk } = await setup()
    mocks.prompt.mockResolvedValue('Backend')
    await operations.handleRename(file('Projects/Go', true))
    expect(disk.get('Projects/Backend/Tasks.md')).toBe('latest draft')
    expect(disk.get('Projects/Backend/nested/design.md')).toBe('nested draft')
    expect(disk.has('Projects/Go/Tasks.md')).toBe(false)
    expect(tabs.value.map(tab => tab.path)).toEqual(['Projects/Backend/Tasks.md', 'Projects/Backend/nested/design.md', 'Projects/Gopher/keep.md'])
    expect(tabs.value.every(tab => !tab.isDirty)).toBe(true)
    expect(store.activeFile).toBe('Projects/Backend/Tasks.md')
    expect(store.pinnedItems[0]?.path).toBe('Projects/Backend/Tasks.md')
    expect(router.currentRoute.value.query.file).toBe('Projects/Backend/Tasks.md')
    expect(focusFolder.value).toBe('Projects/Backend')
    expect(mocks.error).not.toHaveBeenCalled()
  })

  it('waits for an in-flight save, preserves an omitted extension and never recreates the old path', async () => {
    const { operations, draft, disk, tabs } = await setup()
    const pending = deferred<void>()
    vi.mocked(FileService.SaveFile).mockImplementationOnce((_ws, path, content) => backendPromise(pending.promise.then(() => { disk.set(path, content) })))
    const saving = draft.saveTab(0)
    mocks.prompt.mockResolvedValue('Renamed')
    const renaming = operations.handleRename(file(tabs.value[0]!.path))
    await flushPromises()
    expect(FileService.RenameFile).not.toHaveBeenCalled()
    expect(await draft.saveTab(0)).toBe(false)
    pending.resolve()
    await Promise.all([saving, renaming])
    expect(FileService.RenameFile).toHaveBeenCalledWith('C:/vault', 'Projects/Go/Tasks.md', 'Renamed.md')
    expect(disk.has('Projects/Go/Tasks.md')).toBe(false)
    expect(disk.get('Projects/Go/Renamed.md')).toBe('latest draft')
    expect(tabs.value[0]?.name).toBe('Renamed.md')
  })

  it('reserves Windows case aliases before renaming their canonical folder', async () => {
    const { operations, draft, tabs, store } = await setup()
    tabs.value[0]!.path = 'projects/go/tasks.md'
    store.openFile('projects/go/tasks.md')
    store.togglePin('projects/go/tasks.md')
    const pending = deferred<void>()
    vi.mocked(FileService.SaveFile).mockImplementationOnce(() => backendPromise(pending.promise))
    vi.mocked(FileService.ReadFile).mockImplementation((_ws, path) => backendPromise(Promise.resolve(path.toLowerCase().endsWith('tasks.md') ? 'latest draft' : 'nested draft')))
    const saving = draft.saveTab(0)
    mocks.prompt.mockResolvedValue('Backend')
    const renaming = operations.handleRename(file('Projects/Go', true))
    await flushPromises()
    expect(FileService.RenameFile).not.toHaveBeenCalled()
    pending.resolve()
    await Promise.all([saving, renaming])
    expect(tabs.value[0]?.path).toBe('Projects/Backend/tasks.md')
    expect(store.openFiles.every(path => !path.toLowerCase().startsWith('projects/go/'))).toBe(true)
    expect(store.pinnedItems.every(item => !item.path.toLowerCase().startsWith('projects/go/'))).toBe(true)
    tabs.value[0]!.isDirty = true
    await draft.saveTab(0)
    expect(FileService.SaveFile).toHaveBeenLastCalledWith('C:/vault', 'Projects/Backend/tasks.md', 'latest draft')
  })

  it('retains the original path and draft on conflicts or save failures', async () => {
    const { operations, draft, tabs, store } = await setup()
    mocks.prompt.mockResolvedValue('New.md')
    draft.conflictedPaths.value.add(tabs.value[0]!.path)
    await operations.handleRename(file(tabs.value[0]!.path))
    expect(FileService.RenameFile).not.toHaveBeenCalled()
    expect(tabs.value[0]?.content).toBe('latest draft')
    expect(store.activeFile).toBe('Projects/Go/Tasks.md')
    draft.conflictedPaths.value.clear()
    vi.mocked(FileService.SaveFile).mockRejectedValue(new Error('disk full'))
    await operations.handleRename(file(tabs.value[0]!.path))
    expect(FileService.RenameFile).not.toHaveBeenCalled()
    expect(tabs.value[0]?.isDirty).toBe(true)
    expect(mocks.error).toHaveBeenCalledTimes(2)
  })

  it('blocks rename before conflicting Windows alias buffers can overwrite each other', async () => {
    const { operations, tabs, draft } = await setup()
    tabs.value.push({ ...tabs.value[0]!, path: 'projects/go/tasks.md', content: 'other unsaved draft', isDirty: true })
    mocks.prompt.mockResolvedValue('Backend')
    await operations.handleRename(file('Projects/Go', true))
    expect(FileService.SaveFile).not.toHaveBeenCalled()
    expect(FileService.RenameFile).not.toHaveBeenCalled()
    expect(tabs.value[0]?.content).toBe('latest draft')
    expect(tabs.value.at(-1)?.content).toBe('other unsaved draft')
    expect(draft.conflictedPaths.value.size).toBe(2)
    expect(mocks.error).toHaveBeenCalledWith(expect.stringContaining('不同草稿'))
  })

  it('reports target collisions while keeping the original tab, route and pin', async () => {
    const { operations, tabs, router, store } = await setup()
    mocks.prompt.mockResolvedValue('existing.md')
    vi.mocked(FileService.RenameFile).mockRejectedValue(new Error('目标已存在'))
    await operations.handleRename(file(tabs.value[0]!.path))
    expect(tabs.value[0]?.path).toBe('Projects/Go/Tasks.md')
    expect(router.currentRoute.value.query.file).toBe('Projects/Go/Tasks.md')
    expect(store.pinnedItems[0]?.path).toBe('Projects/Go/Tasks.md')
    expect(mocks.error).toHaveBeenCalledWith(expect.stringContaining('已存在'))
  })

  it.each([null, '', '  ', '../escape', 'bad/name', 'bad\\name'])('does not create a folder for cancelled or invalid input %s', async input => {
    const { operations } = await setup()
    mocks.prompt.mockResolvedValue(input)
    await operations.handleNewFolder('Learning')
    expect(FileService.CreateFolder).not.toHaveBeenCalled()
  })

  it('ignores a prompt completed after changing workspace, even when switching back', async () => {
    const { operations, store } = await setup()
    const input = deferred<string>()
    mocks.prompt.mockReturnValue(input.promise)
    const creating = operations.handleNewFolder('Learning')
    const original = store.currentWorkspace!
    store.setCurrentWorkspace({ ...original, path: 'C:/other' })
    store.setCurrentWorkspace(original)
    input.resolve('Late folder')
    await creating
    expect(FileService.CreateFolder).not.toHaveBeenCalled()
  })

  it('does not rename after the workspace changes while a draft is saving', async () => {
    const { operations, store } = await setup()
    const pending = deferred<void>()
    vi.mocked(FileService.SaveFile).mockImplementationOnce(() => backendPromise(pending.promise))
    mocks.prompt.mockResolvedValue('Late.md')
    const renaming = operations.handleRename(file('Projects/Go/Tasks.md'))
    await flushPromises()
    store.setCurrentWorkspace({ ...store.currentWorkspace!, path: 'C:/other' })
    pending.resolve()
    await renaming
    expect(FileService.RenameFile).not.toHaveBeenCalled()
    expect(store.openFiles).toEqual([])
  })

  it('surfaces create failures instead of silently doing nothing', async () => {
    const { operations, refresh } = await setup()
    mocks.prompt.mockResolvedValue('Folder')
    vi.mocked(FileService.CreateFolder).mockRejectedValue(new Error('Permission denied'))
    await operations.handleNewFolder('Projects')
    expect(mocks.error).toHaveBeenCalledWith(expect.stringContaining('Permission denied'))
    expect(refresh).not.toHaveBeenCalled()
  })

  it('moving a folder to trash saves and closes every descendant without recreating removed files', async () => {
    const { operations, tabs, disk, store, router, draft } = await setup()
    vi.mocked(TrashService.MoveToTrash).mockImplementation((_ws, path) => backendPromise(Promise.resolve().then(() => {
      expect(disk.get('Projects/Go/Tasks.md')).toBe('latest draft')
      expect(disk.get('Projects/Go/nested/design.md')).toBe('nested draft')
      for (const key of [...disk.keys()]) if (key.startsWith(path + '/')) disk.delete(key)
      return null
    })))
    await operations.handleTrashFile(file('Projects/Go', true))
    await draft.flushAllTabs()
    expect(tabs.value.map(tab => tab.path)).toEqual(['Projects/Gopher/keep.md'])
    expect([...disk.keys()]).toEqual(['Projects/Gopher/keep.md'])
    expect(store.pinnedItems).toEqual([])
    expect(store.recentFiles.map(item => item.path)).toEqual(['Projects/Gopher/keep.md'])
    expect(router.currentRoute.value.query.file).toBe('Projects/Gopher/keep.md')
    expect(mocks.error).not.toHaveBeenCalled()
  })

  it('preserves unexpected late edits as a visible recovery document after an archive RPC', async () => {
    const { operations, tabs, disk, router } = await setup()
    const pending = deferred<null>()
    vi.mocked(ArchiveService.ArchiveFile).mockReturnValue(backendPromise(pending.promise))
    vi.mocked(FileService.CreateFile).mockImplementation((_ws, path, content) => {
      disk.set(path, content)
      return backendPromise(Promise.resolve(file(path)))
    })
    const archiving = operations.handleArchiveFile(file(tabs.value[0]!.path))
    await flushPromises()
    expect(ArchiveService.ArchiveFile).toHaveBeenCalledOnce()
    tabs.value[0]!.content = 'latest plugin edit during archive'
    tabs.value[0]!.isDirty = true
    pending.resolve(null)
    await archiving
    const recovery = tabs.value.find(tab => tab.path.startsWith('Inbox/恢复草稿-'))!
    expect(recovery.content).toBe('latest plugin edit during archive')
    expect(disk.get(recovery.path)).toBe(recovery.content)
    expect(router.currentRoute.value.query.file).toBe(recovery.path)
    expect(mocks.warning).toHaveBeenCalledWith(expect.stringContaining('新修改已保存'))
  })

  it('a cancelled deletion preserves unsaved content and open paths', async () => {
    const { operations, tabs } = await setup()
    mocks.confirm.mockResolvedValue(false)
    await operations.handleDeleteFile(file('Projects/Go', true))
    expect(FileService.DeleteFile).not.toHaveBeenCalled()
    expect(FileService.SaveFile).not.toHaveBeenCalled()
    expect(tabs.value[0]?.isDirty).toBe(true)
  })
})
