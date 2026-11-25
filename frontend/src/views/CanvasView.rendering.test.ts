// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
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
    { id: 'n1', type: 'text', x: 0, y: 0, width: 250, height: 160, text: '# 标题\n\n正文 **加粗**' },
    { id: 'n2', type: 'file', x: 320, y: 0, width: 250, height: 80, file: 'Projects/A/计划.md' },
    { id: 'n3', type: 'link', x: 0, y: 240, width: 250, height: 80, url: 'https://example.com', color: 'green' },
    { id: 'g1', type: 'group', x: 0, y: 360, width: 400, height: 240, label: '第一周' },
  ],
  edges: [
    { id: 'e1', fromNode: 'n1', toNode: 'n2' },
    { id: 'e2', fromNode: 'n3', toNode: 'n1', fromSide: 'top', toSide: 'bottom', color: 'red' },
  ],
}

beforeEach(() => {
  localStorage.clear()
  resetToasts()
  vi.clearAllMocks()
  vi.mocked(FileService.GetFileTree).mockResolvedValue([])
  vi.mocked(FileService.ReadFile).mockResolvedValue(JSON.stringify(CANVAS))
  vi.mocked(FileService.SaveFile).mockResolvedValue(undefined)
})
afterEach(() => resetToasts())

async function renderCanvas(query: Record<string, string> = { file: PATH }) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore().setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/canvas', component: CanvasView }] })
  await router.push({ path: '/canvas', query })
  const wrapper = mount(CanvasView, { global: { plugins: [pinia, router, i18n], stubs: { Teleport: true } } })
  await flushPromises()
  return { wrapper, router }
}

/** VueWrapper 与 DOMWrapper 都能当查找范围 */
type Scope = Pick<DOMWrapper<Element>, 'get' | 'findAll'>
const node = (scope: Scope, id: string) => scope.get(`[data-node-id="${id}"]`)
const button = (scope: Scope, label: string) =>
  scope.findAll('button').find(item => item.text().trim() === label)!

describe('CanvasView node rendering', () => {
  it('loads the canvas named in the route and renders each kind of card', async () => {
    const { wrapper, router } = await renderCanvas()
    expect(FileService.ReadFile).toHaveBeenCalledWith('/vault', PATH)
    expect(wrapper.get('.canvas-name').text()).toBe('plan.canvas')
    expect(router.currentRoute.value.query.file).toBe(PATH)
    expect(wrapper.get('.save-status').text()).toBe('已保存')

    // 文字卡片走 Markdown 渲染
    const text = node(wrapper, 'n1')
    expect(text.classes()).toContain('node-text')
    expect(text.get('.node-badge').text()).toBe('文字')
    expect(text.get('.node-text h1').text()).toContain('标题')
    expect(text.get('.node-text strong').text()).toBe('加粗')
    expect(text.find('textarea').exists()).toBe(false)

    const file = node(wrapper, 'n2')
    expect(file.classes()).toContain('node-file')
    expect(file.get('.node-badge').text()).toBe('笔记')
    expect(file.get('.node-file-path').text()).toBe('Projects/A/计划.md')
    expect(button(file, '打开笔记')).toBeDefined()
    expect(file.find('input').exists()).toBe(false)

    const link = node(wrapper, 'n3')
    expect(link.get('.node-badge').text()).toBe('链接')
    expect(link.get('.node-link-url').text()).toBe('https://example.com')
    expect(button(link, '打开')).toBeDefined()
    // 彩色卡片带左侧色条
    expect(link.attributes('style')).toContain('border-left: 3px solid rgb(70, 167, 88)')

    const group = node(wrapper, 'g1')
    expect(group.classes()).toContain('node-group')
    expect(group.get('.group-label').text()).toBe('第一周')
    expect(group.find('.node-badge').exists()).toBe(false)

    expect(wrapper.find('.canvas-error').exists()).toBe(false)
    expect(wrapper.find('.canvas-empty-hint').exists()).toBe(false)
  })

  it('places cards at their stored position and size', async () => {
    const { wrapper } = await renderCanvas()
    expect(node(wrapper, 'n2').attributes('style')).toBe('left: 320px; top: 0px; width: 250px; height: 80px;')
    expect(node(wrapper, 'g1').attributes('style')).toContain('left: 0px; top: 360px; width: 400px; height: 240px;')
  })

  it('draws an edge between its two cards with the stored colour', async () => {
    const { wrapper } = await renderCanvas()
    const lines = wrapper.findAll('.edge-line')
    // 默认锚点：起点右侧中点 → 终点左侧中点
    expect(lines[0]!.attributes('d')).toBe('M 250 80 L 320 40')
    // 指定锚边：起点顶边中点 → 终点底边中点
    expect(lines[1]!.attributes('d')).toBe('M 125 240 L 125 160')
    expect(lines[1]!.attributes('style')).toContain('stroke: rgb(229, 72, 77)')
    expect(lines[0]!.attributes('style') ?? '').not.toContain('stroke')
    expect(lines[0]!.attributes('marker-end')).toBe('url(#canvas-arrow)')
    expect(wrapper.findAll('.edge-hit')).toHaveLength(2)
    expect(wrapper.find('.edge-temp').exists()).toBe(false)
  })

  it('shows the empty-canvas hint for a canvas without cards', async () => {
    vi.mocked(FileService.ReadFile).mockResolvedValue('')
    const { wrapper } = await renderCanvas()
    expect(wrapper.get('.canvas-empty-hint').text()).toBe('双击空白处，或点击上方按钮添加卡片')
    expect(wrapper.findAll('.node')).toHaveLength(0)
    expect(wrapper.get('.canvas-name').text()).toBe('plan.canvas')
  })

  it('switches to another canvas when the route points at it', async () => {
    const { wrapper, router } = await renderCanvas()
    expect(wrapper.get('.canvas-name').text()).toBe('plan.canvas')
    vi.mocked(FileService.ReadFile).mockResolvedValue(JSON.stringify({
      nodes: [{ id: 'x1', type: 'link', x: 10, y: 10, width: 250, height: 80, url: 'https://other.example' }],
      edges: [],
    }))
    await router.replace({ path: '/canvas', query: { file: 'other.canvas' } })
    await flushPromises()

    expect(FileService.ReadFile).toHaveBeenLastCalledWith('/vault', 'other.canvas')
    expect(wrapper.get('.canvas-name').text()).toBe('other.canvas')
    expect(wrapper.get('.node-link-url').text()).toBe('https://other.example')
    expect(wrapper.find('[data-node-id="n1"]').exists()).toBe(false)
    expect(wrapper.findAll('.edge-line')).toHaveLength(0)
  })
})

describe('CanvasView view controls', () => {
  it('zooms out, in and back to 100% from the toolbar', async () => {
    const { wrapper } = await renderCanvas()
    const label = () => wrapper.get('.zoom-label').text()
    const world = () => wrapper.get('.canvas-world').attributes('style')!
    expect(label()).toBe('100%')
    expect(world()).toContain('scale(1)')

    await wrapper.get('[title="缩小"]').trigger('click')
    expect(label()).toBe('83%')

    await wrapper.get('[title="放大"]').trigger('click')
    expect(label()).toBe('100%')

    await wrapper.get('[title="放大"]').trigger('click')
    expect(label()).toBe('120%')
    expect(world()).toContain('scale(1.2)')

    await wrapper.get('[title="重置视图"]').trigger('click')
    expect(label()).toBe('100%')
    expect(world()).toContain('translate(40px, 40px) scale(1)')
  })
})
