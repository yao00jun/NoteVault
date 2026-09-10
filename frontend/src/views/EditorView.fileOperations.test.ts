// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { CancellablePromise } from '@wailsio/runtime'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { FileService } from '@/api'
import EditorView from './EditorView.vue'
import FileTree from '@/components/editor/FileTree.vue'
import EditorTabBar from '@/components/editor/EditorTabBar.vue'
import DocumentPropertiesPanel from '@/components/editor/DocumentPropertiesPanel.vue'

const mocks = vi.hoisted(() => ({ prompt: vi.fn() }))
vi.mock('@/composables/usePrompt', () => ({ promptDialog: mocks.prompt }))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => ({}) }))
vi.mock('@/stores/settings', () => ({ useSettingsStore: () => ({ settings: { autoSaveInterval: 250, ai: {}, editor: { folderDisplayNames: {} } } }) }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => () => {}) } }))
vi.mock('@/api', () => ({
  FileService: { ReadFile: vi.fn(), SaveFile: vi.fn(), GetFileTree: vi.fn(), RenameFile: vi.fn(), CreateFolder: vi.fn() },
  TagService: { InvalidateCache: vi.fn(async () => undefined) },
  SearchService: { Search: vi.fn(async () => []) },
  WorkspaceService: {}, ArchiveService: {}, TrashService: {}, SummarizeService: {}, ExportService: {}, CompileService: {},
}))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  vi.mocked(FileService.GetFileTree).mockResolvedValue([])
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
  vi.mocked(FileService.ReadFile).mockImplementation((_ws, path) => Promise.resolve(`# ${path}`) as CancellablePromise<string>)
})
afterEach(() => { vi.unstubAllGlobals() })

async function renderEditor() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/editor', component: EditorView }] })
  await router.push('/editor?file=Projects/Go/Tasks.md')
  const wrapper = shallowMount({ template: '<RouterView />' }, { global: { plugins: [pinia, router, i18n], stubs: { RouterView: false, EditorView: false } } })
  await flushPromises()
  return { wrapper, workspace, router }
}

describe('EditorView file-tree wiring and concurrent navigation', () => {
  it('handles the file tree new-folder command with the themed prompt', async () => {
    const { wrapper } = await renderEditor()
    mocks.prompt.mockResolvedValue('Examples')
    vi.mocked(FileService.CreateFolder).mockResolvedValue({ name: 'Examples', path: 'Learning/Go/Examples', fullPath: '', isDir: true })
    wrapper.getComponent(FileTree).vm.$emit('new-folder', 'Learning/Go')
    await flushPromises()
    expect(FileService.CreateFolder).toHaveBeenCalledWith('C:/vault', 'Learning/Go/Examples')
    expect(wrapper.getComponent(FileTree).props('focusFolder')).toBe('Learning/Go/Examples')
  })

  it('locks properties and follows the latest sidebar document after a delayed rename', async () => {
    const { wrapper, router, workspace } = await renderEditor()
    let finish!: (value: null) => void
    vi.mocked(FileService.RenameFile).mockReturnValue(new Promise<null>(resolve => { finish = resolve }) as CancellablePromise<null>)
    mocks.prompt.mockResolvedValue('Renamed.md')
    wrapper.getComponent(FileTree).vm.$emit('rename', { name: 'Tasks.md', path: 'Projects/Go/Tasks.md', fullPath: '', isDir: false })
    await flushPromises()
    expect(wrapper.get('.editor-content').attributes()).toHaveProperty('inert')
    wrapper.getComponent(DocumentPropertiesPanel).vm.$emit('update:tags', ['late-tag'])
    const before = wrapper.getComponent(EditorTabBar).props('tabs') as { content: string }[]
    expect(before[0]?.content).not.toContain('late-tag')
    workspace.openFile('Learning/Other.md')
    await router.push('/editor?file=Learning/Other.md')
    await flushPromises()
    expect(FileService.ReadFile).not.toHaveBeenCalledWith('C:/vault', 'Learning/Other.md')
    finish(null)
    await flushPromises()
    expect(FileService.ReadFile).toHaveBeenCalledWith('C:/vault', 'Learning/Other.md')
    expect(router.currentRoute.value.query.file).toBe('Learning/Other.md')
    expect(workspace.activeFile).toBe('Learning/Other.md')
    expect(wrapper.getComponent(EditorTabBar).props('activeTab')?.path).toBe('Learning/Other.md')
    expect(wrapper.get('.editor-content').attributes()).not.toHaveProperty('inert')
  })

  it('honors an explicit tab click queued during rename', async () => {
    const { wrapper, router } = await renderEditor()
    await router.push('/editor?file=Learning/Other.md')
    await flushPromises()
    await router.push('/editor?file=Projects/Go/Tasks.md')
    await flushPromises()
    let finish!: (value: null) => void
    vi.mocked(FileService.RenameFile).mockReturnValue(new Promise<null>(resolve => { finish = resolve }) as CancellablePromise<null>)
    mocks.prompt.mockResolvedValue('Renamed.md')
    wrapper.getComponent(FileTree).vm.$emit('rename', { name: 'Tasks.md', path: 'Projects/Go/Tasks.md', fullPath: '', isDir: false })
    await flushPromises()
    wrapper.getComponent(EditorTabBar).vm.$emit('switch-tab', 1)
    await flushPromises()
    finish(null)
    await flushPromises()
    expect(router.currentRoute.value.query.file).toBe('Learning/Other.md')
    expect(wrapper.getComponent(EditorTabBar).props('activeTab')?.path).toBe('Learning/Other.md')
  })

  it('reuses one Windows file tab for differently cased incoming paths', async () => {
    const { wrapper, router, workspace } = await renderEditor()
    workspace.openFile('projects/go/tasks.md')
    await router.push('/editor?file=projects/go/tasks.md')
    await flushPromises()
    expect(wrapper.getComponent(EditorTabBar).props('tabs')).toHaveLength(1)
    expect(workspace.openFiles).toEqual(['Projects/Go/Tasks.md'])
    expect(router.currentRoute.value.query.file).toBe('Projects/Go/Tasks.md')
  })
})
