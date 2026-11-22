<script setup lang="ts">
import { CompileService, CompileResult, CompileAllResult, TaskService } from '@/api'
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Sparkles,
  Play,
  Loader2,
  CheckCircle2,
  AlertTriangle,
  ArrowRight,
  History,
  Settings as SettingsIcon,
  FileText,
  X,
  } from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import { isLocalBaseURL } from '@/utils/localEndpoint'

const router = useRouter()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const settingsStore = useSettingsStore()
const toast = useToast()

const notes = ref<string[]>([])
const isLoading = ref(false)
const compilingPath = ref<string | null>(null)

// 每篇笔记的编译状态（编译完成后就地标记，便于列表内反馈）
const status = reactive<Record<string, 'idle' | 'done' | 'error'>>({})
const errors = reactive<Record<string, string>>({})

// 编译全部的结果面板 + 最近编译列表
const lastResult = ref<CompileAllResult | null>(null)
const recent = ref<CompileResult[]>([])

// --- 异步任务状态（P1-2）---
// 编译改走 TaskService 后，进度来自 task:progress 事件；这里只持有「当前关注的
// 那个任务」的状态，其它任务的事件直接忽略（同一时间可能还有导入任务在跑）。
const activeTaskID = ref<string | null>(null)
const activeTaskKind = ref<'all' | 'one' | null>(null)
const taskPercent = ref(-1)
const taskMessage = ref('')
const isCancelling = ref(false)
const isBusy = computed(() => activeTaskID.value !== null)
let stopTaskEvents: (() => void) | undefined

interface TaskEventPayload {
  id?: string
  status?: string
  percent?: number
  message?: string
  error?: string
}

// 生成层把 Results/Errors 定义为可空数组，这里归一化便于模板安全使用。
const resultList = computed<CompileResult[]>(
  () => (lastResult.value?.Results?.filter(Boolean) as CompileResult[] | undefined) ?? [],
)
const errorList = computed(
  () => (lastResult.value?.Errors?.filter(Boolean) as { Path: string; Error: string }[] | undefined) ?? [],
)

const ai = computed(() => settingsStore.settings.ai)
// AI 就绪判定与 EditorView/QnAView 同口径：本机端点（Ollama 等）免 Key，
// 云端才要求 Key；baseURL 任何时候都必须有。此前这里单独用「apiKey 非空」
// 判定，导致配了本地 Ollama 也一直亮「未配置 API Key」横幅、编译全量被拦
//（2026-09-07 用户实测踩坑）。
const hasApiKey = computed(
  () => Boolean(ai.value.baseURL.trim()) &&
    (isLocalBaseURL(ai.value.baseURL) || ai.value.apiKey.trim() !== ''),
)
const workspacePath = computed(() => workspaceStore.currentWorkspace?.path ?? '')

function basename(p: string): string {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(i + 1) : p
}

async function loadInbox() {
  if (!workspacePath.value) {
    notes.value = []
    return
  }
  isLoading.value = true
  try {
    const data = (await CompileService.ListInbox(workspacePath.value)) as string[] | null
    notes.value = data ?? []
    // 列表刷新后清掉旧状态（已编译的不再出现在 Inbox）
    for (const k of Object.keys(status)) delete status[k]
    for (const k of Object.keys(errors)) delete errors[k]
  } catch (e) {
    console.error('Failed to load Inbox:', e)
    notes.value = []
    toast.error(t('compile.loadFailed', { msg: (e as Error).message }))
  } finally {
    isLoading.value = false
  }
}

// --- 同步路径（后端未接入任务队列时的兜底，行为与改造前一致）---
async function compileOneSync(path: string) {
  compilingPath.value = path
  status[path] = 'idle'
  delete errors[path]
  try {
    const res = (await CompileService.CompileNote(
      workspacePath.value,
      path,
      ai.value.apiKey,
      ai.value.baseURL,
      ai.value.model,
      ai.value.protocol,
    )) as CompileResult | null
    if (!res) throw new Error('empty result')
    status[path] = 'done'
    recent.value = [res, ...recent.value].slice(0, 20)
    toast.success(t('compile.compiledOne', { name: basename(path) }))
    await loadInbox()
  } catch (e) {
    const msg = (e as Error).message
    status[path] = 'error'
    errors[path] = msg
    toast.error(t('compile.compileFailed', { msg }))
  } finally {
    compilingPath.value = null
  }
}

async function compileAllSync() {
  const res = (await CompileService.CompileAll(
    workspacePath.value,
    ai.value.apiKey,
    ai.value.baseURL,
    ai.value.model,
    ai.value.protocol,
  )) as CompileAllResult | null
  if (!res) throw new Error('empty result')
  lastResult.value = res
  recent.value = [...((res.Results ?? []).filter(Boolean) as CompileResult[]), ...recent.value].slice(0, 20)
  const ok = (res.Results ?? []).length
  const fail = (res.Errors ?? []).length
  if (fail === 0) {
    toast.success(t('compile.allDone', { n: ok }))
  } else {
    toast.warning(t('compile.allPartial', { ok, fail }))
  }
  await loadInbox()
}

// --- 全部编译 ---
async function compileAll() {
  if (!workspacePath.value || isBusy.value) return
  if (!hasApiKey.value) {
    toast.warning(t('compile.needKeyToast'))
    return
  }
  lastResult.value = null
  taskPercent.value = -1
  taskMessage.value = ''
  try {
    const taskID = (await CompileService.StartCompileAll(
      workspacePath.value,
      ai.value.apiKey,
      ai.value.baseURL,
      ai.value.model,
      ai.value.protocol,
    )) as string | null
    if (!taskID) {
      // 旧后端 / 未注入任务队列：退回同步语义
      await compileAllSync()
      return
    }
    activeTaskID.value = taskID
    activeTaskKind.value = 'all'
  } catch (e) {
    toast.error(t('compile.compileFailed', { msg: (e as Error).message }))
  }
}

// --- 单篇编译 ---
async function compileOne(path: string) {
  if (!workspacePath.value || isBusy.value || compilingPath.value) return
  compilingPath.value = path
  status[path] = 'idle'
  delete errors[path]
  taskPercent.value = -1
  taskMessage.value = ''
  try {
    const taskID = (await CompileService.StartCompileNote(
      workspacePath.value,
      path,
      ai.value.apiKey,
      ai.value.baseURL,
      ai.value.model,
      ai.value.protocol,
    )) as string | null
    if (!taskID) {
      await compileOneSync(path)
      return
    }
    activeTaskID.value = taskID
    activeTaskKind.value = 'one'
  } catch (e) {
    compilingPath.value = null
    status[path] = 'error'
    errors[path] = (e as Error).message
    toast.error(t('compile.compileFailed', { msg: (e as Error).message }))
  }
}

// 取消当前任务：TaskService.Cancel 是幂等的，任务刚好结束时返回 false，
// 此时不弹错——finished 事件马上会把 UI 收尾。
async function cancelCompile() {
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

// applyTaskEvent 只处理「当前关注的任务」，避免被并发的导入任务进度串扰。
function applyTaskEvent(info: TaskEventPayload | undefined, finished: boolean) {
  if (!info || !activeTaskID.value || info.id !== activeTaskID.value) return
  if (typeof info.percent === 'number') taskPercent.value = info.percent
  if (info.message) taskMessage.value = info.message
  if (finished) void finishTask(info)
}

// finishTask 统一收尾：状态提示 → 取回结构化结果 → 刷新 Inbox → 就地标记失败项。
async function finishTask(info: TaskEventPayload) {
  const id = info.id as string
  const kind = activeTaskKind.value
  const path = compilingPath.value
  const taskStatus = info.status ?? ''

  activeTaskID.value = null
  activeTaskKind.value = null
  isCancelling.value = false
  compilingPath.value = null

  if (taskStatus === 'cancelled') {
    toast.info(t('compile.cancelled'))
  } else if (taskStatus === 'failed') {
    toast.error(t('compile.taskFailed', { msg: info.error || '' }))
  }

  let res: CompileAllResult | null = null
  try {
    res = (await CompileService.CompileTaskResult(id)) as CompileAllResult | null
  } catch (e) {
    console.error('Failed to read compile task result:', e)
  }

  await loadInbox()

  if (!res) return
  const okResults = (res.Results ?? []).filter(Boolean) as CompileResult[]
  const failItems = (res.Errors ?? []).filter(Boolean) as { Path: string; Error: string }[]

  if (kind === 'all') {
    lastResult.value = res
    recent.value = [...okResults, ...recent.value].slice(0, 20)
  } else if (okResults[0]) {
    recent.value = [okResults[0], ...recent.value].slice(0, 20)
  }

  // Inbox 已刷新：失败项还留在列表里，就地标红并显示原因
  for (const item of failItems) {
    if (notes.value.includes(item.Path)) {
      status[item.Path] = 'error'
      errors[item.Path] = item.Error
    }
  }

  if (taskStatus !== 'succeeded') return
  if (kind === 'one' && path && okResults.length === 1) {
    status[path] = 'done'
    toast.success(t('compile.compiledOne', { name: basename(path) }))
  } else if (kind === 'all') {
    if (failItems.length === 0) {
      toast.success(t('compile.allDone', { n: okResults.length }))
    } else {
      toast.warning(t('compile.allPartial', { ok: okResults.length, fail: failItems.length }))
    }
  }
}

function goSettings() {
  router.push('/settings')
}
function goHistory() {
  router.push('/history')
}

onMounted(() => {
  void loadInbox()
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
watch(() => workspaceStore.currentWorkspace?.id, loadInbox)
</script>

<template>
  <div class="compile-view">
    <div class="compile-header">
      <div class="compile-header-left">
        <h2 class="compile-title">
          <Sparkles :size="20" /> {{ t('compile.title') }}
        </h2>
      </div>
      <button
        class="compile-all-btn"
        :disabled="notes.length === 0 || isBusy || !hasApiKey"
        @click="compileAll"
      >
        <Loader2
          v-if="isBusy"
          :size="16"
          class="spin"
        />
        <Play
          v-else
          :size="16"
        />
        {{ t('compile.compileAll') }}
      </button>
    </div>

    <p class="compile-desc">
      {{ t('compile.desc') }}
    </p>

    <div
      v-if="!hasApiKey"
      class="banner warn"
    >
      <AlertTriangle :size="16" />
      <span>{{ t('compile.needKey') }}</span>
      <button
        class="link-btn"
        @click="goSettings"
      >
        <SettingsIcon :size="14" /> {{ t('compile.openSettings') }}
      </button>
    </div>

    <div
      v-if="isBusy"
      class="task-bar"
      data-testid="compile-progress"
    >
      <div class="task-bar-head">
        <Loader2
          :size="14"
          class="spin task-bar-icon"
        />
        <span class="task-bar-message">{{ taskMessage || t('compile.preparing') }}</span>
        <span class="task-bar-pct">{{ taskPercent >= 0 ? `${taskPercent}%` : '…' }}</span>
        <button
          class="task-cancel-btn"
          type="button"
          :disabled="isCancelling"
          data-testid="compile-cancel"
          @click="cancelCompile"
        >
          <X :size="13" />
          {{ isCancelling ? t('compile.cancelling') : t('compile.cancel') }}
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

    <div class="compile-content">
      <div
        v-if="isLoading"
        class="state"
      >
        <div class="spinner" />
        <p>{{ t('common.loading') }}</p>
      </div>

      <div
        v-else-if="notes.length === 0"
        class="state"
      >
        <div class="empty-icon">
          📥
        </div>
        <h3>{{ t('compile.emptyTitle') }}</h3>
        <p>{{ t('compile.emptyDesc') }}</p>
      </div>

      <div
        v-else
        class="note-list"
      >
        <div
          v-for="path in notes"
          :key="path"
          class="note-item"
          :class="{ done: status[path] === 'done', error: status[path] === 'error' }"
        >
          <FileText
            :size="16"
            class="note-icon"
          />
          <div class="note-info">
            <div class="note-path">
              {{ path }}
            </div>
            <div
              v-if="status[path] === 'error'"
              class="note-error"
            >
              {{ errors[path] }}
            </div>
          </div>
          <button
            class="compile-one-btn"
            :disabled="isBusy || compilingPath !== null"
            @click="compileOne(path)"
          >
            <Loader2
              v-if="compilingPath === path"
              :size="14"
              class="spin"
            />
            <CheckCircle2
              v-else-if="status[path] === 'done'"
              :size="14"
            />
            <Play
              v-else
              :size="14"
            />
            {{ t('compile.compileOne') }}
          </button>
        </div>
      </div>

      <!-- 编译全部结果面板 -->
      <div
        v-if="lastResult"
        class="result-panel"
      >
        <h3 class="result-title">
          {{ t('compile.resultsTitle') }}
          <span class="result-counts">
            <span class="ok"><CheckCircle2 :size="14" />{{ t('compile.okCount', { n: resultList.length }) }}</span>
            <span
              v-if="errorList.length"
              class="fail"
            ><AlertTriangle :size="14" />{{ t('compile.failCount', { n: errorList.length }) }}</span>
          </span>
        </h3>

        <div
          v-if="errorList.length"
          class="fail-list"
        >
          <div
            v-for="err in errorList"
            :key="err.Path"
            class="fail-item"
          >
            <div class="fail-path">
              <FileText :size="13" /> {{ err.Path }}
            </div>
            <div class="fail-reason">
              {{ err.Error }}
            </div>
          </div>
        </div>

        <p class="undo-hint">
          <History :size="14" />
          {{ t('compile.undoHint') }}
          <button
            class="link-btn"
            @click="goHistory"
          >
            {{ t('compile.openHistory') }}
          </button>
        </p>
      </div>

      <!-- 最近编译结果 -->
      <div
        v-if="recent.length"
        class="recent-panel"
      >
        <h3 class="recent-title">
          {{ t('compile.recentTitle') }}
        </h3>
        <div
          v-for="r in recent"
          :key="r.SnapshotID + r.Source"
          class="recent-item"
        >
          <div class="recent-route">
            <span class="src">{{ r.Source }}</span>
            <ArrowRight
              :size="13"
              class="arrow"
            />
            <span class="dest">{{ r.Dest }}</span>
          </div>
          <div
            v-if="r.Output"
            class="recent-meta"
          >
            <div
              v-if="r.Output.TLDR"
              class="tldr"
            >
              {{ r.Output.TLDR }}
            </div>
            <div
              v-if="r.Output.Tags && r.Output.Tags.length"
              class="tags"
            >
              <span
                v-for="tag in r.Output.Tags"
                :key="tag"
                class="tag-chip"
              >#{{ tag }}</span>
            </div>
          </div>
          <div class="snapshot-id">
            {{ t('compile.snapshot') }}: <code>{{ r.SnapshotID }}</code>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.compile-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.compile-header { display: flex; align-items: center; justify-content: space-between; padding: var(--space-6) var(--space-8); border-bottom: 1px solid var(--border); background: var(--bg-window); }
.compile-header-left { display: flex; align-items: center; gap: var(--space-3); }
.back-btn { display: flex; align-items: center; gap: 5px; border: none; background: transparent; color: var(--text-muted); font-size: var(--text-sm); cursor: pointer; padding: 5px 10px; border-radius: var(--radius-sm); transition: background var(--transition-fast), color var(--transition-fast); }
.back-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.compile-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.compile-all-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-2) var(--space-4); border: none; border-radius: var(--radius-md); background: var(--accent); color: white; font-size: var(--text-sm); font-weight: 600; cursor: pointer; }
.compile-all-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.compile-desc { padding: var(--space-4) var(--space-8) 0; margin: 0; color: var(--text-muted); font-size: var(--text-sm); }
.banner { display: flex; align-items: center; gap: var(--space-2); margin: var(--space-4) var(--space-8) 0; padding: var(--space-3) var(--space-4); border-radius: var(--radius-md); font-size: var(--text-sm); }
.banner.warn { background: color-mix(in srgb, var(--warning, #f59e0b) 15%, transparent); border: 1px solid var(--warning, #f59e0b); color: var(--text-secondary); }
.task-bar { display: flex; flex-direction: column; gap: var(--space-2); margin: var(--space-4) var(--space-8) 0; padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); }
.task-bar-head { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-sm); color: var(--text-secondary); }
.task-bar-icon { color: var(--accent); flex-shrink: 0; }
.task-bar-message { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task-bar-pct { font-variant-numeric: tabular-nums; font-size: var(--text-xs); color: var(--text-muted); }
.task-cancel-btn { display: inline-flex; align-items: center; gap: 4px; padding: 2px var(--space-2); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-window); color: var(--text-secondary); font-size: var(--text-xs); cursor: pointer; }
.task-cancel-btn:hover:not(:disabled) { color: var(--danger, #ef4444); border-color: var(--danger, #ef4444); }
.task-cancel-btn:disabled { opacity: 0.6; cursor: not-allowed; }
.task-track { height: 4px; border-radius: 2px; background: var(--bg-input); overflow: hidden; }
.task-fill { height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.2s ease; }
.task-fill.indeterminate { animation: task-pulse 1.4s ease-in-out infinite; }
@keyframes task-pulse { 0%, 100% { opacity: 0.45; } 50% { opacity: 1; } }
.link-btn { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; background: none; border: none; color: var(--accent); cursor: pointer; font-size: var(--text-sm); }
.compile-content { flex: 1; overflow-y: auto; padding: var(--space-6) var(--space-8); }
.state { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: var(--space-12) 0; color: var(--text-muted); gap: var(--space-3); }
.state h3 { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); margin: 0; }
.empty-icon { font-size: 48px; opacity: 0.5; }
.spinner { width: 32px; height: 32px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin 0.8s linear infinite; }
.note-list { display: flex; flex-direction: column; gap: var(--space-2); max-width: 820px; }
.note-item { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); }
.note-item.done { border-color: var(--success, #22c55e); }
.note-item.error { border-color: var(--danger, #ef4444); }
.note-icon { color: var(--text-muted); flex-shrink: 0; }
.note-info { flex: 1; min-width: 0; }
.note-path { font-size: var(--text-sm); color: var(--text-primary); word-break: break-all; }
.note-error { font-size: var(--text-xs); color: var(--danger, #ef4444); margin-top: 2px; }
.compile-one-btn { display: inline-flex; align-items: center; gap: 4px; padding: var(--space-1) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-window); color: var(--accent); font-size: var(--text-sm); cursor: pointer; flex-shrink: 0; }
.compile-one-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.result-panel { max-width: 820px; margin-top: var(--space-6); padding: var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); }
.result-title { display: flex; align-items: center; justify-content: space-between; font-size: var(--text-base); font-weight: 700; margin: 0 0 var(--space-3); color: var(--text-primary); }
.result-counts { display: flex; gap: var(--space-3); font-weight: 500; font-size: var(--text-sm); }
.result-counts .ok { display: inline-flex; align-items: center; gap: 4px; color: var(--success, #22c55e); }
.result-counts .fail { display: inline-flex; align-items: center; gap: 4px; color: var(--danger, #ef4444); }
.fail-list { display: flex; flex-direction: column; gap: var(--space-2); margin-bottom: var(--space-3); }
.fail-item { padding: var(--space-2) var(--space-3); border: 1px solid var(--danger, #ef4444); border-radius: var(--radius-sm); background: color-mix(in srgb, var(--danger, #ef4444) 8%, transparent); }
.fail-path { display: flex; align-items: center; gap: 4px; font-size: var(--text-sm); color: var(--text-primary); word-break: break-all; }
.fail-reason { font-size: var(--text-xs); color: var(--danger, #ef4444); margin-top: 2px; word-break: break-all; }
.undo-hint { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--text-muted); margin: 0; }
.undo-hint .link-btn { margin-left: 0; }
.recent-panel { max-width: 820px; margin-top: var(--space-6); }
.recent-title { font-size: var(--text-base); font-weight: 700; margin: 0 0 var(--space-3); color: var(--text-primary); }
.recent-item { padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); margin-bottom: var(--space-2); }
.recent-route { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-sm); }
.recent-route .src { color: var(--text-muted); word-break: break-all; }
.recent-route .dest { color: var(--accent); word-break: break-all; }
.recent-route .arrow { color: var(--text-muted); flex-shrink: 0; }
.recent-meta { margin-top: var(--space-2); }
.tldr { font-size: var(--text-sm); color: var(--text-secondary); }
.tags { display: flex; flex-wrap: wrap; gap: 4px; margin-top: var(--space-1); }
.tag-chip { font-size: var(--text-xs); padding: 1px 8px; border-radius: 8px; background: var(--accent-alpha, rgba(0, 122, 255, 0.1)); color: var(--accent); }
.snapshot-id { margin-top: var(--space-2); font-size: var(--text-xs); color: var(--text-muted); }
.snapshot-id code { font-family: var(--font-mono, monospace); background: var(--bg-input); padding: 1px 6px; border-radius: 4px; }
</style>
