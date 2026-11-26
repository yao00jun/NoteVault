// @vitest-environment jsdom
/**
 * 锚点跳转的时序契约。
 *
 * 这里刻意用手动帧泵（stub requestAnimationFrame）而不是等 setTimeout：
 * 锚点跳转的正确性取决于「第几帧能看到目标元素」，用真实计时器测只能测出
 * 「最终跳到了」，测不出「两帧内没渲染出来就放弃」这类回归。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { CancellablePromise } from '@wailsio/runtime'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { FileService } from '@/api'
import EditorView from './EditorView.vue'

const mocks = vi.hoisted(() => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
}))

vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => ({}) }))
vi.mock('@/stores/settings', () => ({
  useSettingsStore: () => ({ settings: { autoSaveInterval: 60_000, ai: {}, editor: { folderDisplayNames: {} } } }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks.toast }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => () => {}) } }))
vi.mock('@/api', () => ({
  FileService: { ReadFile: vi.fn(), SaveFile: vi.fn(), GetFileTree: vi.fn() },
  TagService: { InvalidateCache: vi.fn(async () => undefined) },
  SearchService: { Search: vi.fn(async () => []) },
  WorkspaceService: {}, ArchiveService: {}, TrashService: {}, SummarizeService: {}, ExportService: {}, CompileService: {},
}))

// 预览容器由测试完全控制：内容原样当 HTML 渲染，便于精确构造「有没有那个标题」。
const FakePreview = defineComponent({
  name: 'MarkdownPreview',
  props: {
    content: { type: String, default: '' },
    workspacePath: { type: String, default: '' },
    currentFileName: { type: String, default: '' },
  },
  emits: ['wiki-link-click', 'anchor-copy'],
  setup(props) {
    return () => h('div', { class: 'markdown-preview', innerHTML: props.content })
  },
})

const TREE = [
  {
    name: 'Learning', path: 'Learning', fullPath: 'C:/vault/Learning', isDir: true,
    children: [
      { name: 'current.md', path: 'Learning/current.md', fullPath: 'C:/vault/Learning/current.md', isDir: false },
      { name: 'target.md', path: 'Learning/target.md', fullPath: 'C:/vault/Learning/target.md', isDir: false },
    ],
  },
]

let frames: FrameRequestCallback[] = []
const scrolled: HTMLElement[] = []

function runFrame() {
  const cb = frames.shift()
  cb?.(16)
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  frames = []
  scrolled.length = 0
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { frames.push(cb); return frames.length })
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  HTMLElement.prototype.scrollIntoView = function (this: HTMLElement) { scrolled.push(this) }
  vi.mocked(FileService.GetFileTree).mockResolvedValue(TREE as never)
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
})
afterEach(() => vi.unstubAllGlobals())

async function renderEditor(files: Record<string, string>, initial = 'Learning/current.md') {
  vi.mocked(FileService.ReadFile).mockImplementation(
    (_ws, path) => Promise.resolve(files[path] ?? '') as CancellablePromise<string>,
  )
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/editor', component: EditorView }] })
  await router.push(`/editor?file=${initial}`)
  const wrapper = shallowMount({ template: '<RouterView />' }, {
    attachTo: document.body,
    global: {
      plugins: [pinia, router, i18n],
      stubs: { RouterView: false, EditorView: false, MarkdownPreview: FakePreview },
    },
  })
  await flushPromises()
  return { wrapper, router }
}

function previewOf(wrapper: Awaited<ReturnType<typeof renderEditor>>['wrapper']) {
  return wrapper.findComponent({ name: 'MarkdownPreview' })
}

describe('anchor jump timing', () => {
  it('retries on the next frame when the heading is not in the DOM yet', async () => {
    const { wrapper } = await renderEditor({
      'Learning/current.md': '<h1>Start</h1>',
      'Learning/target.md': '<h1>Target</h1><h2 data-heading-text="Next">Next</h2>',
    })
    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: 'Next', block: '', raw: 'target#Next' })
    await flushPromises()

    // 目标文件已加载，但模拟「预览这一帧还没渲染出标题」
    const root = document.querySelector('.markdown-preview') as HTMLElement
    expect(root).toBeTruthy()
    root.innerHTML = ''

    runFrame()
    expect(scrolled).toHaveLength(0)

    // 下一帧渲染完成，重试必须命中
    root.innerHTML = '<h2 data-heading-text="Next">Next</h2>'
    runFrame()
    expect(scrolled).toHaveLength(1)
    expect(scrolled[0]?.textContent).toBe('Next')
  })

  it('jumps synchronously when the target tab is already open', async () => {
    const { wrapper, router } = await renderEditor({
      'Learning/current.md': '<h1>Start</h1>',
      'Learning/target.md': '<h2 data-heading-text="Next">Next</h2>',
    })
    await router.push('/editor?file=Learning/target.md')
    await flushPromises()
    await router.push('/editor?file=Learning/current.md')
    await flushPromises()

    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: 'Next', block: '', raw: 'target#Next' })
    await flushPromises()

    // 已有标签页不需要等 activeTab watcher 安排的两帧
    expect(scrolled).toHaveLength(1)
    runFrame()
    runFrame()
    expect(scrolled).toHaveLength(1)
  })

  it('matches the slug id and strips the ¶ copy button from heading text', async () => {
    const { wrapper } = await renderEditor({
      'Learning/current.md': '<h1>Start</h1>',
      'Learning/target.md': '<h2 id="next-step">Next step</h2><h2>Repeat ¶</h2>',
    })
    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: 'Next step', block: '', raw: 'target#Next step' })
    await flushPromises()
    runFrame()
    expect(scrolled).toHaveLength(1)
    expect(scrolled[0]?.id).toBe('next-step')

    // 切文件后预览会按 :key 重挂载，必须重新取实例，否则 emit 落在已卸载的节点上
    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: 'Repeat', block: '', raw: 'target#Repeat' })
    await flushPromises()
    runFrame()
    expect(scrolled).toHaveLength(2)
    // data-heading-text 缺失时按 textContent 兜底，必须剥掉注入的 ¶
    expect(scrolled[1]?.textContent).toBe('Repeat ¶')
  })

  it('jumps to a block reference', async () => {
    const { wrapper } = await renderEditor({
      'Learning/current.md': '<h1>Start</h1>',
      'Learning/target.md': '<p>intro</p><p data-block-id="blk-1">target block</p>',
    })
    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: '', block: 'blk-1', raw: 'target^blk-1' })
    await flushPromises()
    runFrame()
    expect(scrolled).toHaveLength(1)
    expect(scrolled[0]?.dataset.blockId).toBe('blk-1')
  })

  it('warns once and clears the pending anchor when neither frame finds the target', async () => {
    const { wrapper, router } = await renderEditor({
      'Learning/current.md': '<h1>Start</h1>',
      'Learning/target.md': '<h1>Target</h1>',
    })
    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: 'Missing', block: '', raw: 'target#Missing' })
    await flushPromises()

    runFrame()
    runFrame()
    expect(mocks.toast.warning).toHaveBeenCalledTimes(1)
    expect(scrolled).toHaveLength(0)

    // 已经放弃：后续帧与切标签都不该再尝试滚动（pendingAnchor 必须被清空）
    runFrame()
    runFrame()
    await router.push('/editor?file=Learning/current.md')
    await flushPromises()
    runFrame()
    runFrame()
    expect(scrolled).toHaveLength(0)
    expect(mocks.toast.warning).toHaveBeenCalledTimes(1)
  })

  it('does not scroll again after the anchor was consumed', async () => {
    const { wrapper, router } = await renderEditor({
      'Learning/current.md': '<h1>Start</h1>',
      'Learning/target.md': '<h2 data-heading-text="Next">Next</h2>',
    })
    previewOf(wrapper).vm.$emit('wiki-link-click', { file: 'target', anchor: 'Next', block: '', raw: 'target#Next' })
    await flushPromises()
    runFrame()
    expect(scrolled).toHaveLength(1)

    await router.push('/editor?file=Learning/current.md')
    await flushPromises()
    runFrame()
    runFrame()
    expect(scrolled).toHaveLength(1)
  })
})
