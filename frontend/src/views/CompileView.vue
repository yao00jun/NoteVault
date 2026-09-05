<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
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
} from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import { CompileService } from '@bindings/github.com/notevault/notevault/index.js'
import type { CompileResult, CompileAllResult } from '@bindings/github.com/notevault/notevault/models.js'

const router = useRouter()
const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const settingsStore = useSettingsStore()
const toast = useToast()

const notes = ref<string[]>([])
const isLoading = ref(false)
const isCompilingAll = ref(false)
const compilingPath = ref<string | null>(null)

// 每篇笔记的编译状态（编译完成后就地标记，便于列表内反馈）
const status = reactive<Record<string, 'idle' | 'done' | 'error'>>({})
const errors = reactive<Record<string, string>>({})

// 编译全部的结果面板 + 最近编译列表
const lastResult = ref<CompileAllResult | null>(null)
const recent = ref<CompileResult[]>([])

// 生成层把 Results/Errors 定义为可空数组，这里归一化便于模板安全使用。
const resultList = computed<CompileResult[]>(
  () => (lastResult.value?.Results?.filter(Boolean) as CompileResult[] | undefined) ?? [],
)
const errorList = computed(
  () => (lastResult.value?.Errors?.filter(Boolean) as { Path: string; Error: string }[] | undefined) ?? [],
)

const ai = computed(() => settingsStore.settings.ai)
const hasApiKey = computed(() => ai.value.apiKey.trim() !== '')
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

async function compileOne(path: string) {
  if (!workspacePath.value || compilingPath.value) return
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

async function compileAll() {
  if (!workspacePath.value || isCompilingAll.value) return
  if (!hasApiKey.value) {
    toast.warning(t('compile.needKeyToast'))
    return
  }
  isCompilingAll.value = true
  lastResult.value = null
  try {
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
  } catch (e) {
    toast.error(t('compile.compileFailed', { msg: (e as Error).message }))
  } finally {
    isCompilingAll.value = false
  }
}

function goSettings() {
  router.push('/settings')
}
function goHistory() {
  router.push('/history')
}

onMounted(loadInbox)
watch(() => workspaceStore.currentWorkspace?.id, loadInbox)
</script>

<template>
  <div class="compile-view">
    <div class="compile-header">
      <h2 class="compile-title">
        <Sparkles :size="20" /> {{ t('compile.title') }}
      </h2>
      <button
        class="compile-all-btn"
        :disabled="notes.length === 0 || isCompilingAll || !hasApiKey"
        @click="compileAll"
      >
        <Loader2
          v-if="isCompilingAll"
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
          <FileText :size="16" class="note-icon" />
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
            :disabled="compilingPath !== null"
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
            <ArrowRight :size="13" class="arrow" />
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
.compile-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.compile-all-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-2) var(--space-4); border: none; border-radius: var(--radius-md); background: var(--accent); color: white; font-size: var(--text-sm); font-weight: 600; cursor: pointer; }
.compile-all-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.compile-desc { padding: var(--space-4) var(--space-8) 0; margin: 0; color: var(--text-muted); font-size: var(--text-sm); }
.banner { display: flex; align-items: center; gap: var(--space-2); margin: var(--space-4) var(--space-8) 0; padding: var(--space-3) var(--space-4); border-radius: var(--radius-md); font-size: var(--text-sm); }
.banner.warn { background: color-mix(in srgb, var(--warning, #f59e0b) 15%, transparent); border: 1px solid var(--warning, #f59e0b); color: var(--text-secondary); }
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
