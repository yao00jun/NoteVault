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
    GetIndexStats: vi.fn(),
  },
}))

import SearchView from './SearchView.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { WorkspaceService, SearchService } from '@bindings/github.com/notevault/notevault/index.js'

const mockedGetCurrentWorkspace = vi.mocked(WorkspaceService.GetCurrentWorkspace)
const mockedSearch = vi.mocked(SearchService.Search)
const mockedGetIndexStats = vi.mocked(SearchService.GetIndexStats)

/** 造一条搜索结果。modTime 缺省给个固定值，避免每个用例重复写。 */
function result(path: string, title: string, modTime: string, matchCount = 1) {
  return { path, title, snippet: '', matchCount, modTime }
}

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
    mockedGetIndexStats.mockReset()
    mockedGetIndexStats.mockResolvedValue({
      docCount: 0,
      tokenCount: 0,
      scanComplete: true,
      skippedCount: 0,
    })
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
      result('notes/demo.md', 'Demo', '2026-08-30T10:00:00Z'),
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

    resolveFirst([result('old.md', '旧结果', '2026-08-01T00:00:00Z')])
    await flushPromises()
    expect(wrapper.find('[data-testid="search-result"]').exists()).toBe(false)

    resolveSecond([result('new.md', '新结果', '2026-08-31T00:00:00Z')])
    await flushPromises()
    expect(wrapper.find('[data-testid="search-result"]').text()).toContain('新结果')
  })

  // --- P0-4 排序切换 ---

  it('默认按相关性排序，切到「按最近修改」后改按时间倒序', async () => {
    const { wrapper, workspaceStore } = mountSearch()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1', name: '测试库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '',
    })
    // 相关性高但很旧 vs 相关性低但很新
    mockedSearch.mockResolvedValue([
      result('a.md', '高相关但很旧', '2026-01-01T00:00:00Z', 9),
      result('b.md', '低相关但很新', '2026-08-31T00:00:00Z', 1),
    ])
    await wrapper.find('[data-testid="search-input"]').setValue('关键词')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    const titles = () => wrapper.findAll('[data-testid="search-result"]').map(r => r.text())
    expect(titles()[0]).toContain('高相关但很旧')

    await wrapper.find('[data-testid="sort-recent"]').trigger('click')
    await flushPromises()

    expect(titles()[0]).toContain('低相关但很新')
    expect(mockedSearch).toHaveBeenCalledTimes(1) // 排序在前端做，不应重新请求
  })

  it('切回「按相关性」恢复后端给定的顺序', async () => {
    const { wrapper, workspaceStore } = mountSearch()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1', name: '测试库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '',
    })
    mockedSearch.mockResolvedValue([
      result('a.md', '高相关但很旧', '2026-01-01T00:00:00Z', 9),
      result('b.md', '低相关但很新', '2026-08-31T00:00:00Z', 1),
    ])
    await wrapper.find('[data-testid="search-input"]').setValue('关键词')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    await wrapper.find('[data-testid="sort-recent"]').trigger('click')
    await wrapper.find('[data-testid="sort-relevance"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="search-result"]')[0].text()).toContain('高相关但很旧')
  })

  // --- P0-5 索引覆盖提示 ---

  it('有文件因体积过大未被索引时给出提示', async () => {
    mockedGetIndexStats.mockResolvedValue({
      docCount: 5, tokenCount: 100, scanComplete: true, skippedCount: 3,
    })
    const { wrapper, workspaceStore } = mountSearch()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1', name: '测试库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '',
    })
    mockedSearch.mockResolvedValue([result('a.md', 'A', '2026-08-01T00:00:00Z')])
    await wrapper.find('[data-testid="search-input"]').setValue('关键词')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    const warn = wrapper.find('[data-testid="index-warning"]')
    expect(warn.exists()).toBe(true)
    expect(warn.text()).toContain('3')
  })

  it('索引完整覆盖时不显示提示', async () => {
    const { wrapper, workspaceStore } = mountSearch()
    workspaceStore.setCurrentWorkspace({
      id: 'ws_1', name: '测试库', path: '/tmp/vault', createdAt: '', lastOpenedAt: '',
    })
    mockedSearch.mockResolvedValue([result('a.md', 'A', '2026-08-01T00:00:00Z')])
    await wrapper.find('[data-testid="search-input"]').setValue('关键词')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(wrapper.find('[data-testid="index-warning"]').exists()).toBe(false)
  })
})
