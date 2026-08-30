// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  WorkspaceService: {
    GetCurrentWorkspace: vi.fn(),
  },
  SearchService: {
    Search: vi.fn(),
  },
}))

import SearchView from './SearchView.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { WorkspaceService, SearchService } from '@bindings/github.com/notevault/notevault/index.js'

const mockedGetCurrentWorkspace = vi.mocked(WorkspaceService.GetCurrentWorkspace)
const mockedSearch = vi.mocked(SearchService.Search)

enableAutoUnmount(afterEach)

function mountSearch() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/search', component: { template: '<div />' } },
      { path: '/editor', component: { template: '<div />' } },
    ],
  })
  const workspaceStore = useWorkspaceStore()
  const wrapper = mount(SearchView, {
    global: {
      plugins: [pinia, router, i18n],
    },
  })
  return { wrapper, workspaceStore }
}

describe('SearchView', () => {
  beforeEach(() => {
    localStorage.clear()
    ;(i18n.global.locale as any).value = 'zh-CN'
    vi.useFakeTimers()
    mockedGetCurrentWorkspace.mockReset()
    mockedSearch.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('store 为空时先恢复当前工作区，再执行搜索', async () => {
    mockedGetCurrentWorkspace.mockResolvedValue({
      id: 'ws_1',
      name: '测试库',
      path: '/tmp/vault',
      createdAt: '',
      lastOpenedAt: '',
    })
    mockedSearch.mockResolvedValue([
      {
        path: 'notes/demo.md',
        title: 'Demo',
        snippet: 'knowledge',
        matchCount: 1,
      },
    ])
    const { wrapper, workspaceStore } = mountSearch()
    await flushPromises()

    await wrapper.find('[data-testid="search-input"]').setValue('knowledge')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(mockedGetCurrentWorkspace).toHaveBeenCalled()
    expect(mockedSearch).toHaveBeenCalledWith('/tmp/vault', 'knowledge')
    expect(workspaceStore.currentWorkspace?.path).toBe('/tmp/vault')
    expect(wrapper.find('[data-testid="search-result"]').exists()).toBe(true)
  })

  it('无法恢复工作区时显示错误而不是静默返回', async () => {
    mockedGetCurrentWorkspace.mockRejectedValue(new Error('workspace backend unavailable'))
    const { wrapper } = mountSearch()
    await flushPromises()

    await wrapper.find('[data-testid="search-input"]').setValue('knowledge')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(wrapper.find('[data-testid="search-error"]').text()).toContain('尚未选择工作区')
  })

  it('搜索服务返回 null 时显示无结果状态且页面仍可交互', async () => {
    const { wrapper, workspaceStore } = mountSearch()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1',
      name: '测试库',
      path: '/tmp/vault',
      createdAt: '',
      lastOpenedAt: '',
    })
    mockedSearch.mockResolvedValue(null as any)
    await wrapper.find('[data-testid="search-input"]').setValue('不存在的内容')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(wrapper.find('[data-testid="search-input"]').exists()).toBe(true)
    expect(wrapper.find('.search-state').text()).toContain('未找到匹配结果')

    await wrapper.find('.search-clear').trigger('click')
    expect(wrapper.find('[data-testid="search-input"]').exists()).toBe(true)
    expect(wrapper.find('.recent-searches').exists()).toBe(true)
  })

  it('快速变更查询时旧请求结果不会覆盖新查询', async () => {
    const { wrapper, workspaceStore } = mountSearch()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1',
      name: '测试库',
      path: '/tmp/vault',
      createdAt: '',
      lastOpenedAt: '',
    })
    let resolveFirst!: (value: any) => void
    let resolveSecond!: (value: any) => void
    mockedSearch
      .mockReturnValueOnce(new Promise(resolve => { resolveFirst = resolve }) as any)
      .mockReturnValueOnce(new Promise(resolve => { resolveSecond = resolve }) as any)
    await wrapper.find('[data-testid="search-input"]').setValue('旧查询')
    await vi.advanceTimersByTimeAsync(300)
    await wrapper.find('[data-testid="search-input"]').setValue('新查询')
    await vi.advanceTimersByTimeAsync(300)

    resolveFirst([{ path: 'old.md', title: '旧结果', snippet: '', matchCount: 1 }])
    await flushPromises()
    expect(wrapper.find('[data-testid="search-result"]').exists()).toBe(false)

    resolveSecond([{ path: 'new.md', title: '新结果', snippet: '', matchCount: 1 }])
    await flushPromises()
    expect(wrapper.find('[data-testid="search-result"]').text()).toContain('新结果')
  })
})
