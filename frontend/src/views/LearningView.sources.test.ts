// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import LearningView from './LearningView.vue'

const harness = vi.hoisted(() => ({ state: {} as Record<string, unknown>, open: vi.fn(), intake: vi.fn(), copilot: vi.fn() }))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))
vi.mock('@/api', () => ({ AppService: { OpenWorkspaceAttachment: harness.open } }))
vi.mock('@/composables/useSourceImport', () => ({ requestSourceImport: harness.intake }))
vi.mock('@/composables/useCopilotRequest', () => ({ requestCopilot: harness.copilot }))

enableAutoUnmount(afterEach)
const folder = 'Learning/面试宝典'
beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  vi.clearAllMocks()
  harness.open.mockResolvedValue(undefined)
  harness.state = reactive({ books: [{ name: '面试宝典', path: `${folder}/book.md`, folder, status: 'unread', progress: 0, projects: [], chapters: [], noteCount: 0, reviewCount: 0,
    attachments: [{ name: '后端.pdf', path: `${folder}/后端.pdf`, size: 6000, notePath: '', warning: '未提取正文' }], studyPlanPath: `${folder}/ai-plan.md`, sourceRecordsPath: `${folder}/sources.md` }], cards: [], documents: [], reviewTarget: 0, reviewedToday: 0, reviewQueue: [], radar: { path: '', content: '' }, loading: false, error: '' })
})

async function render(query: Record<string, string> = { book: `${folder}/book.md` }) {
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ id: 'vault', name: 'Vault', path: 'E:/Vault', createdAt: '', lastOpenedAt: '' })
  const router = createRouter({ history: createMemoryHistory(), routes: ['/learning', '/editor', '/projects'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push({ path: '/learning', query })
  await router.isReady()
  const wrapper = mount(LearningView, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper, router }
}

describe('Learning book source materials', () => {
  it('shows imported PDFs even when no chapter text exists and opens the original safely', async () => {
    const { wrapper, router } = await render()
    expect(wrapper.get('[data-testid="book-sources"]').text()).toContain('后端.pdf')
    expect(wrapper.text()).toContain('已保存 1 份原始资料')
    expect(wrapper.text()).not.toContain('从第一篇章节笔记开始')
    await wrapper.get('[data-testid="book-open-attachment"]').trigger('click')
    expect(harness.open).toHaveBeenCalledWith('E:/Vault', `${folder}/后端.pdf`)
    expect(router.currentRoute.value.path).toBe('/learning')
    expect(wrapper.get('[data-testid="book-plan-study"]').attributes('disabled')).toBeDefined()
  })

  it('re-extracts saved PDFs in their owning book and exposes import records and AI guidance', async () => {
    const { wrapper, router } = await render()
    await wrapper.get('[data-testid="book-extract-pdfs"]').trigger('click')
    expect(harness.intake).toHaveBeenCalledWith({ kind: 'book', sourceType: 'attachments', name: '面试宝典', source: folder, targetFolder: folder, autoStart: true })
    await wrapper.get('[data-testid="book-source-records"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.file).toBe(`${folder}/sources.md`)
    expect(wrapper.find('[data-testid="book-study-plan"]').exists()).toBe(true)
  })

  it('shows the roadmap checkbox projection as a badge on the book card', async () => {
    // 书架卡片（不带 ?book= 进详情）：roadmapTotal>0 时显示路径勾选对照，全勾变绿
    const book = (harness.state.books as Array<Record<string, unknown>>)[0]!
    book.progress = 40
    book.roadmapDone = 2
    book.roadmapTotal = 2
    const { wrapper } = await render({})
    const badge = wrapper.get('[data-testid="book-roadmap-checked"]')
    expect(badge.text()).toContain('路径勾选 2/2')
    expect(badge.classes()).toContain('complete')
  })

  it('hides the roadmap badge for books no roadmap document references', async () => {
    // 夹具书没有 roadmapTotal（旧快照兼容）→ 不渲染徽章
    const { wrapper } = await render({})
    expect(wrapper.find('[data-testid="book-roadmap-checked"]').exists()).toBe(false)
  })
})
