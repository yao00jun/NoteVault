// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import type { WorkbenchProject, WorkbenchTask } from '@/api/workbench'
import TodayView from './TodayView.vue'

const harness = vi.hoisted(() => ({
  state: {} as Record<string, unknown>,
  addTask: vi.fn(),
  readReport: vi.fn(),
  briefing: vi.fn(),
  startBriefing: vi.fn(),
  briefingResult: vi.fn(),
  cancel: vi.fn(),
  handlers: {} as Record<string, (ev?: unknown) => void>,
}))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))
vi.mock('@/api/workbench', () => ({ WorkbenchService: { ReadDailyReport: harness.readReport } }))
vi.mock('@/api', () => ({
  ReportService: {
    GenerateDailyBriefing: harness.briefing,
    StartDailyBriefing: harness.startBriefing,
    DailyBriefingTaskResult: harness.briefingResult,
  },
  TaskService: { Cancel: harness.cancel },
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))
// 动态 import 的 runtime 被静态 mock 接管：Events.On 把回调登记到 harness，
// 用例里手动投递 task:progress / task:finished 事件。
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (ev?: unknown) => void) => {
      harness.handlers[name] = cb
      return () => {}
    },
  },
}))
vi.mock('@/components/workbench/TaskCard.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/workbench/InterviewReview.vue', () => ({ default: { template: '<div />' } }))

enableAutoUnmount(afterEach)

function project(name: string, values: Partial<WorkbenchProject> = {}): WorkbenchProject {
  return { name, folder: `Projects/${name}`, path: `Projects/${name}/project.md`, status: 'active', nextStep: '', techStack: [], taskIds: [], notes: [], modifiedAt: '', ...values }
}

function task(projectName: string, id: string, date = '2026-09-08'): WorkbenchTask {
  return {
    id, filePath: `Projects/${projectName}/Tasks.md`, fileName: 'Tasks.md', lineIndex: 0,
    sourceLine: '- [ ] Work', content: 'Work', title: 'Work', type: 'US',
    project: projectName, projectPath: `Projects/${projectName}/project.md`, completed: false,
    priority: '', due: '', date, completedAt: '', status: 'todo', blocker: '', progress: [],
  }
}

beforeEach(() => {
  localStorage.clear()
  harness.addTask.mockReset().mockResolvedValue(undefined)
  harness.readReport.mockReset().mockResolvedValue('# 今日工作汇报')
  harness.briefing.mockReset()
  // 默认空任务 ID → 走同步兜底路径（与改造前行为一致）
  harness.startBriefing.mockReset().mockResolvedValue('')
  harness.briefingResult.mockReset()
  harness.cancel.mockReset().mockResolvedValue(true)
  harness.handlers = {}
  harness.state = reactive({
    today: '2026-09-08', tasks: [], todayTasks: [], blockers: [], reviewQueue: [], reviewedToday: 0,
    dueReminders: [], progress: [], warnings: [], loading: false, busy: false,
    error: '', reminderError: '', lastUpdated: '2026-09-08T10:00:00+08:00',
    agentEvents: [],
    projects: [project('A')],
    addTask: harness.addTask, refresh: vi.fn(),
  })
})

async function renderToday() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: '/a', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/today', component: TodayView },
    { path: '/learning', component: { template: '<div />' } },
    { path: '/editor', component: { template: '<div />' } },
  ] })
  await router.push('/today')
  const wrapper = mount(TodayView, { global: { plugins: [pinia, router, i18n] } })
  const button = (label: string) => wrapper.findAll('button').find(item => item.text().includes(label))!
  return { wrapper, workspace, router, button }
}

describe('Today workspace boundaries', () => {
  it('opens the project task form for legacy create links and consumes the action', async () => {
    const { wrapper, router } = await renderToday()
    await router.push('/today?action=new-task')
    await flushPromises()
    expect(wrapper.get('[aria-label="任务标题"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="task-destination"]').text()).toContain('Projects/A/Tasks.md')
    expect(router.currentRoute.value.query.action).toBeUndefined()
    expect(harness.addTask).not.toHaveBeenCalled()
  })

  it('renders agent activity latest-first and opens the daily agent log', async () => {
    harness.state.agentEvents = [
      { time: '09:12', agent: 'claude-code', content: '修复导航测试' },
      { time: '10:30', agent: '', content: '重构 settings store' },
    ]
    const { wrapper, router, workspace } = await renderToday()
    await flushPromises()
    const items = wrapper.get('[data-testid="agent-card"]').findAll('.agent-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('重构 settings store')
    expect(items[1].text()).toContain('claude-code')
    await items[0].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.file).toBe('Daily/Agent/2026-09-08.md')
    expect(workspace.activeFile).toBe('Daily/Agent/2026-09-08.md')
  })

  it('hides the agent card when there is no agent activity', async () => {
    const { wrapper } = await renderToday()
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-card"]').exists()).toBe(false)
  })

  it('opens the saved work log from the secondary header action', async () => {
    const { button, router, workspace } = await renderToday()
    expect(button('查看今日日志')).toBeDefined()
    await button('查看今日日志').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toMatch(/^Daily\/Reports\/\d{4}-\d{2}-\d{2}-日报\.md$/)
    expect(workspace.activeFile).toBe(router.currentRoute.value.query.file)
  })

  it('defaults to the active frequent project and submits the displayed task destination', async () => {
    harness.state.projects = [project('B', { modifiedAt: '2026-09-08T20:00:00+08:00' }), project('A'), project('Closed', { status: 'completed' })]
    harness.state.tasks = [task('A', 'a1'), task('A', 'a2'), task('B', 'b1'), task('Closed', 'c1'), task('Closed', 'c2'), task('Closed', 'c3')]
    const { wrapper, button } = await renderToday()
    await button('新任务').trigger('click')
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/A')
    expect(wrapper.get('[data-testid="task-destination"]').text()).toContain('Projects/A/Tasks.md')
    await wrapper.get('[aria-label="任务标题"]').setValue('联调订单接口')
    await button('添加任务').trigger('click')
    expect(harness.addTask).toHaveBeenCalledWith('Projects/A', '联调订单接口', 'US', '2026-09-08')
  })

  it('keeps a manual project choice during refresh and reselects when that project is removed', async () => {
    harness.state.projects = [project('A'), project('B'), project('C')]
    const { wrapper, button } = await renderToday()
    await button('新任务').trigger('click')
    await wrapper.get('[aria-label="归属项目"]').setValue('Projects/B')
    harness.state.tasks = [task('C', 'c1'), task('C', 'c2')]
    await flushPromises()
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/B')
    expect(wrapper.get('[data-testid="task-destination"]').text()).toContain('Projects/B/Tasks.md')
    harness.state.projects = [project('A'), project('C')]
    await flushPromises()
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/C')
  })

  it('can save a general task before any project has been created', async () => {
    harness.state.projects = []
    const { wrapper, button } = await renderToday()
    await button('新任务').trigger('click')
    await wrapper.get('[aria-label="任务标题"]').setValue('处理报销')
    await button('添加任务').trigger('click')
    expect(harness.addTask).toHaveBeenCalledWith('Projects/通用事务', '处理报销', 'US', '2026-09-08')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('offers general chores without an active project and keeps an explicit general choice', async () => {
    harness.state.projects = [project('Paused', { status: 'paused' })]
    const { wrapper, button } = await renderToday()
    await button('新任务').trigger('click')
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/通用事务')
    expect(wrapper.get('[data-testid="task-destination"]').text()).toContain('Projects/通用事务/Tasks.md')
    await wrapper.get('[aria-label="归属项目"]').setValue('Projects/通用事务')
    harness.state.projects = [project('A'), project('通用事务')]
    harness.state.tasks = [task('A', 'a1')]
    await flushPromises()
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/通用事务')
    expect(wrapper.findAll('option[value="Projects/通用事务"]')).toHaveLength(1)
    await wrapper.get('[aria-label="任务标题"]').setValue('处理报销')
    await button('添加任务').trigger('click')
    expect(harness.addTask).toHaveBeenCalledWith('Projects/通用事务', '处理报销', 'US', '2026-09-08')
  })

  it('adopts a loaded project after the first empty snapshot', async () => {
    harness.state.projects = []
    const { wrapper, button } = await renderToday()
    await button('新任务').trigger('click')
    harness.state.projects = [project('Loaded', { status: '进行中' })]
    await flushPromises()
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/Loaded')
  })

  it('resets an explicit general choice when entering another workspace', async () => {
    const { wrapper, workspace, button } = await renderToday()
    await button('新任务').trigger('click')
    await wrapper.get('[aria-label="归属项目"]').setValue('Projects/通用事务')
    harness.state.projects = []
    workspace.setCurrentWorkspace({ id: 'b', name: 'B', path: '/b', createdAt: '', lastOpenedAt: '' })
    await flushPromises()
    harness.state.projects = [project('B')]
    await button('新任务').trigger('click')
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/B')
  })

  it('opens the interview review tab from the due-review metric', async () => {
    const { button, router } = await renderToday()
    await button('面试待复习').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/learning?tab=review')
  })

  it('clears the old workspace task form and uses the new local date', async () => {
    const { wrapper, workspace, button } = await renderToday()
    await button('新任务').trigger('click')
    await wrapper.get('[aria-label="任务标题"]').setValue('A private task')
    await wrapper.get('[aria-label="归属项目"]').setValue('Projects/A')
    harness.state.today = '2026-09-09'
    harness.state.projects = []
    workspace.setCurrentWorkspace({ id: 'b', name: 'B', path: '/b', createdAt: '', lastOpenedAt: '' })
    await flushPromises()
    expect(wrapper.find('[aria-label="任务标题"]').exists()).toBe(false)
    await button('新任务').trigger('click')
    expect((wrapper.get('[aria-label="任务标题"]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/通用事务')
    expect((wrapper.get('[aria-label="任务截止日期"]').element as HTMLInputElement).value).toBe('2026-09-09')
    harness.state.projects = [project('B')]
    await flushPromises()
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('Projects/B')
  })

  it('does not clear a new workspace draft when an earlier save completes', async () => {
    let finish!: () => void
    harness.addTask.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
    const { wrapper, workspace, button } = await renderToday()
    await button('新任务').trigger('click')
    await wrapper.get('[aria-label="任务标题"]').setValue('A pending save')
    await button('添加任务').trigger('click')
    workspace.setCurrentWorkspace({ id: 'b', name: 'B', path: '/b', createdAt: '', lastOpenedAt: '' })
    await flushPromises()
    if (!wrapper.find('[aria-label="任务标题"]').exists()) await button('新任务').trigger('click')
    await wrapper.get('[aria-label="任务标题"]').setValue('B current draft')
    finish()
    await flushPromises()
    expect(wrapper.find('[aria-label="任务标题"]').exists()).toBe(true)
    expect((wrapper.get('[aria-label="任务标题"]').element as HTMLInputElement).value).toBe('B current draft')
  })

  it('generates the AI briefing for today and restores it from cache without re-calling', async () => {
    harness.briefing.mockResolvedValue({ date: '2026-09-08', briefing: '- 先做支付回调联调\n- 复习导航改动', aiUsed: true })
    const { wrapper } = await renderToday()
    expect(wrapper.get('[data-testid="today-briefing"]').text()).toContain('让 AI 汇总')
    await wrapper.get('[data-testid="briefing-generate"]').trigger('click')
    await flushPromises()
    expect(harness.briefing).toHaveBeenCalledTimes(1)
    expect(harness.briefing.mock.calls[0]?.slice(0, 2)).toEqual(['/a', '2026-09-08'])
    expect(wrapper.get('[data-testid="today-briefing"]').text()).toContain('先做支付回调联调')

    const again = await renderToday()
    expect(again.wrapper.get('[data-testid="today-briefing"]').text()).toContain('先做支付回调联调')
    expect(harness.briefing).toHaveBeenCalledTimes(1)
  })

  it('shows configuration guidance when AI is not set up', async () => {
    useSettingsStore().settings.ai.baseURL = ''
    useSettingsStore().settings.ai.model = ''
    const { wrapper } = await renderToday()
    await wrapper.get('[data-testid="briefing-generate"]').trigger('click')
    await flushPromises()
    expect(harness.briefing).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="today-briefing"]').text()).toContain('未配置 AI 端点')
  })

  it('shows backend briefing failures inline and keeps the card usable', async () => {
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    useSettingsStore().settings.ai.model = 'test-model'
    harness.briefing.mockRejectedValue(new Error('AI 简报生成失败：boom'))
    const { wrapper } = await renderToday()
    await wrapper.get('[data-testid="briefing-generate"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="today-briefing"] [role="alert"]').text()).toContain('boom')
    expect(wrapper.get('[data-testid="briefing-generate"]').attributes('disabled')).toBeUndefined()
  })

  it('briefing goes through the async task: progress shown, result fetched by task ID and cached', async () => {
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    useSettingsStore().settings.ai.model = 'test-model'
    harness.startBriefing.mockResolvedValue('task-5')
    harness.briefingResult.mockResolvedValue({ date: '2026-09-08', briefing: '- 先做支付回调联调', aiUsed: true })
    const { wrapper } = await renderToday()

    await wrapper.get('[data-testid="briefing-generate"]').trigger('click')
    await flushPromises()

    expect(harness.startBriefing).toHaveBeenCalledTimes(1)
    expect(harness.startBriefing.mock.calls[0]?.slice(0, 2)).toEqual(['/a', '2026-09-08'])
    expect(harness.briefing).not.toHaveBeenCalled()
    // 进度行出现（默认提示语），取消按钮可用
    const progress = wrapper.get('[data-testid="briefing-progress"]')
    expect(progress.text()).toContain('正在准备')

    harness.handlers['task:progress']?.({ data: { id: 'task-5', message: '正在生成简报' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="briefing-progress"]').text()).toContain('正在生成简报')

    harness.handlers['task:finished']?.({ data: { id: 'task-5', status: 'succeeded' } })
    await flushPromises()

    expect(harness.briefingResult).toHaveBeenCalledWith('task-5')
    // 完成后收尾：进度行消失、要点渲染、写入当天缓存
    expect(wrapper.find('[data-testid="briefing-progress"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="today-briefing"]').text()).toContain('先做支付回调联调')
    expect(localStorage.getItem('notevault.briefing.%2Fa.2026-09-08')).toContain('先做支付回调联调')
  })

  it('cancelling the briefing task shows the cancelled notice and keeps no result', async () => {
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    useSettingsStore().settings.ai.model = 'test-model'
    harness.startBriefing.mockResolvedValue('task-6')
    const { wrapper } = await renderToday()

    await wrapper.get('[data-testid="briefing-generate"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="briefing-cancel"]').trigger('click')
    await flushPromises()
    expect(harness.cancel).toHaveBeenCalledWith('task-6')

    harness.handlers['task:finished']?.({ data: { id: 'task-6', status: 'cancelled' } })
    await flushPromises()

    // 取消：进度行消失、提示已取消、不取结果、按钮恢复可用
    expect(wrapper.find('[data-testid="briefing-progress"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="today-briefing"]').text()).toContain('已取消简报生成')
    expect(harness.briefingResult).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="briefing-generate"]').attributes('disabled')).toBeUndefined()
  })
})
