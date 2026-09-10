<script setup lang="ts">
/**
 * EditorBacklinks - 编辑器底部反向链接面板
 * 纯展示组件：点击某个反向链接通过 emit 交还父组件打开。
 */
import { ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight, FileText, Link2 } from '@lucide/vue'

const { t } = useI18n()
const listId = useId()
const collapsed = ref(false)
const storageKey = 'notevault:editor-backlinks-collapsed'
try { collapsed.value = localStorage.getItem(storageKey) === 'true' } catch { /* Use the expanded default when storage is unavailable. */ }

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  try { localStorage.setItem(storageKey, String(collapsed.value)) } catch { /* Keep the control usable without persistence. */ }
}

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
    <button
      type="button"
      class="backlinks-header"
      :aria-expanded="!collapsed"
      :aria-controls="listId"
      @click="toggleCollapsed"
    >
      <ChevronRight
        :size="13"
        :class="{ expanded: !collapsed }"
      />
      <Link2 :size="13" />
      <span>{{ t('editor.backlinks', { count: backlinks.length }) }}</span>
    </button>
    <ul
      v-show="!collapsed"
      :id="listId"
      class="backlinks-list"
    >
      <li
        v-for="link in backlinks"
        :key="link.path"
      >
        <button
          type="button"
          class="backlink-item"
          :title="link.path"
          @click="emit('open', link)"
        >
          <FileText :size="14" />
          <span class="backlink-name">{{ link.name }}</span>
          <span class="backlink-path">{{ link.path }}</span>
        </button>
      </li>
    </ul>
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
  display: flex;
  align-items: center;
  gap: var(--space-1);
  width: 100%;
  background: transparent;
  text-align: left;
  cursor: pointer;
  flex-shrink: 0;
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  border-bottom: 1px solid var(--border);
}

.backlinks-header:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.backlinks-header .expanded {
  transform: rotate(90deg);
}

.backlinks-list {
  min-height: 0;
  margin: 0;
  padding: 0 var(--space-3);
  list-style: none;
  overflow-y: auto;
}

.backlink-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  min-width: 0;
  padding: 3px 8px;
  border: none;
  border-bottom: 1px solid var(--border-light);
  border-radius: 0;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  font-size: var(--text-xs);
  cursor: pointer;
  transition: background var(--transition-fast);
}

.backlinks-list > li:last-child .backlink-item {
  border-bottom: none;
}

.backlink-item > svg {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.backlink-name,
.backlink-path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.backlink-name {
  flex: 1;
}

.backlink-path {
  flex: 0 1 50%;
  color: var(--text-secondary);
  text-align: right;
}

.backlink-item:hover {
  background: var(--accent-alpha, var(--bg-active));
}

.backlink-item:focus-visible,
.backlinks-header:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
</style>
