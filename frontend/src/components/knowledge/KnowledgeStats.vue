<script setup lang="ts">
/**
 * KnowledgeStats - 知识库主页统计卡片
 * 纯展示组件：接收父组件计算好的 stats 对象，渲染 6 张统计卡片。
 */
import { useI18n } from 'vue-i18n'
import {
  FileText,
  Star,
  Tags as TagsIcon,
  CheckSquare,
  Sparkles,
} from 'lucide-vue-next'

const { t } = useI18n()

interface StatsData {
  notes: number
  starred: number
  tags: number
  todos: number
  high: number
  done: number
}

defineProps<{ stats: StatsData }>()
</script>

<template>
  <section class="kv-stats">
    <div class="kv-stat-card">
      <div class="kv-stat-icon notes">
        <FileText :size="18" />
      </div>
      <div class="kv-stat-body">
        <div class="kv-stat-value">
          {{ stats.notes }}
        </div>
        <div class="kv-stat-label">
          {{ t('knowledge.stats.docs') }}
        </div>
      </div>
    </div>
    <div class="kv-stat-card">
      <div class="kv-stat-icon starred">
        <Star :size="18" />
      </div>
      <div class="kv-stat-body">
        <div class="kv-stat-value">
          {{ stats.starred }}
        </div>
        <div class="kv-stat-label">
          {{ t('knowledge.stats.starred') }}
        </div>
      </div>
    </div>
    <div class="kv-stat-card">
      <div class="kv-stat-icon tags">
        <TagsIcon :size="18" />
      </div>
      <div class="kv-stat-body">
        <div class="kv-stat-value">
          {{ stats.tags }}
        </div>
        <div class="kv-stat-label">
          {{ t('knowledge.stats.tags') }}
        </div>
      </div>
    </div>
    <div class="kv-stat-card">
      <div class="kv-stat-icon todos">
        <CheckSquare :size="18" />
      </div>
      <div class="kv-stat-body">
        <div class="kv-stat-value">
          {{ stats.todos }}
        </div>
        <div class="kv-stat-label">
          {{ t('knowledge.stats.pendingTodos') }}
        </div>
      </div>
    </div>
    <div
      v-if="stats.high > 0"
      class="kv-stat-card"
    >
      <div class="kv-stat-icon danger">
        <Sparkles :size="18" />
      </div>
      <div class="kv-stat-body">
        <div class="kv-stat-value">
          {{ stats.high }}
        </div>
        <div class="kv-stat-label">
          {{ t('knowledge.stats.highPriority') }}
        </div>
      </div>
    </div>
    <div
      v-if="stats.done > 0"
      class="kv-stat-card"
    >
      <div class="kv-stat-icon warning">
        <CheckSquare :size="18" />
      </div>
      <div class="kv-stat-body">
        <div class="kv-stat-value">
          {{ stats.done }}
        </div>
        <div class="kv-stat-label">
          {{ t('knowledge.stats.completed') }}
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.kv-stats {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: var(--space-3);
  padding: var(--space-4) var(--space-8);
  background: var(--bg-window);
  border-bottom: 1px solid var(--border);
}

.kv-stat-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.kv-stat-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: var(--radius-sm);
  flex-shrink: 0;
}

.kv-stat-icon.notes {
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
}
.kv-stat-icon.starred {
  background: rgba(234, 179, 8, 0.12);
  color: #eab308;
}
.kv-stat-icon.tags {
  background: rgba(34, 197, 94, 0.12);
  color: #22c55e;
}
.kv-stat-icon.todos {
  background: rgba(168, 85, 247, 0.12);
  color: #a855f7;
}
.kv-stat-icon.danger {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}
.kv-stat-icon.warning {
  background: rgba(245, 158, 11, 0.12);
  color: #f59e0b;
}

.kv-stat-value {
  font-size: var(--text-xl);
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1;
}

.kv-stat-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-top: 2px;
}

@media (max-width: 980px) {
  .kv-stats {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 640px) {
  .kv-stats {
    padding-left: var(--space-3);
    padding-right: var(--space-3);
  }
}
</style>
