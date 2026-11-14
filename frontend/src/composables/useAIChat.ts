import { ref, computed, watch, onScopeDispose } from 'vue'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import { Events } from '@wailsio/runtime'
import { QnAService } from '@/api'
import type { QnACitation, RerankProvider } from '@/api'
import { i18n } from '@/i18n'
import { sanitizeHtml } from '@/utils/sanitize'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { conversationContext } from '@/utils/conversation'
import { isLocalBaseURL } from '@/utils/localEndpoint'
import { openDocument } from '@/utils/navigation'

/**
 * AI 问答核心状态机（AI-COPILOT-FLOATING-ORB 蓝图 Step 1）。
 *
 * 从 QnAView.vue 原地抽离：QnAService.Answer 调用、引用溯源、错误处理、
 * 本机端点（Ollama / LM Studio）免 Key 口径全部集中在这里，
 * QnAView 与 AICopilotDrawer 共享同一份纯粹的状态逻辑，零冗余。
 *
 * 本组合函数不含任何 DOM（消息区滚动条由消费方自己持有），
 * 且必须在组件 setup 上下文中调用（内部使用 useRouter）。
 */

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  citations?: QnACitation[]
  error?: boolean
}

export interface AskOptions {
  /**
   * 强制注入上下文（即使抽屉里「关联当前文档上下文」未勾选）。
   * 快捷胶囊「总结 / 提待办 / 润色」这类必须基于文档内容的指令使用。
   */
  forceContext?: boolean
}

/** 构造随问题一起发送的上下文（如当前文档内容）。返回 null 表示本次不带上下文 */
export type ContextBuilder = (force: boolean) => Promise<string | null> | string | null

export interface UseAIChatOptions {
  buildContext?: ContextBuilder
  /** 把对话持久化到本地（按工作区隔离），重开页面时恢复；仅抽屉 / QnA 全页开启，蒸馏窗口等单次场景关闭。 */
  persistHistory?: boolean
  /** 走 AnswerStream 流式回答（qna:delta 事件逐字上屏 + 可停止）；蒸馏窗口等 JSON 草稿场景关闭。 */
  stream?: boolean
}

// ---------------------------------------------------------------------------
// 模块级「AI 正在回答」标志：悬浮球的呼吸/流光动画读取它。
// 抽屉关闭时组件并不销毁（仅 CSS 隐藏），问答照常进行，球照转——
// 所以不能用抽屉组件内部的 isAsking，必须是跨组件可读的模块级状态。
// 用计数而非布尔：未来若允许多路并发问答也不会提前熄灭。
// ---------------------------------------------------------------------------
const activeAskCount = ref(0)
export const isAIAnswering = computed(() => activeAskCount.value > 0)

// ---------------------------------------------------------------------------
// 流式增量（qna:delta）路由：模块级单例订阅，按 requestID 分发到进行中的会话。
// 后端节流发射（≈60ms/批，负载 QnAStreamDelta{requestID, delta}）；最终结果以
// AnswerStream 的返回值为权威（promise resolve 时整体替换），事件只承担"逐字上屏"。
// ---------------------------------------------------------------------------
const QNA_STREAM_EVENT = 'qna:delta'
const deltaHandlers = new Map<string, (delta: string) => void>()
let deltaListenerAttached = false

function newStreamRequestID(): string {
  return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `req-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function ensureDeltaListener() {
  if (deltaListenerAttached) return
  try {
    Events.On(QNA_STREAM_EVENT, (ev?: { data?: { requestID?: string; delta?: string } | null }) => {
      const payload = ev?.data
      if (!payload?.requestID || typeof payload.delta !== 'string' || !payload.delta) return
      deltaHandlers.get(payload.requestID)?.(payload.delta)
    })
    deltaListenerAttached = true
  } catch (e) {
    console.warn('[useAIChat] 流式事件订阅不可用（非 Wails 环境），回退阻塞式:', e)
  }
}

/** 供不经 useAIChat 实例的调用方（OmniSearch）复用同一订阅路由；返回退订函数。 */
export function subscribeQnAStream(requestID: string, onDelta: (delta: string) => void): () => void {
  ensureDeltaListener()
  deltaHandlers.set(requestID, onDelta)
  return () => {
    if (deltaHandlers.get(requestID) === onDelta) deltaHandlers.delete(requestID)
  }
}

// ---------------------------------------------------------------------------
// 会话持久化（按工作区隔离）：开启后重开抽屉/页面可接续上一段对话。
// 错误消息不入档（属瞬态提示）；写入失败（配额/禁用）静默放弃——对话仍在内存可用。
// ---------------------------------------------------------------------------
const HISTORY_KEY_PREFIX = 'notevault.copilot.history.'
const HISTORY_MAX_MESSAGES = 50

function historyKey(workspacePath: string | undefined): string {
  return HISTORY_KEY_PREFIX + encodeURIComponent(workspacePath || 'default')
}

function loadHistory(workspacePath: string | undefined): ChatMessage[] {
  try {
    const raw = localStorage.getItem(historyKey(workspacePath))
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    const restorable = parsed.filter(
      (entry): entry is ChatMessage =>
        !!entry && typeof entry === 'object' &&
        ((entry as ChatMessage).role === 'user' || (entry as ChatMessage).role === 'assistant') &&
        typeof (entry as ChatMessage).content === 'string' && !(entry as ChatMessage).error,
    )
    return restorable.slice(-HISTORY_MAX_MESSAGES)
  } catch {
    return []
  }
}

function saveHistory(workspacePath: string | undefined, messages: ChatMessage[]) {
  try {
    const restorable = messages.filter(message => !message.error)
    // 尾部悬空的用户消息（回答尚未落定或已失败）不入档，避免恢复出"无回应的提问"
    while (restorable.length && restorable[restorable.length - 1]!.role === 'user') restorable.pop()
    const capped = restorable.slice(-HISTORY_MAX_MESSAGES)
    if (!capped.length) localStorage.removeItem(historyKey(workspacePath))
    else localStorage.setItem(historyKey(workspacePath), JSON.stringify(capped))
  } catch {
    // 配额满或存储被禁用时静默放弃
  }
}

export function useAIChat(options: UseAIChatOptions = {}) {
  const t = i18n.global.t
  const router = useRouter()
  const workspaceStore = useWorkspaceStore()
  const settingsStore = useSettingsStore()

  const messages = ref<ChatMessage[]>(options.persistHistory ? loadHistory(workspaceStore.currentWorkspace?.path) : [])
  const question = ref('')
  const isAsking = ref(false)
  // 流式状态（stream 选项开启时使用）：streamingText 是气泡上的渐进文本，
  // 最终消息仍以 AnswerStream 返回值整体入列（persistHistory 只在那时变化一次）。
  const streaming = ref(false)
  const streamingText = ref('')
  let streamRequestID = ''
  let streamCall: ReturnType<typeof QnAService.AnswerStream> | null = null
  let workspaceGeneration = 0
  let disposed = false

  function resetWorkspaceConversation() {
    workspaceGeneration++
    messages.value = []
    question.value = ''
    isAsking.value = false
    if (streamRequestID) deltaHandlers.delete(streamRequestID)
    streaming.value = false
    streamingText.value = ''
    streamCall = null
  }

  // 持久化开启时：切换前把当前对话写入旧工作区档案，再载入新工作区的历史。
  watch(() => workspaceStore.currentWorkspace?.path, (path, previous) => {
    if (options.persistHistory) saveHistory(previous, messages.value)
    resetWorkspaceConversation()
    if (options.persistHistory) messages.value = loadHistory(path)
  }, { flush: 'sync' })

  // 防抖落盘；卸载前先落盘（注册顺序保证先于下方 reset 存到完整对话）
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  if (options.persistHistory) {
    watch(messages, (current) => {
      if (saveTimer) clearTimeout(saveTimer)
      saveTimer = setTimeout(() => {
        saveTimer = null
        saveHistory(workspaceStore.currentWorkspace?.path, current)
      }, 400)
    }, { deep: true })
    onScopeDispose(() => {
      if (saveTimer) {
        clearTimeout(saveTimer)
        saveHistory(workspaceStore.currentWorkspace?.path, messages.value)
      }
    })
  }
  onScopeDispose(() => {
    disposed = true
    resetWorkspaceConversation()
  })

  const canAsk = computed(() => question.value.trim().length > 0 && !isAsking.value)

  /**
   * 发起一次问答。
   * @param questionOverride 快捷指令的预置问题（绕过输入框；此时保留输入框草稿）
   */
  async function ask(questionOverride?: string, opts?: AskOptions) {
    const q = (questionOverride ?? question.value).trim()
    if (!q || isAsking.value || disposed) return

    const ws = workspaceStore.currentWorkspace
    if (!ws) {
      messages.value.push({ role: 'assistant', content: t('qna.noWorkspace'), error: true })
      return
    }
    const ai = settingsStore.settings.ai
    const emb = settingsStore.settings.embedding
    const rerank = settingsStore.settings.rerank
    // 本机端点（Ollama / LM Studio）免 Key——与后端 requireCredential 同口径；
    // 云端端点仍必须填 Key
    if (!isLocalBaseURL(ai.baseURL) && (!ai.apiKey || !ai.apiKey.trim())) {
      messages.value.push({ role: 'assistant', content: t('qna.noApiKey'), error: true })
      return
    }

    const generation = workspaceGeneration
    const isCurrent = () => !disposed && generation === workspaceGeneration && ws.path === workspaceStore.currentWorkspace?.path
    let counted = false
    // Reserve this conversation while context loads; a workspace change can start
    // a new conversation without letting the old request append to it.
    isAsking.value = true
    try {
      let context: string | null = null
      try {
        context = (await options.buildContext?.(opts?.forceContext ?? false)) ?? null
      } catch (e) {
        if (!isCurrent()) return
        console.warn('[useAIChat] 读取上下文失败，本次不带文档上下文:', e)
      }
      if (!isCurrent()) return
      const currentPrompt = context
        ? `${t('copilot.contextPreamble')}\n\n${context}\n\n---\n\n${q}`
        : q
      const payload = conversationContext(messages.value) + currentPrompt

      messages.value.push({ role: 'user', content: q })
      // 快捷指令不清空输入框（用户可能正打着草稿）
      if (questionOverride == null) question.value = ''
      activeAskCount.value += 1
      counted = true

      const rerankCfg = {
        provider: rerank.provider as unknown as RerankProvider,
        baseURL: rerank.baseURL,
        model: rerank.model,
        apiKey: rerank.apiKey,
      }
      let resp: Awaited<ReturnType<typeof QnAService.Answer>>
      if (options.stream) {
        // 流式：注册增量路由 → qna:delta 逐批追加到 streamingText（气泡渐进渲染）；
        // promise resolve 时以下发的 resp.answer 为最终权威整体落一条消息。
        ensureDeltaListener()
        streamRequestID = newStreamRequestID()
        deltaHandlers.set(streamRequestID, (delta) => { streamingText.value += delta })
        streaming.value = true
        streamingText.value = ''
        streamCall = QnAService.AnswerStream(
          ai.apiKey,
          ai.baseURL,
          ai.model,
          ai.protocol,
          emb.baseURL,
          emb.model,
          emb.apiKey,
          rerankCfg,
          ws.path,
          payload,
          streamRequestID,
        )
        resp = await streamCall
      } else {
        resp = await QnAService.Answer(
          ai.apiKey,
          ai.baseURL,
          ai.model,
          ai.protocol,
          emb.baseURL,
          emb.model,
          emb.apiKey,
          rerankCfg,
          ws.path,
          payload,
        )
      }
      if (!isCurrent()) return
      messages.value.push({
        role: 'assistant',
        content: (resp?.answer ?? '').trim() || t('qna.emptyTitle'),
        citations: resp?.citations ?? [],
      })
    } catch (e) {
      if (!isCurrent()) return
      if ((e as { name?: string } | null)?.name === 'CancelError') {
        // 用户主动停止：保留已生成的部分文本为正常回答（无 error 标记）
        const partial = streamingText.value.trim()
        if (partial) messages.value.push({ role: 'assistant', content: partial, citations: [] })
        return
      }
      messages.value.push({
        role: 'assistant',
        content: t('qna.askFailed', { msg: (e as Error).message }),
        error: true,
      })
    } finally {
      if (options.stream) {
        if (streamRequestID) deltaHandlers.delete(streamRequestID)
        streaming.value = false
        streamingText.value = ''
        streamCall = null
      }
      if (isCurrent()) isAsking.value = false
      // Detached calls still count until their actual backend promise settles.
      if (counted) activeAskCount.value -= 1
    }
  }

  function clearConversation() {
    if (isAsking.value) return
    messages.value = []
    if (options.persistHistory) saveHistory(workspaceStore.currentWorkspace?.path, [])
  }

  /** 停止当前流式回答：取消后端调用（Wails ctx 取消中断 SSE），已生成的部分文本保留为普通回答。 */
  function stopStreaming() {
    if (!streaming.value) return
    try {
      streamCall?.cancel?.()
    } catch (e) {
      console.warn('[useAIChat] 停止流式请求失败:', e)
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void ask()
    }
  }

  /**
   * 打开引用来源。P3-9：带上 Heading 让编辑器滚到具体章节而不只是打开文件。
   * heading 为空（旧后端 / 无章节的块）时自动降级为「只打开文件」。
   * chunkIndex 只是占位接收——它到行号没有可靠映射，不据此编造位置。
   */
  function openCitation(citation: { path: string; heading?: string; chunkIndex?: number }) {
    const path = citation?.path
    if (!path) return
    void openDocument(router, workspaceStore, path, citation.heading)
  }

  /** AI 回答渲染为 Markdown 前必须清洗：模型输出也可能带注入 HTML */
  function renderMarkdown(content: string): string {
    return sanitizeHtml(marked.parse(content, { async: false }) as string)
  }

  return {
    messages,
    question,
    isAsking,
    streaming,
    streamingText,
    canAsk,
    ask,
    stopStreaming,
    clearConversation,
    onKeydown,
    openCitation,
    renderMarkdown,
  }
}
