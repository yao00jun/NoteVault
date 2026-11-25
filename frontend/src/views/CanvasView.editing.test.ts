// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'
import { useWorkspaceStore } from '@/stores/workspace'
import { confirmDialog, chooseDialog } from '@/composables/useConfirm'
import { resetToasts, useToast } from '@/composables/useToast'
import { FileService } from '@/api'
import CanvasView from './CanvasView.vue'

vi.mock('@/composables/useConfirm', () => ({ confirmDialog: vi.fn(), chooseDialog: vi.fn() }))
vi.mock('@/composables/usePrompt', () => ({ promptDialog: vi.fn() }))
vi.mock('@/api', () => apiMock({
  FileService: { GetFileTree: vi.fn(), ReadFile: vi.fn(), SaveFile: vi.fn(), CreateFile: vi.fn(), RenameFile: vi.fn(), DeleteFile: vi.fn() },
  CredentialService: { GetCredential: vi.fn(async () => '') },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))

enableAutoUnmount(afterEach)

const PATH = 'boards/plan.canvas'

const CANVAS = {
  nodes: [
    { id: 'n1', type: 'text', x: 0, y: 0, width: 250, height: 160, text: '起点' },
    { id: 'n2', type: 'file', x: 320, y: 0, width: 250, height: 80, file: 'Projects/A/计划.md' },
    { id: 'n3', type: 'link', x: 320, y: 120, width: 250, height: 80, url: 'https://example.com/doc' },
    { id: 'g1', type: 'group', x: 0, y: 360, width: 400, height: 240, label: '第一周' },
    { id: 'n4', type: 'text', x: 40, y: 400, width: 250, height: 160, text: '框内的卡片' },
  ],
  edges: [
    { id: 'e1', fromNode: 'n1', toNode: 'n2' },
    { id: 'e2', fromNode: 'n4', toNode: 'n1' },
  ],
}

beforeEach(() => {
  localStorage.clear()
  resetToasts()
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.mocked(FileService.GetFileTree).mockResolvedValue([])
  vi.mocked(FileService.ReadFile).mockResolvedValue(JSON.stringify(CANVAS))
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
})
afterEach(() => {
  resetToasts()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

async function renderCanvas(raw = JSON.stringify(CANVAS)) {
  vi.mocked(FileService.ReadFile).mockResolvedValue(raw)
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/canvas', component: CanvasView },
    { path: '/editor', component: { template: '<div />' } },
  ] })
  await router.push({ path: '/canvas', query: { file: PATH } })
  const wrapper = mount(CanvasView, { global: { plugins: [pinia, router, i18n], stubs: { Teleport: true } } })
  await flushPromises()
  return { wrapper, router, workspace }
}

type Wrapper = Awaited<ReturnType<typeof renderCanvas>>['wrapper']
/** VueWrapper 与 DOMWrapper 都能当查找范围 */
// 用 VTU 自己的 get/findAll 签名，而不是手写「返回 DOMWrapper<Element>」：
// 2.5 的 get() 返回 Omit<DOMWrapper<any>, 'exists'>，手写窄类型会让 wrapper 传不进去
type Scope = Pick<DOMWrapper<Element>, 'get' | 'findAll'>
const node = (scope: Scope, id: string) => scope.get(`[data-node-id="${id}"]`)
const cardCount = (wrapper: Wrapper) => wrapper.findAll('.node').length
const button = (scope: Scope, label: string) =>
  scope.findAll('button').find(item => item.text().trim() === label)!
const saveStatus = (wrapper: Wrapper) => wrapper.get('.save-status').text()
const saveFile = vi.mocked(FileService.SaveFile)
const savedContent = () => JSON.parse(saveFile.mock.calls.at(-1)![2]) as typeof CANVAS
const toastMessages = () => useToast().toasts.value.map(item => item.message)
/** 拖拽过程中组件在 window 上监听 mousemove / mouseup */
const mouse = (type: 'mousemove' | 'mouseup', clientX: number, clientY: number) =>
  window.dispatchEvent(new MouseEvent(type, { clientX, clientY, bubbles: true }))

describe('CanvasView adding and editing cards', () => {
  it('creates a text card by double-clicking blank space and renders what is typed', async () => {
    const { wrapper } = await renderCanvas('')
    expect(wrapper.find('.canvas-empty-hint').exists()).toBe(true)

    await wrapper.get('.canvas-surface').trigger('dblclick', { clientX: 200, clientY: 150 })
    expect(wrapper.find('.canvas-empty-hint').exists()).toBe(false)
    const editor = wrapper.get('textarea')
    expect((editor.element as HTMLTextAreaElement).value).toBe('')
    expect(wrapper.findAll('.node')).toHaveLength(1)

    await editor.setValue('# 新想法')
    await editor.trigger('blur')
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.get('.node-text h1').text()).toContain('新想法')
    expect(saveStatus(wrapper)).toBe('未保存')
  })

  it('opens an existing text card for editing on double-click and keeps a Markdown body', async () => {
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'n1').get('.node-text').trigger('dblclick')
    const editor = wrapper.get('textarea')
    expect((editor.element as HTMLTextAreaElement).value).toBe('起点')
    await editor.setValue('改过的 **正文**')
    await editor.trigger('keyup.ctrl.enter')
    expect(wrapper.get('[data-node-id="n1"] strong').text()).toBe('正文')
    expect(wrapper.find('textarea').exists()).toBe(false)
  })

  it('edits the note path of a file card inline', async () => {
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'n2').get('.node-file').trigger('dblclick')
    const input = node(wrapper, 'n2').get('input')
    expect((input.element as HTMLInputElement).value).toBe('Projects/A/计划.md')

    await input.setValue('Projects/A/复盘.md')
    await input.trigger('keyup.enter')
    expect(node(wrapper, 'n2').get('.node-file-path').text()).toBe('Projects/A/复盘.md')
    expect(node(wrapper, 'n2').find('input').exists()).toBe(false)
    expect(saveStatus(wrapper)).toBe('未保存')
  })

  it('adds cards of each kind from the toolbar and selects the new one', async () => {
    const { wrapper } = await renderCanvas('')
    expect(button(wrapper, '删除所选').attributes('disabled')).toBeDefined()

    for (const [label, type] of [['文字卡片', 'text'], ['笔记卡片', 'file'], ['链接卡片', 'link'], ['分组', 'group']] as const) {
      await button(wrapper, label).trigger('click')
      const added = wrapper.findAll('.node').filter(item => item.classes().includes(`node-${type}`))
      expect(added).toHaveLength(1)
      expect(added[0]!.classes()).toContain('selected')
    }
    expect(cardCount(wrapper)).toBe(4)
    expect(wrapper.findAll('.node.selected')).toHaveLength(1)
    expect(wrapper.find('.canvas-empty-hint').exists()).toBe(false)
    // 删除所选变为可用
    expect(button(wrapper, '删除所选').attributes('disabled')).toBeUndefined()
  })

  it('names a group by double-clicking it and typing a label', async () => {
    const { wrapper } = await renderCanvas('')
    await button(wrapper, '分组').trigger('click')
    expect(wrapper.find('.node-group input').exists()).toBe(false)

    await wrapper.get('.node-group').trigger('dblclick')
    const input = wrapper.get('.node-group input')
    await input.setValue('第二周')
    await input.trigger('keyup.enter')
    expect(wrapper.get('.node-group .group-label').text()).toBe('第二周')
    expect(wrapper.find('.node-group input').exists()).toBe(false)
  })
})

describe('CanvasView saving', () => {
  it('marks the canvas unsaved after an edit and saves it automatically after a pause', async () => {
    const { wrapper } = await renderCanvas('')
    expect(saveStatus(wrapper)).toBe('已保存')
    await button(wrapper, '笔记卡片').trigger('click')
    expect(saveStatus(wrapper)).toBe('未保存')
    await vi.advanceTimersByTimeAsync(799)
    expect(FileService.SaveFile).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(FileService.SaveFile).toHaveBeenCalledTimes(1)
    expect(saveFile.mock.calls[0]!.slice(0, 2)).toEqual(['/vault', PATH])
    // 落盘的是 JSON Canvas 结构：新卡片带默认尺寸、空路径
    expect(savedContent()).toEqual({
      nodes: [expect.objectContaining({ type: 'file', file: '', width: 250, height: 80 })],
      edges: [],
    })
    expect(saveStatus(wrapper)).toBe('已保存')
  })

  it('saves on demand from the toolbar button', async () => {
    const { wrapper } = await renderCanvas()
    await button(wrapper, '保存').trigger('click')
    await flushPromises()
    expect(FileService.SaveFile).toHaveBeenCalledTimes(1)
    expect(savedContent()).toEqual(CANVAS)
    expect(saveStatus(wrapper)).toBe('已保存')
  })

  it('shows a failed save inline and keeps the canvas marked unsaved', async () => {
    vi.mocked(FileService.SaveFile).mockRejectedValue(new Error('磁盘已满'))
    const { wrapper } = await renderCanvas()
    await button(wrapper, '笔记卡片').trigger('click')
    await button(wrapper, '保存').trigger('click')
    await flushPromises()

    expect(wrapper.get('.canvas-error').text()).toBe('磁盘已满')
    expect(saveStatus(wrapper)).toBe('未保存')
    // 编辑区仍在，内容没有丢
    expect(cardCount(wrapper)).toBe(6)
  })
})

describe('CanvasView undo and redo', () => {
  it('undoes an added card and redoes it', async () => {
    const { wrapper } = await renderCanvas()
    const undo = () => wrapper.get('[title="撤销 (Ctrl+Z)"]')
    const redo = () => wrapper.get('[title="重做 (Ctrl+Y)"]')
    expect(undo().attributes('disabled')).toBeDefined()
    expect(redo().attributes('disabled')).toBeDefined()

    await button(wrapper, '链接卡片').trigger('click')
    expect(cardCount(wrapper)).toBe(6)
    expect(undo().attributes('disabled')).toBeUndefined()

    await undo().trigger('click')
    expect(cardCount(wrapper)).toBe(5)
    expect(redo().attributes('disabled')).toBeUndefined()

    await redo().trigger('click')
    expect(cardCount(wrapper)).toBe(6)
  })

  it('restores a dragged card with Ctrl+Z', async () => {
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'n1').trigger('mousedown', { clientX: 0, clientY: 0 })
    mouse('mousemove', 150, 90)
    mouse('mouseup', 150, 90)
    await flushPromises()
    expect(node(wrapper, 'n1').attributes('style')).toContain('left: 150px; top: 90px')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'z', ctrlKey: true }))
    await flushPromises()
    expect(node(wrapper, 'n1').attributes('style')).toContain('left: 0px; top: 0px')
  })
})

describe('CanvasView deleting', () => {
  it('deletes the selected card and the edges attached to it after confirmation', async () => {
    vi.mocked(confirmDialog).mockResolvedValue(true)
    const { wrapper } = await renderCanvas()
    expect(wrapper.findAll('.edge-line')).toHaveLength(2)

    await node(wrapper, 'n1').trigger('mousedown', { clientX: 0, clientY: 0 })
    await button(wrapper, '删除所选').trigger('click')
    await flushPromises()

    expect(confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ message: '确定删除该节点吗？', danger: true }))
    expect(wrapper.find('[data-node-id="n1"]').exists()).toBe(false)
    expect(wrapper.findAll('.edge-line')).toHaveLength(0)
    expect(button(wrapper, '删除所选').attributes('disabled')).toBeDefined()
    expect(saveStatus(wrapper)).toBe('未保存')
  })

  it('keeps the card when deletion is cancelled', async () => {
    vi.mocked(confirmDialog).mockResolvedValue(false)
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'n2').trigger('mousedown', { clientX: 0, clientY: 0 })
    await button(wrapper, '删除所选').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-node-id="n2"]').exists()).toBe(true)
    expect(wrapper.findAll('.edge-line')).toHaveLength(2)
  })

  it('deletes a selected edge straight away without asking', async () => {
    const { wrapper } = await renderCanvas()
    const hit = wrapper.findAll('.edge-hit')[0]!
    await hit.trigger('mousedown', { clientX: 0, clientY: 0 })
    await button(wrapper, '删除所选').trigger('click')
    await flushPromises()

    expect(confirmDialog).not.toHaveBeenCalled()
    expect(wrapper.findAll('.edge-line')).toHaveLength(1)
    expect(cardCount(wrapper)).toBe(5)
  })

  it('deletes a group on its own when asked to keep the cards', async () => {
    vi.mocked(chooseDialog).mockResolvedValue('alt')
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'g1').trigger('mousedown', { clientX: 0, clientY: 0 })
    await button(wrapper, '删除所选').trigger('click')
    await flushPromises()

    expect(chooseDialog).toHaveBeenCalledWith(expect.objectContaining({
      message: '分组「第一周」内有 1 张卡片，要怎么删？', confirmText: '连卡片一起删', altText: '只删分组框', danger: true,
    }))
    expect(wrapper.find('[data-node-id="g1"]').exists()).toBe(false)
    expect(wrapper.find('[data-node-id="n4"]').exists()).toBe(true)
    expect(wrapper.findAll('.edge-line')).toHaveLength(2)
  })

  it('keeps a group and its cards when the delete prompt is dismissed', async () => {
    vi.mocked(chooseDialog).mockResolvedValue('dismiss')
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'g1').trigger('mousedown', { clientX: 0, clientY: 0 })
    await button(wrapper, '删除所选').trigger('click')
    await flushPromises()

    expect(chooseDialog).toHaveBeenCalledTimes(1)
    expect(cardCount(wrapper)).toBe(5)
    expect(wrapper.findAll('.edge-line')).toHaveLength(2)
    expect(saveStatus(wrapper)).toBe('已保存')
  })
})

describe('CanvasView opening cards elsewhere', () => {
  it('opens the note behind a file card in the editor', async () => {
    const { wrapper, router, workspace } = await renderCanvas()
    await button(node(wrapper, 'n2'), '打开笔记').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe('Projects/A/计划.md')
    expect(workspace.activeFile).toBe('Projects/A/计划.md')
  })

  it('warns instead of navigating when a file card has no note', async () => {
    const { wrapper, router } = await renderCanvas()
    await node(wrapper, 'n2').get('.node-file').trigger('dblclick')
    await node(wrapper, 'n2').get('input').setValue('')
    await node(wrapper, 'n2').get('input').trigger('keyup.enter')
    await button(node(wrapper, 'n2'), '打开笔记').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.fullPath).toBe('/canvas?file=' + PATH)
    expect(toastMessages()).toEqual(['笔记路径为空或文件不存在'])
  })

  it('opens the link behind a link card in a new browser context', async () => {
    const open = vi.fn()
    vi.stubGlobal('open', open)
    const { wrapper, router } = await renderCanvas()
    await button(node(wrapper, 'n3'), '打开').trigger('click')
    expect(open).toHaveBeenCalledWith('https://example.com/doc', '_blank', 'noopener')
    expect(router.currentRoute.value.query.file).toBe(PATH)
  })
})
