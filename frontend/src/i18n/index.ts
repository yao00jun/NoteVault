import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN'
import enUS from './locales/en-US'

export type Locale = 'zh-CN' | 'en-US'

export const SUPPORTED_LOCALES: Locale[] = ['zh-CN', 'en-US']

const SETTINGS_KEY = 'notevault-settings'

/** 从持久化设置中读取初始语言（与 settings store 同一存储键） */
function initialLocale(): Locale {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY)
    if (raw) {
      const lang = JSON.parse(raw).language
      if (lang === 'en-US' || lang === 'zh-CN') return lang
    }
  } catch {
    /* 忽略损坏的存储 */
  }
  return 'zh-CN'
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(),
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS,
  },
})

/** 切换语言并同步 <html lang> */
export function setLocale(locale: Locale): void {
  i18n.global.locale.value = locale
  document.documentElement.setAttribute('lang', locale)
}
