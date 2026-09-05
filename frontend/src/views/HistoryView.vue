<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import {
  History,
  FileText,
  RotateCcw,
  Trash2,
  Camera,
  Eraser,
  ExternalLink,
  AlertTriangle,
} from '@lucide/vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '@/composables/useConfirm'
import { useWorkspaceStore } from '@/stores/workspace'
import { SnapshotService } from '@bindings/github.com/notevault/notevault/index.js'
import type {
  Snapshot,
  SnapshotDiff,
  SnapshotFileSummary,
  SnapshotStats,
} from '@bindings/github.com/notevault/notevault/models.js'

type CompareMode = 'current' | 'previous'

const router = useRouter()
const { t, locale } = useI18n()
const workspaceStore = useWorkspaceStore()

const files = ref<SnapshotFileSummary[]>([])
const snapshots = ref<Snapshot[]>([])
const stats = ref<SnapshotStats | null>(null)
const diff = ref<SnapshotDiff | null>(null)

const selectedPath = ref('')
const selectedID = ref('')
const compareMode = ref<CompareMode>('current')

const isLoadingFiles = ref(false)
const isLoadingSnapshots = ref(false)
const isLoadingDiff = ref(false)
const errorMsg = ref('')
const noticeMsg = ref('')

const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

/** 选中快照在列表中的下标，用于「与上一版本对比」定位前驱 */
const selectedIndex = computed(() => snapshots.value.findIndex(s => s.id === selectedID.value))
const selectedSnapshot = computed(() => snapshots.value[selectedIndex.value] ?? null)
/** 列表按时间倒序，「上一版本」是下标 +1 的那条 */
const previousSnapshot = computed(() => {
  const i = selectedIndex.value
  if (i < 0) return null
  return snapshots.value[i + 1] ?? null
})
const canCompareWithPrevious = computed(() => previousSnapshot.value !== null)

function clearMessages() {
  errorMsg.value = ''
  noticeMsg.value = ''
}

function fail(key: string, e: unknown) {
  const msg = e instanceof Error ? e.message : String(e)
  console.error(`[history] ${key}:`, e)
  errorMsg.value = t(key, { msg })
}

async function loadStats() {
  if (!currentWorkspace.value?.path) return
  try {
    stats.value = await SnapshotService.GetSnapshotStats(currentWorkspace.value.path)
  } catch (e) {
    console.error('[history] failed to load stats:', e)
    stats.value = null
  }
}

async function loadFiles(keepSelection = false) {
  if (!currentWorkspace.value?.path) {
    files.value = []
    snapshots.value = []
    diff.value = null
    return
  }
  isLoadingFiles.value = true
  try {
    const list = await SnapshotService.ListSnapshotFiles(currentWorkspace.value.path)
    files.value = (list ?? []).filter((f): f is SnapshotFileSummary => f !== null)
    const stillThere = files.value.some(f => f.path === selectedPath.value)
    if (!keepSelection || !stillThere) {
      const next = files.value[0]?.path ?? ''
      if (next !== selectedPath.value) {
        await selectFile(next)
      } else if (!next) {
        snapshots.value = []
        diff.value = null
      }
    }
  } catch (e) {
    files.value = []
    fail('history.errors.loadFiles', e)
  } finally {
    isLoadingFiles.value = false
  }
}

async function selectFile(path: string) {
  selectedPath.value = path
  selectedID.value = ''
  snapshots.value = []
  diff.value = null
  clearMessages()
  if (!path) return
  await loadSnapshots()
}

async function loadSnapshots(preferID = '') {
  if (!currentWorkspace.value?.path || !selectedPath.value) return
  isLoadingSnapshots.value = true
  try {
    const list = await SnapshotService.ListSnapshots(currentWorkspace.value.path, selectedPath.value)
    snapshots.value = (list ?? []).filter((s): s is Snapshot => s !== null)
    const wanted = preferID && snapshots.value.some(s => s.id === preferID)
      ? preferID
      : snapshots.value[0]?.id ?? ''
    selectedID.value = wanted
    if (!canCompareWithPrevious.value) compareMode.value = 'current'
    await loadDiff()
  } catch (e) {
    snapshots.value = []
    fail('history.errors.loadSnapshots', e)
  } finally {
    isLoadingSnapshots.value = false
  }
}

async function selectSnapshot(id: string) {
  if (selectedID.value === id) return
  selectedID.value = id
  clearMessages()
  await loadDiff()
}

async function setCompareMode(mode: CompareMode) {
  if (compareMode.value === mode) return
  compareMode.value = mode
  await loadDiff()
}

async function loadDiff() {
  if (!currentWorkspace.value?.path || !selectedID.value) {
    diff.value = null
    return
  }
  const ws = currentWorkspace.value.path
  const id = selectedID.value
  const mode = compareMode.value
  const prev = previousSnapshot.value
  isLoadingDiff.value = true
  try {
    const result = mode === 'previous' && prev
      ? await SnapshotService.DiffSnapshots(ws, prev.id, id)
      : await SnapshotService.DiffWithCurrent(ws, id)
    // 请求返回时选择可能已经变了，丢弃过期结果
    if (selectedID.value !== id || compareMode.value !== mode) return
    diff.value = result
  } catch (e) {
    diff.value = null
    fail('history.errors.loadDiff', e)
  } finally {
    isLoadingDiff.value = false
  }
}

async function createManualSnapshot() {
  if (!currentWorkspace.value?.path || !selectedPath.value) return
  clearMessages()
  try {
    const snap = await SnapshotService.CreateManualSnapshot(currentWorkspace.value.path, selectedPath.value)
    noticeMsg.value = t('history.notices.snapshotCreated')
    await Promise.all([loadStats(), loadFiles(true)])
    await loadSnapshots(snap?.id ?? '')
  } catch (e) {
    fail('history.errors.createSnapshot', e)
  }
}

async function restore(snap: Snapshot) {
  if (!currentWorkspace.value?.path) return
  if (!(await confirmDialog({ message: t('history.confirmRestore', { date: formatDate(snap.createdAt) }) }))) return
  clearMessages()
  try {
    const result = await SnapshotService.RestoreSnapshot(currentWorkspace.value.path, snap.id)
    workspaceStore.incrementFileTreeVersion()
    noticeMsg.value = result?.backupId
      ? t('history.notices.restoredWithBackup', { date: formatDate(snap.createdAt) })
      : t('history.notices.restored', { date: formatDate(snap.createdAt) })
    await Promise.all([loadStats(), loadFiles(true)])
    await loadSnapshots(snap.id)
  } catch (e) {
    fail('history.errors.restore', e)
  }
}

async function deleteSnapshot(snap: Snapshot) {
  if (!currentWorkspace.value?.path) return
  if (!(await confirmDialog({ message: t('history.confirmDeleteSnapshot', { date: formatDate(snap.createdAt) }), danger: true }))) return
  clearMessages()
  try {
    await SnapshotService.DeleteSnapshot(currentWorkspace.value.path, snap.id)
    await Promise.all([loadStats(), loadFiles(true)])
    if (selectedPath.value) await loadSnapshots()
  } catch (e) {
    fail('history.errors.deleteSnapshot', e)
  }
}

async function clearFileHistory() {
  if (!currentWorkspace.value?.path || !selectedPath.value) return
  if (!(await confirmDialog({ message: t('history.confirmClearFile', { path: selectedPath.value }), danger: true }))) return
  clearMessages()
  try {
    await SnapshotService.ClearSnapshots(currentWorkspace.value.path, selectedPath.value)
    await Promise.all([loadStats(), loadFiles(false)])
  } catch (e) {
    fail('history.errors.clear', e)
  }
}

async function prune() {
  if (!currentWorkspace.value?.path) return
  clearMessages()
  try {
    const after = await SnapshotService.PruneSnapshots(currentWorkspace.value.path)
    stats.value = after
    noticeMsg.value = t('history.notices.pruned')
    await loadFiles(true)
    if (selectedPath.value) await loadSnapshots(selectedID.value)
  } catch (e) {
    fail('history.errors.prune', e)
  }
}

function openInEditor() {
  if (!selectedPath.value) return
  const abs = joinWorkspacePath(selectedPath.value)
  if (!abs) return
  router.push('/editor')
  workspaceStore.openFile(abs)
  workspaceStore.setActiveFile(abs)
}

/** 拼出绝对路径时保留工作区原有的分隔符风格，Windows 下不要混用 */
function joinWorkspacePath(relative: string): string {
  const base = currentWorkspace.value?.path
  if (!base) return ''
  const sep = base.includes('\\') ? '\\' : '/'
  const trimmed = base.replace(/[\\/]+$/, '')
  return trimmed + sep + relative.split('/').join(sep)
}

function fileName(path: string): string {
  return path.split('/').pop() || path
}

function fileDir(path: string): string {
  const parts = path.split('/')
  parts.pop()
  return parts.join('/')
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function formatDate(iso: string): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString(locale.value, {
      month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    })
  } catch {
    return iso
  }
}

function reasonLabel(reason: string): string {
  switch (reason) {
    case 'save': return t('history.reasons.save')
    case 'daily': return t('history.reasons.daily')
    case 'manual': return t('history.reasons.manual')
    case 'restore': return t('history.reasons.restore')
    case 'delete': return t('history.reasons.delete')
    default: return reason || t('history.reasons.save')
  }
}

function opPrefix(type: string): string {
  if (type === 'insert') return '+'
  if (type === 'delete') return '-'
  return ' '
}

onMounted(async () => {
  await Promise.all([loadStats(), loadFiles()])
})

watch(() => currentWorkspace.value?.id, async () => {
  selectedPath.value = ''
  selectedID.value = ''
  clearMessages()
  await Promise.all([loadStats(), loadFiles()])
})
</script>

<template>
  <div class="history-view">
    <div class="history-header">
      <div class="header-left">
        <h2 class="history-title">
          <History :size="20" /> {{ t('history.title') }}
        </h2>
        <span
          v-if="stats"
          class="history-stats"
          data-testid="history-stats"
        >
          {{ t('history.statsLine', {
            snapshots: stats.snapshots,
            files: stats.files,
            size: formatSize(stats.diskBytes),
          }) }}
        </span>
      </div>
      <button
        v-if="files.length > 0"
        class="ghost-btn"
        data-testid="prune-btn"
        :title="t('history.pruneHint')"
        @click="prune"
      >
        <Eraser :size="16" /> {{ t('history.pruneBtn') }}
      </button>
    </div>

    <div
      v-if="errorMsg"
      class="banner banner-error"
      data-testid="history-error"
    >
      <AlertTriangle :size="16" />
      <span>{{ errorMsg }}</span>
    </div>
    <div
      v-else-if="noticeMsg"
      class="banner banner-notice"
      data-testid="history-notice"
    >
      <span>{{ noticeMsg }}</span>
    </div>

    <div
      v-if="!currentWorkspace?.path"
      class="history-state"
    >
      <div class="empty-icon">
        🗂️
      </div>
      <h3>{{ t('history.noWorkspaceTitle') }}</h3>
      <p>{{ t('history.noWorkspaceDesc') }}</p>
    </div>
    <div
      v-else-if="isLoadingFiles && files.length === 0"
      class="history-state"
    >
      <div class="spinner" />
      <p>{{ t('common.loading') }}</p>
    </div>
    <div
      v-else-if="files.length === 0"
      class="history-state"
      data-testid="history-empty"
    >
      <div class="empty-icon">
        ⏳
      </div>
      <h3>{{ t('history.emptyTitle') }}</h3>
      <p>{{ t('history.emptyDesc') }}</p>
    </div>

    <div
      v-else
      class="history-body"
    >
      <aside class="files-pane">
        <div class="pane-header">
          {{ t('history.filesHeader', { count: files.length }) }}
        </div>
        <ul class="files-list">
          <li
            v-for="file in files"
            :key="file.path"
            class="file-row"
            :class="{ active: file.path === selectedPath }"
            data-testid="history-file"
            @click="selectFile(file.path)"
          >
            <FileText
              :size="16"
              class="file-row-icon"
            />
            <div class="file-row-text">
              <div class="file-row-name">
                {{ fileName(file.path) }}
              </div>
              <div class="file-row-meta">
                <span>{{ t('history.versionCount', { count: file.count }) }}</span>
                <span>{{ formatSize(file.bytes) }}</span>
              </div>
              <div
                v-if="fileDir(file.path)"
                class="file-row-dir"
              >
                {{ fileDir(file.path) }}
              </div>
            </div>
          </li>
        </ul>
      </aside>

      <section class="detail-pane">
        <div class="detail-header">
          <div class="detail-path">
            <span class="detail-name">{{ fileName(selectedPath) }}</span>
            <span
              v-if="fileDir(selectedPath)"
              class="detail-dir"
            >{{ fileDir(selectedPath) }}</span>
          </div>
          <div class="detail-actions">
            <button
              class="ghost-btn"
              data-testid="open-editor-btn"
              @click="openInEditor"
            >
              <ExternalLink :size="14" /> {{ t('history.openInEditor') }}
            </button>
            <button
              class="ghost-btn"
              data-testid="manual-snapshot-btn"
              @click="createManualSnapshot"
            >
              <Camera :size="14" /> {{ t('history.manualSnapshot') }}
            </button>
            <button
              class="danger-btn"
              data-testid="clear-file-btn"
              @click="clearFileHistory"
            >
              <Trash2 :size="14" /> {{ t('history.clearFile') }}
            </button>
          </div>
        </div>

        <div class="timeline-pane">
          <div
            v-if="isLoadingSnapshots && snapshots.length === 0"
            class="inline-state"
          >
            {{ t('common.loading') }}
          </div>
          <ul
            v-else
            class="timeline"
          >
            <li
              v-for="snap in snapshots"
              :key="snap.id"
              class="timeline-item"
              :class="{ active: snap.id === selectedID }"
              data-testid="history-version"
              @click="selectSnapshot(snap.id)"
            >
              <div class="timeline-dot" />
              <div class="timeline-body">
                <div class="timeline-line1">
                  <span class="timeline-time">{{ formatDate(snap.createdAt) }}</span>
                  <span
                    class="reason-badge"
                    :class="`reason-${snap.reason}`"
                  >{{ reasonLabel(snap.reason) }}</span>
                </div>
                <div class="timeline-line2">
                  {{ formatSize(snap.size) }}
                </div>
              </div>
              <div class="timeline-actions">
                <button
                  class="icon-btn"
                  :title="t('common.restore')"
                  data-testid="restore-btn"
                  @click.stop="restore(snap)"
                >
                  <RotateCcw :size="14" />
                </button>
                <button
                  class="icon-btn icon-btn-danger"
                  :title="t('history.deleteSnapshot')"
                  data-testid="delete-snapshot-btn"
                  @click.stop="deleteSnapshot(snap)"
                >
                  <Trash2 :size="14" />
                </button>
              </div>
            </li>
          </ul>
        </div>

        <div class="diff-pane">
          <div class="diff-toolbar">
            <div class="mode-switch">
              <button
                class="mode-btn"
                :class="{ active: compareMode === 'current' }"
                data-testid="mode-current"
                @click="setCompareMode('current')"
              >
                {{ t('history.compareCurrent') }}
              </button>
              <button
                class="mode-btn"
                :class="{ active: compareMode === 'previous' }"
                :disabled="!canCompareWithPrevious"
                data-testid="mode-previous"
                @click="setCompareMode('previous')"
              >
                {{ t('history.comparePrevious') }}
              </button>
            </div>
            <div
              v-if="diff"
              class="diff-summary"
              data-testid="diff-summary"
            >
              <span class="diff-added">+{{ diff.added }}</span>
              <span class="diff-removed">-{{ diff.removed }}</span>
              <span
                v-if="diff.truncated"
                class="diff-truncated"
              >{{ t('history.diffTruncated') }}</span>
            </div>
          </div>

          <div
            v-if="isLoadingDiff"
            class="inline-state"
          >
            {{ t('common.loading') }}
          </div>
          <div
            v-else-if="!diff"
            class="inline-state"
          >
            {{ t('history.noDiff') }}
          </div>
          <div
            v-else-if="diff.identical"
            class="inline-state"
            data-testid="diff-identical"
          >
            {{ t('history.identical') }}
          </div>
          <div
            v-else
            class="diff-body"
            data-testid="diff-body"
          >
            <div
              v-for="(op, i) in (diff.ops ?? [])"
              :key="i"
              class="diff-row"
              :class="`diff-${op.type}`"
              data-testid="diff-row"
            >
              <template v-if="op.type === 'gap'">
                <span class="diff-gutter" />
                <span class="diff-gutter" />
                <span class="diff-text diff-gap-text">{{ t('history.collapsedLines', { count: op.count ?? 0 }) }}</span>
              </template>
              <template v-else>
                <span class="diff-gutter">{{ op.oldLine || '' }}</span>
                <span class="diff-gutter">{{ op.newLine || '' }}</span>
                <span class="diff-text"><span class="diff-sign">{{ opPrefix(op.type) }}</span>{{ op.text }}</span>
              </template>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.history-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; min-height: 0; }
.history-header { display: flex; align-items: center; justify-content: space-between; padding: var(--space-6) var(--space-8); border-bottom: 1px solid var(--border); background: var(--bg-window); flex-shrink: 0; }
.header-left { display: flex; align-items: baseline; gap: var(--space-4); }
.history-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.history-stats { font-size: var(--text-sm); color: var(--text-muted); }

.banner { display: flex; align-items: center; gap: var(--space-2); padding: var(--space-2) var(--space-8); font-size: var(--text-sm); flex-shrink: 0; }
.banner-error { background: var(--danger-alpha, rgba(239,68,68,0.1)); color: var(--danger, #ef4444); border-bottom: 1px solid var(--danger, #ef4444); }
.banner-notice { background: var(--accent-alpha, rgba(59,130,246,0.1)); color: var(--accent); border-bottom: 1px solid var(--border); }

.history-state { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; color: var(--text-muted); gap: var(--space-3); }
.history-state h3 { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); margin: 0; }
.empty-icon { font-size: 48px; opacity: 0.5; }
.spinner { width: 32px; height: 32px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.history-body { flex: 1; display: flex; min-height: 0; overflow: hidden; }

.files-pane { width: 260px; flex-shrink: 0; border-right: 1px solid var(--border); display: flex; flex-direction: column; min-height: 0; background: var(--bg-sidebar); }
.pane-header { padding: var(--space-3) var(--space-4); font-size: var(--text-xs); font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); border-bottom: 1px solid var(--border); }
.files-list { flex: 1; overflow-y: auto; list-style: none; margin: 0; padding: var(--space-2); display: flex; flex-direction: column; gap: 2px; }
.file-row { display: flex; align-items: flex-start; gap: var(--space-2); padding: var(--space-2) var(--space-3); border-radius: var(--radius-sm); cursor: pointer; color: var(--text-secondary); }
.file-row:hover { background: var(--bg-hover, rgba(127,127,127,0.12)); }
.file-row.active { background: var(--accent); color: white; }
.file-row-icon { flex-shrink: 0; margin-top: 2px; }
.file-row-text { min-width: 0; flex: 1; }
.file-row-name { font-size: var(--text-sm); font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.file-row-meta { display: flex; gap: var(--space-2); font-size: var(--text-xs); opacity: 0.75; }
.file-row-dir { font-size: var(--text-xs); opacity: 0.6; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.detail-pane { flex: 1; display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.detail-header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-3) var(--space-5); border-bottom: 1px solid var(--border); flex-shrink: 0; }
.detail-path { min-width: 0; display: flex; flex-direction: column; }
.detail-name { font-size: var(--text-base); font-weight: 600; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.detail-dir { font-size: var(--text-xs); color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.detail-actions { display: flex; gap: var(--space-2); flex-shrink: 0; }

.ghost-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-1) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-card); color: var(--text-secondary); font-size: var(--text-sm); cursor: pointer; }
.ghost-btn:hover { background: var(--accent); border-color: var(--accent); color: white; }
.danger-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-1) var(--space-3); border: 1px solid var(--danger, #ef4444); border-radius: var(--radius-sm); background: transparent; color: var(--danger, #ef4444); font-size: var(--text-sm); cursor: pointer; }
.danger-btn:hover { background: var(--danger, #ef4444); color: white; }

.timeline-pane { max-height: 210px; overflow-y: auto; border-bottom: 1px solid var(--border); flex-shrink: 0; }
.timeline { list-style: none; margin: 0; padding: var(--space-2) var(--space-4); display: flex; flex-direction: column; gap: 2px; }
.timeline-item { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-2) var(--space-3); border-radius: var(--radius-sm); cursor: pointer; border: 1px solid transparent; }
.timeline-item:hover { background: var(--bg-hover, rgba(127,127,127,0.12)); }
.timeline-item.active { border-color: var(--accent); background: var(--accent-alpha, rgba(59,130,246,0.12)); }
.timeline-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-muted); flex-shrink: 0; }
.timeline-item.active .timeline-dot { background: var(--accent); }
.timeline-body { flex: 1; min-width: 0; }
.timeline-line1 { display: flex; align-items: center; gap: var(--space-2); }
.timeline-time { font-size: var(--text-sm); font-weight: 600; color: var(--text-primary); }
.timeline-line2 { font-size: var(--text-xs); color: var(--text-muted); }
.reason-badge { font-size: var(--text-xs); padding: 1px 6px; border-radius: 999px; background: var(--bg-sidebar); color: var(--text-muted); border: 1px solid var(--border); }
.reason-manual { color: var(--accent); border-color: var(--accent); }
.reason-daily { color: var(--warning, #f59e0b); border-color: var(--warning, #f59e0b); }
.reason-delete { color: var(--danger, #ef4444); border-color: var(--danger, #ef4444); }
.timeline-actions { display: flex; gap: var(--space-1); flex-shrink: 0; }
.icon-btn { display: flex; align-items: center; justify-content: center; width: 26px; height: 26px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-card); color: var(--text-secondary); cursor: pointer; }
.icon-btn:hover { background: var(--accent); border-color: var(--accent); color: white; }
.icon-btn-danger:hover { background: var(--danger, #ef4444); border-color: var(--danger, #ef4444); }

.diff-pane { flex: 1; display: flex; flex-direction: column; min-height: 0; }
.diff-toolbar { display: flex; align-items: center; justify-content: space-between; padding: var(--space-2) var(--space-5); border-bottom: 1px solid var(--border); flex-shrink: 0; }
.mode-switch { display: flex; gap: 0; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
.mode-btn { padding: var(--space-1) var(--space-3); border: none; background: var(--bg-card); color: var(--text-secondary); font-size: var(--text-sm); cursor: pointer; }
.mode-btn.active { background: var(--accent); color: white; }
.mode-btn:disabled { opacity: 0.45; cursor: not-allowed; }
.diff-summary { display: flex; gap: var(--space-3); font-size: var(--text-sm); font-variant-numeric: tabular-nums; }
.diff-added { color: var(--success, #22c55e); }
.diff-removed { color: var(--danger, #ef4444); }
.diff-truncated { color: var(--warning, #f59e0b); }

.inline-state { padding: var(--space-6); text-align: center; color: var(--text-muted); font-size: var(--text-sm); }
.diff-body { flex: 1; overflow: auto; padding: var(--space-2) 0; font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: var(--text-sm); line-height: 1.55; }
.diff-row { display: flex; align-items: flex-start; padding: 0 var(--space-4); white-space: pre-wrap; word-break: break-word; }
.diff-gutter { width: 44px; flex-shrink: 0; text-align: right; padding-right: var(--space-2); color: var(--text-muted); opacity: 0.6; user-select: none; }
.diff-text { flex: 1; min-width: 0; }
.diff-sign { display: inline-block; width: 1ch; opacity: 0.8; }
.diff-insert { background: var(--success-alpha, rgba(34,197,94,0.12)); color: var(--success, #22c55e); }
.diff-delete { background: var(--danger-alpha, rgba(239,68,68,0.12)); color: var(--danger, #ef4444); }
.diff-equal { color: var(--text-secondary); }
.diff-gap { color: var(--text-muted); background: var(--bg-sidebar); }
.diff-gap-text { font-style: italic; opacity: 0.75; }
</style>
