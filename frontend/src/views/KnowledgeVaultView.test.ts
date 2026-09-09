// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent, KeepAlive, onUnmounted } from 'vue'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView, useRouter } from 'vue-router'
import type { WorkbenchDocument, WorkbenchSnapshot } from '@/api/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { useNavigationStore } from '@/stores/navigation'
import GlobalNavigation from '@/components/layout/GlobalNavigation.vue'
import KnowledgeVaultView from './KnowledgeVaultView.vue'

const api = vi.hoisted(() => ({ index: vi.fn(), reminders: vi.fn() }))
vi.mock('@/api', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/api')>()
  return {
    ...original,
    WorkbenchService: { ...original.WorkbenchService, GetWorkbench: api.index },
    ReminderService: { ...original.ReminderService, GetAllReminders: api.reminders },
  }
})

function documents(count: number, folder = 'Projects/分页'): WorkbenchDocument[] {
  return Array.from({ length: count }, (_, index) => {
    const number = String(index + 1).padStart(2, '0')
    return {
      path: `${folder}/文档${number}.md`,
      title: `文档 ${number}`,
      modifiedAt: '2026-09-09T10:00:00+08:00',
    }
  })
}

function snapshot(entries = documents(24)): WorkbenchSnapshot {
  return {
    date: '2026-09-09', tasks: [], projects: [], books: [], cards: [], progress: [],
    radar: { path: 'Learning/技术雷达.md', content: '' },
    documents: entries, indexedAt: '2026-09-09T10:00:00+08:00', warnings: [],
  }
}

const workspaceInfo = (name = 'one') => ({
  id: name, name, path: `/workspace/${name}`, createdAt: '', lastOpenedAt: '',
})

const Shell = defineComponent({
  components: { GlobalNavigation, RouterView, KeepAlive },
  setup() {
    const router = useRouter()
    onUnmounted(useNavigationStore().connect(router, useWorkspaceStore()))
  },
  template: '<GlobalNavigation /><RouterView v-slot="{ Component }"><KeepAlive><component :is="Component" /></KeepAlive></RouterView>',
})

let pinia: ReturnType<typeof createPinia>
enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  vi.resetAllMocks()
  api.index.mockResolvedValue(snapshot())
  api.reminders.mockResolvedValue([])
  pinia = createPinia()
  setActivePinia(pinia)
})
afterEach(() => disposePinia(pinia))

async function renderVault(path = '/vault', options: { shell?: boolean; restoreWorkspace?: boolean } = {}) {
  const workspace = useWorkspaceStore()
  if (!options.restoreWorkspace) workspace.setCurrentWorkspace(workspaceInfo())
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/vault', component: KnowledgeVaultView },
      ...['/', '/today', '/editor', '/insights', '/discover'].map(path => ({ path, component: { template: '<div />' } })),
    ],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(options.shell ? Shell : KnowledgeVaultView, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { wrapper, router, workspace, workbench: useWorkbenchStore() }
}

describe('KnowledgeVaultView pagination', () => {
  it('bounds a 24-document list to ten rows and replaces the rows on each page', async () => {
    const { wrapper, router } = await renderVault()
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(10)
    expect(wrapper.get('[data-testid="vault-pagination-status"]').text()).toContain('1–10')
    expect(wrapper.get('[aria-label="上一页"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[aria-label="下一页"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ page: '2', pageSize: '10' })
    const secondPage = wrapper.findAll('[data-testid="vault-document"]')
    expect(secondPage).toHaveLength(10)
    expect(secondPage[0]!.text()).toContain('文档 11')
    expect(secondPage[9]!.text()).toContain('文档 20')
    expect(wrapper.get('[aria-label="第 2 页"]').attributes('aria-current')).toBe('page')

    await wrapper.get('[aria-label="第 3 页"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(4)
    expect(wrapper.get('[data-testid="vault-pagination-status"]').text()).toContain('21–24')
    expect(wrapper.get('[data-testid="vault-pagination-status"]').text()).toContain('3 / 3')
    expect(wrapper.get('[aria-label="下一页"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="上一页"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('2')
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 11')
  })

  it('resets to the first page when choosing twenty or fifty documents per page', async () => {
    const { wrapper, router } = await renderVault('/vault?page=3&pageSize=10')
    const select = wrapper.get('select[aria-label="每页文档数"]')
    expect(select.findAll('option').map(option => option.attributes('value'))).toEqual(['10', '20', '50'])

    await select.setValue('20')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(20)
    expect(router.currentRoute.value.query).toMatchObject({ page: '1', pageSize: '20' })
    expect(wrapper.get('[data-testid="vault-pagination-status"]').text()).toContain('1 / 2')

    await select.setValue('50')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(24)
    expect(router.currentRoute.value.query).toMatchObject({ page: '1', pageSize: '50' })
    expect(wrapper.get('[aria-label="下一页"]').attributes('disabled')).toBeDefined()
  })

  it('restores the exact page, page size and filters through the global Back control', async () => {
    api.index.mockResolvedValue(snapshot(documents(45)))
    const { wrapper, router } = await renderVault('/vault?space=Projects&folder=Projects/分页&q=文档&sort=title&page=2&pageSize=20', { shell: true })
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(20)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 21')
    const location = router.currentRoute.value.fullPath

    await wrapper.findAll('[data-testid="vault-document"]')[0]!.get('.collection-document-open').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe('Projects/分页/文档21.md')
    expect(wrapper.find('[data-testid="vault-document"]').exists()).toBe(false)

    await wrapper.get('[data-testid="global-back"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe(location)
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(20)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 21')
    expect((wrapper.get('select[aria-label="每页文档数"]').element as HTMLSelectElement).value).toBe('20')

    await wrapper.get('[data-testid="global-forward"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.file).toBe('Projects/分页/文档21.md')
  })

  it('keeps numbered navigation bounded while allowing a jump to the last page', async () => {
    api.index.mockResolvedValue(snapshot(documents(243)))
    const { wrapper, router } = await renderVault('/vault?page=12&pageSize=10')
    const pages = wrapper.get('[aria-label="文档分页"]').findAll('button[aria-label^="第 "]')
    expect(pages.map(page => page.text())).toEqual(['1', '11', '12', '13', '25'])

    await wrapper.get('[aria-label="第 25 页"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('25')
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="vault-pagination-status"]').text()).toContain('241–243')
  })

  it.each([
    ['search', '[data-testid="vault-search"]', '文档 2', 5, '文档 20'],
    ['space', 'select[aria-label="文档空间"]', 'Projects', 10, '文档 01'],
    ['sort', 'select[aria-label="文档排序"]', 'title', 10, '文档 01'],
  ] as const)('resets to page one after changing the %s filter', async (_name, selector, value, count, firstTitle) => {
    const { wrapper, router } = await renderVault('/vault?page=3&pageSize=10')
    await wrapper.get(selector).setValue(value)
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('1')
    const rows = wrapper.findAll('[data-testid="vault-document"]')
    expect(rows).toHaveLength(count)
    expect(rows[0]!.text()).toContain(firstTitle)
  })

  it('resets the page for pinned documents and new folder routes', async () => {
    const { wrapper, router, workspace } = await renderVault('/vault?page=3&pageSize=10')
    workspace.togglePin('Projects/分页/文档24.md', '文档 24')
    await wrapper.get('.vault-document-toolbar button[aria-pressed]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ pinned: '1', page: '1' })
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="vault-document"]').text()).toContain('文档 24')

    await router.push('/vault?folder=Projects/分页&pageSize=20')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(20)
    expect(router.currentRoute.value.query).toMatchObject({ folder: 'Projects/分页', page: '1', pageSize: '20' })
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 01')
  })

  it('clamps a page after documents disappear without resetting a still-valid page', async () => {
    const { wrapper, router, workbench } = await renderVault('/vault?page=3&pageSize=10')
    api.index.mockResolvedValue(snapshot(documents(20)))
    await workbench.refresh()
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('2')
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(10)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 11')

    api.index.mockResolvedValue(snapshot(documents(19)))
    await workbench.refresh()
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('2')
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(9)

    api.index.mockResolvedValue(snapshot([]))
    await workbench.refresh()
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('1')
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('从一篇笔记开始积累')
  })

  it('clamps the restored route when the document count shrinks while the list is cached', async () => {
    const { wrapper, router, workbench } = await renderVault('/vault?page=3&pageSize=10', { shell: true })
    await wrapper.findAll('[data-testid="vault-document"]')[0]!.get('.collection-document-open').trigger('click')
    await flushPromises()
    api.index.mockResolvedValue(snapshot(documents(19)))
    await workbench.refresh()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')

    await wrapper.get('[data-testid="global-back"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ page: '2', pageSize: '10' })
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(9)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 11')
  })

  it('preserves a restored page until the asynchronous index arrives', async () => {
    let finish!: (value: WorkbenchSnapshot) => void
    api.index.mockImplementationOnce(() => new Promise<WorkbenchSnapshot>(resolve => { finish = resolve }))
    const { wrapper, router, workbench } = await renderVault('/vault?page=3&pageSize=10')
    expect(workbench.loading).toBe(true)
    expect(router.currentRoute.value.query.page).toBe('3')
    expect(wrapper.text()).toContain('正在整理文档索引')

    finish(snapshot())
    await flushPromises()
    expect(router.currentRoute.value.query.page).toBe('3')
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(4)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 21')
  })

  it('retains deep-link filters when the initial workspace is restored after mounting', async () => {
    const { wrapper, router, workspace } = await renderVault('/vault?space=Projects&q=文档&page=3&pageSize=10', { restoreWorkspace: true })
    workspace.setCurrentWorkspace(workspaceInfo())
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ space: 'Projects', q: '文档', page: '3', pageSize: '10' })
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(4)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 21')
  })

  it('resets the page and filters when switching an existing workspace', async () => {
    const { wrapper, router, workspace } = await renderVault('/vault?space=Projects&q=文档&page=2&pageSize=20')
    api.index.mockResolvedValue(snapshot(documents(24, 'Learning/Go')))
    workspace.setCurrentWorkspace(workspaceInfo('two'))
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ page: '1', pageSize: '20' })
    expect(router.currentRoute.value.query.q).toBeUndefined()
    expect(router.currentRoute.value.query.space).toBeUndefined()
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(20)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('Learning/Go')
  })

  it.each([
    'page=0&pageSize=0',
    'page=-2&pageSize=30',
    'page=2.5&pageSize=20.5',
    'page=2e1&pageSize=2e1',
    'page=9007199254740993&pageSize=Infinity',
    'page=2&page=3&pageSize=20&pageSize=50',
  ])('normalizes invalid pagination query values safely: %s', async query => {
    const { wrapper, router } = await renderVault(`/vault?${query}&folder=Projects/分页`)
    expect(router.currentRoute.value.query).toMatchObject({ page: '1', pageSize: '10', folder: 'Projects/分页' })
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(10)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 01')
  })

  it('clamps an oversized valid page to the last page after loading', async () => {
    const { wrapper, router } = await renderVault('/vault?page=999&pageSize=20')
    expect(router.currentRoute.value.query).toMatchObject({ page: '2', pageSize: '20' })
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(4)
    expect(wrapper.findAll('[data-testid="vault-document"]')[0]!.text()).toContain('文档 21')
  })
})
