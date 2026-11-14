import { ref } from 'vue'
import { marked } from 'marked'
import { ReportService, type WeeklyReportAIConfig } from '@/api'
import { i18n } from '@/i18n'
import { sanitizeHtml } from '@/utils/sanitize'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'

const BRIEFING_CACHE_PREFIX = 'notevault.briefing.'

/**
 * 今日 AI 简报（打开即今天）：ReportService.GenerateDailyBriefing 的前端状态机。
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
    try {
      const result = await ReportService.GenerateDailyBriefing(ws.path, date, config)
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

  return { briefingText, briefingLoading, briefingError, restoreBriefing, generateBriefing }
}

/** 简报要点是模型输出的 Markdown，渲染前必须清洗。 */
export function renderBriefingHtml(markdown: string): string {
  return sanitizeHtml(marked.parse(markdown, { async: false }) as string)
}
