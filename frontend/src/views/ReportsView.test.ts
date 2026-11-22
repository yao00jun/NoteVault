// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { apiMock } from '@/test/apiMock'

const harness = vi.hoisted(() => ({
  handlers: {} as Record<string, (ev?: unknown) => void>,
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

vi.mock('@/api', () => apiMock({
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  StatsService: { GetWritingActivity: vi.fn(), GetOnThisDay: vi.fn(async () => []) },
  GraphService: { GetGraph: vi.fn() },
  ReportService: {
    GenerateWeeklyReport: vi.fn(),
    StartWeeklyReport: vi.fn(),
    WeeklyReportTaskResult: vi.fn(),
  },
  TaskService: { Cancel: vi.fn() },
  ReviewService: { GenerateReview: vi.fn(), ScheduleWeeklyReview: vi.fn() },
  ReminderService: { GetAllReminders: vi.fn(async () => []) },
}))

import ReportsView from './ReportsView.vue'
import { StatsService, GraphService, ReportService, ReviewService, ReminderService, TaskService } from '@/api'
import { useToast, resetToasts } from '@/composables/useToast'
import { useWorkspaceStore } from '@/stores/workspace'

const mockedActivity = vi.mocked(StatsService.GetWritingActivity)
const mockedGraph = vi.mocked(GraphService.GetGraph)

function mountReports() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useWorkspaceStore()
  ;(store as any).currentWorkspace = { id: 'w1', name: 'WS', path: 'C:\\ws' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/editor', component: { template: '<div />' } },
    ],
  })
  return { wrapper: mount(ReportsView, { global: { plugins: [pinia, router, i18n] } }), store, router }
}

function makeActivity(days: number, marks: Record<number, number>) {
  const cells = Array.from({ length: days }, (_, i) => {
    const d = new Date()
    d.setDate(d.getDate() - (days - 1 - i))
    return { date: d.toISOString().slice(0, 10), edited: marks[i] ?? 0 }
  })
  return {
    days: cells,
    activeDays: Object.keys(marks).length,
    weekEdited: 3,
    monthEdited: 5,
    longestStreak: 2,
  }
}

describe('ReportsView（写作报表中心）', () => {
  beforeEach(() => {
    mockedActivity.mockReset()
    mockedGraph.mockReset()
    vi.mocked(ReportService.StartWeeklyReport).mockReset()
    vi.mocked(ReportService.WeeklyReportTaskResult).mockReset()
    vi.mocked(TaskService.Cancel).mockReset().mockResolvedValue(true)
    resetToasts()
    harness.handlers = {}
    localStorage.clear()
  })

  it('渲染汇总卡片、热力图与被链接 Top10', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, { 89: 1, 90: 3 }) as any)
    mockedGraph.mockResolvedValue({
      nodes: [
        { id: 'a.md', title: 'Go 基础', path: 'a.md', resolved: true },
        { id: 'b.md', title: '术语表', path: 'b.md', resolved: true },
      ],
      edges: [
        { source: 'a.md', target: 'b.md' },
        { source: 'b.md', target: 'a.md' },
        { source: 'b.md', target: 'a.md' },
      ],
    } as any)

    const { wrapper } = mountReports()
    await flushPromises()

    const cards = wrapper.findAll('.rp-card')
    expect(cards).toHaveLength(4)
    // 热力图：91 天补齐首列后应有 91 个有效格子
    expect(wrapper.findAll('.rp-heat-col').length).toBeGreaterThanOrEqual(13)
    expect(wrapper.findAll('.rp-heat-cell:not(.empty)').length).toBeGreaterThanOrEqual(91)

    // a.md 被链接 2 次、b.md 1 次 → Top10 第一名是 a.md
    const items = wrapper.findAll('.rp-topitem')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('Go 基础')
    expect(items[0].text()).toContain('2')
  })

  it('未解析的虚拟节点不进入 Top10', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({
      nodes: [
        { id: 'a.md', title: 'A', path: 'a.md', resolved: true },
        { id: 'unresolved:ghost', title: 'ghost', path: 'unresolved:ghost', resolved: false },
      ],
      edges: [{ source: 'a.md', target: 'unresolved:ghost' }],
    } as any)

    const { wrapper } = mountReports()
    await flushPromises()

    expect(wrapper.findAll('.rp-topitem')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('ghost')
  })

  it('无写作记录时显示空状态提示', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)

    const { wrapper } = mountReports()
    await flushPromises()

    expect(wrapper.findAll('.rp-card')).toHaveLength(4)
    // 热力图空态与 Top10 空态都给出提示
    expect(wrapper.findAll('.rp-empty').length).toBeGreaterThanOrEqual(1)
  })

  it('统计服务抛错时显示错误条而不是白屏', async () => {
    mockedActivity.mockRejectedValue(new Error('bridge down'))

    const { wrapper } = mountReports()
    await flushPromises()

    expect(wrapper.find('.rp-error').exists()).toBe(true)
    expect(wrapper.find('.rp-error').text()).toContain('bridge down')
  })

  it('点击生成周报按钮调用后端并打开生成的笔记', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    const mockedGenerate = vi.mocked(ReportService.GenerateWeeklyReport)
    mockedGenerate.mockReset()
    mockedGenerate.mockResolvedValue({
      path: 'Reports/2026-W36.md',
      aiUsed: true,
      notes: 3,
      todos: 1,
      message: '',
    } as any)

    const { wrapper } = mountReports()
    await flushPromises()

    await wrapper.find('.rp-generate-btn').trigger('click')
    await flushPromises()

    expect(mockedGenerate).toHaveBeenCalledTimes(1)
    // 断言透传的是设置页的 AI 配置（默认值来自 settings store）
    expect(mockedGenerate.mock.calls[0][1]).toMatchObject({
      baseURL: 'https://api.openai.com/v1',
      model: 'gpt-4o-mini',
      protocol: 'openai-chat',
    })
  })

  it('生成本期回顾：调用后端、打开回顾笔记、展示降级说明', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    const mockedGen = vi.mocked(ReviewService.GenerateReview)
    mockedGen.mockResolvedValue({
      path: 'Reviews/回顾-2026-09-04.md',
      summarized: 3,
      failed: 1,
      recapTitles: ['old.md'],
      aiUsed: true,
      message: '3 篇摘要成功，1 篇失败',
    } as any)

    const { wrapper } = mountReports()
    await flushPromises()

    const btns = wrapper.findAll('.rp-review-actions button')
    expect(btns).toHaveLength(2)
    await btns[0].trigger('click')
    await flushPromises()

    expect(mockedGen).toHaveBeenCalledTimes(1)
    const msg = wrapper.find('.rp-generate-msg')
    expect(msg.exists()).toBe(true)
    expect(msg.text()).toContain('3 篇摘要成功')
    expect(msg.classes()).toContain('ok')
  })

  it('预约回顾提醒成功后展示预约时间', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    vi.mocked(ReviewService.ScheduleWeeklyReview).mockResolvedValue({
      id: 'r1',
      filePath: '',
      fileName: '',
      content: '回顾',
      remindAt: '2026-09-07T09:00:00+08:00',
      createdAt: '',
      completed: false,
    } as any)

    const { wrapper } = mountReports()
    await flushPromises()

    const btns = wrapper.findAll('.rp-review-actions button')
    await btns[1].trigger('click')
    await flushPromises()

    const msgs = wrapper.findAll('.rp-generate-msg')
    expect(msgs.length).toBeGreaterThanOrEqual(1)
    expect(msgs[msgs.length - 1].text()).toContain('回顾提醒')
  })

  it('周报生成失败时展示后端错误信息', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    const mockedGenerate = vi.mocked(ReportService.GenerateWeeklyReport)
    mockedGenerate.mockReset()
    mockedGenerate.mockRejectedValue(new Error('写入周报失败'))

    const { wrapper } = mountReports()
    await flushPromises()

    await wrapper.find('.rp-generate-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.rp-generate-msg').exists()).toBe(true)
    expect(wrapper.find('.rp-generate-msg').text()).toContain('写入周报失败')
    expect(wrapper.find('.rp-generate-msg').classes()).not.toContain('ok')
  })

  it('周报生成走异步任务：进度条更新，完成后取回结果并打开周报', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    const mockedSync = vi.mocked(ReportService.GenerateWeeklyReport)
    const mockedResult = vi.mocked(ReportService.WeeklyReportTaskResult)
    vi.mocked(ReportService.StartWeeklyReport).mockResolvedValue('task-9')
    mockedResult.mockResolvedValue({
      path: 'Reports/2026-W47.md',
      aiUsed: true,
      notes: 3,
      todos: 1,
      message: '',
    } as any)

    const { wrapper, router } = mountReports()
    await flushPromises()

    await wrapper.find('.rp-generate-btn').trigger('click')
    await flushPromises()

    expect(vi.mocked(ReportService.StartWeeklyReport)).toHaveBeenCalledTimes(1)
    expect(mockedSync).not.toHaveBeenCalled()
    // 进度条出现，总量未知时为 indeterminate
    expect(wrapper.get('[data-testid="report-progress"]').text()).toContain('正在准备')

    harness.handlers['task:progress']?.({ data: { id: 'task-9', percent: -1, message: '正在生成 AI 回顾' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="report-progress"]').text()).toContain('正在生成 AI 回顾')

    harness.handlers['task:finished']?.({ data: { id: 'task-9', status: 'succeeded' } })
    await flushPromises()

    // 完成后收尾：按任务 ID 取回结果、进度条消失、打开生成的周报
    expect(mockedResult).toHaveBeenCalledWith('task-9')
    expect(wrapper.find('[data-testid="report-progress"]').exists()).toBe(false)
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe('Reports/2026-W47.md')
  })

  it('取消生成：任务记为 cancelled，不打开文件并提示已降级', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    vi.mocked(ReportService.StartWeeklyReport).mockResolvedValue('task-7')
    vi.mocked(ReportService.WeeklyReportTaskResult).mockResolvedValue({
      path: 'Reports/2026-W47.md',
      aiUsed: false,
      notes: 1,
      todos: 0,
      message: 'AI 回顾生成失败，已降级为纯统计周报：context canceled',
    } as any)

    const { wrapper, router } = mountReports()
    await flushPromises()

    await wrapper.find('.rp-generate-btn').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="report-cancel"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(TaskService.Cancel)).toHaveBeenCalledWith('task-7')

    harness.handlers['task:finished']?.({ data: { id: 'task-7', status: 'cancelled' } })
    await flushPromises()

    // 取消：进度条收起、不打开文件、toast 提示纯统计版已生成
    expect(wrapper.find('[data-testid="report-progress"]').exists()).toBe(false)
    expect(router.currentRoute.value.path).not.toBe('/editor')
    const info = useToast().toasts.value.find((t) => t.kind === 'info')
    expect(info?.message).toContain('纯统计版')
  })

  it('后端返回空任务 ID 时退回同步生成路径', async () => {
    mockedActivity.mockResolvedValue(makeActivity(91, {}) as any)
    mockedGraph.mockResolvedValue({ nodes: [], edges: [] } as any)
    const mockedSync = vi.mocked(ReportService.GenerateWeeklyReport)
    mockedSync.mockReset()
    mockedSync.mockResolvedValue({
      path: 'Reports/2026-W47.md',
      aiUsed: false,
      notes: 0,
      todos: 0,
      message: '未配置 AI 端点，已生成纯统计周报（可在设置页配置后重新生成）',
    } as any)
    vi.mocked(ReportService.StartWeeklyReport).mockResolvedValue('')

    const { wrapper } = mountReports()
    await flushPromises()

    await wrapper.find('.rp-generate-btn').trigger('click')
    await flushPromises()

    expect(mockedSync).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.rp-generate-msg').text()).toContain('纯统计周报')
  })
})
