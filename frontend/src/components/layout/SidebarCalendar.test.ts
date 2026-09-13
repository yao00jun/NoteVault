// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia, disposePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { FileService } from '@/api'
import SidebarCalendar from './SidebarCalendar.vue'

vi.mock('@/api', () => ({
  FileService: { GetFileTree: vi.fn().mockResolvedValue([]) },
}))
// openTodayWorkLog 依赖工作台服务，这里只验证侧栏日历会调用它
const openTodayWorkLog = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useWorkLog', () => ({
  useWorkLog: () => ({ openTodayWorkLog }),
}))

enableAutoUnmount(afterEach)

const piniaInstances: Pinia[] = []

function mountCalendar() {
  const pinia = createPinia()
  piniaInstances.push(pinia)
  setActivePinia(pinia)
  const workspaceStore = useWorkspaceStore()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/editor', component: { template: '<div />' } }],
  })
  const wrapper = mount(SidebarCalendar, { global: { plugins: [pinia, router, i18n] } })
  return { wrapper, workspaceStore, router }
}

const now = new Date()
const year = now.getFullYear()
const month = String(now.getMonth() + 1).padStart(2, '0')
const todayDay = String(now.getDate()).padStart(2, '0')
const today = `${year}-${month}-${todayDay}`
const monthPrefix = `${year}-${month}-`
// 本月 12 日有旧式日记，本月今天有日报（日期动态生成，测试不随月份漂移）
const dailyTree = [
  { name: 'Daily', path: 'Daily', fullPath: '/Daily', isDir: true, children: [
    { name: `${monthPrefix}12.md`, path: `Daily/${monthPrefix}12.md`, fullPath: `/Daily/${monthPrefix}12.md`, isDir: false },
    { name: 'Reports', path: 'Daily/Reports', fullPath: '/Daily/Reports', isDir: true, children: [
      { name: `${today}-日报.md`, path: `Daily/Reports/${today}-日报.md`, fullPath: `/Daily/Reports/${today}-日报.md`, isDir: false },
    ] },
  ] },
]

describe('SidebarCalendar', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    openTodayWorkLog.mockClear()
  })

  afterEach(() => {
    piniaInstances.splice(0).forEach(disposePinia)
  })

  it('renders the month header, weekday row and 42 cells', async () => {
    const { wrapper, workspaceStore } = mountCalendar()
    workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
    vi.mocked(FileService.GetFileTree).mockResolvedValue(dailyTree as never)
    await flushPromises()

    expect(wrapper.find('.calendar-month').text()).toBe(`${year}年${now.getMonth() + 1}月`)
    expect(wrapper.findAll('.calendar-weekday')).toHaveLength(7)
    expect(wrapper.findAll('.calendar-weekday').at(0)!.text()).toBe('一')
    expect(wrapper.findAll('.calendar-cell')).toHaveLength(42)
  })

  it('marks logged days with a dot and today with highlight', async () => {
    const { wrapper, workspaceStore } = mountCalendar()
    workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
    vi.mocked(FileService.GetFileTree).mockResolvedValue(dailyTree as never)
    await flushPromises()

    const loggedDays = wrapper.findAll('button.calendar-cell.has-log').map(cell => cell.find('.calendar-day').text())
    expect(loggedDays).toContain('12')
    expect(loggedDays).toContain(todayDay)
    expect(wrapper.findAll('.calendar-cell.is-today')).toHaveLength(1)
    // 无打卡的普通日期渲染为不可交互的 span
    expect(wrapper.findAll('span.calendar-cell').length).toBeGreaterThan(0)
  })

  it('navigates to the daily log when a logged date is clicked', async () => {
    const { wrapper, workspaceStore, router } = mountCalendar()
    workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
    vi.mocked(FileService.GetFileTree).mockResolvedValue(dailyTree as never)
    await flushPromises()

    const logged = wrapper.findAll('button.calendar-cell.has-log').find(cell => cell.find('.calendar-day').text() === '12')!
    await logged.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe(`Daily/${monthPrefix}12.md`)
    expect(workspaceStore.activeFile).toBe(`Daily/${monthPrefix}12.md`)
  })

  it('offers to create today work log when clicking today without a log', async () => {
    const { wrapper, workspaceStore } = mountCalendar()
    workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
    vi.mocked(FileService.GetFileTree).mockResolvedValue([
      { name: 'Daily', path: 'Daily', fullPath: '/Daily', isDir: true, children: [] },
    ] as never)
    await flushPromises()

    const todayCell = wrapper.findAll('button.calendar-cell.is-today')[0]!
    await todayCell.trigger('click')
    await flushPromises()
    expect(openTodayWorkLog).toHaveBeenCalledTimes(1)
  })

  it('shifts months with prev/next and returns via the today button', async () => {
    const { wrapper, workspaceStore } = mountCalendar()
    workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
    await flushPromises()

    const navButtons = wrapper.findAll('.calendar-nav-btn')
    expect(navButtons).toHaveLength(3) // ‹ / 今日 / ›
    await navButtons[0]!.trigger('click')
    const previous = new Date(year, now.getMonth() - 1, 1)
    expect(wrapper.find('.calendar-month').text()).toBe(`${previous.getFullYear()}年${previous.getMonth() + 1}月`)
    await navButtons[2]!.trigger('click') // ›
    expect(wrapper.find('.calendar-month').text()).toBe(`${year}年${now.getMonth() + 1}月`)
    await navButtons[2]!.trigger('click')
    const next = new Date(year, now.getMonth() + 1, 1)
    expect(wrapper.find('.calendar-month').text()).toBe(`${next.getFullYear()}年${next.getMonth() + 1}月`)
    await navButtons[1]!.trigger('click') // 今日
    expect(wrapper.find('.calendar-month').text()).toBe(`${year}年${now.getMonth() + 1}月`)
  })
})

