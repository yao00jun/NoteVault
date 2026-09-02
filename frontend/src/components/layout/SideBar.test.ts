// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { nextTick } from 'vue'
import { i18n } from '@/i18n'

// 模拟 wails bindings — SideBar 依赖 WorkspaceService 和 FileService
vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  FileService: { CreateFile: vi.fn() },
  WorkspaceService: {
    ListWorkspaces: vi.fn().mockResolvedValue([]),
    SetCurrentWorkspace: vi.fn(),
    GetWorkspaceByID: vi.fn(),
    CreateWorkspace: vi.fn(),
  },
}))

// 模拟 @wailsio/runtime
vi.mock('@wailsio/runtime', () => ({
  Dialogs: { OpenFile: vi.fn() },
  default: { Dialogs: { OpenFile: vi.fn() } },
}))

import SideBar from './SideBar.vue'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'

const routes = [
  { path: '/', redirect: '/knowledge' },
  { path: '/knowledge', component: { template: '<div>knowledge</div>' } },
  { path: '/editor', component: { template: '<div>editor</div>' } },
  { path: '/search', component: { template: '<div>search</div>' } },
  { path: '/tags', component: { template: '<div>tags</div>' } },
  { path: '/qna', component: { template: '<div>qna</div>' } },
  { path: '/graph', component: { template: '<div>graph</div>' } },
  { path: '/todos', component: { template: '<div>todos</div>' } },
  { path: '/reminders', component: { template: '<div>reminders</div>' } },
  { path: '/archive', component: { template: '<div>archive</div>' } },
  { path: '/trash', component: { template: '<div>trash</div>' } },
  { path: '/settings', component: { template: '<div>settings</div>' } },
]

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes,
  })
}

function mountSideBar(router = createTestRouter()) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const settingsStore = useSettingsStore()
  // 强制重置 sidebarCollapsed，防止跨测试 localStorage 残留
  settingsStore.settings.sidebarCollapsed = false
  const workspaceStore = useWorkspaceStore()

  const wrapper = mount(SideBar, {
    global: {
      plugins: [pinia, router, i18n],
    },
  })
  return { wrapper, settingsStore, workspaceStore, router }
}

enableAutoUnmount(afterEach)

describe('SideBar', () => {
  beforeEach(async () => {
    // 清除 localStorage 避免跨测试污染（settings store 的 deep watcher 会异步写入）
    localStorage.clear()
    setActivePinia(createPinia())
    document.body.innerHTML = ''
    // 等待上一个测试的 deep watcher 异步回调完成后再继续
    await flushPromises()
  })

  it('应渲染侧边栏根元素', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    expect(wrapper.find('.sidebar').exists()).toBe(true)
  })

  it('应渲染所有导航项', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    const navItems = wrapper.findAll('.nav-item')
    expect(navItems.length).toBe(16)
  })

  it('应包含四个分组标题', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    const headers = wrapper.findAll('.nav-group-header')
    expect(headers.length).toBe(4)
  })

  it('点击导航项应触发路由跳转', async () => {
    const { wrapper, router } = mountSideBar()
    await flushPromises()
    const navItems = wrapper.findAll('.nav-item')

    // 当前顺序：knowledge/graph/bases/canvas/todos/reminders/files/search/tags/...
    // index 7 = search（bases、canvas 在 library 组插入，使 search 后移两位；
    // compile 追加在 manage 组末尾，不影响 search 的索引位置）
    await navItems[7].trigger('click')
    await flushPromises()
    await nextTick()
    expect(router.currentRoute.value.path).toBe('/search')
  })

  it('折叠状态下应隐藏标签文字', async () => {
    const { wrapper, settingsStore } = mountSideBar()
    await flushPromises()
    settingsStore.toggleSidebar()
    await flushPromises()
    await nextTick()
    const labels = wrapper.findAll('.nav-label')
    expect(labels.length).toBe(0)
    const collapsedItems = wrapper.findAll('.nav-item.collapsed')
    expect(collapsedItems.length).toBeGreaterThan(0)
  })

  it('工作区选择器应显示当前工作区名', async () => {
    const { wrapper, workspaceStore } = mountSideBar()
    await flushPromises()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1',
      name: '我的笔记库',
      path: '/tmp/vault',
      createdAt: '',
      lastOpenedAt: '',
    })
    await flushPromises()
    await nextTick()
    const wsName = wrapper.find('.ws-name')
    expect(wsName.exists()).toBe(true)
    expect(wsName.text()).toBe('我的笔记库')
  })

  it('无工作区时应显示默认文本', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    const wsName = wrapper.find('.ws-name')
    expect(wsName.exists()).toBe(true)
    expect(wsName.text()).toBe('选择工作区')
  })

  it('点击折叠按钮应切换侧边栏状态', async () => {
    const { wrapper, settingsStore } = mountSideBar()
    await flushPromises()
    const before = settingsStore.settings.sidebarCollapsed
    await wrapper.find('.collapse-btn').trigger('click')
    expect(settingsStore.settings.sidebarCollapsed).toBe(!before)
  })

  it('点击分组标题应切换展开状态', async () => {
    const { wrapper } = mountSideBar()
    await flushPromises()
    const headers = wrapper.findAll('.nav-group-header')
    expect(headers.length).toBe(4)
    await headers[0].trigger('click')
    const groupItems = wrapper.findAll('.nav-group-items')
    expect(groupItems.length).toBeGreaterThan(0)
  })
})
