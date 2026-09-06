<script setup lang="ts">
/**
 * EditorContextDrawer - 编辑器右侧可开合辅助抽屉（Codex 式侧翼）
 * Phase 1：大纲 (Outline) + 反向链接 (Backlinks)；后续阶段扩展局部图谱/AI 总结。
 * 纯展示组件：大纲跳行与打开反链通过 emit 交还父组件。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ListTree, Link2, X, FileText } from '@lucide/vue'

const { t } = useI18n()

export interface OutlineItem {
  level: number
  text: string
  line: number
}
export interface DrawerBacklink {
  path: string
  name: string
}

const props = defineProps<{
  open: boolean
  outline: OutlineItem[]
  backlinks: DrawerBacklink[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'jump-line', line: number): void
  (e: 'open-path', path: string): void
}>()

type DrawerTab = 'outline' | 'backlinks'
const activeTab = defineModel<DrawerTab>('tab', { default: 'outline' })

const TABS = [
  { id: 'outline', icon: ListTree },
  { id: 'backlinks', icon: Link2 },
] as const

const backlinkCount = computed(() => props.backlinks.length)
</script>

<template>
  <aside
    v-if="open"
    class="ctx-drawer"
    data-testid="ctx-drawer"
  >
    <div class="ctx-header">
      <nav class="ctx-tabs">
        <button
          v-for="x in TABS"
          :key="x.id"
          class="ctx-tab"
          :class="{ active: activeTab === x.id }"
          :data-testid="`ctx-tab-${x.id}`"
          @click="activeTab = x.id"
        >
          <component
            :is="x.icon"
            :size="13"
          />
          <span>{{ t(`editor.drawer.${x.id}`) }}</span>
          <span
            v-if="x.id === 'backlinks' && backlinkCount > 0"
            class="ctx-badge"
          >{{ backlinkCount }}</span>
        </button>
      </nav>
      <button
        class="ctx-close"
        :title="t('common.cancel')"
        data-testid="ctx-drawer-close"
        @click="emit('close')"
      >
        <X :size="14" />
      </button>
    </div>

    <div class="ctx-body">
      <!-- 大纲 -->
      <div
        v-if="activeTab === 'outline'"
        class="ctx-pane"
      >
        <div
          v-if="outline.length === 0"
          class="ctx-empty"
        >
          {{ t('editor.drawer.outlineEmpty') }}
        </div>
        <button
          v-for="(item, i) in outline"
          :key="`${item.line}-${i}`"
          class="ctx-outline-item"
          :style="{ paddingLeft: `${8 + (item.level - 1) * 14}px` }"
          :title="item.text"
          @click="emit('jump-line', item.line)"
        >
          <span class="ctx-outline-level">H{{ item.level }}</span>
          <span class="ctx-outline-text">{{ item.text }}</span>
        </button>
      </div>

      <!-- 反向链接 -->
      <div
        v-else
        class="ctx-pane"
      >
        <div
          v-if="backlinks.length === 0"
          class="ctx-empty"
        >
          {{ t('editor.drawer.backlinksEmpty') }}
        </div>
        <button
          v-for="link in backlinks"
          :key="link.path"
          class="ctx-backlink-item"
          :title="link.path"
          @click="emit('open-path', link.path)"
        >
          <FileText :size="13" />
          <span>{{ link.name }}</span>
        </button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.ctx-drawer {
  width: 260px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-sidebar);
  border-left: 1px solid var(--border);
  overflow: hidden;
}
.ctx-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.ctx-tabs {
  display: flex;
  gap: 2px;
}
.ctx-tab {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.ctx-tab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ctx-tab.active {
  background: var(--bg-active);
  color: var(--text-primary);
}
.ctx-badge {
  font-size: 10px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--bg-hover);
}
.ctx-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.ctx-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ctx-body {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-2);
}
.ctx-pane {
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.ctx-empty {
  color: var(--text-muted);
  font-size: var(--text-xs);
  padding: var(--space-4) var(--space-2);
  text-align: center;
}
.ctx-outline-item {
  display: flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.ctx-outline-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ctx-outline-level {
  font-size: 10px;
  color: var(--text-muted);
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}
.ctx-outline-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ctx-backlink-item {
  display: flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.ctx-backlink-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ctx-backlink-item span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
