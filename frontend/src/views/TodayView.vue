<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowRight, FileCheck2, CheckCircle2, CircleAlert, Clock3, FileText, Plus, RefreshCw, Sparkles, X, BookOpen, MessageSquarePlus, Bot, Loader2 } from '@lucide/vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkLog } from '@/composables/useWorkLog'
import { renderBriefingHtml, useBriefing } from '@/composables/useBriefing'
import { isImeComposing } from '@/utils/ime'
import { isCarryoverTask } from '@/utils/workbench'
import { defaultTaskProject, GENERAL_PROJECT_FOLDER } from '@/utils/defaultTaskProject'
import TaskCard from '@/components/workbench/TaskCard.vue'
import InterviewReview from '@/components/workbench/InterviewReview.vue'
import QuickAccessCard from '@/components/workbench/QuickAccessCard.vue'
import TodayInsights from '@/components/workbench/TodayInsights.vue'

const store = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const route = useRoute()
const { t, locale } = useI18n()
const { openTodayWorkLog } = useWorkLog()
const { briefingText, briefingLoading, briefingError, restoreBriefing, generateBriefing } = useBriefing()
const briefingHtml = computed(() => briefingText.value ? renderBriefingHtml(briefingText.value) : '')
const filter = ref('all')
const creating = ref(false)
const newTitle = ref('')
const newType = ref('US')
const newProject = ref(GENERAL_PROJECT_FOLDER)
const projectSelected = ref(false)
const suggestedProject = computed(() => defaultTaskProject(store.projects, store.tasks, store.today))
const otherProjects = computed(() => store.projects.filter(project => project.folder !== GENERAL_PROJECT_FOLDER))
const newDue = ref(store.today)
const createError = ref('')
let formGeneration = 0
watch(() => workspace.currentWorkspace?.path, () => {
  formGeneration++
  filter.value = 'all'
  creating.value = false
  newTitle.value = ''
  newType.value = 'US'
  projectSelected.value = false
  newProject.value = suggestedProject.value
  newDue.value = store.today
  createError.value = ''
}, { flush: 'sync' })
// 简报缓存按 工作区+日期 隔离：换工作区或跨日自动恢复为当日缓存（可能为空）
watch(() => [workspace.currentWorkspace?.path, store.today], () => {
  restoreBriefing(store.today)
}, { immediate: true })
watch(() => [suggestedProject.value, ...store.projects.map(project => project.folder)], () => {
  const selectionExists = newProject.value === GENERAL_PROJECT_FOLDER || store.projects.some(project => project.folder === newProject.value)
  if (!projectSelected.value || !selectionExists) {
    projectSelected.value = false
    newProject.value = suggestedProject.value
  }
}, { immediate: true, flush: 'sync' })
watch(() => store.today, (day, previous) => {
  if (newDue.value === previous) newDue.value = day
})
watch(() => [route.path, route.query.action, workspace.hasWorkspace], () => {
  if (route.path !== '/today' || route.query.action !== 'new-task' || !workspace.hasWorkspace) return
  creating.value = true
  void router.replace({ path: '/today', query: { ...route.query, action: undefined } })
}, { immediate: true })
const greeting = computed(() => {
  const hour = new Date().getHours()
  return hour < 12 ? t('today.greeting.morning') : hour < 18 ? t('today.greeting.afternoon') : t('today.greeting.evening')
})
const dateLabel = computed(() => new Date(`${store.today}T12:00:00`).toLocaleDateString(locale.value, { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' }))
const completed = computed(() => store.todayTasks.filter(task => task.completed).length)
const completionRate = computed(() => store.todayTasks.length ? Math.round(completed.value / store.todayTasks.length * 100) : 0)
const carryovers = computed(() => store.todayTasks.filter(task => isCarryoverTask(task, store.today)).length)
const visibleTasks = computed(() => filter.value === 'blocked' ? store.blockers : store.todayTasks.filter(task => filter.value === 'all' || task.type === filter.value))
const taskFilters = computed(() => [
  { value: 'all', label: t('today.filter.all') },
  { value: 'US', label: t('today.filter.us') },
  { value: 'DTS', label: t('today.filter.dts') },
  { value: '额外', label: t('today.filter.extra') },
  { value: 'blocked', label: t('today.filter.blocked') },
])

function openReport() { window.dispatchEvent(new CustomEvent('notevault:daily-report')) }
function openFile(path: string) { workspace.openFile(path); void router.push({ path: '/editor', query: { file: path } }) }
function openAgentLog() { openFile(`Daily/Agent/${store.today}.md`) }
async function addTask() {
  if (!newTitle.value.trim() || store.busy) return
  const generation = formGeneration
  createError.value = ''
  try {
    if (newProject.value !== GENERAL_PROJECT_FOLDER && !store.projects.some(project => project.folder === newProject.value)) throw new Error(t('today.projectChanged'))
    await store.addTask(newProject.value, newTitle.value, newType.value, newDue.value)
    if (generation !== formGeneration) return
    newTitle.value = ''
    creating.value = false
  } catch (cause) {
    if (generation === formGeneration) createError.value = cause instanceof Error ? cause.message : String(cause)
  }
}
function onAddEnter(event: KeyboardEvent) { if (!isImeComposing(event)) { event.preventDefault(); void addTask() } }
function showBlockers() { filter.value = 'blocked'; document.getElementById('today-tasks')?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }
</script>

<template>
  <div class="today-view">
    <div class="today-content">
      <header class="today-header">
        <div>
          <p class="eyebrow">
            TODAY · {{ workspace.currentWorkspace?.name || t('today.defaultWorkspaceName') }}
          </p><h1>{{ greeting }}{{ t('today.greetingSuffix') }}</h1><p class="date-line">
            {{ dateLabel }}<span
              class="index-state"
              :class="{ syncing: store.loading || store.busy, failed: !!store.error }"
            ><i />{{ store.error ? t('today.indexFailed') : store.loading || store.busy ? t('today.indexSyncing') : t('today.indexLive') }}</span>
          </p>
        </div>
        <div class="header-actions">
          <button
            type="button"
            class="soft-button"
            @click="openTodayWorkLog"
          >
            <FileCheck2 :size="15" />{{ t('today.viewLog') }}
          </button><button
            type="button"
            class="primary-button"
            :disabled="!workspace.hasWorkspace"
            data-testid="today-report"
            @click="openReport"
          >
            <FileText :size="15" />{{ t('today.genReport') }}
          </button>
        </div>
      </header>

      <div
        v-if="!workspace.hasWorkspace"
        class="notice"
      >
        {{ t('today.noWorkspaceTip') }}<button
          type="button"
          @click="router.push('/')"
        >
          {{ t('today.openWorkspace') }} <ArrowRight :size="13" />
        </button>
      </div>
      <div
        v-if="store.error"
        class="error-banner"
        role="alert"
      >
        {{ store.error }}<button
          type="button"
          :disabled="store.loading"
          @click="store.refresh()"
        >
          {{ t('today.reload') }}
        </button>
      </div>
      <div
        v-if="store.warnings.length"
        class="notice"
        role="status"
      >
        {{ store.warnings.join('；') }}
      </div>

      <section
        class="metrics"
        :aria-label="t('today.metricsAria')"
      >
        <div class="metric">
          <span class="metric-label"><CheckCircle2 :size="15" />{{ t('today.metricTasks') }}</span><div class="metric-value">
            {{ completed }}<small>{{ t('today.completedOf', { n: store.todayTasks.length }) }}</small><span class="rate">{{ completionRate }}%</span>
          </div><div class="metric-track">
            <span :style="{ width: `${completionRate}%` }" />
          </div>
        </div>
        <button
          type="button"
          class="metric"
          :class="{ danger: store.blockers.length }"
          @click="showBlockers"
        >
          <span class="metric-label"><CircleAlert :size="15" />{{ t('today.metricBlockers') }}</span><div class="metric-value">
            {{ store.blockers.length }}<small>{{ t('today.itemsToCoordinate') }}</small>
          </div><span class="metric-foot">{{ store.blockers.length ? t('today.blockersHint') : t('today.noBlockers') }}</span>
        </button>
        <button
          type="button"
          class="metric"
          @click="router.push('/learning?tab=review')"
        >
          <span class="metric-label"><BookOpen :size="15" />{{ t('today.metricReview') }}</span><div class="metric-value">
            {{ store.reviewQueue.length }}<small>{{ t('today.reviewUnit') }}</small>
          </div><span class="metric-foot">{{ store.reviewedToday ? t('today.reviewedToday', { n: store.reviewedToday }) : t('today.reviewHint') }}</span>
        </button>
        <button
          type="button"
          class="metric"
          @click="router.push('/reminders')"
        >
          <span class="metric-label"><Clock3 :size="15" />{{ t('today.metricReminders') }}</span><div class="metric-value">
            {{ store.reminderError ? '—' : store.dueReminders.length }}<small>{{ t('today.itemsPending') }}</small>
          </div><span class="metric-foot">{{ store.reminderError || t('today.remindersHint') }}</span>
        </button>
      </section>

      <div class="workday-grid">
        <div class="main-column">
          <section
            id="today-tasks"
            class="task-section"
          >
            <header class="section-header">
              <div><h2>{{ t('today.tasksTitle') }} <span>{{ visibleTasks.length }}</span></h2><p>{{ carryovers ? t('today.carryoverTip', { n: carryovers }) : t('today.focusTip') }}</p></div><div class="section-actions">
                <button
                  type="button"
                  class="refresh-button"
                  :aria-label="t('today.refreshAria')"
                  :disabled="store.loading"
                  @click="store.refresh()"
                >
                  <RefreshCw
                    :size="14"
                    :class="{ spinning: store.loading }"
                  />
                </button><button
                  type="button"
                  class="soft-button small"
                  :disabled="!workspace.hasWorkspace"
                  @click="creating = !creating"
                >
                  <Plus :size="14" />{{ t('today.newTask') }}
                </button>
              </div>
            </header>
            <div
              class="task-filters"
              :aria-label="t('today.filterAria')"
            >
              <button
                v-for="item in taskFilters"
                :key="item.value"
                type="button"
                :class="{ active: filter === item.value }"
                :aria-pressed="filter === item.value"
                @click="filter = item.value"
              >
                {{ item.label }}
              </button>
            </div>
            <div
              v-if="creating"
              class="new-task"
            >
              <div class="new-task-title">
                <span>{{ t('today.newTaskTitle') }}</span><button
                  type="button"
                  :aria-label="t('today.cancelNewTask')"
                  @click="creating = false"
                >
                  <X :size="15" />
                </button>
              </div>
              <input
                v-model="newTitle"
                type="text"
                :aria-label="t('today.taskTitleAria')"
                :placeholder="t('today.taskTitlePlaceholder')"
                :disabled="store.busy"
                @keydown.enter="onAddEnter"
              >
              <div class="new-task-fields">
                <select
                  v-model="newType"
                  :aria-label="t('today.typeAria')"
                >
                  <option>US</option><option>DTS</option><option value="额外">
                    {{ t('today.filter.extra') }}
                  </option><option value="todo">
                    {{ t('today.typeTodo') }}
                  </option>
                </select><select
                  v-model="newProject"
                  :aria-label="t('today.projectAria')"
                  @change="projectSelected = true"
                >
                  <option :value="GENERAL_PROJECT_FOLDER">
                    {{ t('today.generalProject') }}
                  </option><option
                    v-for="project in otherProjects"
                    :key="project.path"
                    :value="project.folder"
                  >
                    {{ project.name }}
                  </option>
                </select><input
                  v-model="newDue"
                  type="date"
                  :aria-label="t('today.dueAria')"
                ><button
                  type="button"
                  class="primary-button small"
                  :disabled="store.busy || !newTitle.trim()"
                  @click="addTask"
                >
                  {{ store.busy ? t('today.saving') : t('today.addTask') }}
                </button>
              </div>
              <p
                class="task-destination"
                data-testid="task-destination"
                role="status"
              >
                {{ t('today.savedTo') }} <span>{{ newProject }}/Tasks.md</span>
              </p>
              <p
                v-if="createError"
                role="alert"
                class="inline-error"
              >
                {{ createError }}
              </p>
            </div>
            <p
              v-if="store.loading && !store.lastUpdated"
              class="loading-state"
              role="status"
            >
              {{ t('today.loadingTasks') }}
            </p>
            <div
              v-else-if="!visibleTasks.length && !store.error"
              class="empty-tasks"
            >
              <CheckCircle2 :size="30" /><h3>{{ filter === 'blocked' ? t('today.emptyBlocked') : t('today.emptyTitle') }}</h3><p>{{ t('today.emptyHint') }}</p>
            </div>
            <div class="task-list">
              <TaskCard
                v-for="task in visibleTasks"
                :key="task.id"
                :task="task"
              />
            </div>
          </section>

          <section class="timeline-section">
            <header class="section-header">
              <div><h2><MessageSquarePlus :size="16" />{{ t('today.timelineTitle') }}</h2><p>{{ t('today.timelineTip') }}</p></div><span class="timeline-count">{{ t('today.recordCount', { n: store.progress.length }) }}</span>
            </header>
            <ol
              v-if="store.progress.length"
              class="timeline"
            >
              <li
                v-for="entry in store.progress"
                :key="entry.id"
              >
                <time>{{ entry.at.slice(11, 16) }}</time><div>
                  <button
                    type="button"
                    @click="openFile(entry.filePath)"
                  >
                    {{ entry.taskTitle }}<ArrowRight :size="12" />
                  </button><p>{{ entry.note }}</p><span v-if="entry.project">{{ entry.project }}</span>
                </div>
              </li>
            </ol>
            <p
              v-else
              class="timeline-empty"
            >
              {{ t('today.timelineEmpty') }}
            </p>
          </section>
        </div>

        <aside class="focus-column">
          <QuickAccessCard />
          <section class="report-card">
            <div class="report-icon">
              <FileText :size="21" />
            </div><p class="eyebrow">
              DAILY REPORT
            </p><h2>{{ t('today.reportTitle') }}</h2><p>{{ t('today.reportDesc') }}</p><div class="report-summary">
              <span>{{ t('today.reportDone', { n: completed }) }}</span><span>{{ t('today.reportProgress', { n: store.progress.length }) }}</span><span>{{ t('today.reportBlockers', { n: store.blockers.length }) }}</span>
            </div><button
              type="button"
              :disabled="!workspace.hasWorkspace"
              @click="openReport"
            >
              {{ t('today.previewReport') }}<ArrowRight :size="15" />
            </button><small><Sparkles :size="11" />{{ t('today.reportFootnote') }}</small>
          </section>
          <section
            class="briefing-card"
            data-testid="today-briefing"
          >
            <header>
              <h3><Sparkles :size="15" />{{ t('today.briefing.title') }}</h3>
              <button
                type="button"
                class="briefing-action"
                :disabled="briefingLoading || !workspace.hasWorkspace"
                data-testid="briefing-generate"
                @click="generateBriefing(store.today)"
              >
                <Loader2
                  v-if="briefingLoading"
                  :size="12"
                  class="spin"
                /><RefreshCw
                  v-else
                  :size="12"
                />{{ briefingText ? t('today.briefing.regenerate') : t('today.briefing.generate') }}
              </button>
            </header>
            <div
              v-if="briefingText"
              class="briefing-body"
              v-html="briefingHtml"
            />
            <p
              v-else-if="briefingError"
              class="briefing-error"
              role="alert"
            >
              {{ briefingError }}
            </p>
            <p
              v-else
              class="briefing-hint"
            >
              {{ t('today.briefing.hint') }}
            </p>
          </section>
          <section
            v-if="store.agentEvents.length"
            class="agent-card"
            data-testid="agent-card"
          >
            <header>
              <h3><Bot :size="15" />{{ t('today.agentTitle') }}</h3>
              <button
                type="button"
                class="agent-log-btn"
                @click="openAgentLog"
              >
                {{ t('today.agentViewLog') }} <ArrowRight :size="11" />
              </button>
            </header>
            <button
              v-for="(event, index) in store.agentEvents.slice(-6).slice().reverse()"
              :key="index + event.time + event.content"
              type="button"
              class="agent-item"
              :title="event.agent ? `${event.agent} · ${event.content}` : event.content"
              @click="openAgentLog"
            >
              <time>{{ event.time }}</time><span class="agent-content">{{ event.content }}</span><em
                v-if="event.agent"
              >{{ event.agent }}</em>
            </button>
            <p
              v-if="store.agentEvents.length > 6"
              class="agent-more"
            >
              {{ t('today.agentMore', { n: store.agentEvents.length - 6 }) }}
            </p>
          </section>
          <InterviewReview compact />
          <section class="reminder-card">
            <header>
              <h3><Clock3 :size="15" />{{ t('today.attention') }}</h3><button
                type="button"
                @click="router.push('/reminders')"
              >
                {{ t('today.all') }} <ArrowRight :size="12" />
              </button>
            </header><p
              v-if="store.reminderError"
              class="inline-error"
              role="status"
            >
              {{ store.reminderError }}
            </p><p
              v-else-if="!store.dueReminders.length"
              class="reminder-empty"
            >
              {{ t('today.noReminders') }}
            </p><button
              v-for="reminder in store.dueReminders.slice(0, 4)"
              :key="reminder.id"
              type="button"
              class="reminder-item"
              @click="reminder.filePath ? openFile(reminder.filePath) : router.push('/reminders')"
            >
              <i /><span>{{ reminder.content }}</span><time>{{ new Date(reminder.remindAt).toLocaleDateString(locale, { month: 'numeric', day: 'numeric' }) }}</time>
            </button>
          </section>
        </aside>
      </div>

      <TodayInsights />
    </div>
  </div>
</template>

<style scoped>
.today-view { flex: 1; overflow-y: auto; background: var(--bg-content); }
.today-content { max-width: 1280px; margin: 0 auto; padding: 20px 28px 20px; color: var(--text-primary); }
.today-header { display: flex; gap: 16px; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.eyebrow { font-size: 10px; letter-spacing: .12em; font-weight: 650; color: var(--text-muted); margin: 0 0 6px; }
h1 { font-size: 22px; font-weight: 600; letter-spacing: -.03em; margin: 0 0 8px; }
.date-line { display: flex; flex-wrap: wrap; gap: 12px; font-size: 11px; color: var(--text-muted); margin: 0; }
.index-state { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; }
.index-state i { width: 5px; height: 5px; border-radius: 50%; background: #63a581; }
.index-state.syncing i { background: #668ec6; animation: pulse 1.4s infinite; }
.index-state.failed i { background: #d06460; }
.header-actions, .section-actions { display: flex; gap: 8px; align-items: center; }
.soft-button, .primary-button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 8px 12px; border-radius: 7px; font-size: 12px; white-space: nowrap; border: 1px solid var(--border); }
.soft-button { color: var(--text-secondary); background: var(--bg-card); }
.primary-button { color: var(--text-inverse, #fff); background: var(--accent); border-color: var(--accent); }
.small { padding: 6px 9px; font-size: 11px; }
.notice, .error-banner { display: flex; align-items: center; flex-wrap: wrap; justify-content: space-between; gap: 8px; border-radius: 8px; padding: 10px 14px; margin-bottom: 14px; font-size: 12px; background: var(--bg-card); border: 1px solid var(--border); }
.notice button { display: inline-flex; align-items: center; gap: 4px; color: var(--accent); }
.error-banner, .inline-error { color: #cf5a56; }
.error-banner button { text-decoration: underline; }
.metrics { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 12px; margin-bottom: 18px; }
.metric { padding: 12px 14px; min-width: 0; border: 1px solid var(--border); border-radius: 10px; background: var(--bg-card); text-align: center; }
.metric-label { display: flex; gap: 6px; align-items: center; justify-content: center; font-size: 11px; color: var(--text-secondary); }
.metric-label svg { color: var(--text-muted); }
.metric-value { display: flex; align-items: baseline; justify-content: center; gap: 6px; font-size: 24px; font-weight: 600; margin-top: 8px; line-height: 1.3; }
.metric-value small { color: var(--text-muted); font-weight: 400; font-size: 11px; }
.rate { font-size: 11px; color: #559775; }
.metric-track { height: 3px; border-radius: 5px; background: var(--bg-hover); margin-top: 8px; overflow: hidden; }
.metric-track span { display: block; height: 100%; background: #65a687; }
.metric-foot { display: block; margin-top: 6px; color: var(--text-muted); font-size: 10px; }
.metric.danger .metric-value, .metric.danger svg { color: #cf5a56; }
.workday-grid { display: grid; grid-template-columns: minmax(0, 1fr) 360px; gap: 20px; align-items: start; }
.main-column, .focus-column { min-width: 0; }
.focus-column { display: flex; flex-direction: column; gap: 14px; max-width: 360px; width: 100%; }
.section-header { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
.section-header h2 { display: flex; gap: 7px; align-items: center; margin: 0; font-size: 15px; font-weight: 600; }
.section-header h2 span { font-size: 10px; color: var(--text-muted); background: var(--bg-hover); border-radius: 4px; padding: 2px 5px; font-weight: 400; }
.section-header p { font-size: 11px; color: var(--text-muted); margin: 4px 0 0; line-height: 1.5; }
.refresh-button { padding: 6px; color: var(--text-muted); }
.task-filters { display: flex; flex-wrap: wrap; gap: 4px; padding-bottom: 12px; }
.task-filters button { font-size: 11px; padding: 5px 9px; color: var(--text-muted); border-radius: 5px; }
.task-filters button.active { color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); }
.task-list { display: flex; flex-direction: column; gap: 8px; }
.empty-tasks { text-align: center; border: 1px dashed var(--border); border-radius: 10px; padding: 18px 14px; color: var(--text-muted); }
.empty-tasks h3 { color: var(--text-secondary); font-size: 13px; font-weight: 500; margin: 8px 0 5px; }
.empty-tasks p, .loading-state { font-size: 11px; color: var(--text-muted); }
.new-task { border: 1px solid var(--border-accent, var(--accent)); border-radius: 8px; padding: 12px; margin-bottom: 12px; background: var(--bg-card); }
.new-task-title { display: flex; justify-content: space-between; color: var(--text-secondary); font-size: 11px; margin-bottom: 10px; }
.new-task input, .new-task select { border: 1px solid var(--border); border-radius: 5px; padding: 7px; background: var(--bg-content); color: var(--text-primary); min-width: 0; font-size: 11px; }
.new-task>input { width: 100%; box-sizing: border-box; }
.new-task-fields { display: grid; grid-template-columns: 72px minmax(90px,1fr) 125px auto; gap: 6px; margin-top: 8px; }
.task-destination { margin: 8px 0 0; color: var(--text-muted); font-size: 11px; line-height: 1.5; overflow-wrap: anywhere; }
.task-destination span { color: var(--text-secondary); }
.inline-error { font-size: 11px; }
.timeline-section { margin-top: 18px; }
.timeline-count { font-size: 10px; color: var(--text-muted); white-space: nowrap; }
.timeline { list-style: none; padding: 4px 0 0; margin: 0; }
.timeline li { display: flex; gap: 14px; padding: 0 0 14px; position: relative; }
.timeline time { font-size: 10px; color: var(--text-muted); min-width: 34px; padding-top: 3px; }
.timeline li>div { position: relative; border-left: 1px solid var(--border); padding-left: 14px; flex: 1; min-width: 0; }
.timeline li>div::before { content: ''; position: absolute; width: 5px; height: 5px; top: 6px; left: -3px; background: #79ab91; border-radius: 50%; }
.timeline button { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-secondary); text-align: left; }
.timeline p { font-size: 11px; color: var(--text-muted); line-height: 1.6; margin: 4px 0; overflow-wrap: anywhere; }
.timeline li span { font-size: 10px; color: var(--text-muted); }
.timeline-empty { font-size: 11px; color: var(--text-muted); padding: 10px 0; }
.report-card { padding: 16px 18px; border: 1px solid color-mix(in srgb, #78a28e 30%, var(--border)); border-radius: 12px; background: linear-gradient(145deg, color-mix(in srgb, #8bbca2 10%, var(--bg-card)), var(--bg-card)); }
.report-icon { display: flex; align-items: center; justify-content: center; width: 32px; height: 32px; background: color-mix(in srgb, #7dab92 12%, transparent); color: #689c7f; border-radius: 8px; margin-bottom: 12px; }
.report-card .eyebrow { font-size: 9px; }
.report-card h2 { margin: 0; font-size: 16px; font-weight: 550; }
.report-card>p:not(.eyebrow) { font-size: 11px; line-height: 1.6; color: var(--text-muted); margin: 6px 0 12px; }
.report-summary { display: flex; flex-wrap: wrap; gap: 10px; font-size: 10px; color: var(--text-secondary); margin-bottom: 14px; }
.report-card>button { display: flex; align-items: center; justify-content: space-between; width: 100%; color: var(--text-primary); font-size: 11px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 7px; padding: 9px 12px; }
.report-card>small { display: flex; justify-content: center; gap: 5px; align-items: center; font-size: 10px; color: var(--text-muted); margin-top: 8px; }
.agent-card { padding: 14px 16px; border: 1px solid color-mix(in srgb, #78a2c8 30%, var(--border)); border-radius: 12px; background: linear-gradient(145deg, color-mix(in srgb, #8bb4ca 8%, var(--bg-card)), var(--bg-card)); }
.briefing-card { display: grid; gap: 10px; padding: 14px 16px; border: 1px solid color-mix(in srgb, var(--accent) 28%, var(--border)); border-radius: 12px; background: color-mix(in srgb, var(--accent) 5%, var(--bg-card)); }
.briefing-card header { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.briefing-card h3 { display: inline-flex; align-items: center; gap: 6px; margin: 0; font-size: 13px; color: var(--text-primary); }
.briefing-action { display: inline-flex; align-items: center; gap: 5px; color: var(--accent); font-size: 11px; white-space: nowrap; }
.briefing-action:disabled { opacity: .5; cursor: default; }
.briefing-body { font-size: 12px; line-height: 1.8; color: var(--text-secondary); overflow-wrap: anywhere; }
.briefing-body :deep(ul), .briefing-body :deep(ol) { margin: 0; padding-left: 18px; display: grid; gap: 4px; }
.briefing-body :deep(p) { margin: 0; }
.briefing-body :deep(pre) { white-space: pre-wrap; background: var(--bg-content); border-radius: 6px; padding: 8px; }
.briefing-error { margin: 0; color: #cf5a56; font-size: 11px; overflow-wrap: anywhere; }
.briefing-hint { margin: 0; color: var(--text-muted); font-size: 11px; }
.spin { animation: briefing-spin 1s linear infinite; }
@keyframes briefing-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
.agent-card header { display: flex; align-items: center; margin-bottom: 6px; }
.agent-card h3 { margin: 0; font-size: 13px; font-weight: 600; display: inline-flex; align-items: center; gap: 6px; flex: 1; }
.agent-log-btn { display: inline-flex; align-items: center; gap: 4px; font-size: 10px; color: var(--text-muted); }
.agent-log-btn:hover { color: var(--text-primary); }
.agent-item { display: flex; align-items: baseline; gap: 8px; width: 100%; text-align: left; font-size: 11px; padding: 4px 0; color: var(--text-secondary); }
.agent-item:hover { color: var(--text-primary); }
.agent-item time { font-size: 10px; color: var(--text-muted); min-width: 34px; flex-shrink: 0; }
.agent-item .agent-content { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.agent-item em { font-style: normal; font-size: 9px; color: var(--text-muted); border: 1px solid var(--border); border-radius: 4px; padding: 1px 5px; white-space: nowrap; flex-shrink: 0; }
.agent-more { font-size: 10px; color: var(--text-muted); margin: 6px 0 0; }
.reminder-card { background: var(--bg-card); border: 1px solid var(--border); padding: 14px 16px; border-radius: 12px; }
.reminder-card header, .reminder-card h3, .reminder-card header button { display: flex; align-items: center; gap: 6px; }
.reminder-card header { justify-content: space-between; }
.reminder-card h3 { font-size: 12px; margin: 0; font-weight: 550; }
.reminder-card header button { font-size: 10px; color: var(--text-muted); }
.reminder-empty { margin: 12px 0 2px; font-size: 11px; color: var(--text-muted); }
.reminder-item { display: flex; align-items: baseline; gap: 7px; font-size: 11px; text-align: left; width: 100%; margin-top: 10px; color: var(--text-secondary); }
.reminder-item i { width: 4px; height: 4px; border-radius: 50%; background: #c8a061; flex-shrink: 0; }
.reminder-item span { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.reminder-item time { font-size: 10px; color: var(--text-muted); white-space: nowrap; }
button:disabled { opacity: .5; cursor: wait; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.spinning { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@keyframes pulse { 50% { opacity: .3; } }
@media (max-width: 1200px) { .today-content { padding: 16px 18px; } .today-header { align-items: flex-start; } .header-actions { flex-wrap: wrap; justify-content: end; } .workday-grid { gap: 14px; } .metric { padding: 10px 12px; } .metric-foot { line-height: 1.6; } }
@media (max-width: 980px) { .workday-grid { grid-template-columns: 1fr; } .focus-column { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); align-items: start; max-width: none; } .metrics { grid-template-columns: repeat(2,minmax(0,1fr)); } .reminder-card { grid-column: 1/-1; } .agent-card { grid-column: 1/-1; } }
@media (max-width: 620px) { .today-content { padding: 14px 12px; } .today-header { flex-direction: column; } .header-actions { justify-content: start; } h1 { font-size: 20px; } .focus-column { display: flex; } .new-task-fields { grid-template-columns: 1fr 1fr; } .metrics { gap: 8px; } .metric-value { font-size: 22px; } }
</style>
