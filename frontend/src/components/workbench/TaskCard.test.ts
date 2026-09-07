// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import type { WorkbenchTask } from '@/api/workbench'
const api = vi.hoisted(() => ({ update: vi.fn(), snapshot: vi.fn() }))
vi.mock('@/api', () => ({ WorkbenchService: { GetWorkbench: api.snapshot, UpdateWorkbenchTask: api.update }, ReminderService: { GetAllReminders: vi.fn().mockResolvedValue([]) } }))
import TaskCard from './TaskCard.vue'

const task: WorkbenchTask = { id: '1', filePath: 'Projects/商城/Tasks.md', fileName: 'Tasks.md', lineIndex: 4, sourceLine: '- [ ] [DTS] 修复重试 #due/2026-09-07', content: '修复重试', title: '修复重试', type: 'DTS', project: '商城', projectPath: 'Projects/商城', completed: false, priority: 'high', due: '2026-09-07', date: '', completedAt: '', status: 'pending', blocker: '', progress: [] }
let pinia: ReturnType<typeof createPinia>
let wrapper: ReturnType<typeof mount>
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 8, 10))
  api.update.mockReset().mockResolvedValue(undefined)
  api.snapshot.mockResolvedValue({ date: '2026-09-08', tasks: [task], projects: [], books: [], cards: [], progress: [], documents: [], radar: { path: '', content: '' }, indexedAt: '', warnings: [] })
  pinia = createPinia()
  setActivePinia(pinia)
  useWorkspaceStore().setCurrentWorkspace({ id: 'ws', name: '工作', path: '/vault', createdAt: '', lastOpenedAt: '' })
})
afterEach(() => { wrapper?.unmount(); disposePinia(pinia); vi.useRealTimers() })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/editor', component: { template: '<div />' } }] })
  wrapper = mount(TaskCard, { props: { task }, global: { plugins: [pinia, router] } })
  await flushPromises()
}

describe('task card Markdown actions', () => {
  it('highlights carryover and writes an inline progress note without submitting IME composition', async () => {
    await render()
    expect(wrapper.classes()).toContain('carryover')
    expect(wrapper.text()).toContain('DTS')
    await wrapper.get('[data-testid="task-progress"]').trigger('click')
    const input = wrapper.get('input[type="text"]')
    await input.setValue('已定位重试问题')
    await input.trigger('keydown', { key: 'Enter', isComposing: true })
    expect(api.update).not.toHaveBeenCalled()
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith('/vault', task.filePath, 4, task.sourceLine, 'progress', '已定位重试问题', '2026-09-08')
    expect(wrapper.find('input[type="text"]').exists()).toBe(false)
  })

  it('retains entered text and shows a useful error when Markdown changed externally', async () => {
    api.update.mockRejectedValue(new Error('文件已变化，请刷新后重试'))
    await render()
    await wrapper.get('[data-testid="task-blocker"]').trigger('click')
    await wrapper.get('input[type="text"]').setValue('缺环境权限')
    await wrapper.get('[data-testid="task-save-entry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('文件已变化')
    expect((wrapper.get('input[type="text"]').element as HTMLInputElement).value).toBe('缺环境权限')
  })
})
