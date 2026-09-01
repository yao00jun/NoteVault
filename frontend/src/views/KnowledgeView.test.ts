// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  WorkspaceService: { GetCurrentWorkspace: vi.fn() },
  FileService: {
    GetFileTree: vi.fn(),
    CreateFile: vi.fn(),
    CreateFolder: vi.fn(),
  },
  TodoService: { GetAllTodos: vi.fn(), ToggleTodo: vi.fn() },
  TagService: { GetAllTags: vi.fn() },
  ExportService: { ExportWorkspaceMarkdown: vi.fn() },
  TemplateService: {
    ListTemplates: vi.fn(async () => []),
    GetTemplateContent: vi.fn(async () => ''),
    CreateFromTemplate: vi.fn(async () => null),
  },
}))

import KnowledgeView from './KnowledgeView.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import {
  FileService,
  TagService,
  TodoService,
} from '@bindings/github.com/notevault/notevault/index.js'

const mockedTree = vi.mocked(FileService.GetFileTree)
const mockedCreateFile = vi.mocked(FileService.CreateFile)
const mockedTodos = vi.mocked(TodoService.GetAllTodos)
const mockedTags = vi.mocked(TagService.GetAllTags)

enableAutoUnmount(afterEach)

function mountKnowledge() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/knowledge', component: KnowledgeView },
      { path: '/editor', component: { template: '<div />' } },
      { path: '/tags', component: { template: '<div />' } },
      { path: '/todos', component: { template: '<div />' } },
      { path: '/search', component: { template: '<div />' } },
      { path: '/reminders', component: { template: '<div />' } },
      { path: '/archive', component: { template: '<div />' } },
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
  const wrapper = mount(KnowledgeView, {
    global: { plugins: [pinia, router, i18n] },
  })
  return { wrapper }
}

describe('KnowledgeView folder navigation', () => {
  beforeEach(() => {
    localStorage.clear()
    ;(i18n.global.locale as any).value = 'zh-CN'
    mockedTree.mockReset()
    mockedCreateFile.mockReset()
    mockedTodos.mockReset()
    mockedTags.mockReset()
    mockedTree.mockResolvedValue([
      {
        name: 'Java',
        path: 'Java',
        isDir: true,
        children: [
          { name: '基础.md', path: 'Java/基础.md', isDir: false, modTime: '2026-08-28T00:00:00Z' },
          { name: '并发.md', path: 'Java/并发.md', isDir: false, modTime: '2026-08-28T00:00:00Z' },
        ],
      },
      {
        name: 'SQL',
        path: 'SQL',
        isDir: true,
        children: [
          { name: '索引.md', path: 'SQL/索引.md', isDir: false, modTime: '2026-08-28T00:00:00Z' },
        ],
      },
    ] as any)
    mockedTodos.mockResolvedValue([])
    mockedTags.mockResolvedValue([])
    vi.stubGlobal('prompt', vi.fn(() => '新文档.md'))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('按文件夹展示数量，并切换文件夹过滤文档分组', async () => {
    const { wrapper } = mountKnowledge()
    await flushPromises()

    const folderItems = wrapper.findAll('[data-testid="folder-item"]')
    expect(folderItems.some((item) => item.text().includes('Java') && item.text().includes('2'))).toBe(true)
    expect(folderItems.some((item) => item.text().includes('SQL') && item.text().includes('1'))).toBe(true)
    expect(wrapper.findAll('[data-testid="document-item"]')).toHaveLength(3)

    const javaFolder = folderItems.find((item) => item.text().includes('Java'))!
    await javaFolder.trigger('click')
    const visibleDocuments = wrapper.findAll('[data-testid="document-item"]')
    expect(visibleDocuments).toHaveLength(2)
    expect(visibleDocuments.map((item) => item.text()).join(' ')).toContain('基础')
    expect(visibleDocuments.map((item) => item.text()).join(' ')).not.toContain('索引')
  })

  it('在选中文件夹时，新文档创建到该文件夹', async () => {
    mockedCreateFile.mockResolvedValue({ path: 'Java/新文档.md' } as any)
    const { wrapper } = mountKnowledge()
    await flushPromises()

    const javaFolder = wrapper.findAll('[data-testid="folder-item"]').find((item) => item.text().includes('Java'))!
    await javaFolder.trigger('click')
    await wrapper.find('.kv-btn-primary').trigger('click')
    await flushPromises()

    expect(mockedCreateFile).toHaveBeenCalledWith(
      '/tmp/vault',
      'Java/新文档.md',
      '# 新文档\n\n',
    )
  })
})
