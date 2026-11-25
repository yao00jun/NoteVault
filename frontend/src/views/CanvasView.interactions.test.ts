// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'
import { useWorkspaceStore } from '@/stores/workspace'
import { resetToasts } from '@/composables/useToast'
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
    { id: 'g1', type: 'group', x: 0, y: 360, width: 400, height: 240, label: '第一周' },
    { id: 'n4', type: 'text', x: 40, y: 400, width: 250, height: 160, text: '框内的卡片' },
  ],
  edges: [{ id: 'e1', fromNode: 'n1', toNode: 'n2' }],
}

const originalElementFromPoint = document.elementFromPoint

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
  // jsdom 没有 elementFromPoint 的布局实现：还原成原样，避免污染其它文件
  document.elementFromPoint = originalElementFromPoint
  resetToasts()
  vi.useRealTimers()
})

async function renderCanvas() {
  const pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore().setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/canvas', component: CanvasView }] })
  await router.push({ path: '/canvas', query: { file: PATH } })
  const wrapper = mount(CanvasView, { global: { plugins: [pinia, router, i18n], stubs: { Teleport: true } } })
  await flushPromises()
  return { wrapper }
}

type Wrapper = Awaited<ReturnType<typeof renderCanvas>>['wrapper']
const node = (wrapper: Wrapper, id: string) => wrapper.get(`[data-node-id="${id}"]`)
const position = (wrapper: Wrapper, id: string) => node(wrapper, id).attributes('style')!
const saveStatus = (wrapper: Wrapper) => wrapper.get('.save-status').text()

/** 拖拽过程中组件在 window 上监听 mousemove / mouseup */
const mouse = (type: 'mousemove' | 'mouseup', clientX: number, clientY: number) =>
  window.dispatchEvent(new MouseEvent(type, { clientX, clientY, bubbles: true }))
const drag = async (wrapper: Wrapper, selector: string, dx: number, dy: number) => {
  await wrapper.get(selector).trigger('mousedown', { clientX: 0, clientY: 0 })
  mouse('mousemove', dx, dy)
  mouse('mouseup', dx, dy)
  await flushPromises()
}
/** 松开鼠标时指针落在哪个卡片上 */
const dropOn = (element: Element | null) => { document.elementFromPoint = vi.fn(() => element) }

describe('CanvasView selecting and dragging', () => {
  it('selects a card on mouse down, moves it while dragging and stops on mouse up', async () => {
    const { wrapper } = await renderCanvas()
    expect(node(wrapper, 'n1').classes()).not.toContain('selected')
    expect(position(wrapper, 'n1')).toContain('left: 0px; top: 0px')

    await node(wrapper, 'n1').trigger('mousedown', { clientX: 10, clientY: 10 })
    expect(node(wrapper, 'n1').classes()).toContain('selected')
    mouse('mousemove', 130, 60)
    await flushPromises()
    expect(position(wrapper, 'n1')).toContain('left: 120px; top: 50px')
    expect(saveStatus(wrapper)).toBe('未保存')

    mouse('mouseup', 130, 60)
    mouse('mousemove', 500, 500)
    await flushPromises()
    expect(position(wrapper, 'n1')).toContain('left: 120px; top: 50px')

    // 按下空白处取消选中
    await wrapper.get('.canvas-surface').trigger('mousedown', { clientX: 900, clientY: 900 })
    expect(node(wrapper, 'n1').classes()).not.toContain('selected')
  })

  it('moves the cards inside a group along with the group', async () => {
    const { wrapper } = await renderCanvas()
    await drag(wrapper, '[data-node-id="g1"]', 100, 40)
    expect(position(wrapper, 'g1')).toContain('left: 100px; top: 400px')
    expect(position(wrapper, 'n4')).toContain('left: 140px; top: 440px')
    // 框外的卡片不动
    expect(position(wrapper, 'n1')).toContain('left: 0px; top: 0px')
  })

  it('resizes a card from its corner handle', async () => {
    const { wrapper } = await renderCanvas()
    await drag(wrapper, '[data-node-id="n2"] .node-resize', 100, 20)
    expect(position(wrapper, 'n2')).toContain('left: 320px; top: 0px')
    expect(position(wrapper, 'n2')).toContain('width: 350px; height: 100px')
  })

  it('zooms with the mouse wheel', async () => {
    const { wrapper } = await renderCanvas()
    const wheel = (deltaY: number) => {
      wrapper.get('.canvas-surface').element.dispatchEvent(new WheelEvent('wheel', { deltaY, clientX: 0, clientY: 0, bubbles: true, cancelable: true }))
    }
    wheel(-100)
    await nextTick()
    expect(wrapper.get('.zoom-label').text()).toBe('110%')
    wheel(100)
    wheel(100)
    await nextTick()
    expect(wrapper.get('.zoom-label').text()).toBe('91%')
  })
})

describe('CanvasView connecting cards', () => {
  it('draws a preview line while dragging from a port and adds the edge on drop', async () => {
    const { wrapper } = await renderCanvas()
    expect(wrapper.findAll('.edge-line')).toHaveLength(1)

    await node(wrapper, 'n2').get('.node-port').trigger('mousedown', { clientX: 560, clientY: 40 })
    expect(node(wrapper, 'n2').classes()).toContain('connecting')
    expect(wrapper.find('.edge-temp').exists()).toBe(true)

    mouse('mousemove', 300, 60)
    await nextTick()
    // 预览线从起点卡片右侧中点拉到指针处
    expect(wrapper.get('.edge-temp').attributes('d')).toBe('M 570 40 L 260 20')

    dropOn(node(wrapper, 'n1').element)
    mouse('mouseup', 300, 60)
    await flushPromises()
    expect(wrapper.find('.edge-temp').exists()).toBe(false)
    expect(node(wrapper, 'n2').classes()).not.toContain('connecting')
    expect(wrapper.findAll('.edge-line')).toHaveLength(2)
    // 新连线：终点落在目标卡片左侧中点
    expect(wrapper.findAll('.edge-line')[1]!.attributes('d')).toBe('M 570 40 L 0 80')
    expect(saveStatus(wrapper)).toBe('未保存')
  })

  it('adds no edge when the drag ends on blank space, on the same card, or duplicates an existing edge', async () => {
    const { wrapper } = await renderCanvas()
    await node(wrapper, 'n1').get('.node-port').trigger('mousedown', { clientX: 240, clientY: 80 })
    dropOn(null)
    mouse('mouseup', 600, 300)
    await flushPromises()
    expect(wrapper.findAll('.edge-line')).toHaveLength(1)

    await node(wrapper, 'n2').get('.node-port').trigger('mousedown', { clientX: 560, clientY: 40 })
    dropOn(node(wrapper, 'n2').element)
    mouse('mouseup', 600, 300)
    await flushPromises()
    expect(wrapper.findAll('.edge-line')).toHaveLength(1)

    // n1 → n2 已经连过
    await node(wrapper, 'n1').get('.node-port').trigger('mousedown', { clientX: 240, clientY: 80 })
    dropOn(node(wrapper, 'n2').element)
    mouse('mouseup', 600, 300)
    await flushPromises()
    expect(wrapper.findAll('.edge-line')).toHaveLength(1)
    expect(wrapper.find('.edge-temp').exists()).toBe(false)
  })

  it('marks a clicked edge as selected', async () => {
    const { wrapper } = await renderCanvas()
    const hit = wrapper.get('.edge-hit')
    await hit.trigger('mousedown', { clientX: 285, clientY: 60 })
    expect(wrapper.get('.edge-line').classes()).toContain('selected')
  })
})
