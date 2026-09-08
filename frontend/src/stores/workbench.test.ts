// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { WorkbenchSnapshot, WorkbenchTask } from '@/api/workbench'

const api = vi.hoisted(() => ({ snapshot: vi.fn(), update: vi.fn(), review: vi.fn(), add: vi.fn(), distill: vi.fn(), reminders: vi.fn() }))
vi.mock('@/api', () => ({
  WorkbenchService: { GetWorkbench: api.snapshot, UpdateWorkbenchTask: api.update, ReviewInterviewCard: api.review, AddWorkbenchTask: api.add, DistillKnowledge: api.distill },
  ReminderService: { GetAllReminders: api.reminders },
}))
import { useWorkbenchStore } from './workbench'
import { useWorkspaceStore } from './workspace'

const ws = (id: string) => ({ id, name: id, path: `/vault/${id}`, createdAt: '', lastOpenedAt: '' })
const task: WorkbenchTask = { id: 'task', filePath: 'Daily/2026-09-08.md', fileName: '2026-09-08.md', lineIndex: 2, sourceLine: '- [ ] [US] 接口', content: '接口', title: '接口', type: 'US', project: '', projectPath: '', completed: false, priority: 'medium', due: '', date: '2026-09-08', completedAt: '', status: 'pending', blocker: '', progress: [] }
const snapshot = (tasks: WorkbenchTask[] = []): WorkbenchSnapshot => ({ date: '2026-09-08', tasks, projects: [], books: [], cards: [], progress: [], radar: { path: 'Learning/技术雷达.md', content: '' }, documents: [], indexedAt: '2026-09-08T09:00:00+08:00', warnings: [] })
let pinia: ReturnType<typeof createPinia>

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 8, 9))
  localStorage.clear()
  vi.resetAllMocks()
  api.snapshot.mockResolvedValue(snapshot())
  api.reminders.mockResolvedValue([])
  pinia = createPinia()
  setActivePinia(pinia)
})
afterEach(() => { disposePinia(pinia); vi.useRealTimers() })

describe('workbench derived state', () => {
  it('refreshes the Markdown projection and file tree after knowledge distillation', async () => {
    const workspace = useWorkspaceStore()
    workspace.setCurrentWorkspace(ws('a'))
    const store = useWorkbenchStore()
    await flushPromises()
    const before = workspace.fileTreeVersion
    const request = { sourceFile: 'Projects/A/复盘.md', targetMode: 'book' as const, targetFolder: 'Learning/Go', targetTitle: '并发', summary: '限制并发', question: '', answer: '' }
    await store.distillKnowledge(request)
    expect(api.distill).toHaveBeenCalledWith('/vault/a', request)
    expect(workspace.fileTreeVersion).toBe(before + 1)
    expect(api.snapshot).toHaveBeenCalledTimes(2)
    expect(store.busy).toBe(false)
  })

  it('does not refresh a different workspace when an earlier distillation finishes', async () => {
    let finish!: () => void
    api.distill.mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    const workspace = useWorkspaceStore()
    workspace.setCurrentWorkspace(ws('a'))
    const store = useWorkbenchStore()
    await flushPromises()
    const saving = store.distillKnowledge({ sourceFile: 'Projects/A/复盘.md', targetMode: 'interview', targetFolder: 'Learning/面试宝典', targetTitle: 'Go', summary: '', question: '如何定位？', answer: '观察指标' })
    workspace.setCurrentWorkspace(ws('b'))
    await flushPromises()
    const version = workspace.fileTreeVersion
    const reads = api.snapshot.mock.calls.length
    finish()
    await saving
    expect(workspace.fileTreeVersion).toBe(version)
    expect(api.snapshot).toHaveBeenCalledTimes(reads)
    expect(store.busy).toBe(false)
  })

  it('discards a late response from the previous workspace', async () => {
    let finishOld!: (value: WorkbenchSnapshot) => void
    api.snapshot.mockImplementation((path: string) => path === '/vault/a' ? new Promise(resolve => { finishOld = resolve }) : Promise.resolve(snapshot()))
    const workspace = useWorkspaceStore()
    workspace.setCurrentWorkspace(ws('a'))
    const store = useWorkbenchStore()
    workspace.setCurrentWorkspace(ws('b'))
    await flushPromises()
    finishOld(snapshot([task]))
    await flushPromises()
    expect(store.tasks).toEqual([])
    expect(store.loading).toBe(false)
    expect(store.error).toBe('')
  })

  it('writes with the original line and reloads Markdown after a mutation', async () => {
    api.snapshot.mockResolvedValue(snapshot([task]))
    api.update.mockImplementation(async () => { api.snapshot.mockResolvedValue(snapshot([{ ...task, blocker: '缺测试环境', status: 'blocked' }])) })
    useWorkspaceStore().setCurrentWorkspace(ws('a'))
    const store = useWorkbenchStore()
    await flushPromises()
    await store.setBlocker(store.tasks[0], '缺测试环境')
    expect(api.update).toHaveBeenCalledWith('/vault/a', task.filePath, 2, task.sourceLine, 'blocker', '缺测试环境', '2026-09-08')
    expect(store.blockers[0].blocker).toBe('缺测试环境')
    expect(store.busy).toBe(false)
  })

  it('does not optimistically mark a failed write complete', async () => {
    api.snapshot.mockResolvedValue(snapshot([task]))
    api.update.mockRejectedValue(new Error('文件已变化，请刷新'))
    useWorkspaceStore().setCurrentWorkspace(ws('a'))
    const store = useWorkbenchStore()
    await flushPromises()
    await expect(store.toggleTask(store.tasks[0])).rejects.toThrow('文件已变化')
    expect(store.tasks[0].completed).toBe(false)
    expect(store.busy).toBe(false)
  })

  it('rolls the review and task date forward at local midnight', async () => {
    useWorkspaceStore().setCurrentWorkspace(ws('a'))
    const store = useWorkbenchStore()
    await flushPromises()
    vi.setSystemTime(new Date(2026, 8, 9, 0, 0))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(store.today).toBe('2026-09-09')
    expect(api.snapshot).toHaveBeenLastCalledWith('/vault/a', '2026-09-09')
  })
})
