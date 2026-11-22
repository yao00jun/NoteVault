<script setup lang="ts">
/**
 * ReportsView - 写作报表中心（B 期）
 *
 * 三个板块，数据全部自动派生：
 *  1. 汇总卡片：近 7 天 / 近 30 天改动、活跃天数、最长连续（StatsService）
 *  2. GitHub 风格写作热力图（StatsService.GetWritingActivity，纯 CSS grid 渲染）
 *  3. 知识资产 Top10：被 [[双向链接]] 最多的笔记（GraphService 边统计）
 * 不引入图表库，保持本地优先的轻量体积。
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BarChart3, Flame, Link2, CalendarRange, Activity, FileText, History, Loader2, X } from '@lucide/vue'
import { StatsService, GraphService, ReportService, ReviewService, ReminderService, TaskService } from '@/api'
import WeeklyReportModal from '@/components/workbench/WeeklyReportModal.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useToast } from '@/composables/useToast'

const { t, locale } = useI18n()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const settingsStore = useSettingsStore()
const toast = useToast()

interface DayActivity {
  date: string
  edited: number
}
interface WritingActivity {
  days: DayActivity[]
  activeDays: number
  weekEdited: number
  monthEdited: number
  longestStreak: number
}
interface TopLinked {
  path: string
  title: string
  links: number
}

const HEATMAP_DAYS = 91 // 13 周 × 7 天，与 GitHub 首页卡片同规格

const activity = ref<WritingActivity | null>(null)
const topLinked = ref<TopLinked[]>([])
const onThisDay = ref<string[]>([])
const loading = ref(false)
const errorMsg = ref('')

const generating = ref(false)
const generateMsg = ref('')
const generateOk = ref(false)
const weeklyVisible = ref(false)

// --- 周报异步任务状态 ---
// 生成走 TaskService 后，进度来自 task:progress 事件；只关注「当前这个任务」，
// 其它任务（导入/编译等）的事件直接忽略。
const activeTaskID = ref<string | null>(null)
const taskPercent = ref(-1)
const taskMessage = ref('')
const isCancelling = ref(false)
let stopTaskEvents: (() => void) | undefined

interface TaskEventPayload {
  id?: string
  status?: string
  percent?: number
  message?: string
  error?: string
}

const reviewing = ref(false)
const reviewMsg = ref('')
const reviewOk = ref(false)
const scheduling = ref(false)
const scheduleMsg = ref('')
const scheduleOk = ref(false)

async function load() {
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  loading.value = true
  errorMsg.value = ''
  try {
    const [act, graph, memory] = await Promise.all([
      StatsService.GetWritingActivity(ws.path, HEATMAP_DAYS) as Promise<WritingActivity | null>,
      GraphService.GetGraph(ws.path) as Promise<{ nodes: Array<{ id: string; title: string; path: string; resolved: boolean }>; edges: Array<{ source: string; target: string }> } | null>,
      StatsService.GetOnThisDay(ws.path) as Promise<string[] | null>,
    ])
    activity.value = act
    onThisDay.value = memory ?? []

    // 入度统计：一条边指向某笔记就记一次被链接
    const inDegree = new Map<string, number>()
    for (const e of graph?.edges ?? []) {
      inDegree.set(e.target, (inDegree.get(e.target) ?? 0) + 1)
    }
    const titles = new Map<string, string>()
    for (const n of graph?.nodes ?? []) {
      if (n.resolved) titles.set(n.id, n.title)
    }
    topLinked.value = [...inDegree.entries()]
      .filter(([id]) => titles.has(id))
      .map(([id, links]) => ({ path: id, title: titles.get(id) ?? id, links }))
      .sort((a, b) => b.links - a.links)
      .slice(0, 10)
  } catch (e) {
    errorMsg.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
  // 非 Wails 环境（单测）没有 runtime，订阅失败不影响页面可用性
  void import('@wailsio/runtime')
    .then(({ Events }) => {
      const offProgress = Events.On('task:progress', (ev?: { data?: TaskEventPayload }) =>
        applyTaskEvent(ev?.data, false),
      )
      const offFinished = Events.On('task:finished', (ev?: { data?: TaskEventPayload }) =>
        applyTaskEvent(ev?.data, true),
      )
      stopTaskEvents = () => {
        offProgress?.()
        offFinished?.()
      }
    })
    .catch(() => {
      /* 无 runtime：仅同步兜底路径可用 */
    })
})
onUnmounted(() => {
  stopTaskEvents?.()
  stopTaskEvents = undefined
})
watch(() => workspaceStore.currentWorkspace?.id, load)
watch(() => workspaceStore.fileTreeVersion, load)

// 热力图按周分列：前导补齐使第一列从周一开始
const heatmapColumns = computed(() => {
  const days = activity.value?.days ?? []
  if (!days.length) return []
  const firstDow = (new Date(days[0].date + 'T00:00:00').getDay() + 6) % 7 // 0=周一
  const padded: Array<DayActivity | null> = [...Array<DayActivity | null>(firstDow).fill(null), ...days]
  const cols: Array<Array<DayActivity | null>> = []
  for (let i = 0; i < padded.length; i += 7) {
    cols.push(padded.slice(i, i + 7))
  }
  return cols
})

// 色阶：0 / 1 / 2 / 3-4 / 5+ 五档
function level(edited: number): number {
  if (edited <= 0) return 0
  if (edited === 1) return 1
  if (edited === 2) return 2
  if (edited <= 4) return 3
  return 4
}

function openFile(path: string) {
  workspaceStore.openFile(path)
  workspaceStore.incrementFileTreeVersion()
  router.push({ path: '/editor', query: { file: path } })
}

// 生成本期定期回顾：近 7 天笔记逐篇 AI 摘要 + 旧笔记重温清单
async function generateReview() {
  if (reviewing.value) return
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  reviewing.value = true
  reviewMsg.value = ''
  try {
    const ai = settingsStore.settings.ai
    const result = (await ReviewService.GenerateReview(ws.path, {
      baseURL: ai.baseURL,
      model: ai.model,
      apiKey: ai.apiKey,
      protocol: ai.protocol,
    }, 7)) as { path: string; summarized: number; failed: number; aiUsed: boolean; message: string }
    reviewOk.value = result.aiUsed || result.summarized > 0
    reviewMsg.value = result.message ||
      (result.failed > 0
        ? t('reports.review.generatePartial', { ok: result.summarized, failed: result.failed })
        : t('reports.review.generateDone', { n: result.summarized }))
    await load()
    openFile(result.path)
  } catch (e) {
    reviewOk.value = false
    reviewMsg.value = (e as Error).message
  } finally {
    reviewing.value = false
  }
}

// 预约下周一 09:00 的回顾提醒（一次性，到点后可再约）
async function scheduleReview() {
  if (scheduling.value) return
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  scheduling.value = true
  scheduleMsg.value = ''
  try {
    const rem = (await ReviewService.ScheduleWeeklyReview(ws.path)) as { remindAt: string }
    const when = new Date(rem.remindAt)
    scheduleOk.value = true
    scheduleMsg.value = t('reports.scheduleDone', {
      date: when.toLocaleDateString(locale.value),
      time: when.toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit' }),
    })
  } catch (e) {
    scheduleOk.value = false
    scheduleMsg.value = (e as Error).message
  } finally {
    scheduling.value = false
  }
}

// 生成本周知识周报：优先走异步任务（AI 回顾可取消、有进度），
// 后端未注入任务队列时返回空 ID → 退回同步调用（行为与改造前一致）。
async function generateWeeklyReport() {
  if (generating.value) return
  const ws = workspaceStore.currentWorkspace
  if (!ws?.path) return
  generating.value = true
  generateMsg.value = ''
  taskPercent.value = -1
  taskMessage.value = ''
  try {
    const ai = settingsStore.settings.ai
    const taskID = (await ReportService.StartWeeklyReport(ws.path, {
      baseURL: ai.baseURL,
      model: ai.model,
      apiKey: ai.apiKey,
      protocol: ai.protocol,
    })) as string | null
    if (!taskID) {
      await generateWeeklyReportSync(ws.path)
      return
    }
    activeTaskID.value = taskID
  } catch (e) {
    generateOk.value = false
    generateMsg.value = (e as Error).message
    generating.value = false
  }
}

// --- 同步路径（后端未接入任务队列时的兜底）---
async function generateWeeklyReportSync(path: string) {
  try {
    const ai = settingsStore.settings.ai
    const result = (await ReportService.GenerateWeeklyReport(path, {
      baseURL: ai.baseURL,
      model: ai.model,
      apiKey: ai.apiKey,
      protocol: ai.protocol,
    })) as { path: string; aiUsed: boolean; notes: number; message: string }
    generateOk.value = result.aiUsed
    generateMsg.value = result.message
    await load()
    openFile(result.path)
  } catch (e) {
    generateOk.value = false
    generateMsg.value = (e as Error).message
  } finally {
    generating.value = false
  }
}

// 取消当前生成：TaskService.Cancel 是幂等的，任务刚好结束时返回 false，
// 此时不弹错——finished 事件马上会把 UI 收尾。
async function cancelGenerate() {
  const id = activeTaskID.value
  if (!id || isCancelling.value) return
  isCancelling.value = true
  try {
    const accepted = await TaskService.Cancel(id)
    if (!accepted) isCancelling.value = false
  } catch {
    isCancelling.value = false
  }
}

// applyTaskEvent 只处理「当前关注的任务」，避免被并发的编译/导入任务进度串扰。
function applyTaskEvent(info: TaskEventPayload | undefined, finished: boolean) {
  if (!info || !activeTaskID.value || info.id !== activeTaskID.value) return
  if (typeof info.percent === 'number') taskPercent.value = info.percent
  if (info.message) taskMessage.value = info.message
  if (finished) void finishTask(info)
}

// finishTask 统一收尾：状态提示 → 取回结构化结果 → 刷新列表 → 成功时打开周报。
async function finishTask(info: TaskEventPayload) {
  const id = info.id as string
  const taskStatus = info.status ?? ''
  activeTaskID.value = null
  isCancelling.value = false

  if (taskStatus === 'cancelled') {
    toast.info(t('reports.taskCancelled'))
  } else if (taskStatus === 'failed') {
    toast.error(t('reports.taskFailed', { msg: info.error || '' }))
  }

  let res: { path: string; aiUsed: boolean; notes: number; message: string } | null = null
  try {
    res = (await ReportService.WeeklyReportTaskResult(id)) as {
      path: string; aiUsed: boolean; notes: number; message: string
    } | null
  } catch (e) {
    console.error('Failed to read weekly report task result:', e)
  }

  await load()
  generating.value = false

  if (!res || taskStatus === 'cancelled') return
  generateOk.value = res.aiUsed
  generateMsg.value = res.message
  if (taskStatus === 'succeeded') openFile(res.path)
}
</script>

<template>
  <div class="reports-view">
    <header class="rp-banner">
      <div class="rp-banner-icon">
        <BarChart3 :size="24" />
      </div>
      <div class="rp-banner-text">
        <h1 class="rp-title">
          {{ t('reports.title') }}
        </h1>
        <p class="rp-sub">
          {{ t('reports.subtitle') }}
        </p>
      </div>
      <div class="rp-banner-actions">
        <button
          class="rp-weekly-btn"
          type="button"
          data-testid="generate-work-weekly"
          @click="weeklyVisible = true"
        >
          <CalendarRange :size="14" />
          <span>{{ t('reports.generateWorkWeekly') }}</span>
        </button>
        <button
          class="rp-generate-btn"
          :disabled="generating"
          @click="generateWeeklyReport"
        >
          <Loader2
            v-if="generating"
            :size="14"
            class="spin"
          />
          <FileText
            v-else
            :size="14"
          />
          <span>{{ generating ? t('reports.generating') : t('reports.generateWeekly') }}</span>
        </button>
      </div>
    </header>
    <div
      v-if="generateMsg"
      class="rp-generate-msg"
      :class="{ ok: generateOk }"
    >
      {{ generateMsg }}
    </div>

    <!-- 周报异步任务进度条：AI 回顾进行中可取消 -->
    <div
      v-if="activeTaskID"
      class="task-bar"
      data-testid="report-progress"
    >
      <div class="task-bar-head">
        <Loader2
          :size="14"
          class="spin task-bar-icon"
        />
        <span class="task-bar-message">{{ taskMessage || t('reports.taskPreparing') }}</span>
        <span class="task-bar-pct">{{ taskPercent >= 0 ? `${taskPercent}%` : '…' }}</span>
        <button
          class="task-cancel-btn"
          type="button"
          :disabled="isCancelling"
          data-testid="report-cancel"
          @click="cancelGenerate"
        >
          <X :size="13" />
          {{ isCancelling ? t('reports.taskCancelling') : t('reports.taskCancel') }}
        </button>
      </div>
      <div class="task-track">
        <div
          class="task-fill"
          :class="{ indeterminate: taskPercent < 0 }"
          :style="{ width: taskPercent >= 0 ? `${taskPercent}%` : '32%' }"
        />
      </div>
    </div>

    <div
      v-if="errorMsg"
      class="rp-error"
    >
      ⚠️ {{ errorMsg }}
    </div>
    <div
      v-else-if="loading && !activity"
      class="rp-empty"
    >
      {{ t('common.loading') }}
    </div>

    <template v-if="activity">
      <!-- 汇总卡片 -->
      <section class="rp-cards">
        <div class="rp-card">
          <CalendarRange
            class="rp-card-icon week"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.weekEdited }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.week') }}
            </div>
          </div>
        </div>
        <div class="rp-card">
          <Activity
            class="rp-card-icon month"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.monthEdited }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.month') }}
            </div>
          </div>
        </div>
        <div class="rp-card">
          <BarChart3
            class="rp-card-icon active"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.activeDays }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.activeDays') }}
            </div>
          </div>
        </div>
        <div class="rp-card">
          <Flame
            class="rp-card-icon streak"
            :size="18"
          />
          <div>
            <div class="rp-card-value">
              {{ activity.longestStreak }}
            </div>
            <div class="rp-card-label">
              {{ t('reports.longestStreak') }}
            </div>
          </div>
        </div>
      </section>

      <!-- 定期回顾 -->
      <section class="rp-section">
        <h2 class="rp-section-title">
          {{ t('reports.review.title') }}
        </h2>
        <p class="rp-review-desc">
          {{ t('reports.review.desc') }}
        </p>
        <div class="rp-review-actions">
          <button
            class="rp-generate-btn"
            :disabled="reviewing"
            @click="generateReview"
          >
            <Loader2
              v-if="reviewing"
              :size="14"
              class="spin"
            />
            <FileText
              v-else
              :size="14"
            />
            <span>{{ reviewing ? t('reports.review.working') : t('reports.review.generate') }}</span>
          </button>
          <button
            class="rp-schedule-btn"
            :disabled="scheduling"
            @click="scheduleReview"
          >
            <Loader2
              v-if="scheduling"
              :size="14"
              class="spin"
            />
            <CalendarRange
              v-else
              :size="14"
            />
            <span>{{ scheduling ? t('reports.review.scheduling') : t('reports.review.schedule') }}</span>
          </button>
        </div>
        <div
          v-if="reviewMsg"
          class="rp-generate-msg"
          :class="{ ok: reviewOk }"
        >
          {{ reviewMsg }}
        </div>
        <div
          v-if="scheduleMsg"
          class="rp-generate-msg ok"
        >
          {{ scheduleMsg }}
        </div>
      </section>

      <!-- 写作热力图 -->
      <section class="rp-section">
        <h2 class="rp-section-title">
          {{ t('reports.heatmap') }}
        </h2>
        <div
          v-if="!heatmapColumns.length"
          class="rp-empty"
        >
          {{ t('reports.noData') }}
        </div>
        <div
          v-else
          class="rp-heatmap-wrap"
        >
          <div
            class="rp-heatmap"
            role="img"
            :aria-label="t('reports.heatmap')"
          >
            <div
              v-for="(col, ci) in heatmapColumns"
              :key="ci"
              class="rp-heat-col"
            >
              <template
                v-for="(cell, ri) in col"
                :key="ri"
              >
                <div
                  v-if="cell"
                  class="rp-heat-cell"
                  :class="`lv-${level(cell.edited)}`"
                  :title="`${cell.date} · ${cell.edited}`"
                />
                <div
                  v-else
                  class="rp-heat-cell empty"
                />
              </template>
            </div>
          </div>
          <div class="rp-heat-legend">
            <span>{{ t('reports.less') }}</span>
            <span
              v-for="lv in 5"
              :key="lv"
              class="rp-heat-cell"
              :class="`lv-${lv - 1}`"
            />
            <span>{{ t('reports.more') }}</span>
          </div>
        </div>
      </section>

      <!-- 知识资产 Top10 -->
      <section class="rp-section">
        <h2 class="rp-section-title">
          <Link2 :size="15" />
          {{ t('reports.topLinked') }}
        </h2>
        <div
          v-if="!topLinked.length"
          class="rp-empty"
        >
          {{ t('reports.emptyGraph') }}
        </div>
        <ol
          v-else
          class="rp-toplist"
        >
          <li
            v-for="(item, idx) in topLinked"
            :key="item.path"
          >
            <button
              class="rp-topitem"
              @click="openFile(item.path)"
            >
              <span class="rp-toprank">{{ idx + 1 }}</span>
              <span
                class="rp-toptitle"
                :title="item.path"
              >{{ item.title }}</span>
              <span class="rp-toplinks">{{ item.links }} {{ t('reports.links') }}</span>
            </button>
          </li>
        </ol>
      </section>

      <!-- 那年今日 -->
      <section
        v-if="onThisDay.length"
        class="rp-section"
      >
        <h2 class="rp-section-title">
          <History :size="15" />
          {{ t('reports.onThisDay') }}
        </h2>
        <div class="rp-memory-list">
          <button
            v-for="f in onThisDay"
            :key="f"
            class="rp-memory-chip"
            :title="f"
            @click="openFile(f)"
          >
            {{ f.split('/').pop()?.replace(/\.md$/i, '') }}
          </button>
        </div>
      </section>
    </template>
    <WeeklyReportModal
      :visible="weeklyVisible"
      @close="weeklyVisible = false"
    />
  </div>
</template>

<style scoped>
.reports-view {
  flex: 1;
  overflow-y: auto;
  background: var(--bg-content);
  padding: var(--space-6) var(--space-8);
}

.rp-banner {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-5);
}
.rp-banner-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-md);
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
}
.rp-title {
  font-size: var(--text-2xl);
  color: var(--text-primary);
}
.rp-sub {
  font-size: var(--text-sm);
  color: var(--text-secondary);
}

.rp-error,
.rp-empty {
  padding: var(--space-4);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  margin-bottom: var(--space-4);
}
.rp-error {
  color: var(--error);
}

.rp-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-6);
}
.rp-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}
.rp-card-icon {
  flex-shrink: 0;
}
.rp-card-icon.week { color: #3b82f6; }
.rp-card-icon.month { color: #a855f7; }
.rp-card-icon.active { color: #22c55e; }
.rp-card-icon.streak { color: #f97316; }
.rp-card-value {
  font-size: var(--text-xl);
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1.1;
}
.rp-card-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.rp-section {
  margin-bottom: var(--space-6);
}
.rp-section-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-lg);
  color: var(--text-primary);
  margin-bottom: var(--space-3);
}

.rp-heatmap-wrap {
  padding: var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow-x: auto;
}
.rp-heatmap {
  display: flex;
  gap: 3px;
  width: max-content;
}
.rp-heat-col {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.rp-heat-cell {
  width: 12px;
  height: 12px;
  border-radius: 2px;
  background: var(--bg-hover);
}
.rp-heat-cell.empty {
  background: transparent;
}
.rp-heat-cell.lv-0 { background: var(--bg-hover); }
.rp-heat-cell.lv-1 { background: #9be29b; }
.rp-heat-cell.lv-2 { background: #4fc26a; }
.rp-heat-cell.lv-3 { background: #2f9e44; }
.rp-heat-cell.lv-4 { background: #1d6f34; }

.rp-heat-legend {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.rp-toplist {
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: 0;
  margin: 0;
}
.rp-topitem {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  text-align: left;
  transition: border-color var(--transition-fast);
}
.rp-topitem:hover {
  border-color: var(--border-accent);
}
.rp-toprank {
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-active);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  font-weight: 700;
  flex-shrink: 0;
}
.rp-toptitle {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
}
.rp-toplinks {
  font-size: var(--text-xs);
  color: var(--text-muted);
  flex-shrink: 0;
}

.rp-banner-text {
  flex: 1;
  min-width: 0;
}
.rp-weekly-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  background: var(--bg-card);
  color: var(--text-secondary);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-size: var(--text-sm);
  font-weight: 600;
}
.rp-weekly-btn:hover {
  color: var(--text-primary);
}
.rp-banner-actions {
  flex-shrink: 0;
}
.rp-generate-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  background: var(--accent);
  color: var(--accent-contrast);
  border: none;
  border-radius: var(--radius-md);
  font-size: var(--text-sm);
  font-weight: 600;
  transition: opacity var(--transition-fast);
}
.rp-generate-btn:hover:not(:disabled) {
  opacity: 0.9;
}
.rp-generate-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.rp-review-actions {
  display: flex;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.rp-schedule-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  background: var(--bg-card);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-size: var(--text-sm);
  transition: border-color var(--transition-fast);
}
.rp-schedule-btn:hover:not(:disabled) {
  border-color: var(--border-accent);
}
.rp-schedule-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.rp-review-desc {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  margin-bottom: var(--space-3);
}
.rp-generate-msg {
  padding: var(--space-2) var(--space-3);
  margin-bottom: var(--space-4);
  border-radius: var(--radius-md);
  background: rgba(245, 158, 11, 0.12);
  color: var(--warning);
  font-size: var(--text-sm);
}
.rp-generate-msg.ok {
  background: rgba(34, 197, 94, 0.12);
  color: var(--success);
}
/* 周报异步任务进度条（与 CompileView 的 task-bar 同规格） */
.task-bar {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-bottom: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-card);
}
.task-bar-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  color: var(--text-secondary);
}
.task-bar-icon {
  color: var(--accent);
  flex-shrink: 0;
}
.task-bar-message {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-bar-pct {
  font-variant-numeric: tabular-nums;
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.task-cancel-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-window);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
}
.task-cancel-btn:hover:not(:disabled) {
  color: var(--danger, #ef4444);
  border-color: var(--danger, #ef4444);
}
.task-cancel-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.task-track {
  height: 4px;
  border-radius: 2px;
  background: var(--bg-input);
  overflow: hidden;
}
.task-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 2px;
  transition: width 0.2s ease;
}
.task-fill.indeterminate {
  animation: task-pulse 1.4s ease-in-out infinite;
}
@keyframes task-pulse {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 1;
  }
}
.rp-memory-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.rp-memory-chip {
  padding: var(--space-1) var(--space-3);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--text-secondary);
  font-size: var(--text-sm);
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color var(--transition-fast), border-color var(--transition-fast);
}
.rp-memory-chip:hover {
  color: var(--accent);
  border-color: var(--border-accent);
}
.spin {
  animation: rp-spin 1s linear infinite;
}
@keyframes rp-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
