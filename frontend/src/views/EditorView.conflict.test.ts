// @vitest-environment jsdom
/**
 * 保存冲突在「视图层」的契约：横幅出现、三个出口各自做什么、以及冲突期间不得写盘。
 *
 * useEditorDraft 的单测已锁住状态机的内部转移；这里补的是用户真正点得到的那层——
 * 按钮接线断了、横幅不显示，状态机再对也没用。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { CancellablePromise } from '@wailsio/runtime'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { FileService } from '@/api'
import { editorSession } from '@/composables/useEditorSession'
import EditorView from './EditorView.vue'
import DocumentPropertiesPanel from '@/components/editor/DocumentPropertiesPanel.vue'
import EditorTabBar from '@/components/editor/EditorTabBar.vue'

const mocks = vi.hoisted(() => ({
  change: null as ((event: { data: { type: string; path: string } }) => void) | null,
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
}))

vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => ({}) }))
vi.mock('@/stores/settings', () => ({
  useSettingsStore: () => ({ settings: { autoSaveInterval: 60_000, ai: {}, editor: { folderDisplayNames: {} } } }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks.toast }))
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((_name, handler) => {
      mocks.change = handler
      return () => { mocks.change = null }
    }),
  },
}))
vi.mock('@/api', () => ({
  FileService: { ReadFile: vi.fn(), SaveFile: vi.fn(), GetFileTree: vi.fn() },
  TagService: { InvalidateCache: vi.fn(async () => undefined) },
  SearchService: { Search: vi.fn(async () => []) },
  WorkspaceService: {}, ArchiveService: {}, TrashService: {}, SummarizeService: {}, ExportService: {}, CompileService: {},
}))

const PATH = 'Projects/Go/Tasks.md'
let disk = '# Tasks\n'

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  disk = '# Tasks\n'
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  vi.mocked(FileService.GetFileTree).mockResolvedValue([])
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
  vi.mocked(FileService.ReadFile).mockImplementation(() => Promise.resolve(disk) as CancellablePromise<string>)
})
afterEach(() => vi.unstubAllGlobals())

async function renderEditor() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/editor', component: EditorView }] })
  await router.push(`/editor?file=${PATH}`)
  const wrapper = shallowMount({ template: '<RouterView />' }, {
    global: { plugins: [pinia, router, i18n], stubs: { RouterView: false, EditorView: false } },
  })
  await flushPromises()
  return { wrapper }
}

/** 用属性栏打一个标签把当前 tab 变脏，等价于用户改过内容 */
async function makeDirty(wrapper: Awaited<ReturnType<typeof renderEditor>>['wrapper']) {
  wrapper.getComponent(DocumentPropertiesPanel).vm.$emit('update:tags', ['draft-tag'])
  await flushPromises()
}

/** 模拟外部（Git / 同步工具）改写磁盘上的同一文件 */
async function externalEdit(content: string) {
  disk = content
  mocks.change?.({ data: { type: 'modify', path: PATH } })
  await flushPromises()
}

describe('save conflict banner', () => {
  it('marks the dirty tab as conflicted instead of overwriting the external edit', async () => {
    const { wrapper } = await renderEditor()
    await makeDirty(wrapper)
    await externalEdit('# Tasks\n\n> edited on disk\n')

    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="conflict-banner"]').text()).toContain('Tasks.md')
    expect(editorSession.state).toBe('conflict')
    // 冲突期间绝不自动写盘：磁盘版本是别人的
    expect(FileService.SaveFile).not.toHaveBeenCalled()
  })

  it('discards the draft and reloads the disk version when the user picks reload', async () => {
    const { wrapper } = await renderEditor()
    await makeDirty(wrapper)
    await externalEdit('# Tasks\n\n> edited on disk\n')

    await wrapper.get('[data-testid="conflict-reload"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(false)
    expect(editorSession.state).not.toBe('conflict')
    // 丢弃草稿只读盘，不写盘
    expect(FileService.SaveFile).not.toHaveBeenCalled()
    const tabs = wrapper.getComponent(EditorTabBar).props('tabs') as { content: string; isDirty: boolean }[]
    expect(tabs[0]?.content).toContain('edited on disk')
    expect(tabs[0]?.isDirty).toBe(false)
  })

  it('saves the draft as a separate copy and keeps the external original authoritative', async () => {
    const { wrapper } = await renderEditor()
    await makeDirty(wrapper)
    await externalEdit('# Tasks\n\n> edited on disk\n')

    await wrapper.get('[data-testid="conflict-copy"]').trigger('click')
    await flushPromises()

    expect(FileService.SaveFile).toHaveBeenCalledTimes(1)
    const [workspace, copyPath, content] = vi.mocked(FileService.SaveFile).mock.calls[0]!
    expect(workspace).toBe('C:/vault')
    expect(copyPath).toContain('.冲突副本-')
    expect(content).toContain('draft-tag')
    // 原文件仍是磁盘上的版本，横幅随之消解
    expect(copyPath).not.toBe(PATH)
    expect(mocks.toast.success).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(false)
  })

  it('opens a line-by-line diff of disk versus draft', async () => {
    const { wrapper } = await renderEditor()
    await makeDirty(wrapper)
    await externalEdit('# Tasks\n\n> edited on disk\n')

    await wrapper.get('[data-testid="conflict-diff"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('.conflict-diff-mask').exists()).toBe(true)
    expect(wrapper.findAll('.diff-row').length).toBeGreaterThan(0)
    // 对比只是读盘 + 渲染，不得顺手写盘
    expect(FileService.SaveFile).not.toHaveBeenCalled()
  })

  it('ignores an external change to a clean tab and shows no banner', async () => {
    const { wrapper } = await renderEditor()
    await externalEdit('# Tasks\n\n> edited on disk\n')

    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(false)
    expect(FileService.SaveFile).not.toHaveBeenCalled()
  })
})
