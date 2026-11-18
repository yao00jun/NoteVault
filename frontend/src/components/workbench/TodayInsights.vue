<script setup lang="ts">
/**
 * TodayInsights - 今日工作台底部的「手风琴」补充区（P2-3）。
 *
 * 宽屏且任务较少时，主栅格下方会留下大片空白。这里用两块可折叠卡片填充：
 *   1. 迷你月历 + 近 4 周写作热力条（复用 StatsService.GetWritingActivity，
 *      一次取 35 天同时喂两个视图，不新增后端接口）；
 *   2. 结构速览（项目 / 任务 / 阻塞 / 复习 / 提醒），全部由 workbench store 派生，
 *      零额外 IO。
 *
 * 默认全部收起，展开状态记在 localStorage，避免每次进页面都要重新展开；
 * 同一时间只展开一块（手风琴），保持纵向密度。
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CalendarDays, ChevronDown, LayoutList, Loader2, ArrowUpRight } from '@lucide/vue'
import { StatsService } from '@/api'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'

interface DayActivity {
  date: string
  edited: number
}
interface WritingActivity {
  days: DayActivity[]
  activeDays: number
  longestStreak: number
  weekEdited: number
}

type PanelID = 'calendar' | 'overview'
const STORAGE_KEY = 'notevault.today.fold'

const { t, locale } = useI18n()
const store = useWorkbenchStore()
const workspace = useWorkspaceStore()

const stored = (() => {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
})()
const openPanel = ref<PanelID | null>(stored === 'calendar' || stored === 'overview' ? stored : null)

const activity = ref<WritingActivity | null>(null)
const loading = ref(false)
const loadError = ref('')

function toggle(id: PanelID) {
  openPanel.value = openPanel.value === id ? null : id
  try {
    if (openPanel.value) localStorage.setItem(STORAGE_KEY, openPanel.value)
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    /* 隐私模式下 localStorage 不可写，忽略 */
  }
}

async function loadActivity() {
  const path = workspace.currentWorkspace?.path
  if (!path || loading.value) return
  loading.value = true
  loadError.value = ''
  try {
    // 35 天窗口同时覆盖「当月」与「近 4 周」，少一次往返
    const data = (await StatsService.GetWritingActivity(path, 35)) as WritingActivity | null
    activity.value = data ?? null
  } catch (e) {
    loadError.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

watch(
  () => [openPanel.value, workspace.currentWorkspace?.path] as const,
  ([panel, path]) => {
    if (panel === 'calendar' && path) void loadActivity()
  },
  { immediate: true },
)
// 换工作区时丢弃旧活动数据，下次展开重取
watch(
  () => workspace.currentWorkspace?.path,
  () => {
    activity.value = null
  },
)

const editedByDate = computed(() => {
  const map = new Map<string, number>()
  for (const day of activity.value?.days ?? []) map.set(day.date, day.edited)
  return map
})

const todayKey = () => {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

const monthLabel = computed(() =>
  new Date().toLocaleDateString(locale.value, { year: 'numeric', month: 'long' }),
)

const weekdays = computed(() =>
  locale.value.startsWith('zh')
    ? ['一', '二', '三', '四', '五', '六', '日']
    : ['M', 'T', 'W', 'T', 'F', 'S', 'S'],
)

// 月历格子：前导补空使第一天落在正确的星期列，其余补尾空保持整行
const monthCells = computed(() => {
  const now = new Date()
  const year = now.getFullYear()
  const month = now.getMonth()
  const firstDow = (new Date(year, month, 1).getDay() + 6) % 7 // 0=周一
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const today = todayKey()
  const cells: Array<{ day: number | null; date: string; edited: number; isToday: boolean }> = []
  for (let i = 0; i < firstDow; i++) cells.push({ day: null, date: '', edited: 0, isToday: false })
  for (let d = 1; d <= daysInMonth; d++) {
    const date = `${year}-${String(month + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`
    cells.push({ day: d, date, edited: editedByDate.value.get(date) ?? 0, isToday: date === today })
  }
  while (cells.length % 7 !== 0) cells.push({ day: null, date: '', edited: 0, isToday: false })
  return cells
})

const heatDays = computed(() => (activity.value?.days ?? []).slice(-28))

// 色阶与报表中心一致：0 / 1 / 2 / 3-4 / 5+ 五档
function level(edited: number): number {
  if (edited <= 0) return 0
  if (edited === 1) return 1
  if (edited === 2) return 2
  if (edited <= 4) return 3
  return 4
}

const calendarHint = computed(() => {
  if (loadError.value) return loadError.value
  if (!activity.value) return t('today.fold.calendarHint')
  return t('today.fold.weekEdited', { n: activity.value.weekEdited ?? 0 })
})

const overviewItems = computed(() => {
  const activeProjects = store.projects.filter((p) => p.status === 'active').length
  const openTasks = store.tasks.filter((task) => !task.completed).length
  return [
    { id: 'projects', value: activeProjects, to: '/projects' },
    { id: 'tasks', value: openTasks, to: '/todos' },
    { id: 'blockers', value: store.blockers.length, to: '/todos' },
    { id: 'review', value: store.reviewQueue.length, to: '/learning?tab=review' },
    { id: 'reminders', value: store.dueReminders.length, to: '/reminders' },
  ]
})
</script>

<template>
  <section
    class="today-fold"
    data-testid="today-fold"
    :aria-label="t('today.fold.aria')"
  >
    <article
      class="fold-card"
      :class="{ open: openPanel === 'calendar' }"
    >
      <button
        type="button"
        class="fold-head"
        :aria-expanded="openPanel === 'calendar'"
        data-testid="fold-calendar"
        @click="toggle('calendar')"
      >
        <CalendarDays :size="15" />
        <span class="fold-title">{{ t('today.fold.calendarTitle') }}</span>
        <span class="fold-hint">{{ calendarHint }}</span>
        <ChevronDown
          :size="14"
          class="fold-chevron"
        />
      </button>
      <div
        v-if="openPanel === 'calendar'"
        class="fold-body"
        data-testid="fold-calendar-body"
      >
        <div
          v-if="loading"
          class="fold-loading"
        >
          <Loader2
            :size="14"
            class="spin"
          />{{ t('common.loading') }}
        </div>
        <div
          v-else-if="loadError"
          class="fold-error"
          role="status"
        >
          {{ loadError }}
        </div>
        <template v-else>
          <div
            class="mini-calendar"
            data-testid="mini-calendar"
          >
            <div class="cal-month">
              {{ monthLabel }}
            </div>
            <div class="cal-grid cal-week">
              <span
                v-for="(w, i) in weekdays"
                :key="`${w}-${i}`"
              >{{ w }}</span>
            </div>
            <div class="cal-grid">
              <span
                v-for="(cell, i) in monthCells"
                :key="i"
                class="cal-cell"
                :class="{ empty: cell.day === null, today: cell.isToday, active: cell.edited > 0 }"
                :title="cell.date ? `${cell.date} · ${cell.edited}` : ''"
              >{{ cell.day ?? '' }}</span>
            </div>
          </div>
          <div class="heat-block">
            <div class="heat-title">
              {{ t('today.fold.heatTitle') }}
              <small>{{ t('today.fold.heatStats', { days: activity?.activeDays ?? 0, streak: activity?.longestStreak ?? 0 }) }}</small>
            </div>
            <div class="heat-strip">
              <i
                v-for="(day, i) in heatDays"
                :key="`${day.date}-${i}`"
                :class="`l${level(day.edited)}`"
                :title="`${day.date} · ${day.edited}`"
              />
            </div>
            <div class="heat-legend">
              <span>{{ t('today.fold.less') }}</span>
              <i class="l0" /><i class="l1" /><i class="l2" /><i class="l3" /><i class="l4" />
              <span>{{ t('today.fold.more') }}</span>
            </div>
          </div>
        </template>
      </div>
    </article>

    <article
      class="fold-card"
      :class="{ open: openPanel === 'overview' }"
    >
      <button
        type="button"
        class="fold-head"
        :aria-expanded="openPanel === 'overview'"
        data-testid="fold-overview"
        @click="toggle('overview')"
      >
        <LayoutList :size="15" />
        <span class="fold-title">{{ t('today.fold.overviewTitle') }}</span>
        <span class="fold-hint">{{ t('today.fold.overviewHint') }}</span>
        <ChevronDown
          :size="14"
          class="fold-chevron"
        />
      </button>
      <div
        v-if="openPanel === 'overview'"
        class="fold-body"
        data-testid="fold-overview-body"
      >
        <div class="overview-grid">
          <button
            v-for="item in overviewItems"
            :key="item.id"
            type="button"
            class="overview-item"
            :data-testid="`overview-${item.id}`"
            @click="$router.push(item.to)"
          >
            <strong>{{ item.value }}</strong>
            <span>{{ t(`today.fold.items.${item.id}`) }}</span>
            <ArrowUpRight
              :size="12"
              class="overview-arrow"
            />
          </button>
        </div>
      </div>
    </article>
  </section>
</template>

<style scoped>
.today-fold { display: grid; gap: 8px; margin-top: 16px; }
.fold-card { border: 1px solid var(--border); border-radius: 8px; background: var(--bg-card); overflow: hidden; }
.fold-head { display: flex; align-items: center; gap: 8px; width: 100%; padding: 9px 12px; border: none; background: transparent; color: var(--text-secondary); font-size: 12px; text-align: left; cursor: pointer; }
.fold-head:hover { background: var(--bg-hover); }
.fold-title { font-weight: 600; color: var(--text-primary); }
.fold-hint { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; color: var(--text-muted); }
.fold-chevron { color: var(--text-muted); transition: transform 0.18s ease; flex-shrink: 0; }
.fold-card.open .fold-chevron { transform: rotate(180deg); }
.fold-body { display: flex; gap: 24px; flex-wrap: wrap; padding: 4px 14px 14px; border-top: 1px solid var(--border); }
.fold-loading, .fold-error { display: flex; align-items: center; gap: 6px; padding: 10px 0; font-size: 11px; color: var(--text-muted); }
.fold-error { color: var(--danger, #ef4444); }
.spin { animation: spin 0.9s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.mini-calendar { min-width: 190px; }
.cal-month { font-size: 11px; color: var(--text-muted); margin: 6px 0; }
.cal-grid { display: grid; grid-template-columns: repeat(7, 1fr); gap: 2px; }
.cal-week span { text-align: center; font-size: 10px; color: var(--text-muted); }
.cal-cell { display: flex; align-items: center; justify-content: center; height: 22px; border-radius: 4px; font-size: 11px; color: var(--text-secondary); font-variant-numeric: tabular-nums; }
.cal-cell.active { background: color-mix(in srgb, var(--accent) 22%, transparent); color: var(--text-primary); }
.cal-cell.today { outline: 1px solid var(--accent); color: var(--accent); font-weight: 600; }
.cal-cell.empty { visibility: hidden; }

.heat-block { flex: 1; min-width: 240px; }
.heat-title { display: flex; align-items: baseline; gap: 8px; margin: 6px 0; font-size: 11px; color: var(--text-secondary); }
.heat-title small { font-size: 10px; color: var(--text-muted); }
.heat-strip { display: grid; grid-template-rows: repeat(2, 9px); grid-auto-flow: column; grid-auto-columns: 9px; gap: 3px; }
.heat-strip i { border-radius: 2px; display: block; }
.heat-legend { display: flex; align-items: center; gap: 3px; margin-top: 8px; font-size: 10px; color: var(--text-muted); }
.heat-legend i { width: 9px; height: 9px; border-radius: 2px; }
.l0 { background: var(--bg-hover); }
.l1 { background: color-mix(in srgb, var(--accent) 28%, transparent); }
.l2 { background: color-mix(in srgb, var(--accent) 48%, transparent); }
.l3 { background: color-mix(in srgb, var(--accent) 70%, transparent); }
.l4 { background: var(--accent); }

.overview-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 8px; width: 100%; }
.overview-item { position: relative; display: flex; flex-direction: column; gap: 2px; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-window); color: var(--text-secondary); text-align: left; cursor: pointer; }
.overview-item:hover { border-color: var(--border-accent, var(--accent)); }
.overview-item strong { font-size: 18px; font-weight: 600; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.overview-item span { font-size: 11px; color: var(--text-muted); }
.overview-arrow { position: absolute; top: 6px; right: 6px; color: var(--text-muted); }
</style>
