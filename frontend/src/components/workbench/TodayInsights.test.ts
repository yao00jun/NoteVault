// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import TodayInsights from './TodayInsights.vue'

const harness = vi.hoisted(() => ({
  activity: vi.fn(),
  state: {} as Record<string, unknown>,
}))

vi.mock('@/api', () => ({
  StatsService: { GetWritingActivity: harness.activity },
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))

enableAutoUnmount(afterEach)

const days = Array.from({ length: 35 }, (_, i) => {
  const d = new Date(2026, 10, 1 + i)
  const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return { date: key, edited: i % 3 === 0 ? 2 : 0 }
})

async function renderInsights() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'a', name: 'A', path: '/ws', createdAt: '', lastOpenedAt: '' })
  const wrapper = mount(TodayInsights, { global: { plugins: [pinia, i18n] } })
  await flushPromises()
  return { wrapper }
}

beforeEach(() => {
  localStorage.clear()
  ;(i18n.global.locale as any).value = 'zh-CN'
  harness.activity.mockReset().mockResolvedValue({
    days,
    activeDays: 12,
    longestStreak: 4,
    weekEdited: 7,
  })
  harness.state = reactive({
    projects: [{ folder: 'Projects/A', status: 'active' }, { folder: 'Projects/B', status: 'done' }],
    tasks: [{ completed: false }, { completed: true }, { completed: false }],
    blockers: [{ id: 'b1' }],
    reviewQueue: [{ id: 'r1' }, { id: 'r2' }],
    dueReminders: [{ id: 'd1' }],
  })
})

describe('TodayInsights · 今日页手风琴（P2-3）', () => {
  it('默认全部收起，不触发活动度请求', async () => {
    const { wrapper } = await renderInsights()
    expect(wrapper.find('[data-testid="fold-calendar-body"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="fold-overview-body"]').exists()).toBe(false)
    expect(harness.activity).not.toHaveBeenCalled()
  })

  it('展开月历后拉一次 35 天活动度并渲染月历与热力条', async () => {
    const { wrapper } = await renderInsights()
    await wrapper.get('[data-testid="fold-calendar"]').trigger('click')
    await flushPromises()

    expect(harness.activity).toHaveBeenCalledWith('/ws', 35)
    const body = wrapper.get('[data-testid="fold-calendar-body"]')
    expect(body.text()).toContain('活跃 12 天')
    expect(body.text()).toContain('最长连续 4 天')
    // 28 格热力条 + 5 档图例
    expect(body.findAll('.heat-strip i')).toHaveLength(28)
    expect(wrapper.find('[data-testid="mini-calendar"]').exists()).toBe(true)
    // 有改动的日期在月历里打上 active 类
    expect(wrapper.findAll('.cal-cell.active').length).toBeGreaterThan(0)
  })

  it('手风琴互斥：展开速览会收起月历，且速览数值来自 store', async () => {
    const { wrapper } = await renderInsights()
    await wrapper.get('[data-testid="fold-calendar"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="fold-overview"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="fold-calendar-body"]').exists()).toBe(false)
    const overview = wrapper.get('[data-testid="fold-overview-body"]')
    expect(overview.get('[data-testid="overview-projects"]').text()).toContain('1')
    expect(overview.get('[data-testid="overview-tasks"]').text()).toContain('2')
    expect(overview.get('[data-testid="overview-blockers"]').text()).toContain('1')
    expect(overview.get('[data-testid="overview-review"]').text()).toContain('2')
    expect(overview.get('[data-testid="overview-reminders"]').text()).toContain('1')
  })

  it('记住上次展开的面板', async () => {
    localStorage.setItem('notevault.today.fold', 'overview')
    const { wrapper } = await renderInsights()
    expect(wrapper.find('[data-testid="fold-overview-body"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="fold-overview"]').attributes('aria-expanded')).toBe('true')
  })

  it('再点一次同一块即收起，并清掉记忆', async () => {
    const { wrapper } = await renderInsights()
    const head = wrapper.get('[data-testid="fold-overview"]')
    await head.trigger('click')
    expect(wrapper.find('[data-testid="fold-overview-body"]').exists()).toBe(true)
    await head.trigger('click')
    expect(wrapper.find('[data-testid="fold-overview-body"]').exists()).toBe(false)
    expect(localStorage.getItem('notevault.today.fold')).toBeNull()
  })
})
