<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Import, MoreHorizontal, FileText, Download, Zap } from '@lucide/vue'
import { FileService, ExportService } from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { requestSourceImport } from '@/composables/useSourceImport'
import { openDocument } from '@/utils/navigation'
import { isImeComposing } from '@/utils/ime'
import TemplateCreateDialog from './TemplateCreateDialog.vue'

const props = defineProps<{ folder?: string }>()
const workspace = useWorkspaceStore()
const router = useRouter()
const toast = useToast()
const showTemplate = ref(false)
const exporting = ref(false)
const capture = ref('')
const capturing = ref(false)
const menu = ref<HTMLDetailsElement | null>(null)

function newDocument() { window.dispatchEvent(new CustomEvent('notevault:new-file', { detail: { folder: props.folder || 'Inbox' } })) }
function created(path: string) {
  showTemplate.value = false
  workspace.incrementFileTreeVersion()
  void openDocument(router, workspace, path)
}
async function exportWorkspace() {
  const ws = workspace.currentWorkspace
  if (!ws || exporting.value) return
  exporting.value = true
  try {
    const { Dialogs } = await import('@wailsio/runtime')
    const result = await Dialogs.SaveFile({ Title: '导出工作区 Markdown', Filename: `${ws.name}-export.zip`, Filters: [{ DisplayName: 'ZIP 压缩包', Pattern: '*.zip' }] })
    let destination = Array.isArray(result) ? result[0] : result
    if (!destination || ws.path !== workspace.currentWorkspace?.path) return
    if (!destination.toLowerCase().endsWith('.zip')) destination += '.zip'
    await ExportService.ExportWorkspaceMarkdown(ws.path, destination)
    toast.success(`已导出至 ${destination}`)
  } catch (error) { toast.error(`导出失败：${error instanceof Error ? error.message : String(error)}`) }
  finally { exporting.value = false }
}
async function quickCapture(event: KeyboardEvent) {
  if (isImeComposing(event) || capturing.value || !capture.value.trim()) return
  const ws = workspace.currentWorkspace
  if (!ws) return
  const text = capture.value.trim()
  capturing.value = true
  try {
    const now = new Date()
    const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    const stamp = `${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}${String(now.getSeconds()).padStart(2, '0')}-${String(now.getMilliseconds()).padStart(3, '0')}`
    // A unique note avoids read/append races with editors and external sync.
    await FileService.CreateFile(ws.path, `Inbox/${date}-${stamp}-闪念.md`, `# 闪念\n\n${text}\n`)
    if (workspace.currentWorkspace?.path === ws.path) {
      capture.value = ''
      workspace.incrementFileTreeVersion()
      toast.success('已记入收集箱')
    }
  } catch (error) { toast.error(`记录失败：${error instanceof Error ? error.message : String(error)}`) }
  finally { capturing.value = false }
}
watch(() => workspace.currentWorkspace?.path, () => { showTemplate.value = false; capture.value = ''; if (menu.value) menu.value.open = false })
</script>

<template>
  <div class="vault-actions">
    <button
      class="collection-button primary"
      type="button"
      data-testid="vault-new-document"
      @click="newDocument"
    >
      <Plus :size="15" />新建文档
    </button>
    <button
      class="collection-button"
      type="button"
      @click="requestSourceImport({ kind: 'topic', sourceType: 'folder' })"
    >
      <Import :size="15" />从资料创建
    </button>
    <details
      ref="menu"
      class="vault-action-menu"
    >
      <summary
        class="collection-button"
        aria-label="更多文档操作"
      >
        <MoreHorizontal :size="17" />
      </summary>
      <div class="vault-action-popover">
        <button @click="showTemplate = true; menu!.open = false">
          <FileText :size="15" />从模板创建
        </button>
        <button
          :disabled="exporting"
          @click="exportWorkspace"
        >
          <Download :size="15" />{{ exporting ? '正在导出…' : '导出工作区 Markdown' }}
        </button>
        <label><span><Zap :size="14" />快速记录</span><input
          v-model="capture"
          aria-label="快速记录"
          placeholder="记下想法，回车存入收集箱"
          :disabled="capturing"
          @keydown.enter="quickCapture"
        ></label>
      </div>
    </details>
    <TemplateCreateDialog
      v-if="showTemplate"
      :default-folder="folder"
      @close="showTemplate = false"
      @created="created"
    />
  </div>
</template>

<style scoped>
.vault-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.vault-action-menu { position: relative; }
.vault-action-menu > summary { list-style: none; cursor: pointer; }
.vault-action-menu > summary::-webkit-details-marker { display: none; }
.vault-action-popover { position: absolute; z-index: 30; right: 0; top: calc(100% + 6px); width: 275px; padding: 8px; border: 1px solid var(--border); border-radius: var(--panel-radius, 10px); background: var(--surface-panel, var(--bg-card)); box-shadow: var(--shadow-lg); }
.vault-action-popover button { display: flex; gap: 8px; width: 100%; padding: 10px; border: 0; border-radius: var(--control-radius, 6px); color: var(--text-primary); background: transparent; text-align: left; font-size: 12px; }
.vault-action-popover button:hover { background: var(--bg-hover); }
.vault-action-popover label { display: grid; gap: 8px; padding: 10px; border-top: 1px solid var(--border); font-size: 12px; color: var(--text-secondary); }
.vault-action-popover label span { display: flex; gap: 6px; align-items: center; }
.vault-action-popover input { padding: 8px; border: 1px solid var(--border); border-radius: var(--control-radius, 6px); background: var(--bg-content); color: var(--text-primary); font: inherit; width: 100%; box-sizing: border-box; }
</style>
