<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, CalendarDays, CheckCircle2, CircleAlert, Clock3, FileText, Plus, RefreshCw, Sparkles, X, BookOpen, MessageSquarePlus } from '@lucide/vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { useDailyNote } from '@/composables/useDailyNote'
import { isImeComposing } from '@/utils/ime'
import { isCarryoverTask } from '@/utils/workbench'
import TaskCard from '@/components/workbench/TaskCard.vue'
import InterviewReview from '@/components/workbench/InterviewReview.vue'

const store = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const { openTodayNote } = useDailyNote()
const filter = ref('all')
const creating = ref(false)
const newTitle = ref('')
const newType = ref('US')
const newProject = ref('')
const newDue = ref(store.today)
const createError = ref('')
let formGeneration = 0
watch(() => workspace.currentWorkspace?.path, () => {
  formGeneration++
  filter.value = 'all'
  creating.value = false
  newTitle.value = ''
  newType.value = 'US'
  newProject.value = ''
  newDue.value = store.today
  createError.value = ''
}, { flush: 'sync' })
watch(() => store.today, (day, previous) => {
  if (newDue.value === previous) newDue.value = day
})
const greeting = new Date().getHours() < 12 ? '早安' : new Date().getHours() < 18 ? '午安' : '晚安'
const dateLabel = computed(() => new Date(`${store.today}T12:00:00`).toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' }))
const completed = computed(() => store.todayTasks.filter(task => task.completed).length)
const completionRate = computed(() => store.todayTasks.length ? Math.round(completed.value / store.todayTasks.length * 100) : 0)
const carryovers = computed(() => store.todayTasks.filter(task => isCarryoverTask(task, store.today)).length)
const visibleTasks = computed(() => filter.value === 'blocked' ? store.blockers : store.todayTasks.filter(task => filter.value === 'all' || task.type === filter.value))

function openReport() { window.dispatchEvent(new CustomEvent('notevault:daily-report')) }
function openFile(path: string) { workspace.openFile(path); void router.push({ path: '/editor', query: { file: path } }) }
async function addTask() {
  if (!newTitle.value.trim() || store.busy) return
  const generation = formGeneration
  createError.value = ''
  try {
    if (newProject.value && !store.projects.some(project => project.folder === newProject.value)) throw new Error('项目已变更，请重新选择归属项目')
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
            TODAY · {{ workspace.currentWorkspace?.name || '我的工作台' }}
          </p><h1>{{ greeting }}，把今天安排好。</h1><p class="date-line">
            {{ dateLabel }}<span
              class="index-state"
              :class="{ syncing: store.loading || store.busy, failed: !!store.error }"
            ><i />{{ store.error ? '索引暂不可用' : store.loading || store.busy ? '正在同步进展' : '本地索引实时' }}</span>
          </p>
        </div>
        <div class="header-actions">
          <button
            type="button"
            class="soft-button"
            @click="openTodayNote"
          >
            <CalendarDays :size="15" />今日日记
          </button><button
            type="button"
            class="primary-button"
            :disabled="!workspace.hasWorkspace"
            data-testid="today-report"
            @click="openReport"
          >
            <FileText :size="15" />生成日报
          </button>
        </div>
      </header>

      <div
        v-if="!workspace.hasWorkspace"
        class="notice"
      >
        打开一个工作区，开始管理项目、任务和学习进展。<button
          type="button"
          @click="router.push('/')"
        >
          打开工作区 <ArrowRight :size="13" />
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
          重新读取
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
        aria-label="今日概览"
      >
        <div class="metric">
          <span class="metric-label"><CheckCircle2 :size="15" />今日任务</span><div class="metric-value">
            {{ completed }}<small>/ {{ store.todayTasks.length }} 完成</small><span class="rate">{{ completionRate }}%</span>
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
          <span class="metric-label"><CircleAlert :size="15" />阻塞卡点</span><div class="metric-value">
            {{ store.blockers.length }}<small>项待协调</small>
          </div><span class="metric-foot">{{ store.blockers.length ? '优先解决，让工作继续' : '当前没有阻塞事项' }}</span>
        </button>
        <button
          type="button"
          class="metric"
          @click="router.push('/learning?tab=review')"
        >
          <span class="metric-label"><BookOpen :size="15" />面试待复习</span><div class="metric-value">
            {{ store.reviewQueue.length }}<small>题 / 今日五题</small>
          </div><span class="metric-foot">{{ store.reviewedToday ? `今天已复习 ${store.reviewedToday} 题` : '留 10 分钟，巩固一个知识点' }}</span>
        </button>
        <button
          type="button"
          class="metric"
          @click="router.push('/reminders')"
        >
          <span class="metric-label"><Clock3 :size="15" />到期提醒</span><div class="metric-value">
            {{ store.reminderError ? '—' : store.dueReminders.length }}<small>项待处理</small>
          </div><span class="metric-foot">{{ store.reminderError || '把需要记住的事交给提醒' }}</span>
        </button>
      </section>

      <div class="workday-grid">
        <div class="main-column">
          <section
            id="today-tasks"
            class="task-section"
          >
            <header class="section-header">
              <div><h2>今日待办 <span>{{ visibleTasks.length }}</span></h2><p>{{ carryovers ? `${carryovers} 项昨日或逾期任务已顺延，优先处理。` : '专注一件事，完成一件事。' }}</p></div><div class="section-actions">
                <button
                  type="button"
                  class="refresh-button"
                  aria-label="刷新工作台"
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
                  <Plus :size="14" />新任务
                </button>
              </div>
            </header>
            <div
              class="task-filters"
              aria-label="任务类型筛选"
            >
              <button
                v-for="item in [{ value: 'all', label: '全部' }, { value: 'US', label: 'US 用户故事' }, { value: 'DTS', label: 'DTS 问题单' }, { value: '额外', label: '额外' }, { value: 'blocked', label: '阻塞' }]"
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
                <span>安排一个具体的行动</span><button
                  type="button"
                  aria-label="取消新任务"
                  @click="creating = false"
                >
                  <X :size="15" />
                </button>
              </div>
              <input
                v-model="newTitle"
                type="text"
                aria-label="任务标题"
                placeholder="例如：完成订单结算接口联调"
                :disabled="store.busy"
                @keydown.enter="onAddEnter"
              >
              <div class="new-task-fields">
                <select
                  v-model="newType"
                  aria-label="任务类型"
                >
                  <option>US</option><option>DTS</option><option>额外</option><option value="todo">
                    普通待办
                  </option>
                </select><select
                  v-model="newProject"
                  aria-label="归属项目"
                >
                  <option value="">
                    今日日记
                  </option><option
                    v-for="project in store.projects"
                    :key="project.path"
                    :value="project.folder"
                  >
                    {{ project.name }}
                  </option>
                </select><input
                  v-model="newDue"
                  type="date"
                  aria-label="任务截止日期"
                ><button
                  type="button"
                  class="primary-button small"
                  :disabled="store.busy || !newTitle.trim()"
                  @click="addTask"
                >
                  {{ store.busy ? '保存中' : '添加任务' }}
                </button>
              </div>
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
              正在读取今天的任务…
            </p>
            <div
              v-else-if="!visibleTasks.length && !store.error"
              class="empty-tasks"
            >
              <CheckCircle2 :size="30" /><h3>{{ filter === 'blocked' ? '没有阻塞的任务' : '给今天留一个明确的目标' }}</h3><p>新建任务，或在日记和项目笔记中勾选待办。</p>
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
              <div><h2><MessageSquarePlus :size="16" />进展时间线</h2><p>每一笔进展，都会成为今天日报的依据。</p></div><span class="timeline-count">{{ store.progress.length }} 条记录</span>
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
              完成一个小步骤时，在任务下「随手记一笔」。
            </p>
          </section>
        </div>

        <aside class="focus-column">
          <section class="report-card">
            <div class="report-icon">
              <FileText :size="21" />
            </div><p class="eyebrow">
              DAILY REPORT
            </p><h2>收好今天的进展</h2><p>完成了什么、推进到哪里、需要哪些协助，整理成一封清晰的日报。</p><div class="report-summary">
              <span>{{ completed }} 项完成</span><span>{{ store.progress.length }} 条进展</span><span>{{ store.blockers.length }} 处卡点</span>
            </div><button
              type="button"
              :disabled="!workspace.hasWorkspace"
              @click="openReport"
            >
              预览邮件日报<ArrowRight :size="15" />
            </button><small><Sparkles :size="11" />支持 Copilot 润色 · 自动归档</small>
          </section>
          <InterviewReview compact />
          <section class="reminder-card">
            <header>
              <h3><Clock3 :size="15" />需要留意</h3><button
                type="button"
                @click="router.push('/reminders')"
              >
                全部 <ArrowRight :size="12" />
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
              今天没有到期提醒，安心专注。
            </p><button
              v-for="reminder in store.dueReminders.slice(0, 4)"
              :key="reminder.id"
              type="button"
              class="reminder-item"
              @click="reminder.filePath ? openFile(reminder.filePath) : router.push('/reminders')"
            >
              <i /><span>{{ reminder.content }}</span><time>{{ new Date(reminder.remindAt).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }) }}</time>
            </button>
          </section>
        </aside>
      </div>
    </div>
  </div>
</template>

<style scoped>
.today-view { flex: 1; overflow-y: auto; background: var(--bg-content); }
.today-content { max-width: 1440px; margin: 0 auto; padding: 32px 36px 48px; color: var(--text-primary); }
.today-header { display: flex; gap: 20px; align-items: center; justify-content: space-between; margin-bottom: 26px; }
.eyebrow { font-size: 10px; letter-spacing: .12em; font-weight: 650; color: var(--text-muted); margin: 0 0 10px; }
h1 { font-size: 27px; font-weight: 600; letter-spacing: -.03em; margin: 0 0 12px; }
.date-line { display: flex; flex-wrap: wrap; gap: 15px; font-size: 12px; color: var(--text-muted); margin: 0; }
.index-state { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; }
.index-state i { width: 5px; height: 5px; border-radius: 50%; background: #63a581; }
.index-state.syncing i { background: #668ec6; animation: pulse 1.4s infinite; }
.index-state.failed i { background: #d06460; }
.header-actions, .section-actions { display: flex; gap: 9px; align-items: center; }
.soft-button, .primary-button { display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 10px 14px; border-radius: 8px; font-size: 12px; white-space: nowrap; border: 1px solid var(--border); }
.soft-button { color: var(--text-secondary); background: var(--bg-card); }
.primary-button { color: var(--text-inverse, #fff); background: var(--accent); border-color: var(--accent); }
.small { padding: 7px 10px; font-size: 11px; }
.notice, .error-banner { display: flex; align-items: center; flex-wrap: wrap; justify-content: space-between; gap: 10px; border-radius: 8px; padding: 12px 15px; margin-bottom: 18px; font-size: 12px; background: var(--bg-card); border: 1px solid var(--border); }
.notice button { display: inline-flex; align-items: center; gap: 4px; color: var(--accent); }
.error-banner, .inline-error { color: #cf5a56; }
.error-banner button { text-decoration: underline; }
.metrics { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); gap: 13px; margin-bottom: 30px; }
.metric { padding: 18px 19px; min-width: 0; border: 1px solid var(--border); border-radius: 12px; background: var(--bg-card); text-align: left; }
.metric-label { display: flex; gap: 7px; align-items: center; font-size: 12px; color: var(--text-secondary); }
.metric-label svg { color: var(--text-muted); }
.metric-value { display: flex; align-items: baseline; gap: 7px; font-size: 29px; font-weight: 600; margin-top: 12px; line-height: 1.4; }
.metric-value small { color: var(--text-muted); font-weight: 400; font-size: 11px; }
.rate { font-size: 11px; color: #559775; margin-left: auto; }
.metric-track { height: 3px; border-radius: 5px; background: var(--bg-hover); margin-top: 10px; overflow: hidden; }
.metric-track span { display: block; height: 100%; background: #65a687; }
.metric-foot { display: block; margin-top: 8px; color: var(--text-muted); font-size: 10px; }
.metric.danger .metric-value, .metric.danger svg { color: #cf5a56; }
.workday-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(270px, 32%); gap: 26px; align-items: start; }
.main-column, .focus-column { min-width: 0; }
.focus-column { display: flex; flex-direction: column; gap: 20px; }
.section-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
.section-header h2 { display: flex; gap: 8px; align-items: center; margin: 0; font-size: 16px; font-weight: 600; }
.section-header h2 span { font-size: 11px; color: var(--text-muted); background: var(--bg-hover); border-radius: 5px; padding: 2px 6px; font-weight: 400; }
.section-header p { font-size: 11px; color: var(--text-muted); margin: 7px 0 0; line-height: 1.6; }
.refresh-button { padding: 7px; color: var(--text-muted); }
.task-filters { display: flex; flex-wrap: wrap; gap: 5px; padding-bottom: 16px; }
.task-filters button { font-size: 11px; padding: 6px 11px; color: var(--text-muted); border-radius: 6px; }
.task-filters button.active { color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); }
.task-list { display: flex; flex-direction: column; gap: 10px; }
.empty-tasks { text-align: center; border: 1px dashed var(--border); border-radius: 12px; padding: 42px 16px; color: var(--text-muted); }
.empty-tasks h3 { color: var(--text-secondary); font-size: 14px; font-weight: 500; margin: 14px 0 9px; }
.empty-tasks p, .loading-state { font-size: 12px; color: var(--text-muted); }
.new-task { border: 1px solid var(--border-accent, var(--accent)); border-radius: 10px; padding: 15px; margin-bottom: 15px; background: var(--bg-card); }
.new-task-title { display: flex; justify-content: space-between; color: var(--text-secondary); font-size: 12px; margin-bottom: 12px; }
.new-task input, .new-task select { border: 1px solid var(--border); border-radius: 6px; padding: 8px; background: var(--bg-content); color: var(--text-primary); min-width: 0; font-size: 12px; }
.new-task>input { width: 100%; box-sizing: border-box; }
.new-task-fields { display: grid; grid-template-columns: 72px minmax(90px,1fr) 125px auto; gap: 7px; margin-top: 10px; }
.inline-error { font-size: 12px; }
.timeline-section { margin-top: 30px; }
.timeline-count { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.timeline { list-style: none; padding: 5px 0 0; margin: 0; }
.timeline li { display: flex; gap: 18px; padding: 0 0 20px; position: relative; }
.timeline time { font-size: 11px; color: var(--text-muted); min-width: 36px; padding-top: 4px; }
.timeline li>div { position: relative; border-left: 1px solid var(--border); padding-left: 18px; flex: 1; min-width: 0; }
.timeline li>div::before { content: ''; position: absolute; width: 5px; height: 5px; top: 7px; left: -3px; background: #79ab91; border-radius: 50%; }
.timeline button { display: inline-flex; align-items: center; gap: 7px; font-size: 12px; color: var(--text-secondary); text-align: left; }
.timeline p { font-size: 12px; color: var(--text-muted); line-height: 1.7; margin: 6px 0; overflow-wrap: anywhere; }
.timeline li span { font-size: 10px; color: var(--text-muted); }
.timeline-empty { font-size: 12px; color: var(--text-muted); padding: 15px 0; }
.report-card { padding: 23px; border: 1px solid color-mix(in srgb, #78a28e 30%, var(--border)); border-radius: 14px; background: linear-gradient(145deg, color-mix(in srgb, #8bbca2 10%, var(--bg-card)), var(--bg-card)); }
.report-icon { display: flex; align-items: center; justify-content: center; width: 39px; height: 39px; background: color-mix(in srgb, #7dab92 12%, transparent); color: #689c7f; border-radius: 11px; margin-bottom: 18px; }
.report-card .eyebrow { font-size: 9px; }
.report-card h2 { margin: 0; font-size: 18px; font-weight: 550; }
.report-card>p:not(.eyebrow) { font-size: 12px; line-height: 1.9; color: var(--text-muted); margin: 10px 0 16px; }
.report-summary { display: flex; flex-wrap: wrap; gap: 12px; font-size: 10px; color: var(--text-secondary); margin-bottom: 20px; }
.report-card>button { display: flex; align-items: center; justify-content: space-between; width: 100%; color: var(--text-primary); font-size: 12px; background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 11px 12px; }
.report-card>small { display: flex; justify-content: center; gap: 5px; align-items: center; font-size: 10px; color: var(--text-muted); margin-top: 12px; }
.reminder-card { background: var(--bg-card); border: 1px solid var(--border); padding: 20px; border-radius: 14px; }
.reminder-card header, .reminder-card h3, .reminder-card header button { display: flex; align-items: center; gap: 7px; }
.reminder-card header { justify-content: space-between; }
.reminder-card h3 { font-size: 13px; margin: 0; font-weight: 550; }
.reminder-card header button { font-size: 10px; color: var(--text-muted); }
.reminder-empty { margin: 20px 0 3px; font-size: 11px; color: var(--text-muted); }
.reminder-item { display: flex; align-items: baseline; gap: 8px; font-size: 12px; text-align: left; width: 100%; margin-top: 17px; color: var(--text-secondary); }
.reminder-item i { width: 4px; height: 4px; border-radius: 50%; background: #c8a061; flex-shrink: 0; }
.reminder-item span { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.reminder-item time { font-size: 10px; color: var(--text-muted); white-space: nowrap; }
button:disabled { opacity: .5; cursor: wait; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.spinning { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@keyframes pulse { 50% { opacity: .3; } }
@media (max-width: 1200px) { .today-content { padding: 26px 24px; } .today-header { align-items: flex-start; } .header-actions { flex-wrap: wrap; justify-content: end; } .workday-grid { gap: 18px; } .metric { padding: 15px; } .metric-foot { line-height: 1.7; } }
@media (max-width: 980px) { .workday-grid { grid-template-columns: 1fr; } .focus-column { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); align-items: start; } .metrics { grid-template-columns: repeat(2,minmax(0,1fr)); } .reminder-card { grid-column: 1/-1; } }
@media (max-width: 620px) { .today-content { padding: 20px 15px; } .today-header { flex-direction: column; } .header-actions { justify-content: start; } h1 { font-size: 23px; } .focus-column { display: flex; } .new-task-fields { grid-template-columns: 1fr 1fr; } .metrics { gap: 9px; } .metric-value { font-size: 25px; } }
</style>
