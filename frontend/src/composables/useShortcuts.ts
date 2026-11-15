import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

export interface ShortcutEntry {
  keys: string
  desc: string
}

export interface ShortcutGroup {
  id: 'global' | 'editor'
  label: string
  shortcuts: ShortcutEntry[]
}

/**
 * 应用快捷键的唯一清单数据源：设置页「快捷键」分区与速查浮层共用，
 * 新增快捷键只需在此登记（与 App.vue / MarkdownEditor 的真实注册保持同步）。
 */
export function useShortcuts() {
  const { t } = useI18n()

  const groups = computed<ShortcutGroup[]>(() => [
    {
      id: 'global',
      label: t('cheatsheet.groupGlobal'),
      shortcuts: [
        { keys: 'Ctrl + P', desc: t('settings.shortcuts.commandPalette') },
        { keys: 'Ctrl + K', desc: t('settings.shortcuts.aiSearch') },
        { keys: 'Ctrl + J', desc: t('settings.shortcuts.copilot') },
        { keys: 'Ctrl + N', desc: t('settings.shortcuts.newFile') },
        { keys: 'Ctrl + ,', desc: t('settings.shortcuts.openSettings') },
        { keys: '?', desc: t('settings.shortcuts.cheatSheet') },
        { keys: 'Alt + ←', desc: t('settings.shortcuts.back') },
        { keys: 'Alt + →', desc: t('settings.shortcuts.forward') },
      ],
    },
    {
      id: 'editor',
      label: t('cheatsheet.groupEditor'),
      shortcuts: [
        { keys: 'Ctrl + S', desc: t('settings.shortcuts.inEditor', { action: t('settings.shortcuts.saveDoc') }) },
        { keys: 'Ctrl + B', desc: t('settings.shortcuts.inEditor', { action: t('settings.shortcuts.bold') }) },
        { keys: 'Ctrl + I', desc: t('settings.shortcuts.inEditor', { action: t('settings.shortcuts.italic') }) },
        { keys: 'Ctrl + K', desc: t('settings.shortcuts.inEditor', { action: t('settings.shortcuts.link') }) },
        { keys: 'Ctrl + \\', desc: t('settings.shortcuts.inEditor', { action: t('settings.shortcuts.code') }) },
      ],
    },
  ])

  const flat = computed<ShortcutEntry[]>(() => groups.value.flatMap((group) => group.shortcuts))

  return { groups, flat }
}
