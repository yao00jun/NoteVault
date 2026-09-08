<script setup lang="ts">
/**
 * WorkbenchWidgets - 个人工作台的「今日行动中心」看板（UI-WORKBENCH-REDESIGN 上区）
 *
 * 双列大看板（认知负荷原则：能在工作台直接完成的就不让用户跳页）：
 *  - 今日待办：未完成 top5，可直接勾选（ToggleTodo 写回源文档）
 *  - 到期提醒：未完成提醒按时间升序 top4，点击打开关联笔记
 * （原「今日编辑」卡已上移合并为工作台下区「最近编辑」，职责不重复）
 *
 * 数据全部来自后端服务（文件派生），工作区切换 / 文件树变更自动刷新。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ListTodo, AlarmClock, Inbox, Plus } from '@lucide/vue'
import {
  StatsService,
  TodoService,
  ReminderService,
  FileService,
  TemplateService,
} from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { promptDialog } from '@/composables/usePrompt'

const { t } = useI18n()
const router = useRouter()
const toast = useToast()
const workspaceStore = useWorkspaceStore()

interface TodayStats {
  editedToday: number
  streakDays: number
  pendingTodos: number
  highPriorityTodos: number
  dueReminders: number
  recentFiles: string[]
}
interface TodoItem {
  id: string
  filePath: string
  fileName: string
  content: string
  lineIndex: number
  completed: boolean
  priority: string
}
interface ReminderItem {
  id: string
  filePath: string
  fileName: string
  content: string
  remindAt: string
  createdAt: string
  completed: boolean
}

const stats = ref<TodayStats | null>(null)
const todos = ref<TodoItem[]>([])
const reminders = ref<ReminderItem[]>([])

async function load() {
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  // 三个数据源互不依赖，各自静默降级：单个失败不拖垮整行部件
  const [statsRes, todoRes, reminderRes] = await Promise.allSettled([
    StatsService.GetTodayStats(ws.path),
    TodoService.GetAllTodos(ws.path),
    ReminderService.GetAllReminders(ws.path),
  ])
  if (statsRes.status === 'fulfilled') {
    stats.value = (statsRes.value as TodayStats | null) ?? null
  } else {
    console.error('[workbench] stats failed:', statsRes.reason)
  }
  if (todoRes.status === 'fulfilled') {
    const all = (todoRes.value as TodoItem[] | null) ?? []
    todos.value = all.filter((x) => !!x)
  } else {
    console.error('[workbench] todos failed:', todoRes.reason)
  }
  if (reminderRes.status === 'fulfilled') {
    const all = (reminderRes.value as ReminderItem[] | null) ?? []
    reminders.value = all.filter((x) => !!x)
  } else {
    console.error('[workbench] reminders failed:', reminderRes.reason)
  }
}

onMounted(load)
watch(() => workspaceStore.currentWorkspace?.id, load)
watch(() => workspaceStore.fileTreeVersion, load)

// 今日待办：未完成优先，高优先级在前，top 5
const pendingTodos = computed(() =>
  todos.value
    // 空内容的 `- [ ]`（常见于模板留白）没有可读文本，显示时只能
    // 回退到文件名，看起来就像日记混进了待办，因此不进工作台
    .filter((todo) => !todo.completed && (todo.content ?? '').trim() !== '')
    .slice()
    .sort((a, b) => {
      const order: Record<string, number> = { high: 0, medium: 1, low: 2 }
      return (order[a.priority] ?? 1) - (order[b.priority] ?? 1)
    })
    .slice(0, 5),
)

// 到期提醒：未完成按提醒时间升序（过期即最前），top 4
const dueReminders = computed(() =>
  reminders.value
    .filter((r) => !r.completed)
    .slice()
    .sort((a, b) => (a.remindAt || '').localeCompare(b.remindAt || ''))
    .slice(0, 4),
)


function isOverdue(remindAt: string): boolean {
  return !!remindAt && new Date(remindAt).getTime() < Date.now()
}

function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
}

// 临近提醒倒计时：跨天显示日期，当日显示「x 小时 / x 分钟后」，过期显示「已过期」
function countdownLabel(iso: string): string {
  if (!iso) return ''
  const ts = new Date(iso).getTime()
  if (Number.isNaN(ts)) return ''
  const diff = ts - Date.now()
  if (diff < 0) return t('knowledge.workbench.overdue')
  const minutes = Math.floor(diff / 60000)
  if (minutes < 60) return t('knowledge.workbench.inMinutes', { count: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('knowledge.workbench.inHours', { count: hours })
  const d = new Date(iso)
  return `${d.getMonth() + 1}/${d.getDate()}`
}

function todayPath(): string {
  const d = new Date()
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `Daily/${d.getFullYear()}-${mm}-${dd}.md`
}

// 新建待办：写入今天日记（待办的天然归宿），日记不存在时按内置 Daily 模板创建
async function addTodo() {
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  const text = await promptDialog({ message: t('knowledge.workbench.addTodoPrompt') })
  if (!text || !text.trim()) return
  const path = todayPath()
  try {
    let content: string
    try {
      content = await FileService.ReadFile(ws.path, path)
    } catch {
      await TemplateService.CreateFromTemplate(ws.path, 'Daily', path, {})
      content = await FileService.ReadFile(ws.path, path)
    }
    content = `${content.replace(/\s*$/, '')}
- [ ] ${text.trim()}
`
    await FileService.SaveFile(ws.path, path, content)
    await load()
    toast.success(t('knowledge.workbench.todoAdded'))
  } catch (e) {
    console.error('[workbench] add todo failed:', e)
    toast.error(t('knowledge.workbench.addTodoFailed', { msg: (e as Error).message }))
  }
}

// 新建提醒：提醒是挂在笔记上的服务数据（非 Markdown 模板），进入提醒管理页创建
function addReminder() {
  router.push('/review?tab=tasks&sub=reminders')
}

function fileNameOf(path: string): string {
  return path.split('/').pop()?.replace(/\.md$/i, '') ?? path
}

async function toggleTodo(todo: TodoItem) {
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  try {
    await TodoService.ToggleTodo(ws.path, todo.filePath, todo.lineIndex)
    await load()
  } catch (e) {
    console.error('[workbench] toggle todo failed:', e)
  }
}

function openFile(path: string) {
  workspaceStore.openFile(path)
  workspaceStore.incrementFileTreeVersion()
  router.push({ path: '/editor', query: { file: path } })
}
</script>

<template>
  <section class="wb-widgets">
    <!-- 今日待办：可勾选 -->
    <div
      class="wb-card"
      data-testid="wb-todos"
    >
      <div class="wb-card-header">
        <ListTodo :size="14" />
        <span>{{ t('knowledge.workbench.todos') }}</span>
        <span class="wb-count">{{ pendingTodos.length }}</span>
        <button
          class="wb-add"
          data-testid="wb-add-todo"
          :title="t('knowledge.workbench.addTodo')"
          @click="addTodo"
        >
          <Plus :size="14" />
        </button>
      </div>
      <div
        v-if="pendingTodos.length === 0"
        class="wb-empty wb-empty-action"
        @click="addTodo"
      >
        <Inbox :size="18" />
        <span>{{ t('knowledge.workbench.todosEmpty') }}</span>
      </div>
      <ul
        v-else
        class="wb-list"
      >
        <li
          v-for="todo in pendingTodos"
          :key="todo.id"
          class="wb-item"
        >
          <button
            class="wb-check"
            :title="t('knowledge.workbench.toggleTodo')"
            @click="toggleTodo(todo)"
          />
          <span
            class="wb-item-text"
            :class="{ high: todo.priority === 'high' }"
            :title="`${todo.fileName} · ${todo.content}`"
            @click="openFile(todo.filePath)"
          >{{ todo.content.trim() }}</span>
        </li>
      </ul>
    </div>

    <!-- 到期提醒 -->
    <div
      class="wb-card"
      data-testid="wb-reminders"
    >
      <div class="wb-card-header">
        <AlarmClock :size="14" />
        <span>{{ t('knowledge.workbench.reminders') }}</span>
        <span class="wb-count">{{ dueReminders.length }}</span>
        <button
          class="wb-add"
          data-testid="wb-add-reminder"
          :title="t('knowledge.workbench.addReminder')"
          @click="addReminder"
        >
          <Plus :size="14" />
        </button>
      </div>
      <div
        v-if="dueReminders.length === 0"
        class="wb-empty"
      >
        <Inbox :size="18" />
        <span>{{ t('knowledge.workbench.remindersEmpty') }}</span>
      </div>
      <ul
        v-else
        class="wb-list"
      >
        <li
          v-for="r in dueReminders"
          :key="r.id"
          class="wb-item"
          :title="t('knowledge.workbench.goReminders')"
          @click="router.push('/review?tab=tasks&sub=reminders')"
        >
          <span
            class="wb-time"
            :class="{ overdue: isOverdue(r.remindAt) }"
          >{{ formatTime(r.remindAt) }} · {{ countdownLabel(r.remindAt) }}</span>
          <span
            class="wb-item-text"
            :title="`${r.fileName} · ${r.content}`"
          >{{ r.content || r.fileName }}</span>
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.wb-widgets {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-3);
  padding: var(--space-3) var(--space-8) 0;
}

.wb-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  min-height: 140px;
  display: flex;
  flex-direction: column;
}

.wb-card-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: var(--space-2);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.wb-count {
  margin-left: auto;
  font-size: var(--text-xs);
  color: var(--text-muted);
  font-weight: 500;
}

.wb-add {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.wb-add:hover {
  background: var(--bg-hover);
  color: var(--accent);
}

.wb-empty-action {
  cursor: pointer;
  border-radius: var(--radius-sm);
}
.wb-empty-action:hover {
  color: var(--text-secondary);
  opacity: 1;
}

.wb-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
}

.wb-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: var(--text-sm);
  color: var(--text-primary);
  transition: background var(--transition-fast);
}
.wb-item:hover {
  background: var(--bg-hover);
}

.wb-item-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wb-item-text.high {
  color: var(--warning);
  font-weight: 500;
}

.wb-check {
  width: 14px;
  height: 14px;
  border: 1.5px solid var(--border);
  border-radius: 4px;
  background: transparent;
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--transition-fast), border-color var(--transition-fast);
}
.wb-check:hover {
  border-color: var(--accent);
  background: var(--accent-alpha, rgba(0, 122, 255, 0.12));
}

.wb-time {
  font-size: var(--text-xs);
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
.wb-time.overdue {
  color: var(--error);
  font-weight: 600;
}

.wb-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-xs);
  opacity: 0.7;
}

@media (max-width: 980px) {
  .wb-widgets {
    grid-template-columns: 1fr;
  }
}
</style>
