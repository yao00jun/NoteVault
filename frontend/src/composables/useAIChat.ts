import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import { QnAService } from '@/api'
import type { QnACitation, RerankProvider } from '@/api'
import { sanitizeHtml } from '@/utils/sanitize'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { isLocalBaseURL } from '@/utils/localEndpoint'

/**
 * AI 问答核心状态机（AI-COPILOT-FLOATING-ORB 蓝图 Step 1）。
 *
 * 从 QnAView.vue 原地抽离：QnAService.Answer 调用、引用溯源、错误处理、
 * 本机端点（Ollama / LM Studio）免 Key 口径全部集中在这里，
 * QnAView 与 AICopilotDrawer 共享同一份纯粹的状态逻辑，零冗余。
 *
 * 本组合函数不含任何 DOM（消息区滚动条由消费方自己持有），
 * 且必须在组件 setup 上下文中调用（内部使用 useI18n / useRouter）。
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
}

// ---------------------------------------------------------------------------
// 模块级「AI 正在回答」标志：悬浮球的呼吸/流光动画读取它。
// 抽屉关闭时组件并不销毁（仅 CSS 隐藏），问答照常进行，球照转——
// 所以不能用抽屉组件内部的 isAsking，必须是跨组件可读的模块级状态。
// 用计数而非布尔：未来若允许多路并发问答也不会提前熄灭。
// ---------------------------------------------------------------------------
const activeAskCount = ref(0)
export const isAIAnswering = computed(() => activeAskCount.value > 0)

export function useAIChat(options: UseAIChatOptions = {}) {
  const { t } = useI18n()
  const router = useRouter()
  const workspaceStore = useWorkspaceStore()
  const settingsStore = useSettingsStore()

  const messages = ref<ChatMessage[]>([])
  const question = ref('')
  const isAsking = ref(false)

  const canAsk = computed(() => question.value.trim().length > 0 && !isAsking.value)

  /**
   * 发起一次问答。
   * @param questionOverride 快捷指令的预置问题（绕过输入框；此时保留输入框草稿）
   */
  async function ask(questionOverride?: string, opts?: AskOptions) {
    const q = (questionOverride ?? question.value).trim()
    if (!q || isAsking.value) return

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

    // 上下文注入：消费方（Copilot 抽屉）可通过 buildContext 提供当前文档内容。
    // 上下文只拼进发给后端的问题里，消息列表仍展示干净的问题原文。
    let context: string | null = null
    try {
      context = (await options.buildContext?.(opts?.forceContext ?? false)) ?? null
    } catch (e) {
      // 上下文读取失败不阻断问答，退化为纯全库 RAG
      console.warn('[useAIChat] 读取上下文失败，本次不带文档上下文:', e)
    }
    const payload = context
      ? `${t('copilot.contextPreamble')}\n\n${context}\n\n---\n\n${q}`
      : q

    messages.value.push({ role: 'user', content: q })
    // 快捷指令不清空输入框（用户可能正打着草稿）
    if (questionOverride == null) question.value = ''
    isAsking.value = true
    activeAskCount.value += 1
    try {
      const resp = await QnAService.Answer(
        ai.apiKey,
        ai.baseURL,
        ai.model,
        ai.protocol,
        emb.baseURL,
        emb.model,
        emb.apiKey,
        {
          provider: rerank.provider as unknown as RerankProvider,
          baseURL: rerank.baseURL,
          model: rerank.model,
          apiKey: rerank.apiKey,
        },
        ws.path,
        payload,
      )
      messages.value.push({
        role: 'assistant',
        content: (resp?.answer ?? '').trim() || t('qna.emptyTitle'),
        citations: resp?.citations ?? [],
      })
    } catch (e) {
      messages.value.push({
        role: 'assistant',
        content: t('qna.askFailed', { msg: (e as Error).message }),
        error: true,
      })
    } finally {
      isAsking.value = false
      activeAskCount.value -= 1
    }
  }

  function clearConversation() {
    if (isAsking.value) return
    messages.value = []
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void ask()
    }
  }

  function openCitation(path: string) {
    workspaceStore.openFile(path)
    void router.push('/editor')
  }

  /** AI 回答渲染为 Markdown 前必须清洗：模型输出也可能带注入 HTML */
  function renderMarkdown(content: string): string {
    return sanitizeHtml(marked.parse(content, { async: false }) as string)
  }

  return {
    messages,
    question,
    isAsking,
    canAsk,
    ask,
    clearConversation,
    onKeydown,
    openCitation,
    renderMarkdown,
  }
}
