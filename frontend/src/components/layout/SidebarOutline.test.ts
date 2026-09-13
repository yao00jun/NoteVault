// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia, disposePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import SidebarOutline from './SidebarOutline.vue'

enableAutoUnmount(afterEach)

const piniaInstances: Pinia[] = []

async function mountOutline(path = '/editor', file?: string) {
  const pinia = createPinia()
  piniaInstances.push(pinia)
  setActivePinia(pinia)
  const workspaceStore = useWorkspaceStore()
  workspaceStore.setCurrentWorkspace({ id: 'ws', name: 'A', path: 'C:/vault', createdAt: '', lastOpenedAt: '' })
  if (file) workspaceStore.openFile(file)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/editor', component: { template: '<div />' } }, { path: '/today', component: { template: '<div />' } }],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(SidebarOutline, { global: { plugins: [pinia, router, i18n] } })
  return { wrapper, workspaceStore, router }
}

function broadcast(file: string, items: { level: number; text: string; line: number }[]) {
  window.dispatchEvent(new CustomEvent('notevault:outline-changed', { detail: { file, items } }))
}

describe('SidebarOutline', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    piniaInstances.splice(0).forEach(disposePinia)
  })

  it('renders broadcasted multi-level outline with per-level indentation', async () => {
    const { wrapper } = await mountOutline('/editor', 'Notes/a.md')
    broadcast('Notes/a.md', [
      { level: 1, text: '总览', line: 0 },
      { level: 2, text: '安装', line: 4 },
      { level: 3, text: '依赖', line: 8 },
      { level: 4, text: '可选组件', line: 12 },
    ])
    await flushPromises()
    const rendered = wrapper.findAll('.outline-item')
    expect(rendered).toHaveLength(4)
    expect(rendered.map(item => item.find('.outline-text').text())).toEqual(['总览', '安装', '依赖', '可选组件'])
    expect(rendered[0]!.find('.outline-level').classes()).toContain('outline-level-1')
    // 缩进随层级递增：(level-1)*10+6
    expect(rendered[0]!.attributes('style')).toContain('padding-left: 6px')
    expect(rendered[1]!.attributes('style')).toContain('padding-left: 16px')
    expect(rendered[3]!.attributes('style')).toContain('padding-left: 36px')
  })

  it('ignores broadcasts for other documents than the active one', async () => {
    const { wrapper } = await mountOutline('/editor', 'Notes/a.md')
    broadcast('Notes/a.md', [{ level: 1, text: '本文档', line: 0 }])
    await flushPromises()
    broadcast('Notes/other.md', [{ level: 1, text: '别的文档', line: 0 }])
    await flushPromises()
    expect(wrapper.findAll('.outline-item .outline-text').map(item => item.text())).toEqual(['本文档'])
  })

  it('dispatches outline-jump with the heading line when clicked', async () => {
    const { wrapper } = await mountOutline('/editor', 'Notes/a.md')
    broadcast('Notes/a.md', [
      { level: 1, text: '总览', line: 0 },
      { level: 2, text: '安装', line: 4 },
    ])
    await flushPromises()
    const jumps: number[] = []
    window.addEventListener('notevault:outline-jump', (event) => {
      jumps.push((event as CustomEvent<{ line: number }>).detail.line)
    })
    await wrapper.findAll('.outline-item')[1]!.trigger('click')
    expect(jumps).toEqual([4])
  })

  it('navigates to the editor instead of jumping when mounted outside the editor page', async () => {
    const { wrapper, router } = await mountOutline('/today', 'Notes/a.md')
    broadcast('Notes/a.md', [{ level: 1, text: '总览', line: 0 }])
    await flushPromises()
    const jumps: number[] = []
    window.addEventListener('notevault:outline-jump', (event) => {
      jumps.push((event as CustomEvent<{ line: number }>).detail.line)
    })
    await wrapper.get('.outline-item').trigger('click')
    await flushPromises()
    expect(jumps).toEqual([])
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe('Notes/a.md')
  })

  it('shows empty states when no document is open or no headings exist', async () => {
    const { wrapper, workspaceStore } = await mountOutline('/editor')
    await flushPromises()
    expect(wrapper.find('.outline-empty').text()).toContain('打开文档查看大纲')

    workspaceStore.openFile('Notes/empty.md')
    broadcast('Notes/empty.md', [])
    await flushPromises()
    expect(wrapper.find('.outline-empty').text()).toContain('暂无标题大纲')
  })
})
