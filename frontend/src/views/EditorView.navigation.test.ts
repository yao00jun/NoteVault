// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { EditorState, type TransactionSpec } from '@codemirror/state'
import type { CancellablePromise } from '@wailsio/runtime'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { FileService } from '@/api'
import EditorView from './EditorView.vue'
import EditorContextDrawer from '@/components/editor/EditorContextDrawer.vue'

const mocks = vi.hoisted(() => ({ activeEditor: vi.fn(), toggleSidebar: vi.fn() }))
vi.mock('@/plugins/editorBridge', () => ({ getActiveEditor: mocks.activeEditor }))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => ({}) }))
vi.mock('@/stores/settings', () => ({ useSettingsStore: () => ({
  settings: {
    sidebarCollapsed: false,
    autoSaveInterval: 250,
    ai: {},
    editor: { lineHeight: 1.6, previewFontSize: 14 },
  },
  toggleSidebar: mocks.toggleSidebar,
}) }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => () => {}) } }))
vi.mock('@/api', () => ({
  FileService: { ReadFile: vi.fn(), SaveFile: vi.fn(), GetFileTree: vi.fn() },
  TagService: { InvalidateCache: vi.fn(async () => undefined) },
  SearchService: { Search: vi.fn(async () => []) },
  WorkspaceService: {}, ArchiveService: {}, TrashService: {}, SummarizeService: {}, ExportService: {}, CompileService: {},
}))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  vi.stubGlobal('innerWidth', 1600)
  vi.mocked(FileService.GetFileTree).mockResolvedValue([])
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
  mocks.activeEditor.mockReturnValue(null)
})
afterEach(() => vi.unstubAllGlobals())

async function renderEditor(mode = 'preview', content = '# Start\n\n## Next\n') {
  localStorage.setItem('notevault:editor-layout:v1', JSON.stringify({ viewMode: mode }))
  vi.mocked(FileService.ReadFile).mockImplementation(() => Promise.resolve(content) as CancellablePromise<string>)
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/editor', component: EditorView }] })
  await router.push('/editor?file=Learning/Go/Chapter.md')
  const wrapper = shallowMount({ template: '<RouterView />' }, { global: {
    plugins: [pinia, router, i18n],
    stubs: { RouterView: false, EditorView: false, EditorContextDrawer: false, EditorTabBar: false, MarkdownPreview: false },
  } })
  await flushPromises()
  return { wrapper, router }
}

describe('EditorView outline navigation', () => {
  it('opens the outline when a preview document finishes loading', async () => {
    const { wrapper } = await renderEditor()
    expect(wrapper.find('[data-testid="ctx-drawer"]').exists()).toBe(true)
    expect(wrapper.getComponent(EditorContextDrawer).props('tab')).toBe('outline')
    expect(wrapper.findAll('.ctx-outline-item').map(item => item.text())).toEqual(['H1Start', 'H2Next'])
  })

  it.each(['editor', 'split'])('does not automatically open the drawer in %s mode', async (mode) => {
    const { wrapper } = await renderEditor(mode)
    expect(wrapper.find('[data-testid="ctx-drawer"]').exists()).toBe(false)
  })

  it('keeps the drawer closed for a document without headings', async () => {
    const { wrapper } = await renderEditor('preview', 'Plain text only.')
    expect(wrapper.find('[data-testid="ctx-drawer"]').exists()).toBe(false)
  })

  it.each(['ctx-drawer-close', 'editor-drawer-toggle'])('remembers a manual close through %s when changing documents', async (control) => {
    const { wrapper, router } = await renderEditor()
    expect(wrapper.find('[data-testid="ctx-drawer"]').exists()).toBe(true)
    await wrapper.get(`[data-testid="${control}"]`).trigger('click')
    await router.push('/editor?file=Learning/Other.md')
    await flushPromises()
    expect(wrapper.find('[data-testid="ctx-drawer"]').exists()).toBe(false)
    await wrapper.get('[data-testid="editor-drawer-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="ctx-drawer"]').exists()).toBe(true)
  })

  it('respects a manually selected backlinks tab when entering preview', async () => {
    const { wrapper, router } = await renderEditor('editor')
    await wrapper.get('[data-testid="editor-drawer-toggle"]').trigger('click')
    await wrapper.get('[data-testid="ctx-tab-backlinks"]').trigger('click')
    await wrapper.get('[title="视图模式: editor"]').trigger('click')
    await router.push('/editor?file=Learning/Other.md')
    await flushPromises()
    expect(wrapper.getComponent(EditorContextDrawer).props('tab')).toBe('backlinks')
  })

  it('scrolls the preview to the selected heading, including repeated titles and setext headings', async () => {
    const body = '# Start\n\n```md\n# Code example\n~~~\n# Still code\n```\n\nSecond\n------\n\n## Repeat\n\n## Repeat\n'
    const { wrapper } = await renderEditor('preview', `---\ntitle: Example\n---\n${body}`)
    const outline = wrapper.getComponent(EditorContextDrawer).props('outline')
    expect(outline.map(item => item.text)).toEqual(['Start', 'Second', 'Repeat', 'Repeat'])
    const headings = wrapper.findAll('.markdown-preview > h2')
    const scroll = vi.fn()
    Object.defineProperty(headings[2]!.element, 'scrollIntoView', { value: scroll })
    await wrapper.findAll('.ctx-outline-item')[3]!.trigger('click')
    expect(scroll).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
  })

  it('jumps to Markdown headings without counting raw HTML or nested headings', async () => {
    const body = '<h2>Intro</h2>\n\n# Actual section\n\n> ## Quoted section\n\n## Later **section**\n'
    const { wrapper } = await renderEditor('preview', body)
    const headings = wrapper.findAll('.markdown-preview h1, .markdown-preview h2')
    const scrolls = headings.map(heading => {
      const scroll = vi.fn()
      Object.defineProperty(heading.element, 'scrollIntoView', { value: scroll })
      return scroll
    })

    const items = wrapper.findAll('.ctx-outline-item')
    expect(items.map(item => item.text())).toEqual(['H1Actual section', 'H2Later **section**'])
    await items[0]!.trigger('click')
    await items[1]!.trigger('click')
    expect(scrolls[0]).not.toHaveBeenCalled()
    expect(scrolls[1]).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
    expect(scrolls[2]).not.toHaveBeenCalled()
    expect(scrolls[3]).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
  })

  it('maps outline jumps to body lines in the editor without counting frontmatter', async () => {
    const body = '# Start\n\nSecond\n------\n\n## End\n'
    const editor = {
      state: EditorState.create({ doc: body }),
      dispatch(spec: TransactionSpec) { this.state = this.state.update(spec).state },
      focus: vi.fn(),
    }
    mocks.activeEditor.mockReturnValue(editor)
    const { wrapper } = await renderEditor('editor', `---\ntitle: Example\ntags: [Go]\n---\n${body}`)
    await wrapper.get('[data-testid="editor-drawer-toggle"]').trigger('click')
    await wrapper.findAll('.ctx-outline-item').at(-1)!.trigger('click')
    expect(editor.state.selection.main.head).toBe(body.indexOf('## End'))
  })

  it('toggles the sidebar when Ctrl+B is pressed', async () => {
    await renderEditor('split')
    mocks.toggleSidebar.mockClear()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'b', ctrlKey: true }))
    await nextTick()
    await flushPromises()
    expect(mocks.toggleSidebar).toHaveBeenCalledTimes(1)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'b', ctrlKey: true }))
    await nextTick()
    await flushPromises()
    expect(mocks.toggleSidebar).toHaveBeenCalledTimes(2)
  })
})
