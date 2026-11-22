<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Calendar, Check, ChevronLeft, ChevronRight, Copy, FileText, RefreshCw, Save, Sparkles, X } from '@lucide/vue'
import { WorkbenchService } from '@/api'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { buildWeeklyReport, fetchMeetingsSafe, isoWeekWindow, plainReportText, type OutlookMeeting, type WeekWindow } from '@/utils/workbench'
import { requestCopilot } from '@/composables/useCopilotRequest'

// 工作周报弹窗：与日报同一套骨架（姓名/团队 → 自动拼装 → 可编辑 → 归档 → 复制/AI 润色），
// 数据窗口是一整周（完成/推进/阻塞/里程碑/下周计划），归档到 Daily/Reports/<week>-周报.md。
// 保存走显式按钮 + 后端 expectedContent 并发保护，不做自动保存队列（周报写一次，冲突面小）。
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: [] }>()
const workspace = useWorkspaceStore()
const workbench = useWorkbenchStore()
const { t } = useI18n()

const anchor = ref(workbench.today) // 周导航锚点：窗口由锚点所在 ISO 周决定
const draft = ref('')
const author = ref('')
const team = ref('')
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const error = ref('')
const copied = ref(false)
const savedText = ref('')
const meetings = ref<OutlookMeeting[]>([])

const window = computed<WeekWindow>(() => isoWeekWindow(anchor.value))
const archivePath = computed(() => `Daily/Reports/${window.value.week}-周报.md`)
const dirty = computed(() => loaded.value && draft.value !== savedText.value)
const weekRange = computed(() => `${window.value.weekStart} ~ ${window.value.weekEnd}`)

function profileKey(): string {
  return `notevault_report_profile:${workspace.currentWorkspace?.path || 'default'}`
}
function loadProfile() {
  author.value = ''
  team.value = ''
  try {
    const profile = JSON.parse(localStorage.getItem(profileKey()) || '{}')
    if (typeof profile.author === 'string') author.value = profile.author
    if (typeof profile.team === 'string') team.value = profile.team
  } catch {
    // 缺 UI 偏好不影响报告生成
  }
}
function saveProfile() {
  try {
    localStorage.setItem(profileKey(), JSON.stringify({ author: author.value, team: team.value }))
  } catch {
    // 配额满时静默：姓名/团队丢失不影响正文
  }
}
function generate(): string {
  return buildWeeklyReport({
    author: author.value,
    team: team.value,
    tasks: workbench.tasks,
    milestones: workbench.projects.flatMap(project => project.milestones ?? []),
    meetings: meetings.value,
    window: window.value,
  })
}

function shiftWeek(offset: number) {
  const [year, month, date] = anchor.value.split('-').map(Number)
  const next = new Date(year, month - 1, date + offset, 12)
  anchor.value = `${next.getFullYear()}-${String(next.getMonth() + 1).padStart(2, '0')}-${String(next.getDate()).padStart(2, '0')}`
}

async function loadReport() {
  const path = workspace.currentWorkspace?.path
  if (!path || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const week = window.value.week
    const stored = (await WorkbenchService.ReadWeeklyReport(path, week)) || ''
    savedText.value = stored
    // 本周会议摄取：失败静默降级为空段
    meetings.value = await fetchMeetingsSafe(`${window.value.weekStart} 00:00`, `${window.value.nextWeekStart} 00:00`)
    draft.value = stored || generate()
    loaded.value = true
  } catch (cause) {
    error.value = t('workbench.report.weeklyLoadFailed', { msg: cause instanceof Error ? cause.message : String(cause) })
  } finally {
    loading.value = false
  }
}

async function save() {
  const path = workspace.currentWorkspace?.path
  if (!path || saving.value || !loaded.value) return
  saving.value = true
  error.value = ''
  try {
    const week = window.value.week
    await WorkbenchService.SaveWeeklyReport(path, week, draft.value, savedText.value)
    savedText.value = draft.value
    saveProfile()
  } catch (cause) {
    error.value = t('workbench.report.saveFailed', { msg: cause instanceof Error ? cause.message : String(cause) })
  } finally {
    saving.value = false
  }
}

let copiedTimer: ReturnType<typeof setTimeout> | undefined
async function copyReport() {
  try {
    await navigator.clipboard.writeText(plainReportText(draft.value))
    copied.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copied.value = false
    }, 1600)
  } catch (cause) {
    error.value = t('workbench.report.copyFailed', { msg: cause instanceof Error ? cause.message : String(cause) })
  }
}

async function polish() {
  const path = workspace.currentWorkspace?.path
  if (!path || !draft.value.trim()) return
  const week = window.value.week
  const text = draft.value
  requestCopilot({
    source: 'weekly-report',
    workspacePath: path,
    context: text,
    prompt: t('workbench.report.weeklyPolishPrompt'),
    onApply: (content) => {
      if (workspace.currentWorkspace?.path !== path || window.value.week !== week) return
      draft.value = plainReportText(content)
    },
  })
}

function close() {
  emit('close')
}

watch(
  () => props.visible,
  (visible) => {
    if (visible) {
      loadProfile()
      anchor.value = workbench.today
      void loadReport()
    } else {
      loaded.value = false
      draft.value = ''
      savedText.value = ''
      error.value = ''
      if (dirty.value) void save().catch(() => undefined)
    }
  },
  { immediate: true }, // 已打开状态直接挂载（测试/路由恢复）也要加载
)
// 切周：重新读取对应周的归档（未保存的草稿会被覆盖，与日报「重新读取归档」同语义）
watch(anchor, () => {
  if (props.visible && loaded.value) void loadReport()
})
watch([author, team], saveProfile)
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible"
      class="report-overlay"
      @click.self="close"
    >
      <section
        class="report-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="weekly-report-title"
      >
        <header class="report-heading">
          <div>
            <span class="report-mark"><Calendar :size="20" /></span><div>
              <h2 id="weekly-report-title">
                {{ t('workbench.report.weeklyTitle') }}
              </h2><p>{{ t('workbench.report.weeklySubtitle') }}</p>
            </div>
          </div><button
            type="button"
            data-testid="close-weekly-report"
            class="icon-button"
            :aria-label="t('workbench.report.closeAria')"
            @click="close"
          >
            <X :size="18" />
          </button>
        </header>
        <div class="report-profile">
          <label>{{ t('workbench.report.authorLabel') }}<input
            v-model="author"
            type="text"
            :placeholder="t('workbench.report.authorPlaceholder')"
            :disabled="!loaded"
          ></label><label>{{ t('workbench.report.teamLabel') }}<input
            v-model="team"
            type="text"
            :placeholder="t('workbench.report.teamPlaceholder')"
            :disabled="!loaded"
          ></label>
          <div class="week-nav">
            <button
              type="button"
              class="icon-button"
              :aria-label="t('workbench.report.weekNavPrev')"
              :disabled="saving"
              @click="shiftWeek(-7)"
            >
              <ChevronLeft :size="15" />
            </button>
            <span
              class="week-label"
              :title="window.week"
            >{{ t('workbench.report.weekLabel', { weekNumber: window.weekNumber, range: weekRange }) }}</span>
            <button
              type="button"
              class="icon-button"
              :aria-label="t('workbench.report.weekNavNext')"
              :disabled="saving"
              @click="shiftWeek(7)"
            >
              <ChevronRight :size="15" />
            </button>
          </div>
          <button
            type="button"
            class="text-button"
            :disabled="!loaded || loading || saving"
            @click="draft = generate()"
          >
            <RefreshCw :size="13" />{{ t('workbench.report.regenerate') }}
          </button>
        </div>
        <p
          v-if="error"
          class="report-error"
          role="alert"
        >
          {{ error }}
        </p>
        <p
          v-if="loading && !loaded"
          class="report-loading"
          role="status"
        >
          {{ t('workbench.report.loading') }}
        </p>
        <textarea
          v-if="loaded"
          v-model="draft"
          class="report-draft"
          :aria-label="t('workbench.report.weeklyBodyAria')"
          spellcheck="false"
        />
        <div
          class="archive-status"
          role="status"
        >
          <span><Save
            v-if="saving || dirty"
            :size="12"
          /><Check
            v-else
            :size="12"
          />{{ saving ? t('workbench.report.saving') : !loaded ? t('workbench.report.waiting') : dirty ? t('workbench.report.unsaved') : t('workbench.report.archived') }}</span><span>{{ archivePath }}</span>
        </div>
        <footer class="report-footer">
          <button
            type="button"
            class="polish-button"
            data-testid="polish-weekly-report"
            :disabled="!loaded || saving || !draft.trim()"
            @click="polish"
          >
            <Sparkles :size="15" />{{ t('workbench.report.polish') }}
          </button><div>
            <button
              type="button"
              class="save-button"
              data-testid="save-weekly-report"
              :disabled="!loaded || saving || !dirty"
              @click="save"
            >
              <Save :size="14" />{{ t('workbench.report.archiveSave') }}
            </button><button
              type="button"
              class="copy-button"
              data-testid="copy-weekly-report"
              :disabled="!loaded || !draft.trim()"
              @click="copyReport"
            >
              <Check
                v-if="copied"
                :size="15"
              /><Copy
                v-else
                :size="15"
              />{{ copied ? t('workbench.report.copied') : t('workbench.report.copyAll') }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
/* 与 DailyReportModal 同一套对话框视觉（scoped 样式无法跨组件共享，就地复制） */
.report-overlay { position: fixed; inset: 0; z-index: 990; display: flex; align-items: center; justify-content: center; padding: 24px; background: rgba(15,20,24,.38); backdrop-filter: blur(4px); }
.report-dialog { display: flex; flex-direction: column; width: min(800px, 100%); max-height: 90vh; background: var(--bg-card); border: 1px solid var(--border); border-radius: 16px; box-shadow: 0 25px 80px #0003; color: var(--text-primary); overflow: hidden; }
.report-heading { display: flex; justify-content: space-between; gap: 16px; padding: 23px 26px 20px; border-bottom: 1px solid var(--border); }
.report-heading>div { display: flex; gap: 13px; align-items: center; }
.report-mark { display: flex; align-items: center; justify-content: center; width: 41px; height: 41px; border-radius: 11px; color: #5b83b8; background: color-mix(in srgb, #5b83b8 10%, transparent); }
.report-heading h2 { font-size: 18px; font-weight: 600; margin: 0 0 6px; }
.report-heading p { font-size: 11px; color: var(--text-muted); margin: 0; }
.icon-button { align-self: flex-start; padding: 5px; color: var(--text-muted); }
.report-profile { display: flex; align-items: end; flex-wrap: wrap; gap: 12px; padding: 18px 26px; }
.report-profile label { display: flex; flex-direction: column; gap: 7px; color: var(--text-muted); font-size: 11px; }
.report-profile input { border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; background: var(--bg-content); color: var(--text-primary); font-size: 12px; width: 150px; }
.report-profile label:nth-child(2) input { width: 200px; }
.text-button { display: flex; align-items: center; gap: 5px; font-size: 11px; color: var(--text-muted); margin-left: auto; padding: 9px 0; }
.week-nav { display: flex; align-items: center; gap: 6px; margin-left: auto; }
.week-label { font-size: 11px; color: var(--text-secondary); font-variant-numeric: tabular-nums; }
.week-nav .icon-button { align-self: center; }
.report-draft { margin: 0 26px; flex: 1; min-height: 280px; height: 420px; padding: 21px; border: 1px solid var(--border); border-radius: 9px; background: var(--bg-content); color: var(--text-primary); resize: vertical; font: inherit; font-size: 13px; line-height: 1.9; }
.report-draft:focus { outline: 1px solid var(--accent); }
.report-error { margin: 0 26px 14px; padding: 11px; border: 1px solid color-mix(in srgb, #cf5a56 30%, var(--border)); border-radius: 7px; color: #cf5a56; font-size: 12px; }
.report-loading { text-align: center; padding: 60px 20px; color: var(--text-muted); font-size: 13px; }
.archive-status { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; padding: 12px 26px 18px; font-size: 10px; color: var(--text-muted); }
.archive-status span:first-child { color: #609375; display: flex; align-items: center; gap: 5px; }
.report-footer { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; padding: 17px 26px; border-top: 1px solid var(--border); }
.report-footer>div { display: flex; gap: 9px; }
.report-footer button { display: inline-flex; align-items: center; justify-content: center; gap: 7px; font-size: 12px; padding: 10px 13px; border-radius: 7px; white-space: nowrap; }
.polish-button { color: var(--accent); background: color-mix(in srgb, var(--accent) 8%, transparent); }
.save-button { color: var(--text-secondary); border: 1px solid var(--border); }
.copy-button { color: var(--text-inverse, white); background: var(--accent); }
button:disabled { opacity: .45; cursor: wait; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
@media (max-width: 640px) { .report-overlay { padding: 10px; } .report-heading, .report-profile, .report-footer { padding: 16px; } .report-draft { margin: 0 16px; min-height: 200px; } .report-profile label { flex: 1; min-width: 0; } .report-profile input, .report-profile label:nth-child(2) input { width: 100%; box-sizing: border-box; } .archive-status { padding: 12px 16px; } }
</style>
