<script setup lang="ts">
/**
 * TodayPanel - 今日工作台条带
 *
 * "今天过得怎么样"一屏速览：今日编辑、连续记录、待办、到期提醒、最近笔记。
 * 数据全部来自后端 StatsService（从工作区文件派生），本组件不维护任何状态，
 * workspace 切换与文件树变更时自动刷新。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CalendarCheck,
  FileEdit,
  Flame,
  ListTodo,
  AlarmClock,
} from 'lucide-vue-next'
import { StatsService } from '@bindings/github.com/notevault/notevault/index.js'
import { useWorkspaceStore } from '@/stores/workspace'

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()

interface TodayStats {
  editedToday: number
  streakDays: number
  pendingTodos: number
  highPriorityTodos: number
  dueReminders: number
  recentFiles: string[]
}

const stats = ref<TodayStats | null>(null)
const loaded = ref(false)

async function load() {
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  try {
    stats.value = (await StatsService.GetTodayStats(ws.path)) as TodayStats | null
  } catch (e) {
    // 统计是锦上添花，失败静默降级为不显示，不打断主页
    console.error('[today] Failed to load today stats:', e)
    stats.value = null
  } finally {
    loaded.value = true
  }
}

onMounted(load)
watch(() => workspaceStore.currentWorkspace?.id, load)
watch(() => workspaceStore.fileTreeVersion, load)

const cells = computed(() => {
  const s = stats.value
  if (!s) return []
  return [
    {
      key: 'edited',
      icon: FileEdit,
      value: s.editedToday,
      label: t('knowledge.today.edited'),
      tone: 'notes',
    },
    {
      key: 'streak',
      icon: Flame,
      value: s.streakDays,
      label: t('knowledge.today.streak'),
      tone: 'streak',
    },
    {
      key: 'todos',
      icon: ListTodo,
      value: s.pendingTodos,
      label: t('knowledge.today.todos'),
      tone: 'todos',
      warn: s.highPriorityTodos > 0,
      hint: s.highPriorityTodos > 0 ? t('knowledge.today.highTodos', { count: s.highPriorityTodos }) : '',
    },
    {
      key: 'reminders',
      icon: AlarmClock,
      value: s.dueReminders,
      label: t('knowledge.today.dueReminders'),
      tone: 'danger',
      warn: s.dueReminders > 0,
    },
  ]
})

function openFile(path: string) {
  workspaceStore.openFile(path)
  workspaceStore.incrementFileTreeVersion()
}
</script>

<template>
  <section
    v-if="loaded && stats"
    class="today-panel"
  >
    <div class="today-cells">
      <div
        v-for="cell in cells"
        :key="cell.key"
        class="today-cell"
      >
        <div
          class="today-cell-icon"
          :class="cell.tone"
        >
          <component
            :is="cell.icon"
            :size="16"
          />
        </div>
        <div class="today-cell-body">
          <div
            class="today-cell-value"
            :class="{ warn: cell.warn }"
          >
            {{ cell.value }}
          </div>
          <div class="today-cell-label">
            {{ cell.label }}
            <span
              v-if="cell.hint"
              class="today-cell-hint"
            >{{ cell.hint }}</span>
          </div>
        </div>
      </div>
    </div>
    <div
      v-if="stats.recentFiles.length"
      class="today-recent"
    >
      <span class="today-recent-label">
        <CalendarCheck :size="13" />
        {{ t('knowledge.today.continue') }}
      </span>
      <button
        v-for="f in stats.recentFiles"
        :key="f"
        class="today-recent-chip"
        :title="f"
        @click="openFile(f)"
      >
        {{ f.split('/').pop()?.replace(/\.md$/i, '') }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.today-panel {
  padding: var(--space-3) var(--space-8) 0;
}

.today-cells {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: var(--space-3);
}

.today-cell {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.today-cell-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  flex-shrink: 0;
}
.today-cell-icon.notes {
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
}
.today-cell-icon.streak {
  background: rgba(249, 115, 22, 0.14);
  color: #f97316;
}
.today-cell-icon.todos {
  background: rgba(168, 85, 247, 0.12);
  color: #a855f7;
}
.today-cell-icon.danger {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}

.today-cell-value {
  font-size: var(--text-lg);
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1.1;
}
.today-cell-value.warn {
  color: var(--error);
}

.today-cell-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.today-cell-hint {
  color: var(--warning);
}

.today-recent {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-3);
  flex-wrap: wrap;
}

.today-recent-label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.today-recent-chip {
  padding: 2px var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-secondary);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 999px;
  transition: color var(--transition-fast), border-color var(--transition-fast);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.today-recent-chip:hover {
  color: var(--accent);
  border-color: var(--border-accent);
}

@media (max-width: 980px) {
  .today-cells {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
