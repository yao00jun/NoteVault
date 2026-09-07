// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

// QnAService 是被测对象的唯一后端出口；CredentialService / ClipperService
// 是 settings store 初始化的副依赖（缺了会产生 Unhandled Error）
vi.mock('@/api', () => ({
  QnAService: { Answer: vi.fn() },
  CredentialService: {
    GetCredential: vi.fn().mockResolvedValue(''),
    SaveCredential: vi.fn().mockResolvedValue(undefined),
  },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
  FileService: { ReadFile: vi.fn(async () => '') },
}))

import { useAIChat, isAIAnswering } from './useAIChat'
import type { ContextBuilder } from './useAIChat'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { QnAService } from '@/api'

const answerMock = vi.mocked(QnAService.Answer)

const routes = [
  { path: '/', redirect: '/knowledge' },
  { path: '/knowledge', component: { template: '<div>knowledge</div>' } },
  { path: '/editor', component: { template: '<div>editor</div>' } },
]

function createTestRouter() {
  return createRouter({ history: createMemoryHistory(), routes })
}

enableAutoUnmount(afterEach)

/** 通过宿主组件在真实 setup 上下文里调用 useAIChat */
function mountHost(buildContext?: ContextBuilder) {
  const pinia = createPinia()
  setActivePinia(pinia)
  let chat!: ReturnType<typeof useAIChat>
  const Host = defineComponent({
    setup() {
      chat = useAIChat(buildContext ? { buildContext } : {})
      return () => null
    },
  })
  const router = createTestRouter()
  mount(Host, { global: { plugins: [pinia, router, i18n] } })
  return chat
}

describe('useAIChat', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    answerMock.mockReset()
    // 通用应答：带一条引用
    answerMock.mockResolvedValue({
      answer: '这是 AI 的回答',
      citations: [{ index: 1, title: '设计方案.md', path: 'docs/设计方案.md' }],
    } as Awaited<ReturnType<typeof QnAService.Answer>>)
  })

  it('ask 推入用户与助手消息，携带引用溯源', async () => {
    const chat = mountHost()
    const settings = useSettingsStore()
    settings.settings.ai.baseURL = 'http://localhost:11434/v1'
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })
    chat.question.value = '  这个项目怎么设计？ '
    await chat.ask()
    await flushPromises()

    expect(chat.messages.value).toHaveLength(2)
    // 消息列表展示干净的问题原文（两端空白已裁剪）
    expect(chat.messages.value[0]).toMatchObject({ role: 'user', content: '这个项目怎么设计？' })
    expect(chat.messages.value[1]).toMatchObject({
      role: 'assistant',
      content: '这是 AI 的回答',
    })
    expect(chat.messages.value[1]?.citations).toHaveLength(1)
    // 输入框已被清空
    expect(chat.question.value).toBe('')
  })

  it('无工作区时不发起请求，直接推送错误提示', async () => {
    const chat = mountHost()
    await chat.ask('问题')
    await flushPromises()

    expect(answerMock).not.toHaveBeenCalled()
    expect(chat.messages.value).toHaveLength(1)
    expect(chat.messages.value[0]?.error).toBe(true)
  })

  it('云端端点未填 Key 时阻断提问（与后端 requireCredential 同口径）', async () => {
    const chat = mountHost()
    const settings = useSettingsStore()
    settings.settings.ai.baseURL = 'https://api.openai.com/v1'
    settings.settings.ai.apiKey = ''

    await chat.ask('问题')
    await flushPromises()

    expect(answerMock).not.toHaveBeenCalled()
    expect(chat.messages.value[0]?.error).toBe(true)
  })

  it('本机端点（Ollama）免 Key 直接放行，零阻断', async () => {
    const chat = mountHost()
    const settings = useSettingsStore()
    settings.settings.ai.baseURL = 'http://localhost:11434/v1'
    settings.settings.ai.apiKey = ''
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })

    await chat.ask('问题')
    await flushPromises()

    expect(answerMock).toHaveBeenCalledTimes(1)
    expect(chat.messages.value[1]?.error).toBeUndefined()
  })

  it('buildContext 提供的上下文拼进发给后端的问题，消息列表保持干净', async () => {
    const chat = mountHost(() => '当前文档正文内容')
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })

    await chat.ask('总结一下')
    await flushPromises()

    const payload = answerMock.mock.calls[0]?.[9] as string
    expect(payload).toContain('当前文档正文内容')
    expect(payload).toContain('总结一下')
    // 用户气泡只展示原始问题
    expect(chat.messages.value[0]?.content).toBe('总结一下')
  })

  it('Answer 抛错时推送错误消息且不炸 UI', async () => {
    answerMock.mockRejectedValue(new Error('boom'))
    const chat = mountHost()
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })

    await chat.ask('问题')
    await flushPromises()

    expect(chat.messages.value[1]).toMatchObject({ role: 'assistant', error: true })
    expect(String(chat.messages.value[1]?.content)).toContain('boom')
  })

  it('clearConversation 清空消息；isAIAnswering 在问答期间为 true', async () => {
    // 用一个手动 resolve 的 promise 拉长问答窗口
    let resolve!: (v: { answer: string; citations: never[] } | null) => void
    answerMock.mockReturnValue(
      new Promise((r) => { resolve = r }) as ReturnType<typeof QnAService.Answer>,
    )
    const chat = mountHost()
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({
      id: 'ws-1',
      name: '笔记库',
      path: 'C:/notes',
      createdAt: '',
      lastOpenedAt: '',
    })

    const pending = chat.ask('问题')
    await flushPromises()
    expect(isAIAnswering.value).toBe(true)
    // 问答中不允许清空
    chat.clearConversation()
    expect(chat.messages.value.length).toBe(1)

    resolve({ answer: 'ok', citations: [] })
    await pending
    await flushPromises()
    expect(isAIAnswering.value).toBe(false)
    chat.clearConversation()
    expect(chat.messages.value).toHaveLength(0)
  })

  it('切换工作区会丢弃等待中的上下文并保留新工作区输入', async () => {
    let finishContext!: (value: string) => void
    const chat = mountHost(() => new Promise<string>(resolve => { finishContext = resolve }))
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({ id: 'a', name: 'A', path: 'C:/notes-a', createdAt: '', lastOpenedAt: '' })
    chat.question.value = 'A 的问题'
    const pending = chat.ask()
    const busyWhileReading = chat.isAsking.value

    ws.setCurrentWorkspace({ id: 'b', name: 'B', path: 'C:/notes-b', createdAt: '', lastOpenedAt: '' })
    chat.question.value = 'B 尚未发送的草稿'
    finishContext('A 的私有上下文')
    await pending
    await flushPromises()

    expect(busyWhileReading).toBe(true)
    expect(answerMock).not.toHaveBeenCalled()
    expect(chat.messages.value).toEqual([])
    expect(chat.question.value).toBe('B 尚未发送的草稿')
    expect(chat.isAsking.value).toBe(false)
    expect(isAIAnswering.value).toBe(false)
  })

  it.each(['resolve', 'reject'] as const)('切换后丢弃旧工作区的 %s 回答，同时保留新请求与全局回答状态', async (completion) => {
    type Response = Awaited<ReturnType<typeof QnAService.Answer>>
    let finishOld!: (response: Response) => void
    let failOld!: (error: Error) => void
    let finishNew!: (response: Response) => void
    answerMock.mockReturnValueOnce(new Promise<Response>((resolve, reject) => { finishOld = resolve; failOld = reject }) as ReturnType<typeof QnAService.Answer>)
      .mockReturnValueOnce(new Promise<Response>(resolve => { finishNew = resolve }) as ReturnType<typeof QnAService.Answer>)
    const chat = mountHost()
    useSettingsStore().settings.ai.baseURL = 'http://localhost:11434/v1'
    const ws = useWorkspaceStore()
    ws.setCurrentWorkspace({ id: 'a', name: 'A', path: 'C:/notes-a', createdAt: '', lastOpenedAt: '' })
    const oldRequest = chat.ask('A 的问题')
    await flushPromises()
    chat.question.value = 'A 尚未发送的草稿'

    ws.setCurrentWorkspace({ id: 'b', name: 'B', path: 'C:/notes-b', createdAt: '', lastOpenedAt: '' })
    const afterSwitch = { messages: [...chat.messages.value], question: chat.question.value, busy: chat.isAsking.value, globalBusy: isAIAnswering.value }
    const newRequest = chat.ask('B 的问题')
    await flushPromises()
    if (completion === 'resolve') finishOld({ answer: 'A 的私有回答', citations: [] } as Response)
    else failOld(new Error('A 的私有错误'))
    await oldRequest
    await flushPromises()
    const afterOldFinishes = { messages: [...chat.messages.value], busy: chat.isAsking.value, globalBusy: isAIAnswering.value }
    finishNew({ answer: 'B 的回答', citations: [] } as Response)
    await newRequest
    await flushPromises()

    expect(afterSwitch).toEqual({ messages: [], question: '', busy: false, globalBusy: true })
    expect(afterOldFinishes).toEqual({ messages: [{ role: 'user', content: 'B 的问题' }], busy: true, globalBusy: true })
    expect(chat.messages.value).toEqual([{ role: 'user', content: 'B 的问题' }, { role: 'assistant', content: 'B 的回答', citations: [] }])
    expect(answerMock.mock.calls.map(call => call[8])).toEqual(['C:/notes-a', 'C:/notes-b'])
    expect(chat.isAsking.value).toBe(false)
    expect(isAIAnswering.value).toBe(false)
  })
})
