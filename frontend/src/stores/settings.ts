import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import type { AppSettings, ThemeType } from '@/types'
import { VISIBLE_DEFAULT, TOOLBAR_ORDER_DEFAULT } from '@/components/editor/toolbarButtons'
import { setLocale, type Locale } from '@/i18n'

const STORAGE_KEY = 'notevault-settings'

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
      return {
        ...defaultSettings,
        ...stored,
        ai: { ...defaultSettings.ai, ...(stored.ai ?? {}) },
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

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<AppSettings>(loadSettings())

  // 持久化
  watch(
    settings,
    (val) => {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(val))
    },
    { deep: true },
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
