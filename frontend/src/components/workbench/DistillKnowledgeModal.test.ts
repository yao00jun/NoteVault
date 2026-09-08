// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import type { DistillRequest, WorkbenchSnapshot } from '@/api/workbench'
import type { DistillationSource } from '@/utils/distillKnowledge'
import DistillKnowledgeModal from './DistillKnowledgeModal.vue'

const api = vi.hoisted(() => ({ snapshot: vi.fn(), answer: vi.fn() }))
vi.mock('@/api', () => ({
  WorkbenchService: { GetWorkbench: api.snapshot },
  ReminderService: { GetAllReminders: vi.fn(async () => []) },
  QnAService: { Answer: api.answer },
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))

const source: DistillationSource = { workspacePath: '/vault/a', path: 'Projects/物流中台/Kafka排查.md', title: 'Kafka排查', content: '---\ntags: [Go]\n---\n# Kafka排查\n\n限制并发并观测队列长度。' }
const snapshot: WorkbenchSnapshot = {
  date: '2026-09-09', tasks: [], projects: [], cards: [], progress: [], warnings: [], indexedAt: '', radar: { path: '', content: '' },
  books: [{ name: 'Go进阶', path: 'Learning/Go/book.md', folder: 'Learning/Go', status: '在学', progress: 0, projects: [], chapters: [], noteCount: 0, reviewCount: 0 }],
  documents: [{ path: 'Learning/面试宝典/Go面试真题.md', title: 'Go面试真题', modifiedAt: '' }],
}
let pinia: Pinia
let wrappers: VueWrapper[]

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  api.snapshot.mockResolvedValue(snapshot)
  api.answer.mockResolvedValue({ answer: '{"title":"协程泄漏排查","summary":"1. 观察指标\\n2. 限制并发"}', citations: [] })
  pinia = createPinia()
  setActivePinia(pinia)
  wrappers = []
  useWorkspaceStore().setCurrentWorkspace({ id: 'a', name: 'A', path: '/vault/a', createdAt: '', lastOpenedAt: '' })
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); disposePinia(pinia); document.body.innerHTML = ''; vi.restoreAllMocks() })

async function mountModal(persist = vi.fn<(request: DistillRequest) => Promise<void>>().mockResolvedValue(undefined)) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/editor', component: { template: '<div />' } }] })
  await router.push('/editor')
  const wrapper = mount(DistillKnowledgeModal, { props: { source, persist }, attachTo: document.body, global: { plugins: [pinia, router, i18n], stubs: { teleport: true } } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, persist }
}

describe('DistillKnowledgeModal', () => {
  it('submits an editable book chapter and reports its exact Markdown destination', async () => {
    const { wrapper, persist } = await mountModal()
    expect(wrapper.get('[role="dialog"]').attributes('aria-modal')).toBe('true')
    expect(wrapper.text()).toContain(source.path)
    await wrapper.get('[data-testid="distill-title"]').setValue('Kafka消息积压')
    await wrapper.get('[data-testid="distill-summary"]').setValue('先观察指标，再限制消费并发。')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(persist).toHaveBeenCalledWith({ sourceFile: source.path, targetMode: 'book', targetFolder: 'Learning/Go', targetTitle: 'Kafka消息积压', summary: '先观察指标，再限制消费并发。', question: '', answer: '' })
    expect(wrapper.emitted('saved')?.[0]).toEqual(['Learning/Go/Kafka消息积压.md'])
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('supports a new technology book without creating it before submission', async () => {
    const { wrapper, persist } = await mountModal()
    await wrapper.get('[data-testid="distill-book"]').setValue('__new__')
    await wrapper.get('[data-testid="distill-new-book"]').setValue('Kafka')
    expect(wrapper.get('[data-testid="distill-destination"]').text()).toBe('Learning/Kafka/Kafka排查.md')
    expect(persist).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(persist.mock.calls[0]?.[0].targetFolder).toBe('Learning/Kafka')
  })

  it('writes an interview question to an existing or new topic and explains its review interval', async () => {
    const { wrapper, persist } = await mountModal()
    await wrapper.get('[data-testid="distill-mode-interview"]').trigger('click')
    expect(wrapper.text()).toContain('7 天后')
    expect(wrapper.find('option[value="Go面试真题"]').exists()).toBe(true)
    await wrapper.get('[data-testid="distill-topic"]').setValue('架构与中间件.md')
    await wrapper.get('[data-testid="distill-question"]').setValue('如何定位消息积压？')
    await wrapper.get('[data-testid="distill-answer"]').setValue('检查消费速度并限制并发。')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(persist).toHaveBeenCalledWith({ sourceFile: source.path, targetMode: 'interview', targetFolder: 'Learning/面试宝典', targetTitle: '架构与中间件', question: '如何定位消息积压？', answer: '检查消费速度并限制并发。', summary: '' })
  })

  it('makes the default source-note answer valid beneath a level-three SRS question', async () => {
    const { wrapper, persist } = await mountModal()
    await wrapper.get('[data-testid="distill-mode-interview"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const payload = persist.mock.calls[0]?.[0]
    expect(payload?.answer).toContain('限制并发并观测队列长度')
    expect(payload?.answer).not.toMatch(/^#{1,3}\s/m)
  })

  it('keeps manual fields when switching between book and interview modes', async () => {
    const { wrapper } = await mountModal()
    await wrapper.get('[data-testid="distill-summary"]').setValue('My summary')
    await wrapper.get('[data-testid="distill-mode-interview"]').trigger('click')
    await wrapper.get('[data-testid="distill-answer"]').setValue('My answer')
    await wrapper.get('[data-testid="distill-mode-book"]').trigger('click')
    expect((wrapper.get('[data-testid="distill-summary"]').element as HTMLTextAreaElement).value).toBe('My summary')
    await wrapper.get('[data-testid="distill-mode-interview"]').trigger('click')
    expect((wrapper.get('[data-testid="distill-answer"]').element as HTMLTextAreaElement).value).toBe('My answer')
  })

  it('rejects path traversal and empty content before calling the persistence handler', async () => {
    const { wrapper, persist } = await mountModal()
    await wrapper.get('[data-testid="distill-title"]').setValue('../outside')
    expect(wrapper.get('[data-testid="distill-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(persist).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="distill-title"]').setValue('Valid')
    await wrapper.get('[data-testid="distill-summary"]').setValue('  ')
    await wrapper.get('form').trigger('submit')
    expect(persist).not.toHaveBeenCalled()
  })

  it('retains the form after a failed write and permits a deliberate retry', async () => {
    const persist = vi.fn<(request: DistillRequest) => Promise<void>>().mockRejectedValueOnce(new Error('同名笔记已存在')).mockResolvedValue(undefined)
    const { wrapper } = await mountModal(persist)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('同名笔记已存在')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect((wrapper.get('[data-testid="distill-summary"]').element as HTMLTextAreaElement).value).toContain('限制并发')
    await wrapper.get('[data-testid="distill-title"]').setValue('新的复盘')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(persist).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('suppresses duplicate submissions and closing while the Markdown write is pending', async () => {
    let finish!: () => void
    const persist = vi.fn<(request: DistillRequest) => Promise<void>>(() => new Promise<void>(resolve => { finish = resolve }))
    const { wrapper } = await mountModal(persist)
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(persist).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    finish()
    await flushPromises()
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('cannot submit source content into a newly selected workspace', async () => {
    const { wrapper, persist } = await mountModal()
    useWorkspaceStore().setCurrentWorkspace({ id: 'b', name: 'B', path: '/vault/b', createdAt: '', lastOpenedAt: '' })
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    expect(persist).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('discards a late completion after the original modal workspace changes', async () => {
    let finish!: () => void
    const persist = vi.fn<(request: DistillRequest) => Promise<void>>(() => new Promise<void>(resolve => { finish = resolve }))
    const { wrapper } = await mountModal(persist)
    await wrapper.get('form').trigger('submit')
    useWorkspaceStore().setCurrentWorkspace({ id: 'b', name: 'B', path: '/vault/b', createdAt: '', lastOpenedAt: '' })
    finish()
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
  })

  it('previews an AI draft and only replaces editable fields after explicit application', async () => {
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    const { wrapper, persist } = await mountModal()
    await wrapper.get('[data-testid="distill-summary"]').setValue('My draft')
    await wrapper.get('[data-testid="distill-ai"]').trigger('click')
    await flushPromises()
    expect(api.answer).toHaveBeenCalledTimes(1)
    expect(api.answer.mock.calls[0]?.at(-1)).toContain('限制并发并观测队列长度')
    expect((wrapper.get('[data-testid="distill-summary"]').element as HTMLTextAreaElement).value).toBe('My draft')
    expect(wrapper.get('[data-testid="distill-ai-draft"]').text()).toContain('协程泄漏排查')
    await wrapper.get('[data-testid="distill-apply-ai"]').trigger('click')
    expect((wrapper.get('[data-testid="distill-summary"]').element as HTMLTextAreaElement).value).toBe('1. 观察指标\n2. 限制并发')
    expect(persist).not.toHaveBeenCalled()
  })

  it('keeps manual input entered while an AI request is in flight', async () => {
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    let finish!: (value: { answer: string; citations: never[] }) => void
    api.answer.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const { wrapper } = await mountModal()
    await wrapper.get('[data-testid="distill-ai"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="distill-summary"]').setValue('New manual conclusion')
    finish({ answer: '{"summary":"AI conclusion"}', citations: [] })
    await flushPromises()
    expect((wrapper.get('[data-testid="distill-summary"]').element as HTMLTextAreaElement).value).toBe('New manual conclusion')
    expect(wrapper.get('[data-testid="distill-ai-draft"]').text()).toContain('AI conclusion')
  })

  it('keeps the manual draft when AI returns unusable content', async () => {
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    api.answer.mockResolvedValueOnce({ answer: 'not a JSON draft', citations: [] })
    const { wrapper } = await mountModal()
    await wrapper.get('[data-testid="distill-summary"]').setValue('My working draft')
    await wrapper.get('[data-testid="distill-ai"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('草稿')
    expect((wrapper.get('[data-testid="distill-summary"]').element as HTMLTextAreaElement).value).toBe('My working draft')
  })

  it('does not treat an IME Escape key as dismissing the dialog', async () => {
    const { wrapper } = await mountModal()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', isComposing: true, bubbles: true }))
    expect(wrapper.emitted('close')).toBeUndefined()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
