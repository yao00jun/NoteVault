<script setup lang="ts">
/**
 * EditorTabBar - 编辑器顶部标签页栏 + 右侧工具栏
 * 纯展示组件：标签页与工具栏交互通过 emit 交还父组件处理。
 */
import { useI18n } from 'vue-i18n'
import {
  FileText,
  Plus,
  X,
  Save,
  Columns,
  Edit3,
  Eye,
  Sparkles,
  FileDown,
  FileCode,
} from 'lucide-vue-next'

const { t } = useI18n()

interface Tab {
  path: string
  name: string
  content: string
  isDirty: boolean
  lastSavedAt: string
}

defineProps<{
  tabs: Tab[]
  activeTabIndex: number
  isSaving: boolean
  activeTab: Tab | null
  viewMode: 'split' | 'editor' | 'preview'
  isExporting: boolean
}>()

const emit = defineEmits<{
  (e: 'switch-tab', index: number): void
  (e: 'close-tab', index: number, event: Event): void
  (e: 'new-file'): void
  (e: 'summarize'): void
  (e: 'export-md'): void
  (e: 'export-html'): void
  (e: 'save'): void
  (e: 'toggle-view'): void
}>()
</script>

<template>
  <div class="tab-bar">
    <div class="tabs-container">
      <div
        v-for="(tab, index) in tabs"
        :key="tab.path"
        class="tab-item"
        :data-testid="`tab-${tab.name}`"
        :class="{ active: index === activeTabIndex }"
        @click="emit('switch-tab', index)"
      >
        <FileText :size="13" />
        <span class="tab-name">{{ tab.name }}</span>
        <span
          v-if="tab.isDirty"
          class="tab-dirty"
        >●</span>
        <button
          class="tab-close"
          @click="emit('close-tab', index, $event)"
        >
          <X :size="12" />
        </button>
      </div>
    </div>

    <button
      class="tab-new"
      :title="t('editor.newDoc')"
      @click="emit('new-file')"
    >
      <Plus :size="14" />
    </button>

    <!-- 右侧工具栏 -->
    <div class="tab-tools">
      <span
        v-if="isSaving"
        class="save-status"
        data-testid="save-status"
      >{{ t('editor.saving') }}</span>
      <span
        v-else-if="activeTab?.lastSavedAt"
        class="save-status"
        data-testid="save-status"
      >{{ t('editor.savedAt', { time: activeTab.lastSavedAt }) }}</span>
      <button
        class="tool-btn"
        :title="t('editor.aiSummary')"
        @click="emit('summarize')"
      >
        <Sparkles :size="14" />
      </button>
      <button
        class="tool-btn"
        :title="t('editor.exportMd')"
        :disabled="isExporting"
        @click="emit('export-md')"
      >
        <FileDown :size="14" />
      </button>
      <button
        class="tool-btn"
        :title="t('editor.exportHtml')"
        :disabled="isExporting"
        @click="emit('export-html')"
      >
        <FileCode :size="14" />
      </button>
      <button
        class="tool-btn"
        :title="t('editor.save')"
        @click="emit('save')"
        data-testid="save-button"
      >
        <Save :size="14" />
      </button>
      <button
        class="tool-btn"
        :title="t('editor.viewMode', { mode: viewMode })"
        @click="emit('toggle-view')"
      >
        <Columns
          v-if="viewMode === 'split'"
          :size="14"
        />
        <Edit3
          v-else-if="viewMode === 'editor'"
          :size="14"
        />
        <Eye
          v-else
          :size="14"
        />
      </button>
    </div>
  </div>
</template>

<style scoped>
.tab-bar {
  display: flex;
  align-items: center;
  height: 38px;
  background: var(--bg-sidebar);
  border-bottom: 1px solid var(--border);
  padding: 0 var(--space-2);
  gap: 2px;
  flex-shrink: 0;
}

.tabs-container {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: 1;
  overflow-x: auto;
  overflow-y: hidden;
}

.tabs-container::-webkit-scrollbar {
  height: 3px;
}

.tabs-container::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 2px;
}

.tab-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 30px;
  padding: 0 var(--space-2) 0 var(--space-3);
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  color: var(--text-secondary);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
  max-width: 200px;
  flex-shrink: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-bottom: none;
}

.tab-item:hover {
  background: var(--bg-hover);
}

.tab-item.active {
  background: var(--bg-content);
  color: var(--text-primary);
}

.tab-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tab-dirty {
  color: var(--accent);
  font-size: 10px;
  flex-shrink: 0;
}

.tab-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 3px;
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
  flex-shrink: 0;
}

.tab-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tab-new {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
  flex-shrink: 0;
}

.tab-new:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tab-tools {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  margin-left: var(--space-2);
}

.save-status {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.tool-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.tool-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
</style>
