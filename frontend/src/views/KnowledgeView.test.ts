// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

const promptDialogMock = vi.fn()
const openTodayWorkLogMock = vi.fn()
vi.mock('@/composables/useWorkLog', () => ({ useWorkLog: () => ({ openTodayWorkLog: openTodayWorkLogMock }) }))
vi.mock('@/composables/usePrompt', () => ({
  promptDialog: (...args: unknown[]) => promptDialogMock(...args),
}))

vi.mock('@/api', () => ({
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  WorkspaceService: { GetCurrentWorkspace: vi.fn() },
  FileService: {
    GetFileTree: vi.fn(),
    CreateFile: vi.fn(),
    ReadFile: vi.fn(),
    SaveFile: vi.fn(async () => undefined),
  },
  // WorkbenchWidgets（今日行动中心）会调用：mock 面必须覆盖组件树全部依赖
  TodoService: { GetAllTodos: vi.fn(async () => []), ToggleTodo: vi.fn(async () => undefined) },
  ReminderService: { GetAllReminders: vi.fn(async () => []) },
  TagService: { GetAllTags: vi.fn() },
  StatsService: { GetTodayStats: vi.fn(async () => null) },
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
} from '@/api'

const mockedTree = vi.mocked(FileService.GetFileTree)
const mockedCreateFile = vi.mocked(FileService.CreateFile)
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
  return { wrapper, router }
}

describe('KnowledgeView 工作台（UI-WORKBENCH-REDESIGN 瘦身后）', () => {
  it('uses the shared work-log action from its Daily space', async () => {
    const { wrapper } = mountKnowledge()
    await flushPromises()
    await wrapper.get('[data-testid="space-daily"]').trigger('click')
    expect(openTodayWorkLogMock).toHaveBeenCalledOnce()
    expect(mockedCreateFile).not.toHaveBeenCalled()
  })

  beforeEach(() => {
    localStorage.clear()
    ;(i18n.global.locale as any).value = 'zh-CN'
    mockedTree.mockReset()
    mockedCreateFile.mockReset()
    mockedTags.mockReset()
    mockedTree.mockResolvedValue([
      {
        name: 'Java',
        path: 'Java',
        isDir: true,
        children: [
          { name: '基础.md', path: 'Java/基础.md', isDir: false, modTime: '2026-08-28T10:00:00Z' },
          { name: '并发.md', path: 'Java/并发.md', isDir: false, modTime: '2026-08-29T10:00:00Z' },
        ],
      },
      {
        name: 'SQL',
        path: 'SQL',
        isDir: true,
        children: [
          { name: '索引.md', path: 'SQL/索引.md', isDir: false, modTime: '2026-08-30T10:00:00Z' },
        ],
      },
    ] as any)
    mockedTags.mockResolvedValue([])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('不再内嵌文件夹树与文件列表（目录树归 /editor 承载）', async () => {
    const { wrapper } = mountKnowledge()
    await flushPromises()

    expect(wrapper.find('[data-testid="folder-item"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="document-item"]').exists()).toBe(false)
    expect(wrapper.find('.kv-launch-grid').exists()).toBe(false)
    expect(wrapper.find('.kv-footer-links').exists()).toBe(false)
  })

  it('渲染五大知识空间卡片（学习/项目/资料收藏/收集箱/日记）', async () => {
    const { wrapper } = mountKnowledge()
    await flushPromises()

    const spaces = wrapper.findAll('.kv-space-card')
    expect(spaces.length).toBe(5)
    for (const key of ['learning', 'projects', 'resources', 'inbox', 'daily']) {
      expect(wrapper.find(`[data-testid="space-${key}"]`).exists()).toBe(true)
    }
  })

  it('最近编辑卡按修改时间降序展示（下区）', async () => {
    const { wrapper } = mountKnowledge()
    await flushPromises()

    const recent = wrapper.find('[data-testid="recent-notes"]')
    expect(recent.exists()).toBe(true)
    const names = recent.findAll('.wb-side-item .wb-side-item, .wb-side-item').map((w) => w.text())
    // 三篇全在，且最近修改的「索引」（08-30）排最前
    const joined = names.join(' ')
    expect(joined).toContain('索引')
    expect(joined).toContain('并发')
    expect(joined).toContain('基础')
    expect(names[0]!).toContain('索引')
  })

  it('新建文档：无文件夹上下文时创建到工作区根', async () => {
    promptDialogMock.mockResolvedValue('新文档.md')
    mockedCreateFile.mockResolvedValue({ path: '新文档.md' } as any)
    const { wrapper } = mountKnowledge()
    await flushPromises()

    await wrapper.find('.kv-btn-primary').trigger('click')
    await flushPromises()

    expect(mockedCreateFile).toHaveBeenCalledWith(
      '/tmp/vault',
      '新文档.md',
      '# 新文档\n\n',
    )
  })
})
