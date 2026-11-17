// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia, disposePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { nextTick } from 'vue'
import { i18n } from '@/i18n'

// 只替换原生服务边界；workspace store 与 toast 保持真实实现
vi.mock('@/api', () => ({
  FileService: { GetFileTree: vi.fn(async () => []) },
}))

vi.mock('@wailsio/runtime', () => ({
  Dialogs: { OpenFile: vi.fn() },
  Events: { On: vi.fn(() => vi.fn()) },
  default: { Dialogs: { OpenFile: vi.fn() } },
}))

import QuickAccessCard from './QuickAccessCard.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { resetToasts, useToast } from '@/composables/useToast'

const workspace = { id: 'ws_1', name: '我的笔记库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '' }
const piniaInstances: Pinia[] = []
const routes = [
  { path: '/', redirect: '/today' },
  ...['today', 'projects', 'learning', 'editor'].map(path => ({
    path: '/' + path, component: { template: '<div>' + path + '</div>' },
  })),
]

function mountCard() {
  const pinia = createPinia()
  piniaInstances.push(pinia)
  setActivePinia(pinia)
  const workspaceStore = useWorkspaceStore()
  workspaceStore.setCurrentWorkspace({ ...workspace })
  const router = createRouter({ history: createMemoryHistory(), routes })
  const wrapper = mount(QuickAccessCard, { global: { plugins: [pinia, router, i18n] } })
  return { wrapper, workspaceStore, router }
}

enableAutoUnmount(afterEach)

describe('QuickAccessCard', () => {
  beforeEach(async () => {
    await flushPromises()
    localStorage.clear()
    document.body.innerHTML = ''
    vi.clearAllMocks()
    resetToasts()
  })

  afterEach(() => {
    resetToasts()
    piniaInstances.splice(0).forEach(disposePinia)
  })

  it('starts on the pinned tab and explains the empty state', async () => {
    const { wrapper } = mountCard()
    await flushPromises()
    expect(wrapper.get('[data-testid="quick-tab-pins"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="quick-pins-empty"]').text()).toContain('固定')
  })

  it('switches to the recent tab and opens a recent document', async () => {
    const { wrapper, workspaceStore, router } = mountCard()
    workspaceStore.openFile('笔记.md')
    await flushPromises()
    await wrapper.get('[data-testid="quick-tab-recent"]').trigger('click')
    await nextTick()

    const rows = wrapper.findAll('[data-testid="quick-recent-file"]')
    expect(rows).toHaveLength(1)
    await rows[0].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe('笔记.md')
  })

  it.each([
    ['Projects/NoteVault/project.md', '/projects', 'project', 'Projects/NoteVault'],
    ['Areas/Books/Go/book.md', '/learning', 'book', 'Areas/Books/Go'],
    ['Resources/接口联调.md', '/editor', 'file', 'Resources/接口联调.md'],
  ])('opens pinned %s at its content destination', async (path, destination, queryKey, queryValue) => {
    const { wrapper, workspaceStore, router } = mountCard()
    workspaceStore.togglePin(path, '重点内容')
    await flushPromises()
    await wrapper.get('[data-testid="quick-pin"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(destination)
    expect(router.currentRoute.value.query[queryKey]).toBe(queryValue)
    if (destination === '/editor') expect(workspaceStore.activeFile).toBe(path)
  })

  it('reorders pins with Alt+Arrow', async () => {
    const { wrapper, workspaceStore } = mountCard()
    workspaceStore.togglePin('first.md', '第一篇')
    workspaceStore.togglePin('second.md', '第二篇')
    await flushPromises()
    await wrapper.findAll('[data-testid="quick-pin"]')[1].trigger('keydown', { key: 'ArrowUp', altKey: true })
    expect(wrapper.findAll('[data-testid="quick-pin"]').map(item => item.text())).toEqual(['第二篇', '第一篇'])
  })

  it('reorders a dragged pin at the dropped position', async () => {
    const { wrapper, workspaceStore } = mountCard()
    workspaceStore.togglePin('first.md', '第一篇')
    workspaceStore.togglePin('second.md', '第二篇')
    await flushPromises()
    const pins = wrapper.findAll('[data-testid="quick-pin"]')
    await pins[0].trigger('dragstart')
    await pins[1].trigger('drop')
    expect(wrapper.findAll('[data-testid="quick-pin"]').map(item => item.text())).toEqual(['第二篇', '第一篇'])
  })

  it('removes a pin through its context menu without navigating', async () => {
    const { wrapper, workspaceStore, router } = mountCard()
    workspaceStore.togglePin('first.md', '第一篇')
    await router.push('/today')
    await flushPromises()
    await wrapper.get('[data-testid="quick-pin"]').trigger('contextmenu', { clientX: 20, clientY: 80 })
    const unpin = document.querySelector<HTMLButtonElement>('[data-testid="quick-unpin"]')
    expect(unpin).not.toBeNull()
    unpin!.click()
    await nextTick()
    expect(workspaceStore.pinnedItems).toEqual([])
    expect(wrapper.find('[data-testid="quick-pin"]').exists()).toBe(false)
    expect(router.currentRoute.value.path).toBe('/today')
  })

  it('limits pins to eight and explains why a ninth pin was not added', async () => {
    const { wrapper, workspaceStore } = mountCard()
    for (let index = 0; index < 8; index++) workspaceStore.togglePin('note-' + index + '.md')
    workspaceStore.openFile('ninth.md')
    await flushPromises()
    await wrapper.get('[data-testid="quick-pin-current"]').trigger('click')
    expect(wrapper.findAll('[data-testid="quick-pin"]')).toHaveLength(8)
    expect(workspaceStore.isPinned('ninth.md')).toBe(false)
    expect(useToast().toasts.value.some(toast => toast.message.includes('8'))).toBe(true)
  })

  it('renders the calendar when its tab is selected', async () => {
    const { wrapper } = mountCard()
    await wrapper.get('[data-testid="quick-tab-calendar"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sidebar-calendar"]').exists()).toBe(true)
  })
})
