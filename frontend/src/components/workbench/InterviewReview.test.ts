// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
const api = vi.hoisted(() => ({ snapshot: vi.fn(), review: vi.fn() }))
vi.mock('@/api', () => ({ WorkbenchService: { GetWorkbench: api.snapshot, ReviewInterviewCard: api.review }, ReminderService: { GetAllReminders: vi.fn().mockResolvedValue([]) } }))
import InterviewReview from './InterviewReview.vue'
let cleanup = () => {}
afterEach(() => { cleanup(); vi.useRealTimers() })

describe('interview recall flow', () => {
  it('hides the answer until revealed and records the chosen rating before completing the session', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 8, 8, 10))
    const card = { id: 'q1', filePath: 'Learning/面试宝典/Go.md', lineIndex: 2, comment: '<!-- srs: {} -->', question: '什么是 Goroutine 泄漏？', answer: '**取消上下文**防止任务泄漏。', level: '', interval: 0, due: '2026-09-08', reps: 0, failures: 0, lastReviewed: '', weak: false }
    const data = { date: '2026-09-08', tasks: [], projects: [], books: [], cards: [card], progress: [], documents: [], radar: { path: '', content: '' }, indexedAt: '', warnings: [] }
    api.snapshot.mockImplementation(async () => structuredClone(data))
    api.review.mockImplementation(async () => { card.lastReviewed = '2026-09-08'; card.due = '2026-09-15' })
    const pinia = createPinia()
    setActivePinia(pinia)
    useWorkspaceStore().setCurrentWorkspace({ id: 'ws', name: '学习', path: '/vault', createdAt: '', lastOpenedAt: '' })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/editor', component: { template: '<div />' } }] })
    const wrapper = mount(InterviewReview, { global: { plugins: [pinia, router] } })
    cleanup = () => { wrapper.unmount(); disposePinia(pinia) }
    await flushPromises()
    expect(wrapper.text()).toContain('什么是 Goroutine 泄漏')
    expect(wrapper.text()).not.toContain('取消上下文')
    await wrapper.get('[data-testid="reveal-answer"]').trigger('click')
    expect(wrapper.text()).toContain('取消上下文')
    await wrapper.get('[data-testid="rate-mastered"]').trigger('click')
    await flushPromises()
    expect(api.review).toHaveBeenCalledWith('/vault', card.filePath, 2, card.comment, '掌握', '2026-09-08')
    expect(wrapper.text()).toContain('今日复习已完成')
  })
})
