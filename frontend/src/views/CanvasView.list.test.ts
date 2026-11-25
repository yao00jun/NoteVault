// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'
import { useWorkspaceStore } from '@/stores/workspace'
import { confirmDialog } from '@/composables/useConfirm'
import { promptDialog } from '@/composables/usePrompt'
import { resetToasts, useToast } from '@/composables/useToast'
import { FileService } from '@/api'
import CanvasView from './CanvasView.vue'
import type { RawFileNode } from '@/lib/canvas'

vi.mock('@/composables/useConfirm', () => ({ confirmDialog: vi.fn(), chooseDialog: vi.fn() }))
vi.mock('@/composables/usePrompt', () => ({ promptDialog: vi.fn() }))
vi.mock('@/api', () => apiMock({
  FileService: { GetFileTree: vi.fn(), ReadFile: vi.fn(), SaveFile: vi.fn(), CreateFile: vi.fn(), RenameFile: vi.fn(), DeleteFile: vi.fn() },
  CredentialService: { GetCredential: vi.fn(async () => '') },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))

enableAutoUnmount(afterEach)

const file = (name: string, path = name): RawFileNode => ({ name, path, isDir: false })
const folder = (name: string, children: RawFileNode[]): RawFileNode => ({ name, path: name, isDir: true, children })

const TREE: RawFileNode[] = [
  file('索引.md'),
  file('a.canvas'),
  folder('boards', [file('plan.canvas', 'boards/plan.canvas'), file('notes.md', 'boards/notes.md')]),
]

const CANVAS = JSON.stringify({
  nodes: [{ id: 'n1', type: 'text', x: 0, y: 0, width: 250, height: 160, text: '第一张卡片' }],
  edges: [],
})

/** VueWrapper 与 DOMWrapper 都能当查找范围 */
type Scope = Pick<DOMWrapper<Element>, 'findAll'>
const button = (scope: Scope, label: string) =>
  scope.findAll('button').find(item => item.text() === label)!

beforeEach(() => {
  localStorage.clear()
  resetToasts()
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.mocked(FileService.GetFileTree).mockResolvedValue(TREE as never)
  // 只有 boards/plan.canvas 有内容；新建出来的画布是空文件
  vi.mocked(FileService.ReadFile).mockImplementation(
    (_ws, path) => Promise.resolve(path === 'boards/plan.canvas' ? CANVAS : '') as never,
  )
  vi.mocked(FileService.CreateFile).mockResolvedValue(null)
  vi.mocked(FileService.RenameFile).mockResolvedValue(null)
  vi.mocked(FileService.DeleteFile).mockResolvedValue(undefined)
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
})
afterEach(() => {
  resetToasts()
  vi.useRealTimers()
})

async function renderCanvas(query: Record<string, string> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/canvas', component: CanvasView },
    { path: '/editor', component: { template: '<div />' } },
  ] })
  await router.push({ path: '/canvas', query })
  const wrapper = mount(CanvasView, { global: { plugins: [pinia, router, i18n], stubs: { Teleport: true } } })
  await flushPromises()
  return { wrapper, router, workspace }
}

const toastMessages = () => useToast().toasts.value.map(item => item.message)

describe('CanvasView list', () => {
  it('lists every canvas file in the workspace, including nested ones', async () => {
    const { wrapper } = await renderCanvas()
    expect(FileService.GetFileTree).toHaveBeenCalledWith('/vault')
    expect(wrapper.get('.page-header h1').text()).toBe('画布')
    // 只收 .canvas；其它笔记不出现，按名称排序
    expect(wrapper.findAll('.canvas-card-name').map(item => item.text())).toEqual(['a.canvas', 'plan.canvas'])
    expect(button(wrapper, '新建画布')).toBeDefined()
  })

  it('shows the empty state when the workspace has no canvas yet', async () => {
    vi.mocked(FileService.GetFileTree).mockResolvedValue(TREE.slice(0, 1) as never)
    const { wrapper } = await renderCanvas()
    const empty = wrapper.get('.empty-state')
    expect(empty.get('.empty-title').text()).toBe('还没有画布')
    expect(empty.get('.empty-hint').text()).toContain('创建你的第一张白板')
    expect(empty.findAll('button').map(item => item.text())).toEqual(['新建画布'])
  })

  it('shows the loading hint until the file tree arrives', async () => {
    let finish!: (value: RawFileNode[]) => void
    vi.mocked(FileService.GetFileTree).mockReturnValue(new Promise(resolve => { finish = resolve }) as never)
    const { wrapper } = await renderCanvas()
    expect(wrapper.get('.list-hint').text()).toBe('加载中...')
    expect(wrapper.find('.empty-state').exists()).toBe(false)

    finish(TREE)
    await flushPromises()
    expect(wrapper.find('.list-hint').exists()).toBe(false)
    expect(wrapper.findAll('.canvas-card')).toHaveLength(2)
  })

  it('reports a failed file tree as a toast and leaves the page usable', async () => {
    vi.mocked(FileService.GetFileTree).mockRejectedValue(new Error('权限不足'))
    const { wrapper } = await renderCanvas()
    expect(toastMessages()).toEqual(['画布解析失败：权限不足'])
    expect(wrapper.find('.list-hint').exists()).toBe(false)
    expect(button(wrapper, '新建画布')).toBeDefined()
  })

  it('opens a canvas from the list and returns to the list afterwards', async () => {
    const { wrapper, router } = await renderCanvas()
    await wrapper.findAll('.canvas-card')[1]!.trigger('click')
    await flushPromises()

    expect(FileService.ReadFile).toHaveBeenCalledWith('/vault', 'boards/plan.canvas')
    expect(router.currentRoute.value.fullPath).toBe('/canvas?file=boards/plan.canvas')
    expect(wrapper.get('.canvas-name').text()).toBe('plan.canvas')
    expect(wrapper.find('.canvas-grid').exists()).toBe(false)
    expect(wrapper.get('.save-status').text()).toBe('已保存')

    vi.mocked(FileService.GetFileTree).mockClear()
    await button(wrapper, '返回列表').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/canvas')
    expect(FileService.GetFileTree).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('.canvas-card')).toHaveLength(2)
  })
})

describe('CanvasView list actions', () => {
  it('creates a canvas from the prompted name and opens it', async () => {
    vi.mocked(promptDialog).mockResolvedValue('灵感收集')
    const { wrapper, router } = await renderCanvas()
    await button(wrapper, '新建画布').trigger('click')
    await flushPromises()

    // 不带后缀时自动补 .canvas；新画布是空的 JSON Canvas
    expect(FileService.CreateFile).toHaveBeenCalledWith('/vault', '灵感收集.canvas', JSON.stringify({ nodes: [], edges: [] }, null, 2))
    expect(FileService.GetFileTree).toHaveBeenCalledTimes(2)
    expect(FileService.ReadFile).toHaveBeenLastCalledWith('/vault', '灵感收集.canvas')
    expect(wrapper.get('.canvas-name').text()).toBe('灵感收集.canvas')
    expect(router.currentRoute.value.query.file).toBe('灵感收集.canvas')
    expect(wrapper.get('.canvas-empty-hint').text()).toContain('双击空白处')
  })

  it('keeps an explicit .canvas suffix and creates nothing when the prompt is cancelled', async () => {
    const { wrapper } = await renderCanvas()
    vi.mocked(promptDialog).mockResolvedValueOnce('看板.canvas').mockResolvedValueOnce(null)
    await button(wrapper, '新建画布').trigger('click')
    await flushPromises()
    expect(FileService.CreateFile).toHaveBeenCalledWith('/vault', '看板.canvas', expect.any(String))

    vi.mocked(FileService.CreateFile).mockClear()
    await button(wrapper, '返回列表').trigger('click')
    await flushPromises()
    await button(wrapper, '新建画布').trigger('click')
    await flushPromises()
    expect(FileService.CreateFile).not.toHaveBeenCalled()
  })

  it('renames a canvas from the list', async () => {
    vi.mocked(promptDialog).mockResolvedValue('复盘计划')
    const { wrapper } = await renderCanvas()
    const plan = wrapper.findAll('.canvas-card')[1]!
    await button(plan, '重命名').trigger('click')
    await flushPromises()

    expect(promptDialog).toHaveBeenCalledWith(expect.objectContaining({ defaultValue: 'plan.canvas' }))
    expect(FileService.RenameFile).toHaveBeenCalledWith('/vault', 'boards/plan.canvas', '复盘计划.canvas')
    expect(toastMessages()).toEqual(['已重命名为「复盘计划.canvas」'])
    // 改名动作不应把这张画布打开
    expect(FileService.ReadFile).not.toHaveBeenCalled()
    expect(FileService.GetFileTree).toHaveBeenCalledTimes(2)
  })

  it('deletes a canvas only after confirmation', async () => {
    vi.mocked(confirmDialog).mockResolvedValueOnce(false).mockResolvedValueOnce(true)
    const { wrapper } = await renderCanvas()
    const plan = () => wrapper.findAll('.canvas-card')[1]!

    await button(plan(), '删除').trigger('click')
    await flushPromises()
    expect(FileService.DeleteFile).not.toHaveBeenCalled()

    await button(plan(), '删除').trigger('click')
    await flushPromises()
    expect(confirmDialog).toHaveBeenLastCalledWith(expect.objectContaining({ message: '确定删除画布「plan.canvas」吗？此操作不可撤销。', danger: true }))
    expect(FileService.DeleteFile).toHaveBeenCalledWith('/vault', 'boards/plan.canvas')
    // 取消不重拉列表；确认删除后重拉
    expect(FileService.GetFileTree).toHaveBeenCalledTimes(2)
  })
})
