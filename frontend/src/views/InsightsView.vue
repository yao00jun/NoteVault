<script setup lang="ts">
/**
 * InsightsView - 「洞察提炼」容器（Codex 式架构）
 * 知识资产的宏观透视与加工工厂：全库图谱 / 知识编译流水线 / Bases 数据透视。
 * 现有视图原样作为子组件复用，tab 状态走路由 query 支持深链。
 */
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { GitGraph, Sparkles, Table2, ArrowLeft } from '@lucide/vue'
import GraphView from '@/views/GraphView.vue'
import CompileView from '@/views/CompileView.vue'
import BasesView from '@/views/BasesView.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const TABS = [
  { id: 'graph', icon: GitGraph },
  { id: 'compile', icon: Sparkles },
  { id: 'bases', icon: Table2 },
] as const

type TabId = (typeof TABS)[number]['id']

const tab = computed<TabId>(() => {
  const v = route.query.tab
  return TABS.some((x) => x.id === v) ? (v as TabId) : 'graph'
})

function switchTab(id: TabId) {
  router.replace({ query: { ...route.query, tab: id } })
}
</script>

<template>
  <div class="insights-view">
    <header class="insights-header">
      <button
        class="back-btn"
        data-testid="insights-back"
        @click="router.push('/knowledge')"
      >
        <ArrowLeft :size="16" />
        <span>{{ t('common.backToKnowledge') }}</span>
      </button>
      <div class="insights-title">{{ t('insights.title') }}</div>
      <nav class="insights-tabs">
        <button
          v-for="x in TABS"
          :key="x.id"
          class="insights-tab"
          :class="{ active: tab === x.id }"
          :data-testid="`insights-tab-${x.id}`"
          @click="switchTab(x.id)"
        >
          <component
            :is="x.icon"
            :size="14"
          />
          <span>{{ t(`insights.tab.${x.id}`) }}</span>
        </button>
      </nav>
      <div class="insights-spacer" />
    </header>
    <div class="insights-body">
      <GraphView v-if="tab === 'graph'" />
      <CompileView v-else-if="tab === 'compile'" />
      <BasesView v-else />
    </div>
  </div>
</template>

<style scoped>
.insights-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.insights-header {
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
.insights-title {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-primary);
  margin-right: var(--space-2);
}
.insights-tabs {
  display: flex;
  gap: var(--space-1);
}
.insights-spacer {
  flex: 1;
}
.insights-tab {
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
.insights-tab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.insights-tab.active {
  background: var(--bg-active);
  color: var(--text-primary);
  font-weight: 500;
}
/* 子视图（图谱/编译/Bases）均为 flex:1 + 自滚动设计，容器须定高 */
.insights-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: var(--space-3) var(--space-4) var(--space-4);
}
</style>
