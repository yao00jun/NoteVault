// @vitest-environment jsdom
// 结构镜像 DailyReportModal.test.ts（真实 store + GetWorkbench mock + fake timers）。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'

const api = vi.hoisted(() => ({ read: vi.fn(), save: vi.fn(), snapshot: vi.fn() }))
vi.mock('@/api', () => ({
  WorkbenchService: {
    ReadWeeklyReport: api.read,
    SaveWeeklyReport: api.save,
    GetWorkbench: api.snapshot,
    ReadDailyReport: vi.fn(async () => ''),
  },
  ReminderService: { GetAllReminders: vi.fn().mockResolvedValue([]) },
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))
import WeeklyReportModal from './WeeklyReportModal.vue'

let pinia: ReturnType<typeof createPinia>
let wrapper: ReturnType<typeof mount>

const weekTask = (id: string, fields: Record<string, unknown> = {}) => ({
  id, filePath: 'Projects/商城/Tasks.md', fileName: 'Tasks.md', lineIndex: 0,
  sourceLine: `- [ ] ${id}`, content: id, title: id, type: 'US', project: '商城',
  projectPath: 'Projects/商城', completed: false, priority: 'medium', due: '',
  date: '', completedAt: '', status: 'pending', blocker: '', progress: [], ...fields,
})

const snapshot = {
  date: '2026-11-26',
  tasks: [
    weekTask('本周完成', { completed: true, completedAt: '2026-11-24' }),
    weekTask('阻塞任务', { type: 'DTS', blocker: '等待测试环境', status: 'blocked' }),
    weekTask('下周到期', { due: '2026-12-02' }),
  ],
  projects: [
    {
      name: '商城', path: 'Projects/商城/project.md', folder: 'Projects/商城', status: '进行中',
      nextStep: '', techStack: [], taskIds: [], notes: [],
      milestones: [{ name: '需求评审', date: '2026-11-26', done: false }],
      modifiedAt: '2026-11-26T09:00:00+08:00',
    },
  ],
  books: [], cards: [], progress: [], documents: [],
  radar: { path: '', content: '' }, indexedAt: 'now', warnings: [],
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 10, 26, 10)) // 2026-11-26 周四 → ISO W48
  vi.resetAllMocks()
  localStorage.clear()
  api.read.mockResolvedValue('')
  api.save.mockImplementation(async (_ws: string, week: string) => `Daily/Reports/${week}-周报.md`)
  api.snapshot.mockResolvedValue(snapshot)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } })
  pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore().setCurrentWorkspace({ id: 'ws', name: '工作区', path: '/vault', createdAt: '', lastOpenedAt: '' })
})

afterEach(() => {
  wrapper?.unmount()
  disposePinia(pinia)
  vi.useRealTimers()
})

async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await useWorkbenchStore().refresh()
  wrapper = mount(WeeklyReportModal, { props: { visible: true }, global: { plugins: [pinia, router, i18n], stubs: { Teleport: true } } })
  await flushPromises()
}

describe('WeeklyReportModal（工作周报）', () => {
  it('generates the weekly skeleton from tasks and milestones, then archives it by ISO week', async () => {
    await render()
    const draft = wrapper.get('textarea')
    const text = (draft.element as HTMLTextAreaElement).value
    // 无归档时自动生成骨架：主题（ISO 周）+ 本周完成 + 里程碑进展
    expect(text).toContain('主题：【周报】2026 年第 48 周')
    expect(text).toContain('本周完成：\n1. [商城][US] 本周完成')
    expect(text).toContain('⏳ 需求评审（2026-11-26）')
    expect(api.read).toHaveBeenCalledWith('/vault', '2026-W48')

    await draft.setValue(text + '\n补充：已完成验收')
    await wrapper.get('[data-testid="save-weekly-report"]').trigger('click')
    await flushPromises()
    // 首次保存时无归档，expectedContent 为空串
    expect(api.save).toHaveBeenCalledWith('/vault', '2026-W48', expect.stringContaining('补充：已完成验收'), '')
    // 保存后 dirty 消失，保存按钮禁用
    expect(wrapper.get('[data-testid="save-weekly-report"]').attributes('disabled')).toBeDefined()
  })

  it('keeps the archived draft when the week already has one', async () => {
    api.read.mockResolvedValue('主题：【周报】已归档稿')
    await render()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('主题：【周报】已归档稿')
    // 已归档稿未修改 → 保存按钮不可用
    expect(wrapper.get('[data-testid="save-weekly-report"]').attributes('disabled')).toBeDefined()
  })

  it('navigates weeks and reloads the matching archive', async () => {
    await render()
    await wrapper.get('[aria-label="上一周"]').trigger('click')
    await flushPromises()
    expect(api.read).toHaveBeenCalledWith('/vault', '2026-W47')
    await wrapper.get('[aria-label="下一周"]').trigger('click')
    await flushPromises()
    expect(api.read).toHaveBeenCalledWith('/vault', '2026-W48')
  })
})
