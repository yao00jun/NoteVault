// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'
import { useWorkspaceStore } from '@/stores/workspace'
import { confirmDialog } from '@/composables/useConfirm'
import { promptDialog } from '@/composables/usePrompt'
import { BaseService, type BaseDef, type BaseResult, type BaseSummary, type BaseView, type BuiltinTemplate, type PropertyMeta } from '@/api'
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

function emptyFilters(): BaseDef['filters'] {
  return { conjunction: 'and', conditions: [], groups: [] }
}

const DEF: BaseDef = {
  name: '阅读清单', filters: emptyFilters(), views: [view('v-table', 'table')],
}

const READING: BaseSummary = { name: '阅读清单', viewCount: 1, filterCount: 0, updatedAt: '' }
const TODOS: BaseSummary = { name: '待办汇总', viewCount: 1, filterCount: 2, updatedAt: '' }

const TEMPLATE: BuiltinTemplate = {
  id: 'recent', title: '最近修改', description: '最近 7 天改过的笔记',
  def: { name: '最近修改', filters: emptyFilters(), views: [view('t1', 'table')] },
}

const RESULT: BaseResult = {
  viewId: 'v-table', viewName: '表格', viewType: 'table',
  columns: ['file.title'], rows: [], groups: null, total: 0, returned: 0, truncated: false, scanned: 0, warnings: null,
}

/** PropKind 是枚举，而 @/api 被整体 mock 取不到枚举值：造数据时写字面量再断言回去 */
const kind = (value: string) => value as PropertyMeta['kind']

const run = vi.mocked(BaseService.RunBase)
const saveButton = (wrapper: Awaited<ReturnType<typeof renderBases>>['wrapper']) =>
  wrapper.get('[data-testid="bases-save"]').text()

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  vi.mocked(BaseService.ListOperators).mockResolvedValue(['contains'])
  vi.mocked(BaseService.ListTemplates).mockResolvedValue([TEMPLATE])
  vi.mocked(BaseService.ListProperties).mockResolvedValue([
    { name: 'file.title', kind: kind('string'), count: 2, implicit: true, samples: null },
  ])
  vi.mocked(BaseService.ListBases).mockResolvedValue([READING, TODOS])
  // 绑定返回的是 CancellablePromise（带 cancel/cancelOn），桩只需要「可 await」；
  // 用 Promise.resolve(...) as never 而不是 async 函数，否则返回类型是 Promise<T>，
  // 缺 CancellablePromise 的两个方法，typecheck 会拦。
  vi.mocked(BaseService.LoadBase).mockImplementation(
    (_ws, name) => Promise.resolve(JSON.parse(JSON.stringify({ ...DEF, name }))) as never,
  )
  vi.mocked(BaseService.SaveBase).mockResolvedValue(undefined)
  vi.mocked(BaseService.DeleteBase).mockResolvedValue(undefined)
  vi.mocked(BaseService.RenameBase).mockResolvedValue(undefined)
  vi.mocked(BaseService.InvalidateCache).mockResolvedValue(undefined)
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
  return { wrapper }
}

/** 留存调用参数前先取快照：def 是响应式代理，直接读 mock.calls 会被后续编辑改写 */
const plain = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T

describe('BasesView saving', () => {
  it('saves the working definition and clears the unsaved marker', async () => {
    const { wrapper } = await renderBases()
    expect(saveButton(wrapper)).toBe('保存')
    expect(wrapper.get('[data-testid="bases-rename"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="bases-name"]').setValue('精选书单')
    expect(saveButton(wrapper)).toBe('保存 *')

    await wrapper.get('[data-testid="bases-save"]').trigger('click')
    await flushPromises()
    const saveBase = vi.mocked(BaseService.SaveBase)
    expect(saveBase).toHaveBeenCalledTimes(1)
    expect(saveBase.mock.calls[0]?.[0]).toBe('/vault')
    expect(plain(saveBase.mock.calls[0]?.[1])).toMatchObject({ name: '精选书单' })
    expect(saveButton(wrapper)).toBe('保存')
    expect(wrapper.find('[data-testid="bases-error"]').exists()).toBe(false)
  })

  it('refuses to save a base without a name', async () => {
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-name"]').setValue('   ')
    await wrapper.get('[data-testid="bases-save"]').trigger('click')
    await flushPromises()
    expect(BaseService.SaveBase).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="bases-error"]').text()).toBe('请先填写视图名称')
  })

  it('reports a failed save and keeps the base marked unsaved', async () => {
    vi.mocked(BaseService.SaveBase).mockRejectedValue(new Error('磁盘已满'))
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-name"]').setValue('精选书单')
    await wrapper.get('[data-testid="bases-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="bases-error"]').text()).toContain('保存视图失败：磁盘已满')
    expect(saveButton(wrapper)).toBe('保存 *')
  })
})

describe('BasesView deleting', () => {
  it('deletes the saved base after confirmation and opens the next one', async () => {
    vi.mocked(confirmDialog).mockResolvedValue(true)
    // 首次加载给出两条；删除后重新拉取只剩另一条
    vi.mocked(BaseService.ListBases).mockResolvedValueOnce([READING, TODOS]).mockResolvedValue([TODOS])
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-delete"]').trigger('click')
    await flushPromises()

    expect(confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ message: '确定要删除视图 "阅读清单" 吗？', danger: true }))
    expect(BaseService.DeleteBase).toHaveBeenCalledWith('/vault', '阅读清单')
    expect(BaseService.LoadBase).toHaveBeenLastCalledWith('/vault', '待办汇总')
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '待办汇总')
  })

  it('keeps the base when deletion is cancelled', async () => {
    vi.mocked(confirmDialog).mockResolvedValue(false)
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-delete"]').trigger('click')
    await flushPromises()
    expect(BaseService.DeleteBase).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="bases-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '阅读清单')
  })
})

describe('BasesView renaming', () => {
  it('renames the saved base and keeps unsaved query edits', async () => {
    vi.mocked(promptDialog).mockResolvedValue('精选书单')
    const { wrapper } = await renderBases()
    // 先做一处未保存的修改：改名不能把它弄丢
    await wrapper.get('[data-testid="bases-add-condition"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="bases-rename"]').trigger('click')
    await flushPromises()
    expect(promptDialog).toHaveBeenCalledWith(expect.objectContaining({ defaultValue: '阅读清单' }))
    expect(BaseService.RenameBase).toHaveBeenCalledWith('/vault', '阅读清单', '精选书单')
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '精选书单')
    expect(wrapper.findAll('[data-testid="bases-condition"]')).toHaveLength(1)
    expect(saveButton(wrapper)).toBe('保存 *')
  })

  it('does nothing when the rename prompt is cancelled', async () => {
    vi.mocked(promptDialog).mockResolvedValue(null)
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-rename"]').trigger('click')
    await flushPromises()
    expect(BaseService.RenameBase).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '阅读清单')
  })

  it('reports a failed rename', async () => {
    vi.mocked(promptDialog).mockResolvedValue('精选书单')
    vi.mocked(BaseService.RenameBase).mockRejectedValue(new Error('目标名称已存在'))
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-rename"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="bases-error"]').text()).toContain('重命名失败：目标名称已存在')
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '阅读清单')
  })
})

describe('BasesView starting points', () => {
  it('re-scans the property index and re-runs the query on refresh', async () => {
    const { wrapper } = await renderBases()
    run.mockClear()
    expect(BaseService.ListProperties).toHaveBeenCalledTimes(1)

    await wrapper.get('[data-testid="bases-refresh"]').trigger('click')
    await flushPromises()
    expect(BaseService.InvalidateCache).toHaveBeenCalledWith('/vault')
    expect(BaseService.ListProperties).toHaveBeenCalledTimes(2)
    expect(run).toHaveBeenCalledTimes(1)
    expect(run.mock.calls[0]?.[2]).toBe('v-table')
  })

  it('starts an unsaved new base from the rail button', async () => {
    vi.mocked(BaseService.NewBaseTemplate).mockResolvedValue({
      name: '新建视图', filters: emptyFilters(), views: [view('n1', 'table')],
    })
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-new"]').trigger('click')
    await flushPromises()

    expect(BaseService.NewBaseTemplate).toHaveBeenCalledWith('新建视图')
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '新建视图')
    expect(saveButton(wrapper)).toBe('保存 *')
    // 尚未落盘：重命名 / 删除不可用
    expect(wrapper.get('[data-testid="bases-rename"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="bases-delete"]').attributes('disabled')).toBeDefined()
  })

  it('applies a built-in template as an unsaved working copy', async () => {
    const { wrapper } = await renderBases()
    await wrapper.get('[data-testid="bases-template"]').trigger('click')
    await flushPromises()

    expect(run.mock.calls.at(-1)?.[0]).toBe('/vault')
    expect(plain(run.mock.calls.at(-1)?.[1])).toMatchObject({ name: '最近修改' })
    expect(wrapper.get('[data-testid="bases-name"]').element).toHaveProperty('value', '最近修改')
    expect(wrapper.get('[data-testid="bases-rename"]').attributes('disabled')).toBeDefined()
  })
})
