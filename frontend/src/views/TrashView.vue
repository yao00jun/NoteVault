<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { Trash2, RotateCcw, FileText, AlertTriangle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '@/composables/useConfirm'
import { TrashService } from '@bindings/github.com/notevault/notevault/index.js'

interface TrashedFile {
  id: string
  path: string
  name: string
  originalPath: string
  deletedAt: string
  size: number
}

const router = useRouter()
const { t, locale } = useI18n()
const workspaceStore = useWorkspaceStore()

const files = ref<TrashedFile[]>([])
const isLoading = ref(false)
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

const totalSize = computed(() => files.value.reduce((sum, f) => sum + f.size, 0))

async function loadFiles() {
  if (!currentWorkspace.value?.path) return
  isLoading.value = true
  try {
    const data = await TrashService.GetTrashedFiles(currentWorkspace.value.path)
    files.value = data as TrashedFile[]
  } catch (e) {
    console.error('Failed to load trashed files:', e)
    files.value = []
  } finally {
    isLoading.value = false
  }
}

async function restore(file: TrashedFile) {
  if (!currentWorkspace.value?.path) return
  try {
    await TrashService.RestoreFromTrash(currentWorkspace.value.path, file.id)
    workspaceStore.incrementFileTreeVersion()
    await loadFiles()
  } catch (e) {
    console.error('Failed to restore:', e)
    alert(t('common.restoreFailed', { msg: (e as Error).message }))
  }
}

async function permanentlyDelete(file: TrashedFile) {
  if (!currentWorkspace.value?.path) return
  if (!(await confirmDialog({ message: t('trash.confirmDelete', { name: file.name }), danger: true }))) return
  try {
    await TrashService.PermanentlyDelete(currentWorkspace.value.path, file.id)
    await loadFiles()
  } catch (e) {
    console.error('Failed to delete:', e)
  }
}

async function emptyTrash() {
  if (!currentWorkspace.value?.path) return
  if (!(await confirmDialog({ message: t('trash.confirmEmpty'), danger: true }))) return
  try {
    await TrashService.EmptyTrash(currentWorkspace.value.path)
    await loadFiles()
  } catch (e) {
    console.error('Failed to empty trash:', e)
  }
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function formatDate(iso: string): string {
  try { return new Date(iso).toLocaleString(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) } catch { return iso }
}

onMounted(loadFiles)
watch(() => currentWorkspace.value?.id, loadFiles)
</script>

<template>
  <div class="trash-view">
    <div class="trash-header">
      <div class="header-left">
        <h2 class="trash-title">
          <Trash2 :size="20" /> {{ t('trash.title') }}
        </h2>
        <span class="trash-count">{{ t('trash.count', { count: files.length }) }} · {{ formatSize(totalSize) }}</span>
      </div>
      <button
        v-if="files.length > 0"
        class="empty-btn"
        @click="emptyTrash"
      >
        <Trash2 :size="16" /> {{ t('trash.emptyBtn') }}
      </button>
    </div>
    <div class="trash-content">
      <div
        v-if="isLoading"
        class="trash-state"
      >
        <div class="spinner" /><p>{{ t('common.loading') }}</p>
      </div>
      <div
        v-else-if="files.length === 0"
        class="trash-state"
      >
        <div class="empty-icon">
          🗑️
        </div>
        <h3>{{ t('trash.emptyTitle') }}</h3>
        <p>{{ t('trash.emptyDesc') }}</p>
      </div>
      <div
        v-else
        class="warning-banner"
      >
        <AlertTriangle :size="16" />
        <span>{{ t('trash.hint') }}</span>
      </div>
      <div
        v-if="files.length > 0"
        class="files-list"
      >
        <div
          v-for="file in files"
          :key="file.id"
          class="file-item"
        >
          <div class="file-icon">
            <FileText :size="18" />
          </div>
          <div class="file-info">
            <div class="file-name">
              {{ file.name }}
            </div>
            <div class="file-meta">
              <span>{{ t('trash.deletedAt', { date: formatDate(file.deletedAt) }) }}</span>
              <span>{{ formatSize(file.size) }}</span>
            </div>
          </div>
          <div class="file-actions">
            <button
              class="restore-btn"
              @click="restore(file)"
            >
              <RotateCcw :size="16" /> {{ t('common.restore') }}
            </button>
            <button
              class="delete-btn"
              @click="permanentlyDelete(file)"
            >
              <Trash2 :size="16" /> {{ t('trash.deletePermanent') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.trash-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.trash-header { display: flex; align-items: center; justify-content: space-between; padding: var(--space-6) var(--space-8); border-bottom: 1px solid var(--border); background: var(--bg-window); }
.header-left { display: flex; align-items: center; gap: var(--space-4); }
.trash-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.trash-count { font-size: var(--text-sm); color: var(--text-muted); }
.empty-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-2) var(--space-3); border: 1px solid var(--danger, #ef4444); border-radius: var(--radius-md); background: transparent; color: var(--danger, #ef4444); font-size: var(--text-sm); cursor: pointer; }
.empty-btn:hover { background: var(--danger, #ef4444); color: white; }
.trash-content { flex: 1; overflow-y: auto; padding: var(--space-6) var(--space-8); }
.trash-state { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: var(--space-12) 0; color: var(--text-muted); gap: var(--space-3); }
.trash-state h3 { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); margin: 0; }
.empty-icon { font-size: 48px; opacity: 0.5; }
.spinner { width: 32px; height: 32px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.warning-banner { display: flex; align-items: center; gap: var(--space-2); padding: var(--space-2) var(--space-3); margin-bottom: var(--space-4); background: var(--warning-alpha, rgba(245,158,11,0.1)); border: 1px solid var(--warning, #f59e0b); border-radius: var(--radius-sm); color: var(--warning, #f59e0b); font-size: var(--text-sm); max-width: 700px; margin-left: auto; margin-right: auto; }
.files-list { display: flex; flex-direction: column; gap: var(--space-2); max-width: 700px; margin: 0 auto; }
.file-item { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); }
.file-icon { display: flex; align-items: center; justify-content: center; width: 36px; height: 36px; border-radius: var(--radius-sm); background: var(--bg-sidebar); color: var(--text-muted); flex-shrink: 0; }
.file-info { flex: 1; min-width: 0; }
.file-name { font-size: var(--text-base); font-weight: 600; color: var(--text-primary); margin-bottom: 2px; }
.file-meta { display: flex; gap: var(--space-3); font-size: var(--text-xs); color: var(--text-muted); }
.file-actions { display: flex; gap: var(--space-2); flex-shrink: 0; }
.restore-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-1) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-card); color: var(--text-secondary); font-size: var(--text-sm); cursor: pointer; }
.restore-btn:hover { background: var(--accent); border-color: var(--accent); color: white; }
.delete-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-1) var(--space-3); border: 1px solid var(--danger, #ef4444); border-radius: var(--radius-sm); background: transparent; color: var(--danger, #ef4444); font-size: var(--text-sm); cursor: pointer; }
.delete-btn:hover { background: var(--danger, #ef4444); color: white; }
</style>
