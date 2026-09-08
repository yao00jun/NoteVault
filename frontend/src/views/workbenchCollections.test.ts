// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive, defineComponent, h, onUnmounted } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Component } from 'vue'
import type {
  InterviewCard,
  WorkbenchBook,
  WorkbenchDocument,
  WorkbenchProject,
  WorkbenchTask,
} from '@/api/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import ProjectsView from './ProjectsView.vue'
import LearningView from './LearningView.vue'
import KnowledgeVaultView from './KnowledgeVaultView.vue'
import GlobalNavigation from '@/components/layout/GlobalNavigation.vue'
import { useNavigationStore } from '@/stores/navigation'
import { useRouter } from 'vue-router'

const harness = vi.hoisted(() => ({ state: {} as Record<string, unknown>, copilot: vi.fn() }))
vi.mock('@/stores/workbench', () => ({ useWorkbenchStore: () => harness.state }))
vi.mock('@/composables/useCopilotRequest', () => ({ requestCopilot: harness.copilot }))

const document = (path: string, title: string): WorkbenchDocument => ({
  path,
  title,
  modifiedAt: '2026-09-08T10:00:00+08:00',
})
function task(id: string, project: string, values: Partial<WorkbenchTask> = {}): WorkbenchTask {
  return {
    id,
    filePath: `Projects/${project}/Tasks.md`,
    fileName: 'Tasks.md',
    lineIndex: 0,
    sourceLine: `- [ ] [US] ${id}`,
    title: id,
    content: id,
    type: 'US',
    project,
    projectPath: `Projects/${project}/project.md`,
    completed: false,
    priority: '',
    due: '2026-09-08',
    date: '',
    completedAt: '',
    status: '',
    blocker: '',
    progress: [],
    ...values,
  }
}
function project(name: string, status: string, taskIds: string[]): WorkbenchProject {
  return {
    name,
    path: `Projects/${name}/project.md`,
    folder: `Projects/${name}`,
    status,
    nextStep: '完成接口联调',
    techStack: ['Go', 'Redis'],
    taskIds,
    notes: [],
    modifiedAt: '2026-09-08T09:20:00+08:00',
  }
}
function book(name: string, projects: string[]): WorkbenchBook {
  return {
    name,
    path: `Learning/${name}/book.md`,
    folder: `Learning/${name}`,
    status: '在学',
    progress: 40,
    projects,
    chapters: [document(`Learning/${name}/01-并发.md`, '01 并发模型')],
    noteCount: 1,
    reviewCount: 1,
  }
}
function card(id: string, weak: boolean): InterviewCard {
  return {
    id,
    filePath: 'Learning/面试宝典/Go.md',
    lineIndex: 1,
    comment: '<!-- srs: {} -->',
    question: id,
    answer: '通过 context 管理生命周期',
    level: '不会',
    interval: 1,
    due: '2026-09-08',
    reps: 2,
    failures: weak ? 2 : 1,
    lastReviewed: '',
    weak,
  }
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  harness.copilot.mockReset()
  const tasks = [
    task('US 登录接口', '商城'),
    task('DTS 消费权限', '物流', {
      type: 'DTS',
      due: '2026-09-07',
      status: 'blocked',
      blocker: '缺少 MQ 联调权限',
    }),
    task('额外 文档补齐', '商城', { type: '额外', due: '2026-09-20' }),
  ]
  harness.state = reactive({
    today: '2026-09-08',
    tasks,
    projects: [
      project('商城', '进行中', [tasks[0]!.id, tasks[2]!.id]),
      project('物流', '暂停', [tasks[1]!.id]),
    ],
    books: [book('Go', ['商城']), book('Java', ['物流'])],
    cards: [card('Goroutine 泄漏', true), card('Channel 缓冲', false)],
    documents: [
      document('Projects/商城/架构总结.md', '商城服务架构'),
      document('Projects/商城/联调笔记.md', '接口联调笔记'),
      document('Resources/缓存设计.md', '缓存设计指南'),
      document('Inbox/新想法.md', '新想法'),
      document('Daily/2026-09-08.md', '今日日记'),
    ],
    progress: [],
    radar: {
      path: 'Learning/技术雷达.md',
      content:
        '## 评估中\n- Bun：等待生态验证\n## 尝试中\n- Temporal：验证补偿流程\n## 采用推荐\n- Go：部署成本低\n## 放弃废弃\n- 旧 ORM：维护成本高',
    },
    todayTasks: [tasks[0]],
    blockers: [tasks[1]],
    reviewQueue: [card('Goroutine 泄漏', true)],
    reviewedToday: 0,
    reviewTarget: 5,
    loading: false,
    error: '',
    busy: false,
    refresh: vi.fn(),
    toggleTask: vi.fn(),
    recordProgress: vi.fn(),
    setBlocker: vi.fn(),
    reviewCard: vi.fn(),
    addTask: vi.fn(),
  })
})

async function renderView(component: Component, path: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/projects', component: ProjectsView },
      { path: '/learning', component: LearningView },
      { path: '/vault', component: KnowledgeVaultView },
      ...[
        '/',
        '/editor',
        '/insights',
        '/discover',
        '/library',
        '/archive',
        '/review',
        '/import',
        '/plugins',
        '/canvas',
        '/trash',
        '/settings',
      ].map((route) => ({ path: route, component: { template: '<div />' } })),
    ],
  })
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({
    id: 'collections',
    name: '研发笔记',
    path: '/workspace',
    createdAt: '',
    lastOpenedAt: '',
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(component, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { wrapper, router, workspace }
}

describe('ProjectsView', () => {
  it('keeps global blockers visible when status, type and time filters hide that project', async () => {
    const { wrapper } = await renderView(ProjectsView, '/projects')
    expect(wrapper.find('[data-testid="project-status-filter"]').exists()).toBe(true)
    await wrapper.get('[data-testid="project-status-filter"]').setValue('进行中')
    await wrapper.get('[data-testid="project-type-filter"]').setValue('US')
    await wrapper.get('[data-testid="project-period-filter"]').setValue('today')
    expect(wrapper.get('[data-testid="global-blockers"]').text()).toContain('缺少 MQ 联调权限')
    const cards = wrapper.findAll('[data-testid="project-card"]')
    expect(cards).toHaveLength(1)
    expect(cards[0]!.text()).toContain('商城')
    expect(cards[0]!.text()).not.toContain('额外 文档补齐')
  })

  it('opens project architecture and sends project-specific context to Copilot', async () => {
    const { wrapper, router } = await renderView(
      ProjectsView,
      '/projects?project=Projects/商城/project.md'
    )
    expect(wrapper.find('[data-testid="project-tab-architecture"]').exists()).toBe(true)
    await wrapper.get('[data-testid="project-tab-architecture"]').trigger('click')
    expect(wrapper.get('[data-testid="project-detail-content"]').text()).toContain('商城服务架构')
    expect(wrapper.get('[data-testid="project-detail-content"]').text()).not.toContain(
      '缓存设计指南'
    )
    await wrapper.get('[data-testid="project-ai-summary"]').trigger('click')
    expect(harness.copilot).toHaveBeenCalledWith(
      expect.objectContaining({ context: expect.stringContaining('商城'), source: 'project' })
    )
    await wrapper.get('[data-testid="project-open-document"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
  })
})

describe('LearningView', () => {
  it('opens the learning folder when the default radar path has no document yet', async () => {
    harness.state.radar = { path: 'Learning/技术雷达.md', content: '' }
    const { wrapper, router } = await renderView(LearningView, '/learning?tab=radar')
    await wrapper.get('[data-testid="technology-radar"]').get('.collection-section-title button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.folder).toBe('Learning')
  })

  it('opens chapters and navigates related projects from the selected book', async () => {
    const { wrapper, router } = await renderView(LearningView, '/learning?book=Learning/Go/book.md')
    expect(wrapper.find('[data-testid="book-detail"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="book-detail"]').text()).toContain('01 并发模型')
    await wrapper.get('[data-testid="book-related-project"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/projects')
    expect(router.currentRoute.value.query.project).toBe('商城')
  })

  it('shows only persistent weak cards and derives all four radar decisions from Markdown', async () => {
    const { wrapper } = await renderView(LearningView, '/learning')
    expect(wrapper.find('[data-testid="learning-tab-weak"]').exists()).toBe(true)
    await wrapper.get('[data-testid="learning-tab-weak"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="weak-card-list"]').text()).toContain('Goroutine 泄漏')
    expect(wrapper.get('[data-testid="weak-card-list"]').text()).not.toContain('Channel 缓冲')
    await wrapper.get('[data-testid="learning-tab-radar"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="radar-category"]')).toHaveLength(4)
    expect(wrapper.get('[data-testid="technology-radar"]').text()).toContain('验证补偿流程')
    expect(wrapper.get('[data-testid="technology-radar"]').text()).toContain('维护成本高')
  })
})

describe('KnowledgeVaultView', () => {
  it('uses the shell Back control to restore the filtered document list and Forward to reopen its file', async () => {
    const Host = defineComponent({ setup() {
      const router = useRouter()
      onUnmounted(useNavigationStore().connect(router, useWorkspaceStore()))
      return () => h('div', [h(GlobalNavigation), h(KnowledgeVaultView)])
    } })
    const { wrapper, router } = await renderView(Host, '/vault')
    await wrapper.get('[data-testid="vault-search"]').setValue('商城')
    await wrapper.get('select[aria-label="文档空间"]').setValue('Projects')
    await flushPromises()
    const filteredLocation = router.currentRoute.value.fullPath
    await wrapper.findAll('[data-testid="vault-document"]')[0]!.get('.collection-document-open').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    const openedFile = router.currentRoute.value.query.file
    expect(openedFile).toContain('Projects/商城/')
    await wrapper.get('[data-testid="global-back"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe(filteredLocation)
    expect((wrapper.get('[data-testid="vault-search"]').element as HTMLInputElement).value).toBe('商城')
    expect(wrapper.findAll('[data-testid="vault-document"]')).toHaveLength(2)
    await wrapper.get('[data-testid="global-forward"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.file).toBe(openedFile)
  })

  it('searches document titles and paths and shares pin state with the workspace', async () => {
    const { wrapper, workspace } = await renderView(KnowledgeVaultView, '/vault')
    expect(wrapper.find('[data-testid="vault-search"]').exists()).toBe(true)
    await wrapper.get('[data-testid="vault-search"]').setValue('resources/缓存')
    const rows = wrapper.findAll('[data-testid="vault-document"]')
    expect(rows).toHaveLength(1)
    expect(rows[0]!.text()).toContain('缓存设计指南')
    await rows[0]!.get('[data-testid="document-pin"]').trigger('click')
    expect(workspace.isPinned('Resources/缓存设计.md')).toBe(true)
    expect(rows[0]!.get('[data-testid="document-pin"]').attributes('aria-pressed')).toBe('true')
  })

  it('filters all five spaces in the document browser and exposes existing insight tools', async () => {
    const { wrapper, router } = await renderView(KnowledgeVaultView, '/vault')
    expect(wrapper.findAll('[data-testid="vault-space"]')).toHaveLength(5)
    await wrapper.get('[data-space="Daily"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/vault')
    expect(router.currentRoute.value.query.space).toBe('Daily')
    await wrapper.get('[data-testid="vault-insight-bases"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/insights')
    expect(router.currentRoute.value.query.tab).toBe('bases')
  })
})
