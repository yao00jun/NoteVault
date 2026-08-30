<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { CheckSquare, Square, Plus, Trash2, Flag, FileText, AlertCircle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { TodoService, WorkspaceService } from '@bindings/github.com/notevault/notevault/index.js'

interface TodoItem {
  id: string
  filePath: string
  fileName: string
  content: string
  lineIndex: number
  completed: boolean
  priority: string
}

const router = useRouter()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()

const todos = ref<TodoItem[]>([])
const isLoading = ref(false)
const errorMsg = ref('')
const filter = ref<'all' | 'pending' | 'completed'>('all')
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

const filteredTodos = computed(() => {
  if (filter.value === 'pending') return todos.value.filter(t => !t.completed)
  if (filter.value === 'completed') return todos.value.filter(t => t.completed)
  return todos.value
})

const stats = computed(() => ({
  total: todos.value.length,
  completed: todos.value.filter(t => t.completed).length,
  pending: todos.value.filter(t => !t.completed).length,
}))

async function ensureWorkspace() {
  if (!currentWorkspace.value) {
    try {
      const ws = await WorkspaceService.GetCurrentWorkspace()
      if (ws) {
        workspaceStore.setCurrentWorkspace(ws as any)
      } else {
        router.push('/')
        return false
      }
    } catch (e) {
      console.error('Failed to get workspace:', e)
      router.push('/')
      return false
    }
  }
  return true
}

async function loadTodos() {
  errorMsg.value = ''
  if (!await ensureWorkspace()) return
  isLoading.value = true
  try {
    const data = await TodoService.GetAllTodos(currentWorkspace.value!.path)
    todos.value = data as TodoItem[]
  } catch (e) {
    console.error('Failed to load todos:', e)
    errorMsg.value = t('todos.loadFailed', { msg: (e as Error).message })
    todos.value = []
  } finally {
    isLoading.value = false
  }
}

async function toggleTodo(todo: TodoItem) {
  if (!currentWorkspace.value?.path) return
  try {
    await TodoService.ToggleTodo(currentWorkspace.value.path, todo.filePath, todo.lineIndex)
    todo.completed = !todo.completed
  } catch (e) {
    console.error('Failed to toggle todo:', e)
  }
}

function openTodoFile(todo: TodoItem) {
  workspaceStore.openFile(todo.filePath)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

function getPriorityColor(priority: string): string {
  switch (priority) {
    case 'high': return 'var(--danger, #ef4444)'
    case 'medium': return 'var(--warning, #f59e0b)'
    default: return 'var(--text-muted, #6b7280)'
  }
}

onMounted(loadTodos)
watch(() => currentWorkspace.value?.id, loadTodos)
watch(() => workspaceStore.fileTreeVersion, loadTodos)
</script>

<template>
  <div class="todos-view">
    <div class="todos-header">
      <div class="header-left">
        <h2 class="todos-title">
          <CheckSquare :size="20" /> {{ t('todos.title') }}
        </h2>
        <div class="todos-stats">
          <span class="stat">{{ t('todos.pending', { count: stats.pending }) }}</span>
          <span class="stat">{{ t('todos.completed', { count: stats.completed }) }}</span>
          <span class="stat">{{ t('todos.total', { count: stats.total }) }}</span>
        </div>
      </div>
      <div class="filter-tabs">
        <button
          :class="{ active: filter === 'all' }"
          @click="filter = 'all'"
        >
          {{ t('todos.filterAll') }}
        </button>
        <button
          :class="{ active: filter === 'pending' }"
          @click="filter = 'pending'"
        >
          {{ t('todos.filterPending') }}
        </button>
        <button
          :class="{ active: filter === 'completed' }"
          @click="filter = 'completed'"
        >
          {{ t('todos.filterCompleted') }}
        </button>
      </div>
    </div>

    <div class="todos-content">
      <!-- 错误提示 -->
      <div
        v-if="errorMsg"
        class="error-banner"
      >
        <AlertCircle :size="16" />
        <span>{{ errorMsg }}</span>
      </div>
      <div
        v-if="isLoading"
        class="todos-state"
      >
        <div class="spinner" />
        <p>{{ t('common.loading') }}</p>
      </div>
      <div
        v-else-if="filteredTodos.length === 0"
        class="todos-state"
      >
        <div class="empty-icon">
          ✅
        </div>
        <h3>{{ t('todos.emptyTitle') }}</h3>
        <p>{{ t('todos.emptyDesc') }}</p>
        <p class="hint">
          {{ t('todos.example') }}
        </p>
      </div>
      <div
        v-else
        class="todos-list"
      >
        <div
          v-for="todo in filteredTodos"
          :key="todo.id"
          class="todo-item"
          :class="{ completed: todo.completed }"
        >
          <button
            class="todo-checkbox"
            @click="toggleTodo(todo)"
          >
            <CheckSquare
              v-if="todo.completed"
              :size="20"
            />
            <Square
              v-else
              :size="20"
            />
          </button>
          <div
            class="todo-content"
            @click="openTodoFile(todo)"
          >
            <div class="todo-text">
              {{ todo.content }}
            </div>
            <div class="todo-meta">
              <FileText :size="12" />
              <span>{{ todo.fileName }}</span>
              <Flag
                v-if="todo.priority === 'high'"
                :size="12"
                :style="{ color: getPriorityColor(todo.priority) }"
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.todos-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.error-banner {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  margin-bottom: var(--space-4);
  background: rgba(239,68,68,0.1);
  border: 1px solid #ef4444;
  border-radius: var(--radius-sm);
  color: #ef4444;
  font-size: var(--text-sm);
  max-width: 800px;
  margin-left: auto;
  margin-right: auto;
}
.todos-header { padding: var(--space-6) var(--space-8); border-bottom: 1px solid var(--border); background: var(--bg-window); }
.header-left { display: flex; align-items: center; gap: var(--space-4); margin-bottom: var(--space-3); }
.todos-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.todos-stats { display: flex; gap: var(--space-3); }
.stat { font-size: var(--text-sm); color: var(--text-muted); }
.filter-tabs { display: flex; gap: var(--space-2); }
.filter-tabs button { padding: var(--space-1) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-card); color: var(--text-secondary); font-size: var(--text-sm); cursor: pointer; transition: all var(--transition-fast); }
.filter-tabs button.active { background: var(--accent); border-color: var(--accent); color: white; }
.todos-content { flex: 1; overflow-y: auto; padding: var(--space-6) var(--space-8); }
.todos-state { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: var(--space-12) 0; color: var(--text-muted); gap: var(--space-3); }
.todos-state h3 { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); margin: 0; }
.todos-state p { font-size: var(--text-sm); margin: 0; }
.todos-state .hint { font-size: var(--text-xs); opacity: 0.7; }
.empty-icon { font-size: 48px; opacity: 0.5; }
.spinner { width: 32px; height: 32px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.todos-list { display: flex; flex-direction: column; gap: var(--space-2); max-width: 800px; margin: 0 auto; }
.todo-item { display: flex; align-items: flex-start; gap: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); transition: background var(--transition-fast); }
.todo-item:hover { background: var(--bg-hover); }
.todo-item.completed .todo-text { text-decoration: line-through; opacity: 0.6; }
.todo-checkbox { display: flex; align-items: center; justify-content: center; color: var(--text-muted); cursor: pointer; padding: 2px; border-radius: 4px; transition: color var(--transition-fast); }
.todo-checkbox:hover { color: var(--accent); }
.todo-content { flex: 1; cursor: pointer; min-width: 0; }
.todo-text { font-size: var(--text-base); color: var(--text-primary); margin-bottom: 4px; }
.todo-meta { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xs); color: var(--text-muted); }
</style>
