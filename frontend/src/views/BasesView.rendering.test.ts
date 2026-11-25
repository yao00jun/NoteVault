// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'
import { useWorkspaceStore } from '@/stores/workspace'
import { BaseService, type BaseCell, type BaseDef, type BaseResult, type BaseSummary, type BaseView } from '@/api'
import BasesView from './BasesView.vue'

vi.mock('@/composables/useConfirm', () => ({ confirmDialog: vi.fn(), chooseDialog: vi.fn() }))
vi.mock('@/composables/usePrompt', () => ({ promptDialog: vi.fn() }))
vi.mock('@/api', () => apiMock({
  BaseService: {
    ListOperators: vi.fn(), ListTemplates: vi.fn(), ListProperties: vi.fn(), ListBases: vi.fn(),
    LoadBase: vi.fn(), NewBaseTemplate: vi.fn(), RunBase: vi.fn(), SaveBase: vi.fn(),
    DeleteBase: vi.fn(), RenameBase: vi.fn(), InvalidateCache: vi.fn(),
  },
  WorkspaceService: { GetCurrentWorkspace: vi.fn(async () => null) },
  CredentialService: { GetCredential: vi.fn(async () => '') },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))

enableAutoUnmount(afterEach)

function view(id: string, type: string): BaseView {
  return { id, name: '', type, columns: null, sort: null, groupBy: '', limit: 0 }
}

const DEF: BaseDef = {
  name: '阅读清单',
  filters: { conjunction: 'and', conditions: [], groups: [] },
  views: [view('v-table', 'table'), view('v-board', 'board'), view('v-list', 'list')],
}

const BASES: BaseSummary[] = [
  { name: '阅读清单', viewCount: 3, filterCount: 0, updatedAt: '' },
  { name: '待办汇总', viewCount: 1, filterCount: 2, updatedAt: '' },
]

/** PropKind 是枚举，而 @/api 被整体 mock 取不到枚举值：造数据时写字面量再断言回去 */
const kind = (value: string) => value as BaseCell['kind']
const cell = (property: string, display: string, extra: Partial<BaseCell> = {}): BaseCell =>
  ({ property, kind: kind('string'), display, ...extra })

const ROWS: BaseResult['rows'] = [
  { path: 'Books/go.md', title: 'Go 语言圣经', cells: [
    cell('file.title', 'Go 语言圣经'), cell('status', '在读'),
    cell('tags', 'go, 语言', { kind: kind('list'), list: ['go', '语言'] }),
    cell('rating', '5', { kind: kind('number'), num: 5 }), cell('done', 'true', { kind: kind('bool'), bool: true }),
  ] },
  { path: 'Books/rust.md', title: 'Rust 程序设计', cells: [
    cell('file.title', 'Rust 程序设计'), cell('status', ''),
    cell('tags', '', { kind: kind('list'), list: null }),
    cell('rating', '', { kind: kind('number'), empty: true }), cell('done', 'false', { kind: kind('bool'), bool: false }),
  ] },
]

const TABLE_RESULT: BaseResult = {
  viewId: 'v-table', viewName: '表格', viewType: 'table',
  columns: ['file.title', 'status', 'tags', 'rating', 'done'],
  rows: ROWS, groups: null, total: 2, returned: 2, truncated: false, scanned: 42, warnings: null,
}

const RESULTS: Record<string, BaseResult> = {
  'v-table': TABLE_RESULT,
  'v-board': {
    ...TABLE_RESULT, viewId: 'v-board', viewName: '看板', viewType: 'board',
    groups: [
      { key: 'reading', label: '在读', count: 1, rows: [ROWS[0]] },
      { key: 'none', label: '未分类', count: 1, rows: [ROWS[1]] },
    ],
  },
  'v-list': { ...TABLE_RESULT, viewId: 'v-list', viewName: '列表', viewType: 'list' },
}

const run = vi.mocked(BaseService.RunBase)

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  vi.mocked(BaseService.ListOperators).mockResolvedValue(['contains', 'empty'])
  vi.mocked(BaseService.ListTemplates).mockResolvedValue([])
  vi.mocked(BaseService.ListProperties).mockResolvedValue([
    { name: 'file.title', kind: kind('string'), count: 2, implicit: true, samples: null },
    { name: 'status', kind: kind('string'), count: 1, implicit: false, samples: null },
  ])
  vi.mocked(BaseService.ListBases).mockResolvedValue(BASES)
  vi.mocked(BaseService.LoadBase).mockImplementation(
    () => Promise.resolve(JSON.parse(JSON.stringify(DEF))) as never,
  )
  vi.mocked(BaseService.InvalidateCache).mockResolvedValue(undefined)
  run.mockReset()
  run.mockImplementation(
    (_ws, _def, viewId) => Promise.resolve(RESULTS[viewId] ?? null) as never,
  )
})

async function renderBases(query: Record<string, string> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/bases', component: BasesView },
    { path: '/editor', component: { template: '<div />' } },
  ] })
  await router.push({ path: '/bases', query })
  const wrapper = mount(BasesView, { global: { plugins: [pinia, router, i18n] } })
  await flushPromises()
  return { wrapper, router, workspace }
}

/** VueWrapper 与 DOMWrapper 都能当查找范围 */
type Scope = { findAll(selector: string): DOMWrapper<Element>[] }
const texts = (scope: Scope, selector: string) => scope.findAll(selector).map(item => item.text())
const lastRunViewId = () => run.mock.calls.at(-1)?.[2]

describe('BasesView loading and table rendering', () => {
  it('shows the loading state until the first base has been opened', async () => {
    let finish!: (value: BaseSummary[]) => void
    vi.mocked(BaseService.ListBases).mockReturnValue(new Promise(resolve => { finish = resolve }) as never)
    const { wrapper } = await renderBases()
    expect(wrapper.get('.result-area').text()).toBe('加载中...')
    expect(wrapper.find('[data-testid="bases-table"]').exists()).toBe(false)

    finish(BASES)
    await flushPromises()
    expect(wrapper.get('.result-area').text()).not.toContain('加载中')
    expect(wrapper.findAll('[data-testid="bases-tr"]')).toHaveLength(2)
  })

  it('renders the saved bases, then the default table with typed cells', async () => {
    const { wrapper } = await renderBases()
    expect(BaseService.LoadBase).toHaveBeenCalledWith('/vault', '阅读清单')
    expect(run).toHaveBeenCalledTimes(1)
    expect(run.mock.calls[0]).toEqual(['/vault', expect.any(Object), 'v-table'])

    const items = wrapper.findAll('[data-testid="bases-item"]')
    expect(items.map(item => item.get('.rail-item-name').text())).toEqual(['阅读清单', '待办汇总'])
    expect(items.map(item => item.get('.rail-item-meta').text())).toEqual(['0 个条件', '2 个条件'])
    expect(items[0]!.classes()).toContain('active')
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '阅读清单')
    const rows = wrapper.findAll('[data-testid="bases-tr"]')
    // 内置属性显示本地化标签，自定义属性原样显示
    expect(texts(wrapper, '[data-testid="bases-th"]')).toEqual(['标题', 'status', '标签', 'rating', 'done'])
    expect(wrapper.findAll('[data-testid="bases-th"]')[0]!.attributes('title')).toBe('file.title')
    expect(rows[0]!.text()).toContain('Go 语言圣经')
    expect(rows[0]!.text()).toContain('在读')
    expect(texts(rows[0]!, '.tag-chip')).toEqual(['go', '语言'])
    // 布尔画勾 / 中点，数字列带右对齐标记，空值置灰
    expect(rows[0]!.findAll('td').map(td => td.text())).toEqual(['Go 语言圣经', '在读', 'go语言', '5', '✓'])
    expect(rows[1]!.findAll('td').map(td => td.text())).toEqual(['Rust 程序设计', '', '', '', '·'])
    expect(rows[0]!.findAll('td')[3]!.classes()).toContain('numeric')
    expect(rows[1]!.findAll('td')[3]!.classes()).toContain('dim')

    expect(wrapper.get('[data-testid="bases-meta"]').text()).toContain('2 / 2 条')
    expect(wrapper.get('[data-testid="bases-meta"]').text()).toContain('已扫描 42 篇笔记')
    expect(wrapper.find('[data-testid="bases-truncated"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="bases-warnings"]').exists()).toBe(false)
  })

  it('lists backend warnings and the truncation notice with the result', async () => {
    run.mockResolvedValue({
      ...TABLE_RESULT, returned: 2, total: 500, truncated: true,
      warnings: ['属性 foo 未找到', '正则 "(" 无效'],
    })
    const { wrapper } = await renderBases()
    expect(texts(wrapper, '[data-testid="bases-warnings"] li')).toEqual(['属性 foo 未找到', '正则 "(" 无效'])
    expect(wrapper.get('[data-testid="bases-meta"]').text()).toContain('2 / 500 条')
    expect(wrapper.get('[data-testid="bases-truncated"]').text()).toContain('结果已截断')
  })

  it('shows the empty state when the query matches nothing', async () => {
    run.mockResolvedValue({ ...TABLE_RESULT, rows: [], total: 0, returned: 0 })
    const { wrapper } = await renderBases()
    const empty = wrapper.get('[data-testid="bases-empty"]')
    expect(empty.text()).toContain('没有匹配的笔记')
    expect(empty.text()).toContain('试着放宽筛选条件')
    expect(wrapper.find('[data-testid="bases-table"]').exists()).toBe(false)
  })

  it('opens the requested base from the ?base= deep link instead of the first one', async () => {
    await renderBases({ base: '待办汇总' })
    expect(BaseService.LoadBase).toHaveBeenCalledTimes(1)
    expect(BaseService.LoadBase).toHaveBeenCalledWith('/vault', '待办汇总')
  })

  it('navigates to the editor when a row is clicked', async () => {
    const { wrapper, router, workspace } = await renderBases()
    await wrapper.findAll('[data-testid="bases-tr"]')[1]!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/editor?file=Books/rust.md')
    expect(workspace.activeFile).toBe('Books/rust.md')
  })
})

describe('BasesView view switching', () => {
  it('renders board columns with their cards after switching to the board tab', async () => {
    const { wrapper } = await renderBases()
    expect(texts(wrapper, '[data-testid="bases-view-tab"]')).toEqual(['表格', '看板', '列表'])

    const tabs = wrapper.findAll('[data-testid="bases-view-tab"]')
    await tabs[1]!.trigger('click')
    await flushPromises()
    expect(lastRunViewId()).toBe('v-board')
    expect(tabs[1]!.classes()).toContain('active')

    const board = wrapper.get('[data-testid="bases-board"]')
    expect(texts(board, '.board-label')).toEqual(['在读', '未分类'])
    expect(texts(board, '.board-count')).toEqual(['1', '1'])
    const columns = board.findAll('[data-testid="bases-board-column"]')
    expect(columns[0]!.get('[data-testid="bases-board-card"]').text()).toContain('Go 语言圣经')
    expect(texts(columns[0]!, '.tag-chip')).toEqual(['go', '语言'])
    expect(columns[1]!.get('[data-testid="bases-board-card"]').text()).toContain('Rust 程序设计')
    expect(wrapper.find('[data-testid="bases-table"]').exists()).toBe(false)
  })

  it('renders flat rows after switching to the list tab, and opens a row from the board', async () => {
    const { wrapper, router } = await renderBases()
    const tabs = wrapper.findAll('[data-testid="bases-view-tab"]')
    await tabs[2]!.trigger('click')
    await flushPromises()
    expect(lastRunViewId()).toBe('v-list')
    const list = wrapper.get('[data-testid="bases-list"]')
    expect(texts(list, '.list-title')).toEqual(['Go 语言圣经', 'Rust 程序设计'])
    expect(texts(list.findAll('[data-testid="bases-list-row"]')[0]!, '.tag-chip')).toEqual(['go', '语言'])
    expect(wrapper.find('[data-testid="bases-table"]').exists()).toBe(false)

    await tabs[1]!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="bases-board-card"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/editor?file=Books/go.md')
  })
})

describe('BasesView failure states', () => {
  it('surfaces a failed query inline and drops the previous result', async () => {
    const { wrapper } = await renderBases()
    expect(wrapper.findAll('[data-testid="bases-tr"]')).toHaveLength(2)
    run.mockRejectedValue(new Error('正则表达式非法'))
    await wrapper.findAll('[data-testid="bases-view-tab"]')[1]!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="bases-error"]').text()).toContain('执行查询失败：正则表达式非法')
    expect(wrapper.find('[data-testid="bases-tr"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="bases-meta"]').exists()).toBe(false)
    // 查询失败 ≠ 没有匹配：不能误报空态
    expect(wrapper.find('[data-testid="bases-empty"]').exists()).toBe(false)
  })

  it('reports a base that cannot be opened without running a query', async () => {
    vi.mocked(BaseService.LoadBase).mockRejectedValue(new Error('文件被占用'))
    const { wrapper } = await renderBases()
    expect(wrapper.get('[data-testid="bases-error"]').text()).toContain('打开视图失败：文件被占用')
    expect(BaseService.RunBase).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="bases-tr"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="bases-name"]').exists()).toBe(false)
    expect(wrapper.get('.result-area').text()).not.toContain('加载中')
  })

  it('asks for a workspace when none is selected and the backend has none either', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bases', component: BasesView }] })
    await router.push('/bases')
    const wrapper = mount(BasesView, { global: { plugins: [pinia, router, i18n] } })
    await flushPromises()
    const empty = wrapper.get('[data-testid="bases-no-workspace"]')
    expect(empty.text()).toContain('尚未选择工作区')
    expect(empty.text()).toContain('请先在欢迎页或侧边栏选择一个笔记工作区')
    expect(BaseService.ListBases).not.toHaveBeenCalled()
  })
})
