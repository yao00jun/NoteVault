import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { CredentialService } from '@bindings/github.com/notevault/notevault/index.js'
import type { AppSettings, ThemeType } from '@/types'
import { VISIBLE_DEFAULT, TOOLBAR_ORDER_DEFAULT } from '@/components/editor/toolbarButtons'
import { setLocale, type Locale } from '@/i18n'

const STORAGE_KEY = 'notevault-settings'

// P2-5：API Key 不再进 localStorage（任何拿到 WebView 执行权的代码都能读），
// 改存系统凭据库（Windows 凭据管理器，经后端 CredentialService）。
const API_KEY_CREDENTIAL = 'ai.apiKey'
// P1-3：embedding 端点同样可能带 Key（如云端 embedding），与 ai 各自独立存系统凭据库。
const EMBEDDING_API_KEY_CREDENTIAL = 'embedding.apiKey'
// P1-3b：rerank 端点（cohere 等）同样可能带 Key，与 ai / embedding 各自独立存系统凭据库。
const RERANK_API_KEY_CREDENTIAL = 'rerank.apiKey'

const defaultSettings: AppSettings = {
  theme: 'islands-dark',
  language: 'zh-CN',
  sidebarCollapsed: false,
  autoSaveInterval: 500,
  editorMode: 'split',
  fontSize: 13,
  ai: {
    baseURL: 'https://api.openai.com/v1',
    model: 'gpt-4o-mini',
    apiKey: '',
  },
  // P1-3：语义检索的 embedding 端点，默认本机 Ollama + bge-m3（中文强）。
  embedding: {
    baseURL: 'http://localhost:11434/v1',
    model: 'bge-m3',
    apiKey: '',
  },
  // P1-3b：重排序端点。默认**关闭**（provider 留空）——因为本机 Ollama 原生不支持
  // /api/rerank（上游 PR 从未合并），默认开 Ollama 会静默 404 降级为纯 RRF，用户完全无感。
  // 需显式选 Cohere（真实支持重排）或将来接入支持重排的端点才生效。
  rerank: {
    provider: '',
    baseURL: 'http://localhost:11434',
    model: '',
    apiKey: '',
  },
  editor: {
    lineHeight: 1.6,
    previewFontSize: 14,
  },
  toolbar: {
    mode: 'top',
    visibleButtons: VISIBLE_DEFAULT,
    order: TOOLBAR_ORDER_DEFAULT,
    customCommands: [],
  },
  reminder: {
    defaultTime: '09:00',
    doNotDisturb: {
      enabled: false,
      start: '22:00',
      end: '08:00',
    },
    repeatOverdue: true,
  },
  trash: {
    autoPurgeDays: 30,
    confirmDelete: true,
  },
  errorReport: {
    sentryDSN: '',
    enableLocalLog: true,
  },
}

function loadSettings(): AppSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const stored = JSON.parse(raw) as Partial<AppSettings>
      // P2-5 迁移：旧版本把 apiKey 明文存在 localStorage 里。这里绝不把它
      // 恢复进 localStorage 流程——留给 setup 里的 migrateLegacyApiKey 转存凭据库。
      if (stored.ai) stored.ai.apiKey = ''
      if (stored.embedding) stored.embedding.apiKey = ''
      if (stored.rerank) stored.rerank.apiKey = ''
      return {
        ...defaultSettings,
        ...stored,
        ai: { ...defaultSettings.ai, ...(stored.ai ?? {}) },
        embedding: { ...defaultSettings.embedding, ...(stored.embedding ?? {}) },
        rerank: { ...defaultSettings.rerank, ...(stored.rerank ?? {}) },
        editor: { ...defaultSettings.editor, ...(stored.editor ?? {}) },
        reminder: {
          ...defaultSettings.reminder,
          ...(stored.reminder ?? {}),
          doNotDisturb: {
            ...defaultSettings.reminder.doNotDisturb,
            ...(stored.reminder?.doNotDisturb ?? {}),
          },
        },
        trash: { ...defaultSettings.trash, ...(stored.trash ?? {}) },
        errorReport: { ...defaultSettings.errorReport, ...(stored.errorReport ?? {}) },
        toolbar: {
          ...defaultSettings.toolbar,
          ...(stored.toolbar ?? {}),
          visibleButtons: stored.toolbar?.visibleButtons ?? defaultSettings.toolbar.visibleButtons,
          order: stored.toolbar?.order ?? defaultSettings.toolbar.order,
          customCommands: stored.toolbar?.customCommands ?? defaultSettings.toolbar.customCommands,
        },
      }
    }
  } catch (e) {
    console.warn('Failed to load settings:', e)
  }
  return defaultSettings
}

/**
 * P2-5 迁移：把旧版本遗留在 localStorage 里的明文 apiKey 搬进系统凭据库，
 * 然后从 localStorage 中彻底清除。只需执行一次（搬完即从存储里消失）。
 */
function migrateLegacyApiKey(): void {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const stored = JSON.parse(raw) as Partial<AppSettings>
    const ai = stored.ai
    if (!ai) return
    const legacy = ai.apiKey
    if (typeof legacy !== 'string' || legacy === '') return
    void CredentialService.SaveCredential(API_KEY_CREDENTIAL, legacy).catch((e) => {
      console.warn('[settings] 迁移 API Key 到系统凭据库失败:', e)
    })
    ai.apiKey = ''
    localStorage.setItem(STORAGE_KEY, JSON.stringify(stored))
  } catch (e) {
    console.warn('[settings] API Key 迁移检查失败:', e)
  }
}

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<AppSettings>(loadSettings())

  /**
   * P2-5：从系统凭据库恢复 apiKey / embedding apiKey（应用启动路径，异步不阻塞首屏）。
   * 凭据库不可用（如未授权访问）时不阻塞：用户在设置页重填一次即可。
   */
  async function restoreApiKey(): Promise<void> {
    try {
      const key = await CredentialService.GetCredential(API_KEY_CREDENTIAL)
      if (typeof key === 'string' && key !== '' && settings.value.ai.apiKey === '') {
        settings.value.ai.apiKey = key
      }
    } catch (e) {
      console.warn('[settings] 从系统凭据库恢复 API Key 失败:', e)
    }
    try {
      const embKey = await CredentialService.GetCredential(EMBEDDING_API_KEY_CREDENTIAL)
      if (typeof embKey === 'string' && embKey !== '' && settings.value.embedding.apiKey === '') {
        settings.value.embedding.apiKey = embKey
      }
    } catch (e) {
      console.warn('[settings] 从系统凭据库恢复 Embedding Key 失败:', e)
    }
    try {
      const rerankKey = await CredentialService.GetCredential(RERANK_API_KEY_CREDENTIAL)
      if (typeof rerankKey === 'string' && rerankKey !== '' && settings.value.rerank.apiKey === '') {
        settings.value.rerank.apiKey = rerankKey
      }
    } catch (e) {
      console.warn('[settings] 从系统凭据库恢复 Rerank Key 失败:', e)
    }
  }

  // 迁移与恢复在 store 首次创建时执行
  migrateLegacyApiKey()
  void restoreApiKey()

  // 持久化：apiKey / embedding.apiKey 单独走系统凭据库，localStorage 里永远只有空串占位
  watch(
    settings,
    (val) => {
      const { ai, embedding, rerank, ...rest } = val
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          ...rest,
          ai: { ...ai, apiKey: '' },
          embedding: { ...embedding, apiKey: '' },
          rerank: { ...rerank, apiKey: '' },
        }),
      )
    },
    { deep: true },
  )

  // apiKey 变化单独同步进系统凭据库；空串由后端翻译成删除
  watch(
    () => settings.value.ai.apiKey,
    (key) => {
      void CredentialService.SaveCredential(API_KEY_CREDENTIAL, key ?? '').catch((e) => {
        console.warn('[settings] 保存 API Key 到系统凭据库失败:', e)
      })
    },
  )

  // embedding apiKey 同上
  watch(
    () => settings.value.embedding.apiKey,
    (key) => {
      void CredentialService.SaveCredential(EMBEDDING_API_KEY_CREDENTIAL, key ?? '').catch((e) => {
        console.warn('[settings] 保存 Embedding Key 到系统凭据库失败:', e)
      })
    },
  )

  // rerank apiKey 同上（P1-3b）
  watch(
    () => settings.value.rerank.apiKey,
    (key) => {
      void CredentialService.SaveCredential(RERANK_API_KEY_CREDENTIAL, key ?? '').catch((e) => {
        console.warn('[settings] 保存 Rerank Key 到系统凭据库失败:', e)
      })
    },
  )

  // 主题切换
  function setTheme(theme: ThemeType) {
    settings.value.theme = theme
    applyTheme(theme)
  }

  function applyTheme(theme: ThemeType) {
    document.documentElement.setAttribute('data-theme', theme)
  }

  function toggleSidebar() {
    settings.value.sidebarCollapsed = !settings.value.sidebarCollapsed
  }

  function setEditorMode(mode: 'split' | 'editor' | 'preview') {
    settings.value.editorMode = mode
  }

  // 语言切换（同步 vue-i18n 实例）
  function setLanguage(language: Locale) {
    settings.value.language = language
    setLocale(language)
  }

  // 初始化时应用主题
  applyTheme(settings.value.theme)

  return {
    settings,
    setTheme,
    toggleSidebar,
    setEditorMode,
    setLanguage,
  }
})
