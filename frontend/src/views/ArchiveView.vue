<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { Archive, RotateCcw, FileText, FolderOpen } from '@lucide/vue'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { ArchiveService } from '@bindings/github.com/notevault/notevault/index.js'

interface ArchivedFile {
  path: string
  name: string
  originalPath: string
  archivedAt: string
  size: number
}

const router = useRouter()
const { t, locale } = useI18n()
const workspaceStore = useWorkspaceStore()

const files = ref<ArchivedFile[]>([])
const isLoading = ref(false)
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

async function loadFiles() {
  if (!currentWorkspace.value?.path) return
  isLoading.value = true
  try {
    const data = await ArchiveService.GetArchivedFiles(currentWorkspace.value.path)
    files.value = Array.isArray(data) ? data as ArchivedFile[] : []
  } catch (e) {
    console.error('Failed to load archived files:', e)
    files.value = []
  } finally {
    isLoading.value = false
  }
}

async function unarchive(file: ArchivedFile) {
  if (!currentWorkspace.value?.path) return
  try {
    await ArchiveService.UnarchiveFile(currentWorkspace.value.path, file.path)
    workspaceStore.incrementFileTreeVersion()
    await loadFiles()
  } catch (e) {
    console.error('Failed to unarchive:', e)
    alert(t('common.restoreFailed', { msg: (e as Error).message }))
  }
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function formatDate(iso: string): string {
  try { return new Date(iso).toLocaleDateString(locale.value) } catch { return iso }
}

onMounted(loadFiles)
watch(() => currentWorkspace.value?.id, loadFiles)
</script>

<template>
  <div class="archive-view">
    <div class="archive-header">
      <h2 class="archive-title">
        <Archive :size="20" /> {{ t('archive.title') }}
      </h2>
      <span class="archive-count">{{ t('archive.total', { count: files.length }) }}</span>
    </div>
    <div class="archive-content">
      <div
        v-if="isLoading"
        class="archive-state"
      >
        <div class="spinner" /><p>{{ t('common.loading') }}</p>
      </div>
      <div
        v-else-if="files.length === 0"
        class="archive-state"
      >
        <div class="empty-icon">
          📦
        </div>
        <h3>{{ t('archive.emptyTitle') }}</h3>
        <p>{{ t('archive.emptyDesc') }}</p>
      </div>
      <div
        v-else
        class="files-list"
      >
        <div
          v-for="file in files"
          :key="file.path"
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
              <span>{{ t('archive.archivedAt', { date: formatDate(file.archivedAt) }) }}</span>
              <span>{{ formatSize(file.size) }}</span>
            </div>
          </div>
          <button
            class="action-btn"
            @click="unarchive(file)"
          >
            <RotateCcw :size="16" /> {{ t('common.restore') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.archive-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.archive-header { display: flex; align-items: center; justify-content: space-between; padding: var(--space-6) var(--space-8); border-bottom: 1px solid var(--border); background: var(--bg-window); }
.archive-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.archive-count { font-size: var(--text-sm); color: var(--text-muted); }
.archive-content { flex: 1; overflow-y: auto; padding: var(--space-6) var(--space-8); }
.archive-state { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: var(--space-12) 0; color: var(--text-muted); gap: var(--space-3); }
.archive-state h3 { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); margin: 0; }
.empty-icon { font-size: 48px; opacity: 0.5; }
.spinner { width: 32px; height: 32px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.files-list { display: flex; flex-direction: column; gap: var(--space-2); max-width: 700px; margin: 0 auto; }
.file-item { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); }
.file-icon { display: flex; align-items: center; justify-content: center; width: 36px; height: 36px; border-radius: var(--radius-sm); background: var(--bg-sidebar); color: var(--accent); flex-shrink: 0; }
.file-info { flex: 1; min-width: 0; }
.file-name { font-size: var(--text-base); font-weight: 600; color: var(--text-primary); margin-bottom: 2px; }
.file-meta { display: flex; gap: var(--space-3); font-size: var(--text-xs); color: var(--text-muted); }
.action-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-1) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-card); color: var(--text-secondary); font-size: var(--text-sm); cursor: pointer; }
.action-btn:hover { background: var(--accent); border-color: var(--accent); color: white; }
</style>
