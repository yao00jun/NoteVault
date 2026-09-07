// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@/api', () => ({
  QnAService: { Answer: vi.fn() },
  FileService: { ReadFile: vi.fn(async () => '# 设计方案\n\n- [ ] 评审架构\n') },
  CredentialService: {
    GetCredential: vi.fn().mockResolvedValue(''),
    SaveCredential: vi.fn().mockResolvedValue(undefined),
  },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))

vi.mock('@/plugins/editorBridge', () => ({
  insertAtCursor: vi.fn(() => true),
  getActiveEditor: vi.fn(() => null),
}))

import AICopilotDrawer from './AICopilotDrawer.vue'
import { insertAtCursor } from '@/plugins/editorBridge'
import { QnAService, FileService } from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { resetToasts } from '@/composables/useToast'

const answerMock = vi.mocked(QnAService.Answer)
const readMock = vi.mocked(FileService.ReadFile)
const insertMock = vi.mocked(insertAtCursor)

const routes = [
  { path: '/', redirect: '/knowledge' },
  { path: '/knowledge', component: { template: '<div>knowledge</div>' } },
  { path: '/editor', component: { template: '<div>editor</div>' } },
]

function mountDrawer(visible = true) {
  const pinia = createPinia()
  setActivePinia(pinia)
  // 默认本机 Ollama 免 Key 端点，避免云端未填 Key 的阻断拦截问答
  const settingsStore = useSettingsStore()
  settingsStore.settings.ai.baseURL = 'http://localhost:11434/v1'
  const router = createRouter({ history: createMemoryHistory(), routes })
  const wrapper = mount(AICopilotDrawer, {
    props: { visible },
    global: { plugins: [pinia, router, i18n] },
  })
  return { wrapper, workspaceStore: useWorkspaceStore() }
}

enableAutoUnmount(afterEach)

describe('AICopilotDrawer', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    readMock.mockReset().mockResolvedValue('# 设计方案\n\n- [ ] 评审架构\n')
    resetToasts()
    answerMock.mockResolvedValue({
      answer: '三大要点：\n1. 架构清晰\n2. 检索混合\n3. 本地优先',
      citations: [{ index: 1, title: '设计方案.md', path: 'docs/设计方案.md' }],
    } as Awaited<ReturnType<typeof QnAService.Answer>>)
  })

  it('打开时展示上下文感知条（当前文档名）与 4 个快捷胶囊', async () => {
    const { wrapper, workspaceStore } = mountDrawer()
    workspaceStore.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })
    workspaceStore.setActiveFile('docs/设计方案.md')
    await flushPromises()

    expect(wrapper.find('[data-ai-drawer]').classes()).toContain('open')
    expect(wrapper.find('.context-name').text()).toContain('设计方案.md')
    expect(wrapper.findAll('.chip')).toHaveLength(4)
  })

  it('visible=false 时抽屉收起（无 open 类）', () => {
    const { wrapper } = mountDrawer(false)
    expect(wrapper.find('[data-ai-drawer]').classes()).not.toContain('open')
  })

  it('点击「总结当前文档」胶囊：强制注入文档内容并发起问答', async () => {
    const { wrapper, workspaceStore } = mountDrawer()
    workspaceStore.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })
    workspaceStore.setActiveFile('docs/设计方案.md')
    await flushPromises()

    const summaryChip = wrapper
      .findAll('.chip')
      .find((c) => c.text().includes('总结当前文档'))
    expect(summaryChip).toBeTruthy()
    await summaryChip!.trigger('click')
    await flushPromises()

    expect(readMock).toHaveBeenCalledWith('C:/notes', 'docs/设计方案.md')
    expect(answerMock).toHaveBeenCalledTimes(1)
    const payload = answerMock.mock.calls[0]?.[9] as string
    // 文档内容 + 胶囊指令都进了发给后端的问题
    expect(payload).toContain('评审架构')
    expect(payload).toContain('总结这篇文档的核心要点')
    // 消息列表展示干净的问题
    expect(wrapper.text()).toContain('总结这篇文档的核心要点')
    // 回答渲染进消息流
    expect(wrapper.text()).toContain('本地优先')
  })

  it('回答后展示引用卡片与「插入当前笔记」按钮，插入走 editorBridge', async () => {
    const { wrapper, workspaceStore } = mountDrawer()
    workspaceStore.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })
    workspaceStore.setActiveFile('docs/设计方案.md')
    await flushPromises()

    await wrapper.find('.copilot-input textarea').setValue('总结一下')
    await wrapper.find('.copilot-input textarea').trigger('keydown', { key: 'Enter' })
    await flushPromises()

    const citation = wrapper.find('.citation-chip')
    expect(citation.exists()).toBe(true)
    expect(citation.text()).toContain('设计方案.md')

    const insertBtn = wrapper.find('.insert-btn')
    expect(insertBtn.exists()).toBe(true)
    await insertBtn.trigger('click')

    expect(insertMock).toHaveBeenCalledTimes(1)
    expect(String(insertMock.mock.calls[0]?.[0])).toContain('本地优先')
  })

  it('右上角 X 触发 close', async () => {
    const { wrapper } = mountDrawer()
    // icon-btn 顺序：清空（仅在有消息时渲染）→ 关闭；空会话时关闭是最后一个
    await wrapper.findAll('.icon-btn').at(-1)!.trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('点击抽屉外部（mousedown）触发 close，点抽屉内部不触发', async () => {
    const { wrapper } = mountDrawer()
    // 外部点击
    const outside = document.createElement('div')
    document.body.appendChild(outside)
    outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('close')).toBeTruthy()

    // 抽屉内部点击不关闭
    wrapper.emitted('close')!.length = 0
    await wrapper.find('.copilot-header').trigger('mousedown')
    expect(wrapper.emitted('close') ?? []).toHaveLength(0)
  })

  it('使用工作台日报上下文并将选中的润色回答应用回日报', async () => {
    const { wrapper, workspaceStore } = mountDrawer()
    workspaceStore.setCurrentWorkspace({ id: 'ws-1', name: '笔记库', path: 'C:/notes', createdAt: '', lastOpenedAt: '' })
    workspaceStore.setActiveFile('docs/设计方案.md')
    await flushPromises()
    const onApply = vi.fn()
    await wrapper.setProps({ request: { id: 'report-1', source: 'daily-report', workspacePath: 'C:/notes', prompt: '润色这份日报', context: '今日完成：订单联调已验收', onApply } })
    await flushPromises()
    const payload = answerMock.mock.calls[0]?.[9] as string
    expect(payload).toContain('订单联调已验收')
    expect(payload).not.toContain('评审架构')
    expect(wrapper.find('.context-name').text()).toContain('日报')
    await wrapper.get('[data-testid="apply-report-answer"]').trigger('click')
    expect(onApply).toHaveBeenCalledWith(expect.stringContaining('本地优先'))
    expect(insertMock).not.toHaveBeenCalled()
  })

  it('清空已有对话后仍能把新的润色回答应用到当前日报', async () => {
    const { wrapper, workspaceStore } = mountDrawer()
    workspaceStore.setCurrentWorkspace({ id: 'ws-1', name: '笔记库', path: 'C:/notes', createdAt: '', lastOpenedAt: '' })
    await wrapper.get('.copilot-input textarea').setValue('先聊一下项目')
    await wrapper.get('.btn-ask').trigger('click')
    await flushPromises()

    const onApply = vi.fn()
    await wrapper.setProps({ request: { id: 'report-clear', source: 'daily-report', workspacePath: 'C:/notes', prompt: '润色日报', context: '今日完成：订单联调', onApply } })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="apply-report-answer"]')).toHaveLength(1)

    const clearButton = wrapper.findAll('.header-actions button').find(button => button.attributes('title') === i18n.global.t('copilot.clear'))
    await clearButton!.trigger('click')
    expect(wrapper.findAll('.msg')).toHaveLength(0)
    answerMock.mockResolvedValueOnce({ answer: '重新润色后的日报', citations: [] } as Awaited<ReturnType<typeof QnAService.Answer>>)
    await wrapper.get('.copilot-input textarea').setValue('请重新润色')
    await wrapper.get('.btn-ask').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="apply-report-answer"]')).toHaveLength(1)
    await wrapper.get('[data-testid="apply-report-answer"]').trigger('click')
    expect(onApply).toHaveBeenCalledWith('重新润色后的日报')
  })

  it('工作区切换后迟到的同名文档读取不会覆盖新工作区上下文', async () => {
    let finishOldRead!: (content: string) => void
    readMock.mockImplementation((workspacePath) => (workspacePath === 'C:/old-notes'
      ? new Promise<string>(resolve => { finishOldRead = resolve })
      : Promise.resolve('新工作区的设计正文')) as ReturnType<typeof FileService.ReadFile>)
    const { wrapper, workspaceStore } = mountDrawer()
    workspaceStore.setCurrentWorkspace({ id: 'old', name: '旧工作区', path: 'C:/old-notes', createdAt: '', lastOpenedAt: '' })
    workspaceStore.setActiveFile('设计.md')
    await flushPromises()

    workspaceStore.setCurrentWorkspace({ id: 'new', name: '新工作区', path: 'C:/new-notes', createdAt: '', lastOpenedAt: '' })
    workspaceStore.setActiveFile('设计.md')
    await flushPromises()
    finishOldRead('旧工作区的私有正文')
    await flushPromises()
    await wrapper.findAll('.chip')[0]!.trigger('click')
    await flushPromises()

    expect(answerMock).toHaveBeenCalledTimes(1)
    expect(answerMock.mock.calls[0]?.[8]).toBe('C:/new-notes')
    expect(answerMock.mock.calls[0]?.[9]).toContain('新工作区的设计正文')
    expect(answerMock.mock.calls[0]?.[9]).not.toContain('旧工作区的私有正文')
  })
})
