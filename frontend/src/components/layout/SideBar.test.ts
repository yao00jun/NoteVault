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
import { FileService, ReminderService, TemplateService, WorkbenchService } from '@/api'
import type { WorkbenchSnapshot } from '@/api/workbench'
import { promptDialog } from '@/composables/usePrompt'
import { localDateKey } from '@/utils/workbench'
import { resetToasts, useToast } from '@/composables/useToast'

const workspace = { id: 'ws_1', name: '我的笔记库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '' }
const piniaInstances: Pinia[] = []
const routes = [
  { path: '/', redirect: '/today' },
  { path: '/knowledge', redirect: '/today' },
  ...['today', 'projects', 'learning', 'vault', 'library', 'editor', 'trash', 'settings', 'review', 'canvas', 'discover', 'insights', 'import', 'plugins', 'archive'].map(path => ({
    path: '/' + path, component: { template: '<div>' + path + '</div>' },
  })),
  // 今日脉搏的提醒入口走真实重定向链：/reminders → /review?tab=tasks&sub=reminders
  { path: '/reminders', redirect: (to: { query: Record<string, unknown> }) => ({ path: '/review', query: { ...to.query, tab: 'tasks', sub: 'reminders' } }) },
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

  it('renders grouped page navigation and direct actions as pure navigation', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    // 分组平铺（2026-11）：三组小标题 + 全部一级直达，无折叠子树
    expect(wrapper.findAll('.sidebar-scroll .section-heading').map(heading => heading.text())).toEqual([
      '工作流', '知识', '工具',
    ])
    expect(wrapper.findAll('.nav-item').map(item => item.attributes('data-testid'))).toEqual([
      'nav-today', 'nav-projects', 'nav-learning',
      'nav-vault', 'nav-discover', 'nav-insights', 'nav-review', 'nav-canvas',
      'nav-reports', 'nav-plugins', 'nav-trash', 'nav-settings',
    ])
    expect(wrapper.find('[data-testid="action-new"]').text()).toContain('新建')
    expect(wrapper.find('[data-testid="action-daily"]').text()).toContain('每日工作日志')
    expect(wrapper.find('[data-testid="action-report"]').text()).toContain('生成日报')
    // 侧栏只做导航：固定/最近/日历/脉搏/扩展区已迁出，
    // 分别由今日页「快捷入口」卡与 Ctrl+K 命令面板承接。
    expect(wrapper.find('[data-testid="sidebar-pins"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-recent"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-pulse"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-extended"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-tree"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="sidebar-tree-search"]').exists()).toBe(false)
    expect(wrapper.findAll('.sidebar-scroll .sidebar-zone')).toHaveLength(1)
  })

  it('navigates knowledge and tool groups directly without a folded subtree', async () => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    // 折叠子树已移除：原库子树的目的地在「知识」组一级直达
    expect(wrapper.find('[data-testid="sidebar-vault-subtree"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="nav-discover"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav-insights"]').exists()).toBe(true)

    await wrapper.get('[data-testid="nav-canvas"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/canvas')
    expect(wrapper.get('[data-testid="nav-canvas"]').attributes('aria-current')).toBe('page')
  })

  it.each([
    ['today', '/today'],
    ['projects', '/projects'],
    ['learning', '/learning'],
    // 知识库直达 Mybase 式左树右文编辑器（空态即浏览模式），仪表盘保留在 /vault
    ['vault', '/editor'],
  ])('opens the %s workflow and marks it active', async (destination, expectedPath) => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    await wrapper.get('[data-testid="nav-' + destination + '"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(expectedPath)
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

  it('keeps actions reachable and labeled when collapsed', async () => {
    const { wrapper, settingsStore } = mountSideBar()
    await flushPromises()
    settingsStore.toggleSidebar()
    await nextTick()
    expect(wrapper.findAll('.nav-label')).toHaveLength(0)
    expect(wrapper.findAll('.nav-item.collapsed')).toHaveLength(12)
    // 折叠态组标题隐藏、只留分隔线
    expect(wrapper.findAll('.section-heading')).toHaveLength(0)
    expect(wrapper.findAll('.nav-group-divider')).toHaveLength(3)
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
      date: '2026-09-08', tasks: [], projects: [], books: [], cards: [], progress: [], agentEvents: [],
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

  it.each(['trash', 'settings'])('opens the %s destination from the tools group', async destination => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    await wrapper.get('[data-testid="nav-' + destination + '"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/' + destination)
  })
})
