import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    name: 'welcome',
    component: () => import('@/views/WelcomeView.vue'),
  },
  {
    path: '/knowledge',
    name: 'knowledge',
    component: () => import('@/views/KnowledgeView.vue'),
  },
  {
    path: '/editor',
    name: 'editor',
    component: () => import('@/views/EditorView.vue'),
  },
  // 旧路由重定向到「发现」容器（Codex 式架构收敛，2026-09）
  { path: '/search', redirect: (to) => ({ path: '/discover', query: { tab: 'search', ...to.query } }) },
  { path: '/qna', redirect: (to) => ({ path: '/discover', query: { tab: 'qna', ...to.query } }) },
  { path: '/tags', redirect: (to) => ({ path: '/discover', query: { tab: 'views', view: 'tags' } }) },
  { path: '/graph', redirect: (to) => ({ path: '/insights', query: { tab: 'graph', ...to.query } }) },
  { path: '/bases', redirect: (to) => ({ path: '/insights', query: { tab: 'bases', ...to.query } }) },
  // 旧路由重定向到「回顾」容器
  { path: '/reports', redirect: (to) => ({ path: '/review', query: { tab: 'reports', ...to.query } }) },
  { path: '/todos', redirect: (to) => ({ path: '/review', query: { tab: 'tasks', sub: 'todos' } }) },
  { path: '/reminders', redirect: (to) => ({ path: '/review', query: { tab: 'tasks', sub: 'reminders' } }) },
  { path: '/history', redirect: (to) => ({ path: '/review', query: { tab: 'versions', ...to.query } }) },
  {
    path: '/bases',
    name: 'bases',
    component: () => import('@/views/BasesView.vue'),
  },
  {
    path: '/canvas',
    name: 'canvas',
    component: () => import('@/views/CanvasView.vue'),
  },
  {
    path: '/qna',
    name: 'qna',
    component: () => import('@/views/QnAView.vue'),
  },
  {
    path: '/import',
    name: 'import',
    component: () => import('@/views/ImportView.vue'),
  },
  {
    path: '/plugins',
    name: 'plugins',
    component: () => import('@/views/PluginView.vue'),
  },
  {
    path: '/todos',
    name: 'todos',
    component: () => import('@/views/TodosView.vue'),
  },
  {
    path: '/reminders',
    name: 'reminders',
    component: () => import('@/views/RemindersView.vue'),
  },
  {
    path: '/archive',
    name: 'archive',
    component: () => import('@/views/ArchiveView.vue'),
  },
  {
    path: '/history',
    name: 'history',
    component: () => import('@/views/HistoryView.vue'),
  },
  {
    path: '/trash',
    name: 'trash',
    component: () => import('@/views/TrashView.vue'),
  },
  { path: '/compile', redirect: (to) => ({ path: '/insights', query: { tab: 'compile', ...to.query } }) },
  {
    // 「发现」容器：搜索 / 语义问答 / 视图（标签·图谱·Bases）
    path: '/discover',
    name: 'discover',
    component: () => import('@/views/DiscoverView.vue'),
  },
  {
    // 「回顾」容器：报表 / 任务（待办·提醒）/ 版本
    path: '/review',
    name: 'review',
    component: () => import('@/views/ReviewView.vue'),
  },
  {
    // 「洞察提炼」容器：图谱 / 编译 / Bases
    path: '/insights',
    name: 'insights',
    component: () => import('@/views/InsightsView.vue'),
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('@/views/SettingsView.vue'),
  },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
