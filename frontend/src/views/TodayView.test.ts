// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import TodayView from './TodayView.vue'

const harness = vi.hoisted(() => ({ state: {} as Record<string, unknown>, addTask: vi.fn() }))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))
vi.mock('@/composables/useDailyNote', () => ({ useDailyNote: () => ({ openTodayNote: vi.fn() }) }))
vi.mock('@/components/workbench/TaskCard.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/workbench/InterviewReview.vue', () => ({ default: { template: '<div />' } }))

enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  harness.addTask.mockReset().mockResolvedValue(undefined)
  harness.state = reactive({
    today: '2026-09-08', todayTasks: [], blockers: [], reviewQueue: [], reviewedToday: 0,
    dueReminders: [], progress: [], warnings: [], loading: false, busy: false,
    error: '', reminderError: '', lastUpdated: '2026-09-08T10:00:00+08:00',
    projects: [{ name: 'Project A', folder: 'Projects/A', path: 'Projects/A/project.md' }],
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
  ] })
  await router.push('/today')
  const wrapper = mount(TodayView, { global: { plugins: [pinia, router] } })
  const button = (label: string) => wrapper.findAll('button').find(item => item.text().includes(label))!
  return { wrapper, workspace, router, button }
}

describe('Today workspace boundaries', () => {
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
    workspace.setCurrentWorkspace({ id: 'b', name: 'B', path: '/b', createdAt: '', lastOpenedAt: '' })
    await flushPromises()
    expect(wrapper.find('[aria-label="任务标题"]').exists()).toBe(false)
    await button('新任务').trigger('click')
    expect((wrapper.get('[aria-label="任务标题"]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[aria-label="归属项目"]').element as HTMLSelectElement).value).toBe('')
    expect((wrapper.get('[aria-label="任务截止日期"]').element as HTMLInputElement).value).toBe('2026-09-09')
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
})
