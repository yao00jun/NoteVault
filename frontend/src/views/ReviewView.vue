<script setup lang="ts">
/**
 * ReviewView - 「回顾」容器（Codex 式架构：同族功能 tab 化聚合）
 * 报表(热力图/周报/那年今日) / 任务(待办+提醒) / 版本(历史)——都是「回看过去」的同一族能力。
 * 现有视图原样作为子组件复用，零逻辑改动；tab 状态走路由 query 支持深链。
 */
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BarChart3, CheckSquare, History, ArrowLeft } from '@lucide/vue'
import ReportsView from '@/views/ReportsView.vue'
import TodosView from '@/views/TodosView.vue'
import RemindersView from '@/views/RemindersView.vue'
import HistoryView from '@/views/HistoryView.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const TABS = [
  { id: 'reports', icon: BarChart3 },
  { id: 'tasks', icon: CheckSquare },
  { id: 'versions', icon: History },
] as const

type TabId = (typeof TABS)[number]['id']

const tab = computed<TabId>(() => {
  const v = route.query.tab
  return TABS.some((x) => x.id === v) ? (v as TabId) : 'reports'
})

const TASK_TABS = ['todos', 'reminders'] as const
type TaskTabId = (typeof TASK_TABS)[number]

const taskTab = computed<TaskTabId>(() => {
  const v = route.query.sub
  return TASK_TABS.some((x) => x === v) ? (v as TaskTabId) : 'todos'
})

function setQuery(patch: Record<string, string>) {
  router.replace({ query: { ...route.query, ...patch } })
}
</script>

<template>
  <div class="review-view">
    <header class="review-header">
      <button
        class="back-btn"
        data-testid="review-back"
        @click="router.push('/knowledge')"
      >
        <ArrowLeft :size="16" />
        <span>{{ t('common.backToKnowledge') }}</span>
      </button>
      <div class="review-title">
        {{ t('review.title') }}
      </div>
      <nav class="review-tabs">
        <button
          v-for="x in TABS"
          :key="x.id"
          class="review-tab"
          :class="{ active: tab === x.id }"
          :data-testid="`review-tab-${x.id}`"
          @click="setQuery({ tab: x.id })"
        >
          <component
            :is="x.icon"
            :size="14"
          />
          <span>{{ t(`review.tab.${x.id}`) }}</span>
        </button>
      </nav>
      <nav
        v-if="tab === 'tasks'"
        class="review-subtabs"
      >
        <button
          v-for="v in TASK_TABS"
          :key="v"
          class="review-subtab"
          :class="{ active: taskTab === v }"
          :data-testid="`review-tasktab-${v}`"
          @click="setQuery({ sub: v })"
        >
          {{ t(`review.tasktab.${v}`) }}
        </button>
      </nav>
      <div
        v-else
        class="review-spacer"
      />
    </header>
    <div class="review-body">
      <ReportsView v-if="tab === 'reports'" />
      <TodosView v-else-if="tab === 'tasks' && taskTab === 'todos'" />
      <RemindersView v-else-if="tab === 'tasks' && taskTab === 'reminders'" />
      <HistoryView v-else-if="tab === 'versions'" />
    </div>
  </div>
</template>

<style scoped>
.review-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.review-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4) 0;
  flex-shrink: 0;
}
.back-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: var(--text-sm);
  cursor: pointer;
  padding: 5px 10px;
  border-radius: var(--radius-sm);
  transition: background var(--transition-fast), color var(--transition-fast);
}
.back-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.review-title {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-primary);
  margin-right: var(--space-2);
}
.review-tabs,
.review-subtabs {
  display: flex;
  gap: var(--space-1);
}
.review-spacer {
  flex: 1;
}
.review-tab,
.review-subtab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: var(--radius-sm);
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.review-subtab {
  padding: 4px 10px;
  font-size: var(--text-xs);
}
.review-tab:hover,
.review-subtab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.review-tab.active,
.review-subtab.active {
  background: var(--bg-active);
  color: var(--text-primary);
  font-weight: 500;
}
.review-body {
  flex: 1;
  /* 子视图（图谱/搜索/Bases…）都是 flex:1 + 自滚动设计：
     容器必须是定高 flex，否则子视图高度塌陷（图谱曾因此渲染空白） */
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: var(--space-3) var(--space-4) var(--space-4);
}
</style>
