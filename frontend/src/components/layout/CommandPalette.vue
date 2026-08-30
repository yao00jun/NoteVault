<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUnmount } from 'vue'
import { Search, FileText, Folder, Palette, Settings, Save, X, Columns, ChevronRight, MessageCircle, Upload, Puzzle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'

const { t } = useI18n()

const props = defineProps<{
  visible: boolean
}>()

const emit = defineEmits<{
  'close': []
  'new-file': []
  'save': []
  'toggle-view': []
}>()

const router = useRouter()
const settingsStore = useSettingsStore()
const workspaceStore = useWorkspaceStore()
const pluginRuntimeStore = usePluginRuntimeStore()

const searchQuery = ref('')
const selectedIndex = ref(0)
const inputRef = ref<HTMLInputElement | null>(null)

interface Command {
  id: string
  label: string
  description: string
  icon: any
  action: () => void
  shortcut?: string
}

interface PluginCommand extends Command {
  description: string
  id: string
  label: string
  pluginId: string
}

const allCommands = computed<Command[]>(() => [
  {
    id: 'new-file',
    label: t('palette.commands.newFile.label'),
    description: t('palette.commands.newFile.desc'),
    icon: FileText,
    shortcut: 'Ctrl+N',
    action: () => { emit('new-file'); close() },
  },
  {
    id: 'search',
    label: t('palette.commands.globalSearch.label'),
    description: t('palette.commands.globalSearch.desc'),
    icon: Search,
    shortcut: 'Ctrl+Shift+F',
    action: () => { router.push('/search'); close() },
  },
  {
    id: 'tags',
    label: t('palette.commands.tags.label'),
    description: t('palette.commands.tags.desc'),
    icon: Folder,
    action: () => { router.push('/tags'); close() },
  },
  {
    id: 'todos',
    label: t('palette.commands.todos.label'),
    description: t('palette.commands.todos.desc'),
    icon: FileText,
    action: () => { router.push('/todos'); close() },
  },
  {
    id: 'reminders',
    label: t('palette.commands.reminders.label'),
    description: t('palette.commands.reminders.desc'),
    icon: Settings,
    action: () => { router.push('/reminders'); close() },
  },
  {
    id: 'archive',
    label: t('palette.commands.archive.label'),
    description: t('palette.commands.archive.desc'),
    icon: Folder,
    action: () => { router.push('/archive'); close() },
  },
  {
    id: 'trash',
    label: t('palette.commands.trash.label'),
    description: t('palette.commands.trash.desc'),
    icon: X,
    action: () => { router.push('/trash'); close() },
  },
  {
    id: 'qna',
    label: t('palette.commands.qna.label'),
    description: t('palette.commands.qna.desc'),
    icon: MessageCircle,
    action: () => { router.push('/qna'); close() },
  },
  {
    id: 'import',
    label: t('palette.commands.import.label'),
    description: t('palette.commands.import.desc'),
    icon: Upload,
    action: () => { router.push('/import'); close() },
  },
  {
    id: 'plugins',
    label: t('palette.commands.plugins.label'),
    description: t('palette.commands.plugins.desc'),
    icon: Puzzle,
    action: () => { router.push('/plugins'); close() },
  },
  {
    id: 'toggle-theme-macos',
    label: t('palette.commands.themeMacOS.label'),
    description: t('palette.commands.themeMacOS.desc'),
    icon: Palette,
    action: () => { settingsStore.setTheme('macos'); close() },
  },
  {
    id: 'toggle-theme-winui',
    label: t('palette.commands.themeWinUI.label'),
    description: t('palette.commands.themeWinUI.desc'),
    icon: Palette,
    action: () => { settingsStore.setTheme('winui'); close() },
  },
  {
    id: 'toggle-theme-islands',
    label: t('palette.commands.themeIslands.label'),
    description: t('palette.commands.themeIslands.desc'),
    icon: Palette,
    action: () => { settingsStore.setTheme('islands-dark'); close() },
  },
  {
    id: 'save',
    label: t('palette.commands.saveDoc.label'),
    description: t('palette.commands.saveDoc.desc'),
    icon: Save,
    shortcut: 'Ctrl+S',
    action: () => { emit('save'); close() },
  },
  {
    id: 'toggle-view',
    label: t('palette.commands.toggleView.label'),
    description: t('palette.commands.toggleView.desc'),
    icon: Columns,
    action: () => { emit('toggle-view'); close() },
  },
  {
    id: 'settings',
    label: t('palette.commands.settings.label'),
    description: t('palette.commands.settings.desc'),
    icon: Settings,
    shortcut: 'Ctrl+,',
    action: () => { router.push('/settings'); close() },
  },
  {
    id: 'home',
    label: t('palette.commands.home.label'),
    description: t('palette.commands.home.desc'),
    icon: ChevronRight,
    action: () => { router.push('/'); close() },
  },
])

const runtimeCommands = computed<PluginCommand[]>(() => pluginRuntimeStore.commands.map(command => ({
  id: command.id,
  label: command.label,
  description: command.description || '',
  icon: Puzzle,
  pluginId: command.pluginId,
  action: () => { void pluginRuntimeStore.runCommand(command.pluginId, command.id) },
})))

const mergedCommands = computed<Command[]>(() => [...allCommands.value, ...runtimeCommands.value])

const filteredCommands = computed(() => {
  if (!searchQuery.value.trim()) return mergedCommands.value
  const query = searchQuery.value.toLowerCase()
  return mergedCommands.value.filter(
    (cmd) =>
      cmd.label.toLowerCase().includes(query) ||
      cmd.description.toLowerCase().includes(query)
  )
})

watch(() => props.visible, (val) => {
  if (val) {
    searchQuery.value = ''
    selectedIndex.value = 0
    nextTick(() => {
      inputRef.value?.focus()
    })
  }
})

watch(searchQuery, () => {
  selectedIndex.value = 0
})

function close() {
  emit('close')
}

function selectCommand(index: number) {
  if (index >= 0 && index < filteredCommands.value.length) {
    filteredCommands.value[index].action()
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = Math.min(selectedIndex.value + 1, filteredCommands.value.length - 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = Math.max(selectedIndex.value - 1, 0)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    selectCommand(selectedIndex.value)
  }
}

function handleGlobalKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' || !props.visible || e.defaultPrevented) return
  e.preventDefault()
  close()
}

window.addEventListener('keydown', handleGlobalKeydown)

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible"
      class="command-palette-overlay"
      @click.self="close"
    >
      <div
        class="command-palette"
        @keydown="handleKeydown"
      >
        <div class="palette-header">
          <Search
            :size="18"
            class="palette-icon"
          />
          <input
            ref="inputRef"
            v-model="searchQuery"
            type="text"
            class="palette-input"
            data-testid="command-palette-input"
            :placeholder="t('palette.placeholder')"
            autofocus
          >
          <kbd class="palette-shortcut">ESC</kbd>
        </div>
        <div class="palette-commands">
          <div
            v-if="filteredCommands.length === 0"
            class="no-results"
          >
            {{ t('palette.noMatch') }}
          </div>
          <div
            v-for="(cmd, index) in filteredCommands"
            :key="cmd.id"
            class="command-item"
            v-bind="'pluginId' in cmd ? {
              'data-testid': 'plugin-command',
              'data-plugin-id': cmd.pluginId,
            } : { 'data-testid': 'command-item' }"
            :class="{ selected: index === selectedIndex }"
            @click="selectCommand(index)"
            @mouseenter="selectedIndex = index"
          >
            <div class="command-icon">
              <component
                :is="cmd.icon"
                :size="16"
              />
            </div>
            <div class="command-info">
              <div class="command-label">
                {{ cmd.label }}
              </div>
              <div class="command-desc">
                {{ cmd.description }}
              </div>
            </div>
            <kbd
              v-if="cmd.shortcut"
              class="command-shortcut"
            >{{ cmd.shortcut }}</kbd>
          </div>
        </div>
        <div
          v-if="pluginRuntimeStore.failedPlugins.length > 0"
          class="runtime-failed"
          data-testid="plugin-runtime-failed"
        >
          <Puzzle :size="14" />
          <span>{{ pluginRuntimeStore.failedPlugins.map(plugin => plugin.name).join('、') }}</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.command-palette-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 15vh;
  z-index: 9999;
}

.command-palette {
  width: 560px;
  max-width: 90vw;
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
  overflow: hidden;
}

.palette-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}

.palette-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.palette-input {
  flex: 1;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-base);
  outline: none;
}

.palette-input::placeholder {
  color: var(--text-muted);
}

.palette-shortcut {
  font-size: var(--text-xs);
  color: var(--text-muted);
  background: var(--bg-card);
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--border);
}

.runtime-failed {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  border-top: 1px solid var(--border);
  color: #ef4444;
  font-size: var(--text-xs);
}

.palette-commands {
  max-height: 400px;
  overflow-y: auto;
  padding: var(--space-2);
}

.no-results {
  padding: var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--text-sm);
}

.command-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--transition-fast);
}

.command-item.selected {
  background: var(--accent-alpha);
}

.command-item:hover {
  background: var(--bg-hover);
}

.command-item.selected:hover {
  background: var(--accent-alpha);
}

.command-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-secondary);
  flex-shrink: 0;
}

.command-item.selected .command-icon {
  background: var(--accent);
  color: white;
}

.command-info {
  flex: 1;
  min-width: 0;
}

.command-label {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 2px;
}

.command-desc {
  font-size: var(--text-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.command-shortcut {
  font-size: var(--text-xs);
  color: var(--text-muted);
  background: var(--bg-card);
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--border);
  flex-shrink: 0;
}
</style>
