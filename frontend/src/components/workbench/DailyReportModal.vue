<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Check, Copy, FileText, RefreshCw, Save, Sparkles, Undo2, X } from '@lucide/vue'
import { WorkbenchService } from '@/api'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { buildDailyReport, plainReportText } from '@/utils/workbench'
import { requestCopilot } from '@/composables/useCopilotRequest'
import { isImeComposing } from '@/utils/ime'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: [] }>()
const workspace = useWorkspaceStore()
const workbench = useWorkbenchStore()
const draft = ref('')
const author = ref('')
const team = ref('')
const previousDraft = ref('')
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const error = ref('')
const copied = ref(false)
const savedText = ref('')
const day = ref(workbench.today)
const dialog = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)
let sessionPath = ''
let session = 0
let generation = 0
let autosave: ReturnType<typeof setTimeout> | undefined
let copiedTimer: ReturnType<typeof setTimeout> | undefined
let saveQueue: Promise<void> = Promise.resolve()
let archiveRevision = { text: '', pending: 0 }
let previousFocus: HTMLElement | null = null
let keepOnOpen = false
const archivePath = computed(() => `Daily/Reports/${day.value}-日报.md`)
const dirty = computed(() => loaded.value && draft.value !== savedText.value)

function profileKey() { return `notevault_report_profile:${sessionPath}` }
function loadProfile() {
  author.value = ''; team.value = ''
  try {
    const profile = JSON.parse(localStorage.getItem(profileKey()) || '{}')
    if (typeof profile.author === 'string') author.value = profile.author
    if (typeof profile.team === 'string') team.value = profile.team
  } catch { /* A missing UI preference does not affect Markdown reports. */ }
}
function generate() {
  return buildDailyReport({ date: day.value, author: author.value, team: team.value, tasks: workbench.tasks })
}

/** Serialize disk writes; every queued write captures its original workspace/day. */
function persist(): Promise<void> {
  clearTimeout(autosave)
  if (!loaded.value || !sessionPath) return saveQueue
  if (!dirty.value && !saving.value) return saveQueue
  const text = draft.value
  const path = sessionPath
  const date = day.value
  const currentSession = session
  const revision = archiveRevision
  revision.pending++
  saving.value = true
  saveQueue = saveQueue.catch(() => undefined).then(async () => {
    try {
      if (text !== revision.text) await WorkbenchService.SaveDailyReport(path, date, text, revision.text)
      revision.text = text
      if (currentSession === session) {
        savedText.value = text
        error.value = ''
      }
      if (path === workspace.currentWorkspace?.path) workspace.incrementFileTreeVersion()
    } catch (cause) {
      if (currentSession === session) error.value = `保存失败：${cause instanceof Error ? cause.message : String(cause)}`
      throw cause
    } finally {
      revision.pending--
      if (currentSession === session) saving.value = revision.pending > 0
    }
  })
  return saveQueue
}

async function loadReport() {
  const path = workspace.currentWorkspace?.path
  if (!path) { error.value = '请先打开工作区'; return }
  if (keepOnOpen && path === sessionPath) { keepOnOpen = false; return }
  // A failed save stays editable when the same dialog is reopened.
  if (loaded.value && sessionPath === path && day.value === workbench.today && (dirty.value || saving.value)) return
  if (dirty.value) void persist().catch(() => undefined)
  const request = ++generation
  session++
  const revision = { text: '', pending: 0 }
  archiveRevision = revision
  sessionPath = path
  day.value = workbench.today
  loaded.value = false
  loading.value = true
  saving.value = false
  error.value = ''
  previousDraft.value = ''
  loadProfile()
  try {
    const [stored] = await Promise.all([WorkbenchService.ReadDailyReport(path, day.value), workbench.refresh()])
    if (request !== generation || path !== workspace.currentWorkspace?.path || !props.visible) return
    if (!stored && workbench.error) throw new Error(workbench.error)
    revision.text = stored
    draft.value = stored || generate()
    savedText.value = stored
    loaded.value = true
    if (!stored) await persist()
    await nextTick()
    textarea.value?.focus()
  } catch (cause) {
    if (request === generation && !error.value) error.value = `日报读取失败：${cause instanceof Error ? cause.message : String(cause)}`
  } finally {
    if (request === generation) loading.value = false
  }
}

watch([draft, savedText], () => {
  clearTimeout(autosave)
  if (!loaded.value || !dirty.value) return
  autosave = setTimeout(() => { void persist().catch(() => undefined) }, 500)
}, { flush: 'sync' })

watch(() => [props.visible, workspace.currentWorkspace?.path] as const, ([visible]) => {
  if (visible) {
    previousFocus = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', onKeydown)
    void loadReport()
  } else {
    generation++
    loading.value = false
    window.removeEventListener('keydown', onKeydown)
    previousFocus?.focus()
  }
}, { immediate: true })

async function close() {
  const currentSession = session
  if (dirty.value || saving.value) {
    try { await persist() } catch { return }
  }
  if (currentSession !== session) return
  emit('close')
}
function onKeydown(event: KeyboardEvent) {
  if (isImeComposing(event)) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); void close() }
  if (event.key !== 'Tab' || !dialog.value) return
  const elements = Array.from(dialog.value.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), [tabindex="0"]'))
  const first = elements[0], last = elements.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
async function copyReport() {
  try {
    await navigator.clipboard.writeText(plainReportText(draft.value))
    copied.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copied.value = false }, 2200)
  } catch (cause) { error.value = `复制失败，请选中正文手动复制：${cause instanceof Error ? cause.message : String(cause)}` }
}
function updateSubject() {
  try { localStorage.setItem(profileKey(), JSON.stringify({ author: author.value, team: team.value })) } catch { /* Preference only. */ }
  const subject = generate().split('\n')[0]
  draft.value = /^主题[：:]/.test(draft.value) ? draft.value.replace(/^主题[^\n]*/, subject) : `${subject}\n\n${draft.value}`
}
async function regenerate() {
  const currentSession = session
  await workbench.refresh()
  if (currentSession !== session) return
  if (workbench.error) { error.value = workbench.error; return }
  previousDraft.value = draft.value
  draft.value = generate()
  await persist().catch(() => undefined)
}
async function reloadArchive() {
  clearTimeout(autosave)
  const currentSession = session
  const request = ++generation
  const path = sessionPath
  const date = day.value
  loading.value = true
  try {
    await saveQueue.catch(() => undefined)
    const stored = await WorkbenchService.ReadDailyReport(path, date)
    if (currentSession !== session || request !== generation) return
    previousDraft.value = draft.value
    archiveRevision.text = stored
    savedText.value = stored
    draft.value = stored
    error.value = ''
  } catch (cause) {
    if (currentSession === session && request === generation) error.value = `重新读取失败：${cause instanceof Error ? cause.message : String(cause)}`
  } finally {
    if (currentSession === session && request === generation) loading.value = false
  }
}
function undo() { const current = draft.value; draft.value = previousDraft.value; previousDraft.value = current }
async function polish() {
  const currentSession = session
  const path = sessionPath
  const date = day.value
  try { await persist() } catch { return }
  if (currentSession !== session) return
  requestCopilot({
    source: 'daily-report', workspacePath: path, context: draft.value,
    prompt: '请将这份日报润色为简洁、严谨的企业邮件。保持主题、今日完成、进行中、阻塞与求助、明日计划五段结构，保留任务编号和所有事实。不要虚构进度、成果、时间或人员。只输出可直接复制的纯文本邮件正文。',
    onApply: (content) => {
      if (workspace.currentWorkspace?.path !== path || sessionPath !== path || day.value !== date) return
      generation++
      loading.value = false
      previousDraft.value = draft.value
      draft.value = plainReportText(content)
      keepOnOpen = !props.visible
      void persist().catch(() => undefined)
      window.dispatchEvent(new CustomEvent('notevault:daily-report'))
    },
  })
  emit('close')
}

onBeforeUnmount(() => {
  generation++
  clearTimeout(autosave)
  clearTimeout(copiedTimer)
  window.removeEventListener('keydown', onKeydown)
  if (dirty.value) void persist().catch(() => undefined)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible"
      class="report-overlay"
      @click.self="close"
    >
      <section
        ref="dialog"
        class="report-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="daily-report-title"
      >
        <header class="report-heading">
          <div>
            <span class="report-mark"><FileText :size="20" /></span><div>
              <h2 id="daily-report-title">
                今天的邮件日报
              </h2><p>{{ day }} · 汇总任务、进展与卡点</p>
            </div>
          </div><button
            type="button"
            data-testid="close-report"
            class="icon-button"
            aria-label="关闭日报"
            @click="close"
          >
            <X :size="18" />
          </button>
        </header>
        <div class="report-profile">
          <label>姓名<input
            v-model="author"
            type="text"
            placeholder="可选"
            :disabled="!loaded"
            @change="updateSubject"
          ></label><label>团队<input
            v-model="team"
            type="text"
            placeholder="可选，如全栈研发组"
            :disabled="!loaded"
            @change="updateSubject"
          ></label><button
            type="button"
            class="text-button"
            :disabled="!loaded || loading || saving"
            @click="regenerate"
          >
            <RefreshCw :size="13" />重新汇总
          </button>
        </div>
        <p
          v-if="error"
          class="report-error"
          role="alert"
        >
          {{ error }}<button
            v-if="loaded"
            type="button"
            @click="persist().catch(() => undefined)"
          >
            重试保存
          </button><button
            v-if="loaded"
            type="button"
            data-testid="reload-report"
            :disabled="saving || loading"
            @click="reloadArchive"
          >
            重新读取归档（保留草稿）
          </button><button
            v-if="!loaded"
            type="button"
            @click="loadReport"
          >
            重新读取
          </button>
        </p>
        <p
          v-if="loading && !loaded"
          class="report-loading"
          role="status"
        >
          正在整理今天的进展…
        </p>
        <textarea
          v-if="loaded"
          ref="textarea"
          v-model="draft"
          class="report-draft"
          aria-label="日报正文"
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
          />{{ saving ? '正在保存…' : !loaded ? '等待载入' : dirty ? '有未保存的修改' : '已归档保存' }}</span><span>{{ archivePath }}</span><button
            v-if="previousDraft"
            type="button"
            @click="undo"
          >
            <Undo2 :size="12" />恢复上一稿
          </button>
        </div>
        <footer class="report-footer">
          <button
            type="button"
            class="polish-button"
            data-testid="polish-report"
            :disabled="!loaded || saving || !draft.trim()"
            @click="polish"
          >
            <Sparkles :size="15" />AI 润色为邮件体
          </button><div>
            <button
              type="button"
              class="save-button"
              :disabled="!loaded || saving"
              @click="persist().catch(() => undefined)"
            >
              <Save :size="14" />归档保存
            </button><button
              type="button"
              class="copy-button"
              data-testid="copy-report"
              :disabled="!loaded || !draft.trim()"
              @click="copyReport"
            >
              <Check
                v-if="copied"
                :size="15"
              /><Copy
                v-else
                :size="15"
              />{{ copied ? '已复制' : '一键复制纯文本' }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.report-overlay { position: fixed; inset: 0; z-index: 990; display: flex; align-items: center; justify-content: center; padding: 24px; background: rgba(15,20,24,.38); backdrop-filter: blur(4px); }
.report-dialog { display: flex; flex-direction: column; width: min(800px, 100%); max-height: 90vh; background: var(--bg-card); border: 1px solid var(--border); border-radius: 16px; box-shadow: 0 25px 80px #0003; color: var(--text-primary); overflow: hidden; }
.report-heading { display: flex; justify-content: space-between; gap: 16px; padding: 23px 26px 20px; border-bottom: 1px solid var(--border); }
.report-heading>div { display: flex; gap: 13px; align-items: center; }
.report-mark { display: flex; align-items: center; justify-content: center; width: 41px; height: 41px; border-radius: 11px; color: #639776; background: color-mix(in srgb, #639776 10%, transparent); }
.report-heading h2 { font-size: 18px; font-weight: 600; margin: 0 0 6px; }
.report-heading p { font-size: 11px; color: var(--text-muted); margin: 0; }
.icon-button { align-self: flex-start; padding: 5px; color: var(--text-muted); }
.report-profile { display: flex; align-items: end; flex-wrap: wrap; gap: 12px; padding: 18px 26px; }
.report-profile label { display: flex; flex-direction: column; gap: 7px; color: var(--text-muted); font-size: 11px; }
.report-profile input { border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; background: var(--bg-content); color: var(--text-primary); font-size: 12px; width: 150px; }
.report-profile label:nth-child(2) input { width: 200px; }
.text-button { display: flex; align-items: center; gap: 5px; font-size: 11px; color: var(--text-muted); margin-left: auto; padding: 9px 0; }
.report-draft { margin: 0 26px; flex: 1; min-height: 280px; height: 420px; padding: 21px; border: 1px solid var(--border); border-radius: 9px; background: var(--bg-content); color: var(--text-primary); resize: vertical; font: inherit; font-size: 13px; line-height: 1.9; }
.report-draft:focus { outline: 1px solid var(--accent); }
.report-error { margin: 0 26px 14px; padding: 11px; border: 1px solid color-mix(in srgb, #cf5a56 30%, var(--border)); border-radius: 7px; color: #cf5a56; font-size: 12px; }
.report-error button { margin-left: 12px; text-decoration: underline; }
.report-loading { text-align: center; padding: 60px 20px; color: var(--text-muted); font-size: 13px; }
.archive-status { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; padding: 12px 26px 18px; font-size: 10px; color: var(--text-muted); }
.archive-status span:first-child { color: #609375; display: flex; align-items: center; gap: 5px; }
.archive-status button { display: inline-flex; gap: 4px; align-items: center; margin-left: auto; color: var(--accent); }
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
