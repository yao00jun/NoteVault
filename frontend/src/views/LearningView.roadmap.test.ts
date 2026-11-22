// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useToast } from '@/composables/useToast'
import LearningView from './LearningView.vue'
import { localDateKey } from '@/utils/workbench'

const harness = vi.hoisted(() => ({
  state: {} as Record<string, unknown>,
  progress: vi.fn(),
  decompose: vi.fn(),
  blockerResult: vi.fn(),
  createFile: vi.fn(),
  refresh: vi.fn(),
  handlers: {} as Record<string, (ev?: unknown) => void>,
}))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))
vi.mock('@/api', () => ({
  AppService: { OpenWorkspaceAttachment: vi.fn().mockResolvedValue(undefined) },
  RoadmapService: { GetRoadmapProgress: harness.progress },
  ReportService: { StartDecomposeBlocker: harness.decompose, DecomposeTaskResult: harness.blockerResult },
  FileService: { CreateFile: harness.createFile },
  TaskService: { Cancel: vi.fn() },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
}))
vi.mock('@/composables/useCopilotRequest', () => ({ requestCopilot: vi.fn() }))
vi.mock('@/composables/useSourceImport', () => ({ requestSourceImport: vi.fn() }))
vi.mock('@wailsio/runtime', () => ({
    Events: {
      On: (name: string, cb: (ev?: unknown) => void) => {
        harness.handlers[name] = cb
        return () => {}
      },
    },
  }))

enableAutoUnmount(afterEach)

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  harness.decompose.mockReset().mockResolvedValue('task-9')
  harness.blockerResult.mockReset().mockResolvedValue({
    concept: 'RAG 分块策略', context: '', message: '',
    prerequisites: [{ name: 'Python 基础', reason: '先会读代码', hours: 1.5 }],
  })
  harness.createFile.mockReset().mockResolvedValue('Inbox/卡点/x.md')
  harness.handlers = {}
  harness.state = reactive({
    books: [], cards: [], documents: [], reviewTarget: 0, reviewedToday: 0,
    reviewQueue: [], radar: { path: '', content: '' }, loading: false, error: '',
    addTask: vi.fn(), refresh: harness.refresh,
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
  return { wrapper, router }
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
          { name: '轨一：语言基础', total: 3, done: 1, percent: 33, totalHours: 5, doneHours: 1, next: '[[第2章 语法基础]] #est/1.5h', nextHours: 1.5 },
          { name: '轨二：AI 应用开发', total: 1, done: 0, percent: 0, totalHours: 2.5, doneHours: 0 },
        ],
        total: 4, done: 1, percent: 25, totalHours: 7.5, doneHours: 1, nextStep: '[[第2章 语法基础]] #est/1.5h', nextHours: 1.5,
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
    expect(card.text()).toContain('下一步')
    expect(card.text()).toContain('[[第2章 语法基础]] #est/1.5h')
    expect(card.text()).toContain('约 1.5h')
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

describe('Learning blocker decomposition (学习卡点拆解)', () => {
  it('submits the blocker task, renders prerequisites and saves to Inbox', async () => {
    const { wrapper, router } = await render()
    await wrapper.get('[data-testid="blocker-record"]').trigger('click')
    await wrapper.get('[data-testid="blocker-concept"]').setValue('RAG 分块策略')
    await wrapper.get('[data-testid="blocker-context"]').setValue('在读第 4 章')
    useSettingsStore().settings.ai.baseURL = 'http://127.0.0.1:11434/v1'
    useSettingsStore().settings.ai.model = 'qwen2.5:7b'
    useSettingsStore().settings.ai.protocol = 'openai-chat'
    await wrapper.get('[data-testid="blocker-submit"]').trigger('click')
    await flushPromises()

    expect(harness.decompose).toHaveBeenCalledWith('RAG 分块策略', '在读第 4 章', {
      baseURL: 'http://127.0.0.1:11434/v1', model: 'qwen2.5:7b', apiKey: '', protocol: 'openai-chat',
    })
    // finished 事件 → 按 ID 取回结果 → 前置清单渲染
    harness.handlers['task:finished']?.({ data: { id: 'task-9', status: 'succeeded' } })
    await flushPromises()
    const result = wrapper.get('[data-testid="blocker-result"]')
    expect(result.text()).toContain('Python 基础')
    expect(result.text()).toContain('先会读代码')

    await wrapper.get('[data-testid="blocker-save"]').trigger('click')
    await flushPromises()
    const today = localDateKey()
    const notePath = `Inbox/卡点/${today}-RAG 分块策略.md`
    expect(harness.createFile).toHaveBeenCalledWith(
      'E:/Vault',
      notePath,
      expect.stringContaining('# 卡点：RAG 分块策略'),
    )
    expect(router.currentRoute.value.query.file).toBe(notePath)
  })

  it('warns instead of decomposing when the concept is blank', async () => {
    const { wrapper } = await render()
    await wrapper.get('[data-testid="blocker-record"]').trigger('click')
    await wrapper.get('[data-testid="blocker-submit"]').trigger('click')
    await flushPromises()
    expect(harness.decompose).not.toHaveBeenCalled()
    expect(useToast().toasts.value.some(t => t.message.includes('先写下看不懂的概念'))).toBe(true)
  })
})
