<script setup lang="ts">
/**
 * DiscoverView - 「发现」容器（Codex 式架构：同族功能 tab 化聚合）
 * 搜索 / 语义问答(QnA) / 视图（标签·图谱·Bases）——都是「从库里找答案」的同一族能力。
 * 现有视图原样作为子组件复用，零逻辑改动；tab 状态走路由 query 支持深链。
 */
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Search, MessageCircle, LayoutGrid } from '@lucide/vue'
import SearchView from '@/views/SearchView.vue'
import QnAView from '@/views/QnAView.vue'
import TagsView from '@/views/TagsView.vue'
import GraphView from '@/views/GraphView.vue'
import BasesView from '@/views/BasesView.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const TABS = [
  { id: 'search', icon: Search },
  { id: 'qna', icon: MessageCircle },
  { id: 'views', icon: LayoutGrid },
] as const

type TabId = (typeof TABS)[number]['id']

const tab = computed<TabId>(() => {
  const v = route.query.tab
  return TABS.some((x) => x.id === v) ? (v as TabId) : 'search'
})

const VIEW_TABS = ['tags', 'graph', 'bases'] as const
type ViewTabId = (typeof VIEW_TABS)[number]

const viewTab = computed<ViewTabId>(() => {
  const v = route.query.view
  return VIEW_TABS.some((x) => x === v) ? (v as ViewTabId) : 'tags'
})

function setQuery(patch: Record<string, string>) {
  router.replace({ query: { ...route.query, ...patch } })
}

function switchTab(id: TabId) {
  setQuery({ tab: id })
}
</script>

<template>
  <div class="discover-view">
    <header class="discover-header">
      <div class="discover-title">
        {{ t('discover.title') }}
      </div>
      <nav class="discover-tabs">
        <button
          v-for="x in TABS"
          :key="x.id"
          class="discover-tab"
          :class="{ active: tab === x.id }"
          :data-testid="`discover-tab-${x.id}`"
          @click="switchTab(x.id)"
        >
          <component
            :is="x.icon"
            :size="14"
          />
          <span>{{ t(`discover.tab.${x.id}`) }}</span>
        </button>
      </nav>
      <nav
        v-if="tab === 'views'"
        class="discover-subtabs"
      >
        <button
          v-for="v in VIEW_TABS"
          :key="v"
          class="discover-subtab"
          :class="{ active: viewTab === v }"
          :data-testid="`discover-viewtab-${v}`"
          @click="setQuery({ view: v })"
        >
          {{ t(`discover.viewtab.${v}`) }}
        </button>
      </nav>
      <div
        v-else
        class="discover-spacer"
      />
    </header>
    <div class="discover-body">
      <SearchView v-if="tab === 'search'" />
      <QnAView v-else-if="tab === 'qna'" />
      <TagsView v-else-if="tab === 'views' && viewTab === 'tags'" />
      <GraphView v-else-if="tab === 'views' && viewTab === 'graph'" />
      <BasesView v-else-if="tab === 'views' && viewTab === 'bases'" />
    </div>
  </div>
</template>

<style scoped>
.discover-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.discover-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4) 0;
  flex-shrink: 0;
}
.discover-title {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-primary);
  margin-right: var(--space-2);
}
.discover-tabs,
.discover-subtabs {
  display: flex;
  gap: var(--space-1);
}
.discover-spacer {
  flex: 1;
}
.discover-tab,
.discover-subtab {
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
.discover-subtab {
  padding: 4px 10px;
  font-size: var(--text-xs);
}
.discover-tab:hover,
.discover-subtab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.discover-tab.active,
.discover-subtab.active {
  background: var(--bg-active);
  color: var(--text-primary);
  font-weight: 500;
}
.discover-body {
  flex: 1;
  /* 子视图（图谱/搜索/Bases…）都是 flex:1 + 自滚动设计：
     容器必须是定高 flex，否则子视图高度塌陷（图谱曾因此渲染空白） */
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: var(--space-3) var(--space-4) var(--space-4);
}
</style>
