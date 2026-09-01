<script setup lang="ts">
/**
 * KnowledgeTodoPanel - 知识库主页待办面板
 * 纯展示组件：接收待办列表（父组件已按优先级排序、截断），
 * 通过 emit 把"切换完成/打开文件"动作交还父组件处理。
 */
import { useI18n } from 'vue-i18n'
import { CheckSquare, ArrowUpRight } from 'lucide-vue-next'
import type { TodoItem } from '@bindings/github.com/notevault/notevault/models.js'

const { t } = useI18n()

defineProps<{ todos: TodoItem[] }>()

const emit = defineEmits<{
  (e: 'toggle', todo: TodoItem): void
  (e: 'open', todo: TodoItem): void
}>()

function isOverdue(todo: TodoItem): boolean {
  return todo.priority === 'high' && !todo.completed
}

function isToday(_todo: TodoItem): boolean {
  return false // 当前 Todo 模型不含 dueDate
}
</script>

<template>
  <div class="kv-card">
    <div class="kv-card-header">
      <h3>
        <CheckSquare :size="14" />
        <span>{{ t('knowledge.todosPanel') }}</span>
      </h3>
      <router-link
        to="/todos"
        class="kv-card-link"
      >
        {{ t('knowledge.viewAll') }} <ArrowUpRight :size="12" />
      </router-link>
    </div>
    <div
      v-if="todos.length === 0"
      class="kv-card-empty"
    >
      {{ t('knowledge.noTodos') }}
    </div>
    <ul
      v-else
      class="kv-todo-list"
    >
      <li
        v-for="todo in todos"
        :key="todo.id"
        class="kv-todo-item"
        :class="{ overdue: isOverdue(todo), today: isToday(todo) }"
      >
        <button
          class="kv-todo-check"
          :title="todo.completed ? t('knowledge.markIncomplete') : t('knowledge.markComplete')"
          @click.stop="emit('toggle', todo)"
        >
          <CheckSquare
            v-if="todo.completed"
            :size="14"
          />
          <span
            v-else
            class="kv-todo-checkbox"
          />
        </button>
        <div
          class="kv-todo-body"
          @click="emit('open', todo)"
        >
          <div class="kv-todo-content">
            {{ todo.content }}
          </div>
          <div class="kv-todo-meta">
            <span class="kv-todo-doc">{{ todo.fileName.replace(/\.(md|markdown)$/, '') }}</span>
            <span
              v-if="todo.priority === 'high'"
              class="kv-todo-due"
            >{{ t('knowledge.highPriority') }}</span>
            <span
              v-else-if="todo.priority === 'low'"
              class="kv-todo-due"
            >{{ t('knowledge.lowPriority') }}</span>
          </div>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.kv-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
  flex-shrink: 0;
}

.kv-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
}

.kv-card-header h3 {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.kv-card-link {
  display: flex;
  align-items: center;
  gap: 2px;
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-decoration: none;
}

.kv-card-link:hover {
  color: var(--accent);
}

.kv-card-empty {
  padding: var(--space-4);
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-align: center;
}

/* 待办列表 */
.kv-todo-list {
  list-style: none;
  margin: 0;
  padding: var(--space-2);
}

.kv-todo-item {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  transition: background var(--transition-fast);
}

.kv-todo-item:hover {
  background: var(--bg-hover);
}

.kv-todo-item.overdue .kv-todo-content {
  color: #ef4444;
}

.kv-todo-item.today .kv-todo-content {
  font-weight: 600;
}

.kv-todo-check {
  flex-shrink: 0;
  margin-top: 2px;
  color: var(--text-muted);
  transition: color var(--transition-fast);
}

.kv-todo-check:hover {
  color: var(--accent);
}

.kv-todo-checkbox {
  display: block;
  width: 14px;
  height: 14px;
  border: 1.5px solid var(--text-muted);
  border-radius: 3px;
}

.kv-todo-body {
  flex: 1;
  min-width: 0;
  cursor: pointer;
}

.kv-todo-content {
  font-size: var(--text-sm);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.kv-todo-meta {
  display: flex;
  justify-content: space-between;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-top: 2px;
}

.kv-todo-doc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.kv-todo-due {
  font-weight: 600;
  color: var(--text-muted);
  white-space: nowrap;
}

.kv-todo-item.overdue .kv-todo-due {
  color: #ef4444;
}

.kv-todo-item.today .kv-todo-due {
  color: #3b82f6;
}
</style>
