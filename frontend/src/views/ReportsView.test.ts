// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  StatsService: { GetWritingActivity: vi.fn() },
  GraphService: { GetGraph: vi.fn() },
}))

import ReportsView from './ReportsView.vue'
import { StatsService, GraphService } from '@bindings/github.com/notevault/notevault/index.js'
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
})
