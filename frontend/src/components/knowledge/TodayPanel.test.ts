// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  StatsService: { GetTodayStats: vi.fn() },
}))

import TodayPanel from './TodayPanel.vue'
import { StatsService } from '@bindings/github.com/notevault/notevault/index.js'
import { useWorkspaceStore } from '@/stores/workspace'

const mockedStats = vi.mocked(StatsService.GetTodayStats)

function mountPanel() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useWorkspaceStore()
  // 组件只读 path / id，不依赖真实工作区对象
  ;(store as any).currentWorkspace = { id: 'w1', name: 'WS', path: 'C:\\ws' }
  return { wrapper: mount(TodayPanel, { global: { plugins: [pinia, i18n] } }), store }
}

const sampleStats = {
  editedToday: 3,
  streakDays: 5,
  pendingTodos: 4,
  highPriorityTodos: 1,
  dueReminders: 2,
  recentFiles: ['notes/a.md', 'notes/b.md'],
}

describe('TodayPanel（今日工作台条带）', () => {
  beforeEach(() => {
    mockedStats.mockReset()
    localStorage.clear()
  })

  it('渲染四个统计格与最近笔记 chips', async () => {
    mockedStats.mockResolvedValue(sampleStats as any)
    const { wrapper } = mountPanel()
    await flushPromises()

    const cells = wrapper.findAll('.today-cell')
    expect(cells).toHaveLength(4)
    expect(cells[0].text()).toContain('3')
    expect(cells[1].text()).toContain('5')

    const chips = wrapper.findAll('.today-recent-chip')
    expect(chips).toHaveLength(2)
    expect(chips[0].text()).toBe('a')
  })

  it('高优先级待办与到期提醒标红警示', async () => {
    mockedStats.mockResolvedValue(sampleStats as any)
    const { wrapper } = mountPanel()
    await flushPromises()

    const values = wrapper.findAll('.today-cell-value')
    const warned = values.filter((v) => v.classes().includes('warn'))
    expect(warned.length).toBe(2)
  })

  it('统计加载失败时静默隐藏整条面板，不让主页崩掉', async () => {
    mockedStats.mockRejectedValue(new Error('bridge down'))
    const { wrapper } = mountPanel()
    await flushPromises()

    expect(wrapper.find('.today-panel').exists()).toBe(false)
  })

  it('点击最近笔记 chip 会打开对应文件', async () => {
    mockedStats.mockResolvedValue(sampleStats as any)
    const { wrapper, store } = mountPanel()
    await flushPromises()

    const openSpy = vi.spyOn(store, 'openFile').mockImplementation(() => {})
    await wrapper.findAll('.today-recent-chip')[0].trigger('click')
    expect(openSpy).toHaveBeenCalledWith('notes/a.md')
  })
})
