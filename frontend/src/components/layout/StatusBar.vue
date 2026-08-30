<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'

const { t } = useI18n()
const settingsStore = useSettingsStore()
const workspaceStore = useWorkspaceStore()

const themeLabel = computed(() => {
  const map: Record<string, string> = {
    macos: 'macOS',
    winui: 'WinUI',
    'islands-dark': 'Islands Dark',
  }
  return map[settingsStore.settings.theme] || settingsStore.settings.theme
})

const currentFile = computed(() => {
  if (!workspaceStore.activeFile) return t('statusbar.noFileOpen')
  const parts = workspaceStore.activeFile.split('/')
  return parts[parts.length - 1]
})
</script>

<template>
  <footer class="statusbar">
    <div class="status-left">
      <span class="status-item">
        <span class="status-dot saved" />
        {{ t('statusbar.saved') }}
      </span>
      <span class="status-separator">·</span>
      <span class="status-item">{{ currentFile }}</span>
    </div>

    <div class="status-right">
      <span class="status-item">UTF-8</span>
      <span class="status-separator">·</span>
      <span class="status-item">Markdown</span>
      <span class="status-separator">·</span>
      <span class="status-item">{{ t('statusbar.lineCol', { line: 1, col: 1 }) }}</span>
      <span class="status-separator">·</span>
      <span class="status-item theme-indicator">{{ themeLabel }}</span>
    </div>
  </footer>
</template>

<style scoped>
.statusbar {
  height: var(--statusbar-height);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-3);
  background: var(--bg-sidebar);
  border-top: 1px solid var(--border);
  font-size: var(--text-xs);
  color: var(--text-secondary);
  flex-shrink: 0;
}

.status-left,
.status-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.status-item {
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.status-dot.saved {
  background: var(--success);
}

.status-separator {
  color: var(--text-muted);
  opacity: 0.5;
}

.theme-indicator {
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--bg-active);
  color: var(--accent);
  font-weight: 500;
}
</style>
