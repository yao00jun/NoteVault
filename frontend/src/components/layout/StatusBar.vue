<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { usePageContext } from '@/composables/usePageContext'
import { editorSession } from '@/composables/useEditorSession'
import { useRoute, useRouter } from 'vue-router'
import { openDocument } from '@/utils/navigation'
import { useSourceImport, requestSourceImport } from '@/composables/useSourceImport'

const { t } = useI18n()
const settingsStore = useSettingsStore()
const workspaceStore = useWorkspaceStore()
const workbench = useWorkbenchStore()
const { context } = usePageContext()
const route = useRoute()
const router = useRouter()
const sourceImport = useSourceImport()
const sessionMatches = computed(() => editorSession.workspacePath === workspaceStore.currentWorkspace?.path)
const editing = computed(() => route.path === '/editor' && sessionMatches.value && context.value.file === editorSession.path)
const saveLabel = computed(() => ({ idle: '未打开文档', saved: '已保存', dirty: '等待保存', saving: '正在保存', error: '保存失败', conflict: '外部修改冲突' })[editorSession.state])

const themeLabel = computed(() => {
  const map: Record<string, string> = {
    macos: 'macOS',
    winui: 'WinUI',
    'islands-dark': 'Islands Dark',
  }
  return map[settingsStore.settings.theme] || settingsStore.settings.theme
})

const currentFile = computed(() => {
  return context.value.title
})
</script>

<template>
  <footer class="statusbar">
    <div class="status-left">
      <span
        v-if="editing"
        class="status-item"
        :title="editorSession.error"
        aria-live="polite"
        data-testid="editor-save-status"
      >
        <span
          class="status-dot"
          :class="editorSession.state"
        />{{ saveLabel }}
      </span>
      <button
        v-else-if="sessionMatches && editorSession.dirtyCount"
        class="status-draft"
        @click="openDocument(router, workspaceStore, editorSession.draftPath)"
      >
        未保存草稿 {{ editorSession.dirtyCount }}
      </button>
      <span
        v-else
        class="status-item"
      >{{ workspaceStore.hasWorkspace ? `${workbench.documents.length} 篇文档` : '本地优先 · Markdown' }}</span>
      <span class="status-separator">·</span>
      <span class="status-item">{{ currentFile }}</span>
    </div>

    <div class="status-right">
      <button
        v-if="sourceImport.hasTask"
        class="status-import"
        data-testid="source-import-status"
        @click="requestSourceImport()"
      >
        {{ sourceImport.busy ? '资料导入中…' : sourceImport.current?.phase === 'succeeded' ? '导入完成' : '查看导入任务' }}
      </button>
      <template v-if="editing">
        <span class="status-item">Markdown · UTF-8</span>
        <span class="status-separator">·</span>
        <span
          class="status-item"
          data-testid="editor-cursor-status"
        >{{ t('statusbar.lineCol', { line: editorSession.line, col: editorSession.column }) }}</span>
        <span class="status-separator">·</span>
      </template>
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
.status-dot.saving { background: var(--accent); }
.status-dot.dirty, .status-dot.conflict { background: var(--warning, #c28a25); }
.status-dot.error { background: var(--error, #c74b4b); }
.status-draft { font: inherit; color: var(--warning, #c28a25); border: 0; background: none; cursor: pointer; }
.status-import { font: inherit; color: var(--accent); border: 0; background: none; cursor: pointer; }
.status-left { min-width: 0; }
.status-left > .status-item:last-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }

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
