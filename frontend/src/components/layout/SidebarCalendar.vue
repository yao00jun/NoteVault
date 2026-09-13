<!--
  SidebarCalendar.vue：侧栏迷你月历（Obsidian Calendar 式）
  - 展示当月 42 格（补齐上月末/下月初灰显日期），周一为一周起点
  - 从工作区文件树扫描工作日志打卡：Daily/YYYY-MM-DD.md 或 Daily/Reports/YYYY-MM-DD-日报.md
  - 点击有打卡的日期直达对应日志；今天尚未生成时走 openTodayWorkLog
-->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { FileService } from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkLog } from '@/composables/useWorkLog'
import { normalizeNotePath } from '@/utils/navigation'
import { localDateKey } from '@/utils/workbench'
import type { FileNode } from '@/components/editor/FileTree.vue'

defineOptions({ name: 'SidebarCalendar' })

const workspaceStore = useWorkspaceStore()
const router = useRouter()
const { openTodayWorkLog } = useWorkLog()

const now = new Date()
const viewYear = ref(now.getFullYear())
const viewMonth = ref(now.getMonth())
const today = ref(localDateKey())
const logDates = ref<Set<string>>(new Set())
const logPaths = ref<Map<string, string>>(new Map())

const monthLabel = computed(() => `${viewYear.value}年${viewMonth.value + 1}月`)
const weekdays = ['一', '二', '三', '四', '五', '六', '日']

function formatKey(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

// 42 格：从当月第一周周一开始（周一为一周起点，偏移 (getDay()+6)%7）
const cells = computed(() => {
  const first = new Date(viewYear.value, viewMonth.value, 1)
  const startOffset = (first.getDay() + 6) % 7
  const start = new Date(first.getFullYear(), first.getMonth(), 1 - startOffset)
  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start.getFullYear(), start.getMonth(), start.getDate() + index)
    const key = formatKey(date)
    return {
      key,
      day: date.getDate(),
      inMonth: date.getMonth() === viewMonth.value,
      isToday: key === today.value,
      hasLog: logDates.value.has(key),
    }
  })
})

async function loadLogDates() {
  const workspacePath = workspaceStore.currentWorkspace?.path
  if (!workspacePath) {
    logDates.value = new Set()
    logPaths.value = new Map()
    return
  }
  try {
    const tree = await FileService.GetFileTree(workspacePath)
    const dates = new Set<string>()
    const paths = new Map<string, string>()
    const walk = (nodes?: FileNode[]) => {
      for (const node of nodes ?? []) {
        if (node.isDir) {
          walk(node.children)
          continue
        }
        const path = normalizeNotePath(node.path)
        // 工作日志两种落点：Daily/YYYY-MM-DD.md 与 Daily/Reports/YYYY-MM-DD-日报.md
        const match = /^Daily\/(?:Reports\/)?(\d{4}-\d{2}-\d{2})(?:-日报)?\.md$/i.exec(path)
        if (match?.[1]) {
          dates.add(match[1])
          if (!paths.has(match[1])) paths.set(match[1], path)
        }
      }
    }
    walk(tree as FileNode[])
    logDates.value = dates
    logPaths.value = paths
  } catch (error) {
    console.error('Failed to scan daily logs for sidebar calendar:', error)
  }
}

watch(() => [workspaceStore.currentWorkspace?.path, workspaceStore.fileTreeVersion], loadLogDates, { immediate: true })

function shiftMonth(delta: number) {
  const date = new Date(viewYear.value, viewMonth.value + delta, 1)
  viewYear.value = date.getFullYear()
  viewMonth.value = date.getMonth()
}

function backToToday() {
  const now = new Date()
  viewYear.value = now.getFullYear()
  viewMonth.value = now.getMonth()
}

function openDay(cell: { key: string; isToday: boolean; hasLog: boolean }) {
  const path = logPaths.value.get(cell.key)
  if (path) {
    workspaceStore.openFile(path)
    router.push({ path: '/editor', query: { file: path } })
    return
  }
  if (cell.isToday) void openTodayWorkLog()
}
</script>

<template>
  <div
    class="sidebar-calendar"
    data-testid="sidebar-calendar"
  >
    <div class="calendar-header">
      <button
        class="calendar-nav-btn"
        aria-label="上一月"
        @click="shiftMonth(-1)"
      >
        ‹
      </button>
      <span class="calendar-month">{{ monthLabel }}</span>
      <button
        class="calendar-nav-btn calendar-today-btn"
        aria-label="回到本月"
        title="回到本月"
        @click="backToToday"
      >
        今日
      </button>
      <button
        class="calendar-nav-btn"
        aria-label="下一月"
        @click="shiftMonth(1)"
      >
        ›
      </button>
    </div>
    <div class="calendar-grid">
      <span
        v-for="weekday in weekdays"
        :key="weekday"
        class="calendar-weekday"
      >{{ weekday }}</span>
      <template
        v-for="cell in cells"
        :key="cell.key"
      >
        <button
          v-if="cell.inMonth && (cell.hasLog || cell.isToday)"
          class="calendar-cell is-interactive"
          :class="{ 'is-today': cell.isToday, 'has-log': cell.hasLog }"
          :title="cell.hasLog ? `${cell.key} · 已完成工作日报` : `${cell.key} · 尚无日志，点击生成`"
          @click="openDay(cell)"
        >
          <span class="calendar-day">{{ cell.day }}</span>
          <span class="calendar-dot" />
        </button>
        <span
          v-else
          class="calendar-cell"
          :class="{ 'is-outside': !cell.inMonth, 'is-today': cell.isToday }"
          :title="cell.hasLog ? `${cell.key} · 已完成工作日报` : undefined"
        >
          <span class="calendar-day">{{ cell.day }}</span>
          <span
            v-if="cell.hasLog"
            class="calendar-dot"
          />
        </span>
      </template>
    </div>
  </div>
</template>

<style scoped>
.sidebar-calendar { padding: 2px 2px 4px; }
.calendar-header {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-bottom: 4px;
}
.calendar-month {
  flex: 1;
  min-width: 0;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
.calendar-nav-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 22px;
  height: 20px;
  padding: 0 4px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.calendar-nav-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.calendar-today-btn { font-size: 10px; }

.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 1px;
}
.calendar-weekday {
  text-align: center;
  font-size: 10px;
  color: var(--text-muted);
  padding: 1px 0 2px;
}
.calendar-cell {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1px;
  height: 24px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
  user-select: none;
}
span.calendar-cell { cursor: default; }
.calendar-cell.is-outside { color: var(--text-muted); opacity: .35; }
.calendar-cell.is-today { border-color: var(--accent); color: var(--accent); }
button.calendar-cell.is-interactive { cursor: pointer; transition: background var(--transition-fast); }
button.calendar-cell.is-interactive:hover { background: var(--bg-hover); }
button.calendar-cell.has-log { color: var(--text-primary); }
.calendar-dot {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--success, #559775);
  box-shadow: 0 0 3px var(--success, #559775);
}
</style>
