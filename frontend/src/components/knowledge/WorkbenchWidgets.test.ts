// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { i18n } from '@/i18n'

const mocked = {
  stats: vi.fn(),
  todos: vi.fn(),
  reminders: vi.fn(),
}

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  StatsService: { GetTodayStats: (...a: unknown[]) => mocked.stats(...a) },
  TodoService: {
    GetAllTodos: (...a: unknown[]) => mocked.todos(...a),
    ToggleTodo: vi.fn(),
  },
  ReminderService: { GetAllReminders: (...a: unknown[]) => mocked.reminders(...a) },
}))

import { useWorkspaceStore } from '@/stores/workspace'
import WorkbenchWidgets from './WorkbenchWidgets.vue'

enableAutoUnmount(afterEach)

function mountWidgets() {
  const pinia = createPinia()
  setActivePinia(pinia)
  // 组件仅在存在当前工作区时加载数据
  const wsStore = useWorkspaceStore()
  wsStore.setCurrentWorkspace({ id: 'ws', name: 'V', path: '/tmp/v', createdAt: '', lastOpenedAt: '' })
  const wrapper = mount(WorkbenchWidgets, { global: { plugins: [pinia, i18n] } })
  return { wrapper }
}

beforeEach(() => {
  for (const fn of Object.values(mocked)) fn.mockReset()
  mocked.stats.mockResolvedValue({
    editedToday: 2,
    streakDays: 1,
    pendingTodos: 2,
    highPriorityTodos: 1,
    dueReminders: 2,
    recentFiles: ['Java/Java基础.md', 'SQL/索引设计.md'],
  })
  mocked.todos.mockResolvedValue([
    { id: 't1', filePath: 'a.md', fileName: 'a.md', content: '高优先任务', lineIndex: 0, completed: false, priority: 'high' },
    { id: 't2', filePath: 'b.md', fileName: 'b.md', content: '普通任务', lineIndex: 1, completed: false, priority: 'low' },
    { id: 't3', filePath: 'c.md', fileName: 'c.md', content: '已完成', lineIndex: 2, completed: true, priority: 'high' },
  ])
  mocked.reminders.mockResolvedValue([
    { id: 'r1', filePath: 'x.md', fileName: 'x.md', content: '过期提醒', remindAt: '2020-01-01T09:00:00Z', createdAt: '', completed: false },
    { id: 'r2', filePath: 'y.md', fileName: 'y.md', content: '未来提醒', remindAt: '2099-01-01T09:00:00Z', createdAt: '', completed: false },
  ])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('WorkbenchWidgets', () => {
  it('渲染三个部件：待办过滤已完成且高优先级在前，提醒含过期标记', async () => {
    const { wrapper } = mountWidgets()
    await flushPromises()

    expect(wrapper.find('[data-testid="wb-todos"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wb-reminders"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wb-edited"]').exists()).toBe(true)

    const todoTexts = wrapper.findAll('[data-testid="wb-todos"] .wb-item-text').map((w) => w.text())
    expect(todoTexts).toEqual(['高优先任务', '普通任务']) // 已完成的被过滤，高优先级在前

    // 过期提醒时间带 overdue 样式
    const times = wrapper.findAll('[data-testid="wb-reminders"] .wb-time')
    expect(times[0]!.classes()).toContain('overdue')
    expect(times[1]!.classes()).not.toContain('overdue')

    // 今日编辑来自 StatsService.recentFiles
    const edited = wrapper.findAll('[data-testid="wb-edited"] .wb-item-text').map((w) => w.text())
    expect(edited).toEqual(['Java基础', '索引设计'])
  })

  it('数据为空时显示空态而不是报错', async () => {
    mocked.stats.mockResolvedValue({ editedToday: 0, streakDays: 0, pendingTodos: 0, highPriorityTodos: 0, dueReminders: 0, recentFiles: [] })
    mocked.todos.mockResolvedValue([])
    mocked.reminders.mockResolvedValue([])
    const { wrapper } = mountWidgets()
    await flushPromises()

    const empties = wrapper.findAll('.wb-empty')
    expect(empties.length).toBe(3)
  })

  it('单个数据源失败不拖垮其余部件', async () => {
    mocked.todos.mockRejectedValue(new Error('boom'))
    const { wrapper } = mountWidgets()
    await flushPromises()

    expect(wrapper.find('[data-testid="wb-todos"] .wb-empty').exists()).toBe(true)
    expect(wrapper.find('[data-testid="wb-edited"] .wb-item-text').exists()).toBe(true)
  })
})
