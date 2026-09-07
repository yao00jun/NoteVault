<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import { AlertTriangle, ArrowUpRight, MessageSquarePlus, X, Check, Clock3 } from '@lucide/vue'
import type { WorkbenchTask } from '@/api/workbench'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { isCarryoverTask } from '@/utils/workbench'
import { isImeComposing } from '@/utils/ime'

const props = defineProps<{ task: WorkbenchTask }>()
const store = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const mode = ref<'progress' | 'blocker' | null>(null)
const entry = ref('')
const error = ref('')
const input = ref<HTMLInputElement | null>(null)
const carryover = computed(() => isCarryoverTask(props.task, store.today))
const blocked = computed(() => !props.task.completed && !!(props.task.blocker || props.task.status === 'blocked'))
const kind = computed(() => ({ US: 'us', DTS: 'dts', 额外: 'extra', todo: 'plain' })[props.task.type])
const latest = computed(() => props.task.progress.at(-1))

async function run(operation: () => Promise<void>): Promise<boolean> {
  error.value = ''
  try { await operation(); return true } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
    return false
  }
}
async function toggle(event: Event) {
  // Keep a failed disk write from leaving the native checkbox visually checked.
  ;(event.target as HTMLInputElement).checked = props.task.completed
  await run(() => store.toggleTask(props.task))
}
async function openEntry(value: 'progress' | 'blocker') {
  mode.value = mode.value === value ? null : value
  entry.value = value === 'blocker' ? props.task.blocker : ''
  await nextTick()
  input.value?.focus()
}
async function saveEntry() {
  if (!mode.value || !entry.value.trim() || store.busy) return
  const saved = await run(() => mode.value === 'progress' ? store.recordProgress(props.task, entry.value) : store.setBlocker(props.task, entry.value))
  if (saved) { mode.value = null; entry.value = '' }
}
function onEnter(event: KeyboardEvent) {
  if (isImeComposing(event)) return
  event.preventDefault()
  void saveEntry()
}
function openSource() {
  workspace.openFile(props.task.filePath)
  void router.push({ path: '/editor', query: { file: props.task.filePath, line: props.task.lineIndex + 1 } })
}
</script>

<template>
  <article
    class="task-card"
    :class="{ carryover, blocked, completed: task.completed }"
  >
    <div class="task-main">
      <input
        class="task-check"
        type="checkbox"
        :checked="task.completed"
        :disabled="store.busy"
        :aria-label="`${task.completed ? '取消完成' : '完成'}：${task.title}`"
        @change="toggle"
      >
      <div class="task-body">
        <div class="task-meta">
          <span
            class="task-kind"
            :class="kind"
          >{{ task.type === 'todo' ? '待办' : task.type }}</span>
          <span
            v-if="task.project"
            class="task-project"
          >{{ task.project }}</span>
          <span
            v-if="carryover"
            class="carryover-label"
          ><Clock3 :size="11" /> {{ task.date === store.today ? '已逾期' : '昨日 / 逾期顺延' }}</span>
          <span
            v-if="task.due"
            class="task-due"
          >{{ task.due.slice(5) }} 到期</span>
        </div>
        <h3 class="task-title">
          {{ task.title || task.content }}
        </h3>
        <p
          v-if="blocked"
          class="blocker-reason"
        >
          <AlertTriangle :size="13" />{{ task.blocker || '等待协调解决' }}
        </p>
        <p
          v-else-if="latest"
          class="latest-progress"
        >
          {{ latest.note }}
        </p>
      </div>
      <button
        type="button"
        class="source-button"
        title="打开任务笔记"
        aria-label="打开任务笔记"
        @click="openSource"
      >
        <ArrowUpRight :size="16" />
      </button>
    </div>
    <div class="task-actions">
      <button
        type="button"
        data-testid="task-progress"
        :class="{ selected: mode === 'progress' }"
        :disabled="store.busy"
        @click="openEntry('progress')"
      >
        <MessageSquarePlus :size="13" />随手记一笔
      </button>
      <button
        v-if="!task.completed"
        type="button"
        data-testid="task-blocker"
        :class="{ danger: blocked }"
        :disabled="store.busy"
        @click="openEntry('blocker')"
      >
        <AlertTriangle :size="13" />{{ blocked ? '编辑卡点' : '标记阻塞' }}
      </button>
      <button
        v-if="blocked"
        type="button"
        class="resolve-button"
        :disabled="store.busy"
        @click="run(() => store.setBlocker(task, ''))"
      >
        <Check :size="13" />解除阻塞
      </button>
    </div>
    <div
      v-if="mode"
      class="task-entry"
      :class="{ 'blocker-entry': mode === 'blocker' }"
    >
      <input
        ref="input"
        v-model="entry"
        type="text"
        :aria-label="mode === 'progress' ? '进展记录' : '阻塞原因'"
        :placeholder="mode === 'progress' ? '记录进展，回车保存到任务与今日日记…' : '具体卡在哪里，需要谁来协助？'"
        :disabled="store.busy"
        @keydown.enter="onEnter"
        @keydown.esc="mode = null"
      >
      <button
        type="button"
        data-testid="task-save-entry"
        :disabled="store.busy || !entry.trim()"
        @click="saveEntry"
      >
        {{ store.busy ? '保存中' : '保存' }}
      </button>
      <button
        type="button"
        aria-label="取消记录"
        :disabled="store.busy"
        @click="mode = null"
      >
        <X :size="14" />
      </button>
    </div>
    <p
      v-if="error"
      class="task-error"
      role="alert"
    >
      {{ error }} <button
        type="button"
        @click="store.refresh()"
      >
        刷新任务
      </button>
    </p>
  </article>
</template>

<style scoped>
.task-card { border: 1px solid var(--border); border-radius: 12px; padding: 16px; background: var(--bg-card); transition: border-color .2s; }
.task-card:hover { border-color: var(--border-accent, var(--accent)); }
.task-card.carryover { border-left: 3px solid #d7aa3e; background: color-mix(in srgb, #f5c951 7%, var(--bg-card)); }
.task-card.blocked { border-color: color-mix(in srgb, #df615d 40%, var(--border)); background: color-mix(in srgb, #df615d 5%, var(--bg-card)); }
.task-main { display: flex; gap: 12px; align-items: flex-start; }
.task-check { margin-top: 5px; width: 17px; height: 17px; accent-color: var(--accent); flex-shrink: 0; cursor: pointer; }
.task-body { flex: 1; min-width: 0; }
.task-meta { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; color: var(--text-muted); font-size: 11px; }
.task-kind { border-radius: 4px; padding: 2px 6px; font-size: 10px; font-weight: 700; letter-spacing: .04em; background: var(--bg-hover); }
.task-kind.us { color: #447acb; background: color-mix(in srgb, #447acb 12%, transparent); }
.task-kind.dts { color: #ce6b42; background: color-mix(in srgb, #ce6b42 12%, transparent); }
.task-kind.extra { color: #9365c3; background: color-mix(in srgb, #9365c3 12%, transparent); }
.task-project { color: var(--text-secondary); }
.carryover-label { display: inline-flex; gap: 4px; align-items: center; color: #a57d1e; }
.task-due { margin-left: auto; }
.task-title { margin: 8px 0 0; font-size: 14px; line-height: 1.6; font-weight: 550; color: var(--text-primary); overflow-wrap: anywhere; }
.completed .task-title { text-decoration: line-through; color: var(--text-muted); }
.blocker-reason, .latest-progress { display: flex; align-items: baseline; gap: 6px; margin: 6px 0 0; font-size: 12px; line-height: 1.6; color: var(--text-muted); overflow-wrap: anywhere; }
.blocker-reason { color: #cf5a56; }
.blocker-reason svg { flex-shrink: 0; }
.source-button { color: var(--text-muted); padding: 3px; border-radius: 4px; }
.task-actions { display: flex; flex-wrap: wrap; gap: 12px; margin: 12px 0 0 29px; }
.task-actions button { display: inline-flex; align-items: center; gap: 5px; color: var(--text-muted); font-size: 11px; padding: 2px 0; }
.task-actions button:hover, .task-actions .selected { color: var(--accent); }
.task-actions .danger { color: #cf5a56; }
.task-actions .resolve-button { color: #4e9b72; }
.task-entry { display: flex; gap: 7px; align-items: center; padding-top: 12px; }
.task-entry input { flex: 1; min-width: 0; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-content); color: var(--text-primary); font-size: 12px; }
.task-entry button { color: var(--accent); font-size: 12px; }
.blocker-entry input { border-color: color-mix(in srgb, #cf5a56 50%, var(--border)); }
.task-error { font-size: 12px; color: #cf5a56; margin: 10px 0 0; }
.task-error button { text-decoration: underline; }
button:disabled, input:disabled { opacity: .5; cursor: wait; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
</style>
