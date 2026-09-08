// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia, disposePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { nextTick } from 'vue'
import { i18n } from '@/i18n'

// Keep stores and work-log behavior real; replace only native service boundaries.
vi.mock('@/api', async () => ({
  WorkbenchService: (await import('@/api/workbench')).WorkbenchService,
  ReminderService: { GetAllReminders: vi.fn().mockResolvedValue([]) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  FileService: { CreateFile: vi.fn() },
  TemplateService: { CreateFromTemplate: vi.fn().mockResolvedValue(undefined) },
  WorkspaceService: {
    ListWorkspaces: vi.fn().mockResolvedValue([]),
    SetCurrentWorkspace: vi.fn(),
    GetWorkspaceByID: vi.fn(),
    CreateWorkspace: vi.fn(),
  },
  CredentialService: {
    GetCredential: vi.fn().mockResolvedValue(''),
    SaveCredential: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('@/api/workbench', () => ({
  WorkbenchService: {
    GetWorkbench: vi.fn(async (_workspacePath: string, date: string) => ({
      date, tasks: [], projects: [], books: [], cards: [], progress: [],
      radar: { path: '', content: '' }, documents: [],
      indexedAt: '2026-09-08T08:00:00Z', warnings: [],
    })),
    UpdateWorkbenchTask: vi.fn(),
    ReviewInterviewCard: vi.fn(),
    ReadDailyReport: vi.fn(),
    SaveDailyReport: vi.fn(),
    AddWorkbenchTask: vi.fn(),
  },
}))

vi.mock('@wailsio/runtime', () => ({
  Dialogs: { OpenFile: vi.fn() },
  Events: { On: vi.fn(() => vi.fn()) },
  default: { Dialogs: { OpenFile: vi.fn() } },
}))

vi.mock('@/composables/usePrompt', () => ({ promptDialog: vi.fn() }))

import SideBar from './SideBar.vue'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { FileService, TemplateService, WorkbenchService } from '@/api'
import type { WorkbenchSnapshot } from '@/api/workbench'
import { promptDialog } from '@/composables/usePrompt'
import { localDateKey } from '@/utils/workbench'
import { resetToasts, useToast } from '@/composables/useToast'

const workspace = { id: 'ws_1', name: '我的笔记库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '' }
const piniaInstances: Pinia[] = []
const routes = [
  { path: '/', redirect: '/today' },
  { path: '/knowledge', redirect: '/today' },
  ...['today', 'projects', 'learning', 'vault', 'library', 'editor', 'trash', 'settings'].map(path => ({
    path: '/' + path, component: { template: '<div>' + path + '</div>' },
  })),
]

function mountSideBar() {
  const pinia = createPinia()
  piniaInstances.push(pinia)
  setActivePinia(pinia)
  const settingsStore = useSettingsStore()
  settingsStore.settings.sidebarCollapsed = false
  const workspaceStore = useWorkspaceStore()
  const workbenchStore = useWorkbenchStore()
  const router = createRouter({ history: createMemoryHistory(), routes })
  const wrapper = mount(SideBar, { global: { plugins: [pinia, router, i18n] } })
  return { wrapper, settingsStore, workspaceStore, workbenchStore, router }
}

enableAutoUnmount(afterEach)

describe('SideBar', () => {
  beforeEach(async () => {
    await flushPromises()
    localStorage.clear()
    document.body.innerHTML = ''
    vi.clearAllMocks()
    vi.mocked(WorkbenchService.ReadDailyReport).mockReset().mockResolvedValue('')
    resetToasts()
  })

  afterEach(() => {
    resetToasts()
    piniaInstances.splice(0).forEach(disposePinia)
  })

  it('renders four workflow destinations separately from the three direct actions', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    expect(wrapper.findAll('.nav-item').map(item => item.attributes('data-testid'))).toEqual([
      'nav-today', 'nav-projects', 'nav-learning', 'nav-vault',
    ])
    expect(wrapper.find('[data-testid="action-new"]').text()).toContain('新建')
    expect(wrapper.find('[data-testid="action-daily"]').text()).toContain('每日工作日志')
    expect(wrapper.find('[data-testid="action-report"]').text()).toContain('生成日报')
    expect(wrapper.find('[data-testid="sidebar-pins"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sidebar-recent"]').exists()).toBe(true)
  })

  it.each(['today', 'projects', 'learning', 'vault'])('opens the %s workflow and marks it active', async (destination) => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    await wrapper.get('[data-testid="nav-' + destination + '"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/' + destination)
    expect(wrapper.get('[data-testid="nav-' + destination + '"]').attributes('aria-current')).toBe('page')
  })

  it('opens Today when the work-log action has no workspace', async () => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    await wrapper.get('[data-testid="action-daily"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/today')
  })

  it('opens the saved daily report without creating a diary', async () => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    vi.mocked(WorkbenchService.ReadDailyReport).mockResolvedValueOnce('# 今日工作汇报\n已完成联调')
    workspaceStore.setCurrentWorkspace({ ...workspace })
    await flushPromises()
    await wrapper.get('[data-testid="action-daily"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe(`Daily/Reports/${localDateKey()}-日报.md`)
    expect(workspaceStore.activeFile).toBe(`Daily/Reports/${localDateKey()}-日报.md`)
    expect(WorkbenchService.ReadDailyReport).toHaveBeenCalledWith('/tmp/vault', localDateKey())
    expect(FileService.CreateFile).not.toHaveBeenCalled()
    expect(TemplateService.CreateFromTemplate).not.toHaveBeenCalled()
  })

  it('opens the existing generator when today has no saved work log', async () => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    await router.push('/projects')
    const openReport = vi.fn()
    window.addEventListener('notevault:daily-report', openReport)
    try {
      await wrapper.get('[data-testid="action-daily"]').trigger('click')
      await flushPromises()
      expect(openReport).toHaveBeenCalledTimes(1)
      expect(router.currentRoute.value.path).toBe('/projects')
      expect(workspaceStore.activeFile).toBeNull()
      expect(useToast().toasts.value.some(toast => toast.kind === 'info' && toast.message.includes('尚未生成'))).toBe(true)
      expect(FileService.CreateFile).not.toHaveBeenCalled()
      expect(TemplateService.CreateFromTemplate).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener('notevault:daily-report', openReport)
    }
  })

  it('surfaces a work-log read error without routing to a nonexistent report', async () => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    await router.push('/projects')
    vi.mocked(WorkbenchService.ReadDailyReport).mockRejectedValueOnce(new Error('没有读取权限'))
    await wrapper.get('[data-testid="action-daily"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/projects')
    expect(workspaceStore.activeFile).toBeNull()
    expect(useToast().toasts.value.some(toast => toast.kind === 'error' && toast.message.includes('没有读取权限'))).toBe(true)
  })

  it('opens the shared report preview without changing the current page', async () => {
    const { wrapper, router } = mountSideBar()
    await router.push('/projects')
    const openReport = vi.fn()
    window.addEventListener('notevault:daily-report', openReport)
    try {
      await wrapper.get('[data-testid="action-report"]').trigger('click')
      expect(openReport).toHaveBeenCalledTimes(1)
      expect(router.currentRoute.value.path).toBe('/projects')
    } finally {
      window.removeEventListener('notevault:daily-report', openReport)
    }
  })

  it('exposes document creation for the command palette and opens the created file immediately', async () => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    vi.mocked(promptDialog).mockResolvedValueOnce('接口联调.md')
    vi.mocked(FileService.CreateFile).mockResolvedValueOnce({ path: '接口联调.md' } as Awaited<ReturnType<typeof FileService.CreateFile>>)
    await (wrapper.vm as unknown as { createNewDoc: () => Promise<void> }).createNewDoc()
    await flushPromises()
    expect(workspaceStore.activeFile).toBe('接口联调.md')
    expect(router.currentRoute.value.query.file).toBe('接口联调.md')
    expect(router.currentRoute.value.path).toBe('/editor')
  })

  it.each([
    ['Projects/NoteVault/project.md', '/projects', 'project', 'Projects/NoteVault'],
    ['Areas/Books/Go/book.md', '/learning', 'book', 'Areas/Books/Go'],
    ['Resources/接口联调.md', '/editor', 'file', 'Resources/接口联调.md'],
  ])('opens pinned %s at its content destination', async (path, destination, queryKey, queryValue) => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    workspaceStore.togglePin(path, '重点内容')
    await flushPromises()
    await wrapper.get('[data-testid="sidebar-pin"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(destination)
    expect(router.currentRoute.value.query[queryKey]).toBe(queryValue)
    if (destination === '/editor') expect(workspaceStore.activeFile).toBe(path)
  })

  it('reorders pins with Alt+Arrow and retains their order after remount', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    workspaceStore.togglePin('first.md', '第一篇')
    workspaceStore.togglePin('second.md', '第二篇')
    await flushPromises()
    await wrapper.findAll('[data-testid="sidebar-pin"]')[1].trigger('keydown', { key: 'ArrowUp', altKey: true })
    expect(wrapper.findAll('[data-testid="sidebar-pin"]').map(item => item.text())).toEqual(['第二篇', '第一篇'])
    wrapper.unmount()
    const reopened = mountSideBar()
    reopened.workspaceStore.setCurrentWorkspace({ ...workspace })
    await flushPromises()
    expect(reopened.wrapper.findAll('[data-testid="sidebar-pin"]').map(item => item.text())).toEqual(['第二篇', '第一篇'])
  })

  it('reorders a dragged pin at the dropped position', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    workspaceStore.togglePin('first.md', '第一篇')
    workspaceStore.togglePin('second.md', '第二篇')
    await flushPromises()
    const pins = wrapper.findAll('[data-testid="sidebar-pin"]')
    await pins[0].trigger('dragstart')
    await pins[1].trigger('drop')
    expect(wrapper.findAll('[data-testid="sidebar-pin"]').map(item => item.text())).toEqual(['第二篇', '第一篇'])
  })

  it('removes a pin through its context menu without navigating', async () => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    workspaceStore.togglePin('first.md', '第一篇')
    await router.push('/today')
    await flushPromises()
    await wrapper.get('[data-testid="sidebar-pin"]').trigger('contextmenu', { clientX: 20, clientY: 80 })
    const unpin = document.querySelector<HTMLButtonElement>('[data-testid="sidebar-unpin"]')
    expect(unpin).not.toBeNull()
    unpin!.click()
    await nextTick()
    expect(workspaceStore.pinnedItems).toEqual([])
    expect(wrapper.find('[data-testid="sidebar-pin"]').exists()).toBe(false)
    expect(router.currentRoute.value.path).toBe('/today')
  })

  it('limits pins to eight and explains why a ninth pin was not added', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    for (let index = 0; index < 8; index++) workspaceStore.togglePin('note-' + index + '.md')
    workspaceStore.openFile('ninth.md')
    await flushPromises()
    await wrapper.get('[data-testid="sidebar-pin-current"]').trigger('click')
    expect(wrapper.findAll('[data-testid="sidebar-pin"]')).toHaveLength(8)
    expect(workspaceStore.isPinned('ninth.md')).toBe(false)
    expect(useToast().toasts.value.some(toast => toast.message.includes('8'))).toBe(true)
  })

  it('shows only the four most recent files and opens the selected file', async () => {
    const { wrapper, workspaceStore, router } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    for (let index = 1; index <= 6; index++) workspaceStore.openFile('note-' + index + '.md')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="sidebar-recent-file"]').map(item => item.text())).toEqual([
      'note-6.md', 'note-5.md', 'note-4.md', 'note-3.md',
    ])
    await wrapper.findAll('[data-testid="sidebar-recent-file"]')[2].trigger('click')
    await flushPromises()
    expect(workspaceStore.activeFile).toBe('note-4.md')
    expect(router.currentRoute.value.query.file).toBe('note-4.md')
  })

  it('keeps actions reachable and labeled when collapsed', async () => {
    const { wrapper, settingsStore } = mountSideBar()
    await flushPromises()
    settingsStore.toggleSidebar()
    await nextTick()
    expect(wrapper.findAll('.nav-label')).toHaveLength(0)
    expect(wrapper.findAll('.nav-item.collapsed')).toHaveLength(4)
    expect(wrapper.get('[data-testid="action-daily"]').attributes('aria-label')).toContain('每日工作日志')
    expect(wrapper.get('[data-testid="action-report"]').attributes('aria-label')).toContain('生成日报')
  })

  it('shows the current workspace name', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    workspaceStore.setCurrentWorkspace({ ...workspace })
    await flushPromises()
    expect(wrapper.get('.ws-name').text()).toBe('我的笔记库')
  })

  it('shows the workspace picker when no workspace is open', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    expect(wrapper.get('.ws-name').text()).toBe('选择工作区')
    expect(wrapper.get('[data-testid="sidebar-index-status"]').attributes('data-state')).toBe('idle')
  })

  it('shows syncing until the workspace index has actually loaded', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    let resolveSnapshot!: (snapshot: WorkbenchSnapshot) => void
    vi.mocked(WorkbenchService.GetWorkbench).mockImplementationOnce(() => new Promise(resolve => { resolveSnapshot = resolve }))
    workspaceStore.setCurrentWorkspace({ ...workspace })
    await nextTick()
    expect(wrapper.get('[data-testid="sidebar-index-status"]').text()).toContain('更新索引')
    resolveSnapshot({
      date: '2026-09-08', tasks: [], projects: [], books: [], cards: [], progress: [],
      radar: { path: '', content: '' }, documents: [], indexedAt: '2026-09-08T08:00:00Z', warnings: [],
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-index-status"]').text()).toContain('索引就绪')
    expect(wrapper.get('[data-testid="sidebar-index-status"]').attributes('data-state')).toBe('ready')
    expect(wrapper.get('.shortcut-hints').text()).toContain('Ctrl+K')
    expect(wrapper.get('.shortcut-hints').text()).toContain('Ctrl+J')
  })

  it('shows index failures instead of claiming the workspace is ready', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    vi.mocked(WorkbenchService.GetWorkbench).mockRejectedValueOnce(new Error('读盘失败'))
    workspaceStore.setCurrentWorkspace({ ...workspace })
    await flushPromises()
    expect(wrapper.get('[data-testid="sidebar-index-status"]').text()).toContain('索引失败')
    expect(wrapper.get('[data-testid="sidebar-index-status"]').attributes('title')).toContain('读盘失败')
  })

  it('toggles sidebar width from the footer', async () => {
    const { wrapper, settingsStore } = mountSideBar()
    await flushPromises()
    await wrapper.get('.collapse-btn').trigger('click')
    expect(settingsStore.settings.sidebarCollapsed).toBe(true)
  })

  it.each(['trash', 'settings'])('opens the footer %s destination', async destination => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    await wrapper.get('[data-testid="sidebar-' + destination + '"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/' + destination)
  })
})
