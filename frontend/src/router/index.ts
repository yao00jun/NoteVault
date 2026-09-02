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
  {
    path: '/search',
    name: 'search',
    component: () => import('@/views/SearchView.vue'),
  },
  {
    path: '/tags',
    name: 'tags',
    component: () => import('@/views/TagsView.vue'),
  },
  {
    path: '/graph',
    name: 'graph',
    component: () => import('@/views/GraphView.vue'),
  },
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
  {
    path: '/compile',
    name: 'compile',
    component: () => import('@/views/CompileView.vue'),
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
