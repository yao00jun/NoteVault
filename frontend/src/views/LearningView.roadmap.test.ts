// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import LearningView from './LearningView.vue'

const harness = vi.hoisted(() => ({
  state: {} as Record<string, unknown>,
  progress: vi.fn(),
}))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))
vi.mock('@/api', () => ({
  AppService: { OpenWorkspaceAttachment: vi.fn().mockResolvedValue(undefined) },
  RoadmapService: { GetRoadmapProgress: harness.progress },
}))
vi.mock('@/composables/useCopilotRequest', () => ({ requestCopilot: vi.fn() }))
vi.mock('@/composables/useSourceImport', () => ({ requestSourceImport: vi.fn() }))

enableAutoUnmount(afterEach)

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  harness.state = reactive({
    books: [], cards: [], documents: [], reviewTarget: 0, reviewedToday: 0,
    reviewQueue: [], radar: { path: '', content: '' }, loading: false, error: '',
  })
})

async function render() {
  setActivePinia(createPinia())
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'vault', name: 'Vault', path: 'E:/Vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: ['/learning', '/editor', '/projects'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push({ path: '/learning', query: { tab: 'roadmap' } })
  await router.isReady()
  const wrapper = mount(LearningView, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper }
}

describe('Learning roadmap progress projection', () => {
  it('projects roadmap documents as overall and per-track progress', async () => {
    harness.progress.mockResolvedValue([
      {
        path: 'Learning/AI转型路径-13周.md',
        title: 'AI 转型路径（13 周）',
        goal: '三个月内能承担 AI 应用开发任务',
        deadline: '2027-02-22',
        weeklyHours: '6',
        tracks: [
          { name: '轨一：语言基础', total: 3, done: 1, percent: 33, totalHours: 5, doneHours: 1 },
          { name: '轨二：AI 应用开发', total: 1, done: 0, percent: 0, totalHours: 2.5, doneHours: 0 },
        ],
        total: 4, done: 1, percent: 25, totalHours: 7.5, doneHours: 1,
      },
    ])
    const { wrapper } = await render()

    const card = wrapper.get('[data-testid="roadmap-progress"]')
    expect(card.text()).toContain('AI 转型路径（13 周）')
    expect(card.text()).toContain('三个月内能承担 AI 应用开发任务')
    expect(card.text()).toContain('25%')
    expect(card.text()).toContain('轨一：语言基础')
    expect(card.text()).toContain('1/3')
    expect(card.text()).toContain('1h / 共 5h')
    expect(card.text()).toContain('2027-02-22')
    expect(harness.progress).toHaveBeenCalledWith('E:/Vault')
  })

  it('shows the creation hint when no roadmap document exists', async () => {
    harness.progress.mockResolvedValue([])
    const { wrapper } = await render()
    expect(wrapper.get('[data-testid="roadmap-progress"]').text()).toContain('还没有路径文档')
    expect(wrapper.text()).toContain('kind: roadmap')
  })

  it('surfaces projection errors inline without breaking the tab', async () => {
    harness.progress.mockRejectedValue(new Error('目录读取失败'))
    const { wrapper } = await render()
    expect(wrapper.get('[role="alert"]').text()).toContain('目录读取失败')
    expect(wrapper.find('[data-testid="roadmap-progress"]').exists()).toBe(true)
  })
})
