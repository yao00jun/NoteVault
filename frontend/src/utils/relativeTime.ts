import { i18n } from '@/i18n'

/**
 * 相对时间显示：<7 天用"n 前的人类语言"，更早回落到绝对时间（月/日 时:分）。
 * 用于最近文档、文档列表等"时间只为了解新旧程度"的场景。
 */
export function formatRelativeTime(value: string): string {
  if (!value) return i18n.global.t('common.noRecord')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return i18n.global.t('common.noRecord')

  const diffMs = Date.now() - date.getTime()
  const minutes = Math.floor(diffMs / 60000)
  if (minutes < 1) return i18n.global.t('common.justNow')
  if (minutes < 60) return i18n.global.t('common.minutesAgo', { n: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return i18n.global.t('common.hoursAgo', { n: hours })
  const days = Math.floor(hours / 24)
  if (days < 7) return i18n.global.t('common.daysAgo', { n: days })

  // 超过一周回落绝对时间（月/日 时:分），与 collectionDateLabel 口径一致
  return date.toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
