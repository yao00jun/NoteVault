import { getCurrentScope, onScopeDispose, ref } from 'vue'
import { marked } from 'marked'
import { ReportService, TaskService, type WeeklyReportAIConfig } from '@/api'
import { i18n } from '@/i18n'
import { sanitizeHtml } from '@/utils/sanitize'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'

const BRIEFING_CACHE_PREFIX = 'notevault.briefing.'

interface TaskEventPayload {
  id?: string
  status?: string
  percent?: number
  message?: string
  error?: string
}

/**
 * 今日 AI 简报（打开即今天）：ReportService 生成接口的前端状态机。
 * 优先走异步任务（StartDailyBriefing，可取消、有进度），后端未注入任务队列时
 * 返回空 ID → 退回同步调用（行为与改造前一致）。
 * 简报不落盘——按 工作区+日期 缓存在 localStorage，当天重进不重复调用；
 * 换工作区或跨日自动失效（调用方 watch 后调 restoreBriefing）。
 * AI 未配置/失败时把原因放进 briefingError——工作台本身的确定性指标卡不受影响。
 */
export function useBriefing() {
  const workspace = useWorkspaceStore()
  const settings = useSettingsStore()

  const briefingText = ref('')
  const briefingLoading = ref(false)
  const briefingError = ref('')
  // 异步任务状态：进度来自 task:progress 事件，只关注「当前这个任务」
  const activeTaskID = ref<string | null>(null)
  const briefingProgress = ref('')
  const isCancelling = ref(false)
  let activeDate = ''
  let stopTaskEvents: (() => void) | undefined

  if (getCurrentScope()) {
    onScopeDispose(() => {
      stopTaskEvents?.()
      stopTaskEvents = undefined
    })
  }

  function cacheKey(date: string): string {
    return BRIEFING_CACHE_PREFIX + encodeURIComponent(workspace.currentWorkspace?.path || 'default') + '.' + date
  }

  function restoreBriefing(date: string) {
    briefingText.value = ''
    briefingError.value = ''
    try {
      briefingText.value = localStorage.getItem(cacheKey(date)) || ''
    } catch {
      // 存储不可用时静默：当作无缓存
    }
  }

  function aiConfig(): WeeklyReportAIConfig | null {
    const ai = settings.settings.ai
    if (!ai.baseURL?.trim() || !ai.model?.trim()) return null
    return { baseURL: ai.baseURL, model: ai.model, apiKey: ai.apiKey, protocol: ai.protocol }
  }

  // 事件订阅懒建立：同步兜底路径（旧后端/单测无 runtime）不付这份开销
  async function ensureTaskEvents() {
    if (stopTaskEvents) return
    try {
      const { Events } = await import('@wailsio/runtime')
      const offProgress = Events.On('task:progress', (ev?: { data?: TaskEventPayload }) =>
        applyTaskEvent(ev?.data, false),
      )
      const offFinished = Events.On('task:finished', (ev?: { data?: TaskEventPayload }) =>
        applyTaskEvent(ev?.data, true),
      )
      stopTaskEvents = () => {
        offProgress?.()
        offFinished?.()
      }
    } catch {
      // 无 runtime：仅同步兜底路径可用
    }
  }

  // applyTaskEvent 只处理「当前关注的任务」，避免被并发的编译/导入任务进度串扰。
  function applyTaskEvent(info: TaskEventPayload | undefined, finished: boolean) {
    if (!info || !activeTaskID.value || info.id !== activeTaskID.value) return
    if (info.message) briefingProgress.value = info.message
    if (finished) void finishTask(info)
  }

  async function finishTask(info: TaskEventPayload) {
    const id = info.id as string
    const date = activeDate
    const taskStatus = info.status ?? ''
    activeTaskID.value = null
    isCancelling.value = false
    briefingLoading.value = false
    briefingProgress.value = ''

    if (taskStatus === 'cancelled') {
      briefingError.value = i18n.global.t('today.briefing.cancelled')
      return
    }
    if (taskStatus === 'failed') {
      briefingError.value = info.error || i18n.global.t('today.briefing.failed')
      return
    }

    let res: { briefing?: string; aiUsed?: boolean; date?: string } | null = null
    try {
      res = (await ReportService.DailyBriefingTaskResult(id)) as {
        briefing?: string
        aiUsed?: boolean
        date?: string
      } | null
    } catch (e) {
      console.error('Failed to read daily briefing task result:', e)
    }
    const text = res?.briefing?.trim() || ''
    if (!text) {
      briefingError.value = i18n.global.t('today.briefing.empty')
      return
    }
    briefingText.value = text
    try {
      localStorage.setItem(cacheKey(date), text)
    } catch {
      // 配额满时静默：本次会话内仍可阅读
    }
  }

  async function generateBriefing(date: string) {
    const ws = workspace.currentWorkspace
    if (!ws || briefingLoading.value) return
    const config = aiConfig()
    if (!config) {
      briefingError.value = i18n.global.t('today.briefing.noAI')
      return
    }
    briefingLoading.value = true
    briefingError.value = ''
    briefingProgress.value = ''
    try {
      const taskID = (await ReportService.StartDailyBriefing(ws.path, date, config)) as string | null
      if (!taskID) {
        // 旧后端 / 未注入任务队列：退回同步语义
        await generateBriefingSync(ws.path, date, config)
        return
      }
      activeTaskID.value = taskID
      activeDate = date
      await ensureTaskEvents()
    } catch (cause) {
      briefingError.value = cause instanceof Error ? cause.message : String(cause)
      briefingLoading.value = false
    }
  }

  // --- 同步路径（后端未接入任务队列时的兜底，行为与改造前一致）---
  async function generateBriefingSync(path: string, date: string, config: WeeklyReportAIConfig) {
    try {
      const result = await ReportService.GenerateDailyBriefing(path, date, config)
      const text = result?.briefing?.trim() || ''
      if (!text) {
        briefingError.value = i18n.global.t('today.briefing.empty')
        return
      }
      briefingText.value = text
      try {
        localStorage.setItem(cacheKey(date), text)
      } catch {
        // 配额满时静默：本次会话内仍可阅读
      }
    } catch (cause) {
      briefingError.value = cause instanceof Error ? cause.message : String(cause)
    } finally {
      briefingLoading.value = false
    }
  }

  // 取消当前生成：TaskService.Cancel 是幂等的，任务刚好结束时返回 false，
  // 此时不弹错——finished 事件马上会把 UI 收尾。
  async function cancelBriefing() {
    const id = activeTaskID.value
    if (!id || isCancelling.value) return
    isCancelling.value = true
    try {
      const accepted = await TaskService.Cancel(id)
      if (!accepted) isCancelling.value = false
    } catch {
      isCancelling.value = false
    }
  }

  return {
    briefingText,
    briefingLoading,
    briefingError,
    briefingProgress,
    isCancelling,
    restoreBriefing,
    generateBriefing,
    cancelBriefing,
  }
}

/** 简报要点是模型输出的 Markdown，渲染前必须清洗。 */
export function renderBriefingHtml(markdown: string): string {
  return sanitizeHtml(marked.parse(markdown, { async: false }) as string)
}
