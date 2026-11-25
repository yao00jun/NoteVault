// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'
import { useWorkspaceStore } from '@/stores/workspace'
import { BaseService, type BaseCell, type BaseDef, type BaseResult, type BaseView } from '@/api'
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
  views: [view('v-table', 'table'), view('v-board', 'board')],
}

/** PropKind 是枚举，而 @/api 被整体 mock 取不到枚举值：造数据时写字面量再断言回去 */
const kind = (value: string) => value as BaseCell['kind']

const RESULT: BaseResult = {
  viewId: 'v-table', viewName: '表格', viewType: 'table',
  columns: ['file.title', 'status'],
  rows: [{ path: 'Books/go.md', title: 'Go 语言圣经', cells: [
    { property: 'file.title', kind: kind('string'), display: 'Go 语言圣经' },
    { property: 'status', kind: kind('string'), display: '在读' },
  ] }],
  groups: null, total: 1, returned: 1, truncated: false, scanned: 12, warnings: null,
}

const run = vi.mocked(BaseService.RunBase)

/** 最近一次提交给后端的定义。def 是响应式代理，留存调用参数会被后续编辑改写，故取快照 */
function lastSubmitted(): BaseDef {
  const call = run.mock.calls.at(-1)
  if (!call) throw new Error('RunBase was never called')
  return JSON.parse(JSON.stringify(call[1])) as BaseDef
}

const settle = async () => {
  // 先把事件处理里的同步修改落到 DOM，再推进防抖定时器
  await flushPromises()
  await vi.advanceTimersByTimeAsync(220)
  await flushPromises()
}

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  // 只伪造防抖用的 setTimeout：flushPromises 走 setImmediate，保持真实才不会挂起
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.mocked(BaseService.ListOperators).mockResolvedValue(['contains', 'notEmpty', 'empty'])
  vi.mocked(BaseService.ListTemplates).mockResolvedValue([])
  vi.mocked(BaseService.ListProperties).mockResolvedValue([
    { name: 'file.title', kind: kind('string'), count: 2, implicit: true, samples: null },
    { name: 'status', kind: kind('string'), count: 1, implicit: false, samples: null },
  ])
  vi.mocked(BaseService.ListBases).mockResolvedValue([{ name: '阅读清单', viewCount: 2, filterCount: 0, updatedAt: '' }])
  vi.mocked(BaseService.LoadBase).mockImplementation(
    () => Promise.resolve(JSON.parse(JSON.stringify(DEF))) as never,
  )
  run.mockReset()
  run.mockResolvedValue(RESULT)
})
afterEach(() => vi.useRealTimers())

async function renderBases() {
  const pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore().setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/bases', component: BasesView }] })
  await router.push('/bases')
  const wrapper = mount(BasesView, { global: { plugins: [pinia, router, i18n] } })
  await flushPromises()
  run.mockClear()
  return { wrapper }
}

async function addCondition(wrapper: Awaited<ReturnType<typeof renderBases>>['wrapper']) {
  await wrapper.get('[data-testid="bases-add-condition"]').trigger('click')
}

describe('BasesView condition builder', () => {
  it('adds a condition on the first property and re-runs the query after the debounce', async () => {
    const { wrapper } = await renderBases()
    expect(wrapper.find('[data-testid="bases-condition"]').exists()).toBe(false)

    await addCondition(wrapper)
    const condition = wrapper.get('[data-testid="bases-condition"]')
    const property = condition.get('[data-testid="bases-cond-property"]')
    expect(property.findAll('option').map(option => option.text())).toEqual(['标题', 'status (1)'])
    expect((property.element as HTMLSelectElement).value).toBe('file.title')
    expect(condition.findAll('[data-testid="bases-cond-operator"] option').map(option => option.text()))
      .toEqual(['包含', '不为空', '为空'])

    // 防抖：编辑后不是立刻重跑
    expect(run).not.toHaveBeenCalled()
    await settle()
    expect(run).toHaveBeenCalledTimes(1)
    expect(lastSubmitted().filters).toEqual({ conjunction: 'and', conditions: [{ property: 'file.title', operator: 'contains', value: '' }], groups: [] })
  })

  it('sends the typed value with the re-run query', async () => {
    const { wrapper } = await renderBases()
    await addCondition(wrapper)
    await wrapper.get('[data-testid="bases-cond-value"]').setValue('Go')
    await settle()
    expect(lastSubmitted().filters?.conditions).toEqual([{ property: 'file.title', operator: 'contains', value: 'Go' }])
  })

  it('collapses a burst of typing into a single re-run', async () => {
    const { wrapper } = await renderBases()
    await addCondition(wrapper)
    await settle()
    expect(run).toHaveBeenCalledTimes(1)
    run.mockClear()

    const input = wrapper.get('[data-testid="bases-cond-value"]')
    await input.setValue('g')
    await input.setValue('go')
    await input.setValue('gop')
    expect(run).not.toHaveBeenCalled()

    await settle()
    expect(run).toHaveBeenCalledTimes(1)
    expect(lastSubmitted().filters?.conditions?.[0]?.value).toBe('gop')
  })

  it('removes the value box for operators that need no value', async () => {
    const { wrapper } = await renderBases()
    await addCondition(wrapper)
    await wrapper.get('[data-testid="bases-cond-value"]').setValue('Go')
    await settle()

    await wrapper.get('[data-testid="bases-cond-operator"]').setValue('empty')
    const condition = wrapper.get('[data-testid="bases-condition"]')
    expect(condition.find('[data-testid="bases-cond-value"]').exists()).toBe(false)
    expect(condition.get('.value-na').text()).toBe('—')
    await settle()
    expect(lastSubmitted().filters?.conditions).toEqual([{ property: 'file.title', operator: 'empty', value: '' }])

    // 切回需要值的运算符：输入框恢复
    await wrapper.get('[data-testid="bases-cond-operator"]').setValue('contains')
    expect(condition.find('[data-testid="bases-cond-value"]').exists()).toBe(true)
  })

  it('removes a condition and re-runs without it', async () => {
    const { wrapper } = await renderBases()
    await addCondition(wrapper)
    await settle()
    expect(wrapper.findAll('[data-testid="bases-condition"]')).toHaveLength(1)

    await wrapper.get('[data-testid="bases-remove-condition"]').trigger('click')
    expect(wrapper.find('[data-testid="bases-condition"]').exists()).toBe(false)
    await settle()
    expect(lastSubmitted().filters?.conditions).toEqual([])
  })

  it('switches the condition conjunction to any', async () => {
    const { wrapper } = await renderBases()
    expect(wrapper.get('[data-testid="bases-conj-and"]').classes()).toContain('active')
    await wrapper.get('[data-testid="bases-conj-or"]').trigger('click')
    expect(wrapper.get('[data-testid="bases-conj-or"]').classes()).toContain('active')
    expect(wrapper.get('[data-testid="bases-conj-and"]').classes()).not.toContain('active')
    await settle()
    expect(lastSubmitted().filters?.conjunction).toBe('or')
  })

  it('hides the builder and brings it back without re-running the query', async () => {
    const { wrapper } = await renderBases()
    await addCondition(wrapper)
    await settle()
    run.mockClear()

    const toggle = wrapper.get('[data-testid="bases-toggle-builder"]')
    expect(toggle.text()).toBe('隐藏筛选器')
    await toggle.trigger('click')
    expect(wrapper.find('[data-testid="bases-builder"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="bases-toggle-builder"]').text()).toBe('显示筛选器')

    await wrapper.get('[data-testid="bases-toggle-builder"]').trigger('click')
    expect(wrapper.get('[data-testid="bases-builder"]').text()).toContain('添加条件')
    expect(wrapper.findAll('[data-testid="bases-condition"]')).toHaveLength(1)
    expect(run).not.toHaveBeenCalled()
  })
})

describe('BasesView sort, group and column settings', () => {
  it('sorts by the chosen property and flips the direction', async () => {
    const { wrapper } = await renderBases()
    expect(wrapper.get('[data-testid="bases-sort-dir"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="bases-sort-property"]').setValue('status')
    expect(wrapper.findAll('[data-testid="bases-th"]')[1]!.classes()).toContain('sorted')
    await settle()
    expect(lastSubmitted().views?.[0]?.sort).toEqual([{ property: 'status', desc: false }])

    const direction = wrapper.get('[data-testid="bases-sort-dir"]')
    expect(direction.attributes('disabled')).toBeUndefined()
    expect(direction.attributes('title')).toBe('升序')
    await direction.trigger('click')
    expect(direction.attributes('title')).toBe('降序')
    await settle()
    expect(lastSubmitted().views?.[0]?.sort).toEqual([{ property: 'status', desc: true }])
  })

  it('sorts ascending from a column header and clears a descending sort from the header', async () => {
    const { wrapper } = await renderBases()
    const header = () => wrapper.findAll('[data-testid="bases-th"]')[0]!
    await header().trigger('click')
    await settle()
    expect(lastSubmitted().views?.[0]?.sort).toEqual([{ property: 'file.title', desc: false }])
    expect(header().classes()).toContain('sorted')

    await wrapper.get('[data-testid="bases-sort-dir"]').trigger('click')
    await settle()
    expect(lastSubmitted().views?.[0]?.sort).toEqual([{ property: 'file.title', desc: true }])

    await header().trigger('click')
    await settle()
    expect(lastSubmitted().views?.[0]?.sort).toEqual([])
    expect(header().classes()).not.toContain('sorted')
  })

  it('sends the chosen group property with the active view', async () => {
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-group-property"]').setValue('status')
    await settle()
    expect(lastSubmitted().views?.[0]?.groupBy).toBe('status')
    expect(run.mock.calls.at(-1)?.[2]).toBe('v-table')
  })

  it('toggles columns on and off', async () => {
    const { wrapper } = await renderBases()
    const chip = () => wrapper.findAll('[data-testid="bases-column-chip"]').find(item => item.attributes('title') === 'status')!
    expect(chip().classes()).not.toContain('on')

    await chip().trigger('click')
    expect(chip().classes()).toContain('on')
    await settle()
    expect(lastSubmitted().views?.[0]?.columns).toEqual(['status'])

    await chip().trigger('click')
    expect(chip().classes()).not.toContain('on')
    await settle()
    expect(lastSubmitted().views?.[0]?.columns).toEqual([])
  })
})
