/**
 * useEditorToolbarSettings - 设置页编辑器区块的编辑工具栏配置逻辑
 * （按钮显隐 / 拖拽排序 / 自定义命令管理）。从 SettingsView 抽出。
 */
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { TOOLBAR_ITEMS, VISIBLE_DEFAULT, TOOLBAR_ORDER_DEFAULT, type ToolbarItem } from '@/components/editor/toolbarButtons'

export function useEditorToolbarSettings() {
  const { t } = useI18n()
  const settingsStore = useSettingsStore()

  function toggleToolbarButton(id: string) {
    const arr = settingsStore.settings.toolbar.visibleButtons
    if (arr.includes(id)) {
      settingsStore.settings.toolbar.visibleButtons = arr.filter((x) => x !== id)
    } else {
      settingsStore.settings.toolbar.visibleButtons = [...arr, id]
    }
  }

  function resetToolbarButtons() {
    settingsStore.settings.toolbar.visibleButtons = [...VISIBLE_DEFAULT]
    settingsStore.settings.toolbar.order = [...TOOLBAR_ORDER_DEFAULT]
  }

  // 工具栏按钮名称 / 固定标识查询
  const itemMap = new Map<string, ToolbarItem>(TOOLBAR_ITEMS.map((i) => [i.id as string, i]))
  function toolbarLabel(id: string): string {
    const it = itemMap.get(id)
    return it ? t(it.i18nKey || id) : id
  }
  function isFixedButton(id: string): boolean {
    return itemMap.get(id)?.fixed === true
  }

  // 拖拽排序（对应 Editing Toolbar 的 menu dragging and sorting）
  let dragId = ''
  function onDragStart(id: string, e: DragEvent) {
    dragId = id
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function onDrop(targetId: string) {
    const order = [...settingsStore.settings.toolbar.order]
    const from = order.indexOf(dragId)
    const to = order.indexOf(targetId)
    if (from < 0 || to < 0 || from === to) return
    order.splice(from, 1)
    order.splice(to, 0, dragId)
    settingsStore.settings.toolbar.order = order
  }

  // 自定义命令管理
  function addCustomCommand() {
    settingsStore.settings.toolbar.customCommands = [
      ...settingsStore.settings.toolbar.customCommands,
      {
        id: 'cmd-' + Date.now().toString(36),
        name: t('settings.editor.toolbar.newCommand'),
        type: 'wrap',
        prefix: '',
        suffix: '',
      },
    ]
  }
  function removeCustomCommand(idx: number) {
    settingsStore.settings.toolbar.customCommands = settingsStore.settings.toolbar.customCommands.filter(
      (_, i) => i !== idx,
    )
  }

  return {
    toggleToolbarButton,
    resetToolbarButtons,
    toolbarLabel,
    isFixedButton,
    onDragStart,
    onDrop,
    addCustomCommand,
    removeCustomCommand,
    t,
  }
}
