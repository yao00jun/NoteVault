<script setup lang="ts">
/**
 * EditorBacklinks - 编辑器底部反向链接面板
 * 纯展示组件：点击某个反向链接通过 emit 交还父组件打开。
 */
import { useI18n } from 'vue-i18n'
import { FileText } from '@lucide/vue'

const { t } = useI18n()

interface Backlink {
  path: string
  name: string
}

defineProps<{ backlinks: Backlink[] }>()

const emit = defineEmits<{
  (e: 'open', link: Backlink): void
}>()
</script>

<template>
  <div
    v-if="backlinks.length > 0"
    class="backlinks-panel"
  >
    <div class="backlinks-header">
      <span>🔗 {{ t('editor.backlinks', { count: backlinks.length }) }}</span>
    </div>
    <div class="backlinks-list">
      <div
        v-for="link in backlinks"
        :key="link.path"
        class="backlink-item"
        @click="emit('open', link)"
      >
        <FileText :size="14" />
        <span>{{ link.name }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backlinks-panel {
  border-top: 1px solid var(--border);
  background: var(--bg-sidebar);
  max-height: 150px;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.backlinks-header {
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  border-bottom: 1px solid var(--border);
}

.backlinks-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  overflow-y: auto;
}

.backlink-item {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.backlink-item:hover {
  background: var(--accent-alpha);
  border-color: var(--accent);
  color: var(--accent);
}
</style>
