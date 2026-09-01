// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  WorkspaceService: { GetCurrentWorkspace: vi.fn() },
  TodoService: {
    GetAllTodos: vi.fn(async () => sampleTodos),
    ToggleTodo: vi.fn(async () => undefined),
  },
}))

import TodosView from './TodosView.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { TodoService } from '@bindings/github.com/notevault/notevault/index.js'

const sampleTodos = [
  { id: '1', filePath: 'a.md', fileName: 'a.md', content: '任务A', lineIndex: 1, completed: false, priority: 'high' },
  { id: '2', filePath: 'a.md', fileName: 'a.md', content: '任务B', lineIndex: 2, completed: true, priority: 'medium' },
  { id: '3', filePath: 'b.md', fileName: 'b.md', content: '任务C', lineIndex: 1, completed: false, priority: 'low' },
]

const mockedTodos = vi.mocked(TodoService.GetAllTodos)
const mockedToggle = vi.mocked(TodoService.ToggleTodo)

enableAutoUnmount(afterEach)

function mountTodos() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/editor', component: { template: '<div />' } },
      { path: '/todos', component: TodosView },
    ],
  })
  const workspaceStore = useWorkspaceStore()
  workspaceStore.setCurrentWorkspace({
    id: 'ws_1',
    name: '测试库',
    path: '/tmp/vault',
    createdAt: '',
    lastOpenedAt: '',
  })
  const wrapper = mount(TodosView, { global: { plugins: [pinia, router, i18n] } })
  return { wrapper }
}

describe('TodosView kanban', () => {
  beforeEach(() => {
    mockedTodos.mockImplementation((async () => sampleTodos) as any)
    mockedToggle.mockResolvedValue(undefined as any)
  })

  it('默认列表视图渲染全部待办', async () => {
    const { wrapper } = mountTodos()
    await flushPromises()
    expect(wrapper.find('.todos-list').exists()).toBe(true)
    expect(wrapper.findAll('.todo-item')).toHaveLength(3)
  })

  it('切换到看板（按状态）显示两列且计数正确', async () => {
    const { wrapper } = mountTodos()
    await flushPromises()
    const viewBtns = wrapper.findAll('.view-tabs button')
    await viewBtns[1].trigger('click') // 看板
    await flushPromises()
    const cols = wrapper.findAll('.kanban-col')
    expect(cols).toHaveLength(2)
    // 待完成列（2 项：A、C）
    const todoCol = wrapper.find('.kanban-col-todo')
    expect(todoCol.find('.kanban-col-title').text()).toContain('待完成')
    expect(todoCol.findAll('.kanban-card')).toHaveLength(2)
    // 已完成列（1 项：B）
    const doneCol = wrapper.find('.kanban-col-done')
    expect(doneCol.findAll('.kanban-card')).toHaveLength(1)
  })

  it('按优先级分组显示 高/中/低/无 四列', async () => {
    const { wrapper } = mountTodos()
    await flushPromises()
    const viewBtns = wrapper.findAll('.view-tabs button')
    await viewBtns[1].trigger('click')
    await flushPromises()
    const groupBtns = wrapper.findAll('.group-tabs button')
    await groupBtns[1].trigger('click') // 优先级
    await flushPromises()
    const cols = wrapper.findAll('.kanban-col')
    expect(cols).toHaveLength(4)
    expect(wrapper.find('.kanban-col-high').findAll('.kanban-card')).toHaveLength(1)
    expect(wrapper.find('.kanban-col-medium').findAll('.kanban-card')).toHaveLength(1)
    expect(wrapper.find('.kanban-col-low').findAll('.kanban-card')).toHaveLength(1)
  })

  it('按来源文件分组按文件名聚合', async () => {
    const { wrapper } = mountTodos()
    await flushPromises()
    const viewBtns = wrapper.findAll('.view-tabs button')
    await viewBtns[1].trigger('click')
    await flushPromises()
    const groupBtns = wrapper.findAll('.group-tabs button')
    await groupBtns[2].trigger('click') // 来源文件
    await flushPromises()
    const cols = wrapper.findAll('.kanban-col')
    expect(cols).toHaveLength(2)
    const aCol = wrapper.findAll('.kanban-col').find((c) => c.find('.kanban-col-title').text() === 'a.md')
    expect(aCol?.findAll('.kanban-card')).toHaveLength(2)
  })

  it('看板卡片勾选调用 ToggleTodo', async () => {
    const { wrapper } = mountTodos()
    await flushPromises()
    const viewBtns = wrapper.findAll('.view-tabs button')
    await viewBtns[1].trigger('click')
    await flushPromises()
    await wrapper.find('.kanban-card .todo-checkbox').trigger('click')
    await flushPromises()
    expect(mockedToggle).toHaveBeenCalled()
  })
})
