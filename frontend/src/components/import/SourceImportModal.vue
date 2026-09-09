<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="source-import-overlay"
      @click.self="emit('close')"
    >
      <section
        ref="dialog"
        class="source-import-modal source-import-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="source-import-title"
        aria-describedby="source-import-description"
        tabindex="-1"
        @keydown="onDialogKeydown"
      >
        <header class="si-header">
          <div>
            <p class="si-eyebrow">
              工作台 · 新建
            </p>
            <h2 id="source-import-title">
              {{ intake.hasTask ? '来源任务' : '把资料带进工作台' }}
            </h2>
            <p
              id="source-import-description"
              class="si-hint"
            >
              创建项目、图书或专题，把已有资料整理到一个地方。
            </p>
          </div>
          <button
            type="button"
            class="si-close"
            aria-label="关闭新建窗口"
            @click="emit('close')"
          >
            <X :size="18" />
          </button>
        </header>

        <div
          v-if="workspace.currentWorkspace"
          class="si-workspace"
        >
          <FolderOpen
            :size="15"
            aria-hidden="true"
          />
          <strong>{{ workspace.currentWorkspace.name }}</strong>
          <span>{{ workspace.currentWorkspace.path }}</span>
        </div>
        <p
          v-if="!state"
          role="alert"
          class="si-error"
        >
          请先打开工作区后再新建。
        </p>

        <template v-else>
          <form
            v-if="!intake.hasTask"
            class="si-form"
            @submit.prevent="submit"
            @keydown.enter="guardComposition"
          >
            <div class="si-field si-kind">
              <label for="source-kind">类型</label>
              <select
                id="source-kind"
                v-model="state.draft.kind"
                :disabled="state.readingFiles"
                data-testid="source-kind"
              >
                <option value="project">
                  项目
                </option>
                <option value="book">
                  图书
                </option>
                <option value="topic">
                  专题
                </option>
              </select>
            </div>

            <div
              class="si-tabs"
              role="tablist"
              aria-label="资料来源"
            >
              <button
                v-for="(tab, index) in sourceTabs"
                :id="`source-tab-${tab.value}`"
                :key="tab.value"
                type="button"
                role="tab"
                class="si-tab"
                :class="{ active: state.draft.sourceType === tab.value }"
                :aria-selected="state.draft.sourceType === tab.value"
                aria-controls="source-panel"
                :tabindex="state.draft.sourceType === tab.value ? 0 : -1"
                :disabled="state.readingFiles"
                @click="selectSource(tab.value)"
                @keydown="onTabKeydown($event, index)"
              >
                {{ tab.label }}
              </button>
            </div>

            <div
              id="source-panel"
              class="si-source"
              role="tabpanel"
              :aria-labelledby="`source-tab-${state.draft.sourceType}`"
            >
              <p
                v-if="state.draft.sourceType === 'empty'"
                class="si-hint"
              >
                从一个空白{{ kindLabel }}开始，之后随时添加笔记和资料。
              </p>
              <template v-else-if="state.draft.sourceType !== 'files'">
                <label for="source-location">{{ state.draft.sourceType === 'url' ? '网页、仓库或在线资料链接' : ['adopt', 'attachments'].includes(state.draft.sourceType) ? '工作区中的已有目录' : '来源文件夹' }}</label>
                <div class="si-location-row">
                  <input
                    id="source-location"
                    v-model="state.draft.source"
                    data-testid="source-location"
                    type="text"
                    autocomplete="off"
                    spellcheck="false"
                    :placeholder="state.draft.sourceType === 'url' ? 'https://…' : ['adopt', 'attachments'].includes(state.draft.sourceType) ? `${space}/集合名称` : '输入绝对路径，如 E:\\资料\\项目'"
                  >
                  <button
                    v-if="!['url', 'attachments'].includes(state.draft.sourceType)"
                    type="button"
                    class="si-btn"
                    data-testid="source-pick-folder"
                    :disabled="choosingFolder"
                    @click="chooseFolder"
                  >
                    选择文件夹
                  </button>
                </div>
                <p class="si-hint">
                  {{ state.draft.sourceType === 'attachments' ? '按标题、段落和代码块提取 PDF 正文。未改动的旧章节可更新排版，原文件和已编辑笔记会保留。' : state.draft.sourceType === 'adopt' ? '为已有目录补齐工作台入口，保留原有笔记内容。' : state.draft.sourceType === 'folder' ? '复制支持的资料，保留来源文件夹。' : '支持公开网页、GitHub 仓库及可下载的资料链接。' }}
                </p>
              </template>
              <template v-else>
                <label
                  class="si-dropzone"
                  :class="{ dragging }"
                  data-testid="source-dropzone"
                  for="source-files"
                  @dragover.prevent="dragging = true"
                  @dragleave.prevent="dragging = false"
                  @drop.prevent="onDrop"
                >
                  <Upload
                    :size="22"
                    aria-hidden="true"
                  />
                  <strong>{{ state.readingFiles ? '正在读取文件…' : '选择文件，或拖到这里' }}</strong>
                  <span>Markdown、文本、PDF、Word、EPUB、ZIP 等</span>
                  <span>单个文件 20 MiB · 每次共 100 MiB，最多 500 个</span>
                  <input
                    id="source-files"
                    data-testid="source-files"
                    class="si-file-input"
                    type="file"
                    multiple
                    aria-label="选择资料文件"
                    :disabled="state.readingFiles"
                    @change="onFileSelection"
                  >
                </label>
                <p
                  v-if="state.draft.source"
                  class="si-hint"
                >
                  请在文件选择器中选择：{{ state.draft.source }}
                </p>
                <ul
                  v-if="state.draft.files.length"
                  class="si-files"
                  aria-label="已选文件"
                >
                  <li
                    v-for="(file, index) in state.draft.files"
                    :key="file.name"
                  >
                    <span>{{ file.name }}</span>
                    <button
                      type="button"
                      :aria-label="`移除 ${file.name}`"
                      :disabled="state.readingFiles"
                      @click="intake.removeFile(index)"
                    >
                      <X :size="14" />
                    </button>
                  </li>
                </ul>
              </template>
            </div>

            <div class="si-fields">
              <div class="si-field">
                <label for="source-name">名称<span v-if="state.draft.sourceType !== 'empty'">（可从来源推断）</span></label>
                <input
                  id="source-name"
                  v-model="state.draft.name"
                  data-testid="source-name"
                  type="text"
                  :placeholder="`${kindLabel}名称`"
                  :disabled="state.readingFiles"
                >
              </div>
              <div class="si-field">
                <label for="source-destination">保存位置<span>（可留空）</span></label>
                <input
                  id="source-destination"
                  v-model="state.draft.targetFolder"
                  data-testid="source-destination"
                  type="text"
                  :placeholder="`${space}/${state.draft.name.trim() || '集合名称'}`"
                  spellcheck="false"
                  :disabled="state.readingFiles"
                >
              </div>
            </div>
            <div class="si-destination">
              <span>工作台入口</span>
              <code
                v-if="metadataPath"
                data-testid="source-metadata-path"
              >{{ metadataPath }}</code>
              <span
                v-else
                class="si-hint"
              >预览后显示完整的 Markdown 保存路径</span>
            </div>

            <details class="si-options">
              <summary>导入选项与 AI 梳理</summary>
              <div class="si-field">
                <label for="source-conflict">同名文件</label>
                <select
                  id="source-conflict"
                  v-model="state.draft.conflictStrategy"
                  data-testid="source-conflict"
                >
                  <option value="skip">
                    保留已有文件，跳过同名项
                  </option>
                  <option value="update">
                    更新来源文件，保留手动修改
                  </option>
                  <option value="copy">
                    另存副本
                  </option>
                </select>
              </div>
              <label class="si-checkbox"><input
                v-model="state.draft.enrich"
                type="checkbox"
                data-testid="source-enrich"
              >导入后用 AI 整理</label>
              <p
                v-if="state.draft.enrich && !aiConfigured"
                class="si-hint"
              >
                尚未配置 AI，将先完成资料导入。之后可以在设置中配置 AI。
              </p>
              <div class="si-field">
                <label for="source-instruction">整理要求<span>（可选，仅用于 AI 梳理）</span></label>
                <textarea
                  id="source-instruction"
                  v-model="state.draft.instruction"
                  data-testid="source-instruction"
                  rows="2"
                  maxlength="4000"
                  placeholder="例如：按章节梳理关键概念，保留原文引用"
                />
              </div>
            </details>

            <details
              v-if="state.preview"
              class="si-preview"
              open
            >
              <summary>{{ state.preview.existing ? '已有目录' : '将创建' }} · {{ state.preview.fileCount }} 个源文件</summary>
              <code>{{ state.preview.metadataPath }}</code>
              <p
                v-if="state.preview.existing"
                class="si-hint"
              >
                同名文件会按所选策略处理，已有笔记内容会保留。
              </p>
              <ul v-if="state.preview.files.length">
                <li
                  v-for="file in state.preview.files.slice(0, 8)"
                  :key="file"
                >
                  {{ file }}
                </li>
              </ul>
              <p
                v-if="state.preview.files.length > 8"
                class="si-hint"
              >
                还有 {{ state.preview.files.length - 8 }} 项
              </p>
              <ul
                v-if="state.preview.warnings.length"
                class="si-warnings"
              >
                <li
                  v-for="warning in state.preview.warnings"
                  :key="warning"
                >
                  {{ warning }}
                </li>
              </ul>
            </details>

            <p
              v-if="state.error"
              class="si-error"
              role="alert"
            >
              {{ state.error }}
            </p>
            <footer class="si-actions">
              <button
                type="button"
                class="si-btn"
                data-testid="source-preview"
                :disabled="state.previewing || state.readingFiles"
                @click="intake.preview()"
              >
                {{ state.previewing ? '正在预览…' : '预览来源' }}
              </button>
              <button
                type="submit"
                class="si-btn primary"
                data-testid="source-start"
                :disabled="!canSubmit || state.previewing || state.readingFiles"
              >
                {{ state.draft.sourceType === 'empty' ? `创建${kindLabel}` : `导入并创建${kindLabel}` }}
              </button>
            </footer>
          </form>

          <div
            v-else
            class="si-task"
            aria-live="polite"
          >
            <section
              v-if="intake.busy"
              class="si-progress"
            >
              <div class="si-progress-heading">
                <Loader2
                  v-if="state.phase !== 'unknown'"
                  class="si-spin"
                  :size="19"
                  aria-hidden="true"
                /><h3>{{ state.phase === 'unknown' ? '任务状态待确认' : state.cancelRequested ? '正在取消…' : state.phase === 'starting' ? '正在准备来源…' : '正在导入资料' }}</h3>
              </div>
              <p>{{ state.task?.message || (state.phase === 'starting' ? '正在检查来源和保存位置' : '正在等待任务进度') }}</p>
              <div
                v-if="state.phase !== 'unknown'"
                class="si-progress-track"
                role="progressbar"
                aria-label="来源导入进度"
                aria-valuemin="0"
                aria-valuemax="100"
                :aria-valuenow="state.task?.total ? progress : undefined"
              >
                <span :style="{ width: `${progress}%` }" />
              </div>
              <div
                v-if="state.task?.total"
                class="si-progress-count"
              >
                {{ state.task.done }} / {{ state.task.total }} · {{ progress }}%
              </div>
              <code v-if="state.preview">{{ state.preview.metadataPath }}</code>
              <p class="si-hint">
                {{ state.phase === 'unknown' ? '状态确认前不会重复提交导入。' : '关闭窗口后任务仍会继续，可从后台任务入口重新查看。' }}
              </p>
            </section>

            <section
              v-if="state.result"
              class="si-result"
            >
              <h3>{{ resultTitle }}</h3>
              <p
                v-if="state.draft.sourceType !== 'empty' && (state.result.readable || state.result.attachments)"
                class="si-hint"
                data-testid="source-content-summary"
              >
                正文可用 {{ state.result.readable ?? 0 }} 份 · 仅保存附件 {{ state.result.attachments ?? 0 }} 份
              </p>
              <code>{{ state.result.metadataPath }}</code>
              <div class="si-stats">
                <div data-testid="source-result-imported">
                  <strong>{{ state.result.imported }}</strong><span>新增</span>
                </div>
                <div data-testid="source-result-updated">
                  <strong>{{ state.result.updated }}</strong><span>更新</span>
                </div>
                <div data-testid="source-result-skipped">
                  <strong>{{ state.result.skipped }}</strong><span>跳过</span>
                </div>
                <div data-testid="source-result-conflicts">
                  <strong>{{ state.result.conflicts.length }}</strong><span>冲突</span>
                </div>
              </div>
              <details v-if="state.result.conflicts.length">
                <summary>查看冲突文件</summary><ul>
                  <li
                    v-for="file in state.result.conflicts"
                    :key="file"
                  >
                    {{ file }}
                  </li>
                </ul>
              </details>
              <ul
                v-if="state.result.warnings.length"
                class="si-warnings"
              >
                <li
                  v-for="warning in state.result.warnings"
                  :key="warning"
                >
                  {{ warning }}
                </li>
              </ul>
            </section>

            <p
              v-if="state.error"
              class="si-error"
              role="alert"
            >
              {{ state.error }}
            </p>
            <footer class="si-actions">
              <button
                v-if="state.phase === 'unknown' && state.taskId"
                type="button"
                class="si-btn"
                data-testid="source-check-status"
                @click="intake.checkStatus()"
              >
                重新检查状态
              </button>
              <button
                v-if="state.phase === 'unknown' && !state.taskId"
                type="button"
                class="si-btn"
                data-testid="source-inspected-reset"
                @click="intake.resetAfterInspection()"
              >
                已检查工作区，返回填写
              </button>
              <button
                v-if="intake.busy && state.taskId && !['failed', 'cancelled', 'succeeded'].includes(state.task?.status ?? '')"
                type="button"
                class="si-btn"
                data-testid="source-cancel-task"
                :disabled="state.cancelRequested"
                @click="intake.cancel()"
              >
                {{ state.cancelRequested ? '正在取消…' : '取消任务' }}
              </button>
              <button
                v-if="state.result && ['failed', 'cancelled'].includes(state.phase)"
                type="button"
                class="si-btn"
                data-testid="source-retry"
                @click="intake.retry(aiConfig)"
              >
                重试未完成的导入
              </button>
              <button
                v-if="state.result"
                type="button"
                class="si-btn"
                @click="newDraft"
              >
                新建另一项
              </button>
              <button
                v-if="state.result?.metadataPath"
                type="button"
                class="si-btn primary"
                data-testid="source-open-result"
                @click="openResult"
              >
                打开{{ kindLabel }}
              </button>
              <button
                v-else
                type="button"
                class="si-btn primary"
                @click="emit('close')"
              >
                {{ state.phase === 'unknown' ? '关闭并检查工作区' : '在后台继续' }}
              </button>
            </footer>
          </div>
        </template>
      </section>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onScopeDispose, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { FolderOpen, Loader2, Upload, X } from '@lucide/vue'
import { AppService } from '@/api'
import type { SourceImportOpenRequest, SourceImportResult, SourceImportType } from '@/api/sourceImport'
import { collectionMetadata, collectionSpaces, useSourceImport } from '@/composables/useSourceImport'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { isLocalBaseURL } from '@/utils/localEndpoint'
import { isImeComposing } from '@/utils/ime'

const props = defineProps<{ open: boolean; request?: SourceImportOpenRequest | null }>()
const emit = defineEmits<{ close: []; completed: [result: SourceImportResult] }>()
const router = useRouter()
const workspace = useWorkspaceStore()
const settings = useSettingsStore()
const intake = useSourceImport()
const state = computed(() => intake.current)
const dialog = ref<HTMLElement | null>(null)
const dragging = ref(false)
const choosingFolder = ref(false)
let opener: HTMLElement | null = null
let mounted = true

const sourceTabOptions: { value: SourceImportType; label: string }[] = [
  { value: 'empty', label: '空白新建' }, { value: 'folder', label: '本地文件夹' },
  { value: 'files', label: '上传文件' }, { value: 'url', label: '链接' }, { value: 'adopt', label: '已有目录' }, { value: 'attachments', label: '已有 PDF' },
]
const sourceTabs = computed(() => sourceTabOptions.filter(tab => tab.value !== 'attachments' || state.value?.draft.kind === 'book'))
const kindLabel = computed(() => ({ project: '项目', book: '图书', topic: '专题' }[state.value?.draft.kind ?? 'project']))
const space = computed(() => collectionSpaces[state.value?.draft.kind ?? 'project'])
const aiConfig = computed(() => ({ apiKey: settings.settings.ai.apiKey ?? '', baseURL: settings.settings.ai.baseURL ?? '', model: settings.settings.ai.model ?? '' }))
const aiConfigured = computed(() => !!aiConfig.value.baseURL && !!aiConfig.value.model && (!!aiConfig.value.apiKey || isLocalBaseURL(aiConfig.value.baseURL)))
const progress = computed(() => Math.round(Math.max(0, Math.min(100, Number(state.value?.task?.percent) || 0))))
const resultTitle = computed(() => {
  const entry = state.value
  if (!entry?.result) return ''
  const hasSaved = !!entry.result.metadataPath || entry.result.files.length > 0 || entry.result.imported + entry.result.updated > 0
  if (entry.phase === 'cancelled' || entry.result.cancelled) return hasSaved ? '已取消，已保存的内容会保留' : '已取消'
  if (entry.phase === 'failed') return hasSaved ? '任务未完成，已保存的内容会保留' : '任务未完成'
  if (entry.result.attachments) return entry.result.readable ? '部分正文已提取，原始附件已保留' : '原始资料已保存，正文尚未提取'
  return entry.draft.sourceType === 'empty' ? '创建完成' : '导入完成'
})
const canSubmit = computed(() => {
  const draft = state.value?.draft
  return !!draft && (draft.sourceType === 'empty' ? !!draft.name.trim() : draft.sourceType === 'files' ? draft.files.length > 0 : !!draft.source.trim())
})
const metadataPath = computed(() => {
  const entry = state.value
  if (!entry) return ''
  if (entry.preview) return entry.preview.metadataPath
  const name = entry.draft.name.trim()
  const target = entry.draft.targetFolder.trim().replace(/\\/g, '/').replace(/\/+$/, '')
  return target || name ? `${target || `${space.value}/${name}`}/${collectionMetadata[entry.draft.kind]}` : ''
})

watch([() => props.open, () => props.request], ([open, request]) => {
  if (!open) return
  const accepted = intake.prepare(request ?? {}, true)
  if (accepted && request?.autoStart && request.kind && request.source?.trim() && ['folder', 'adopt', 'url', 'attachments'].includes(request.sourceType ?? '')) void intake.start(aiConfig.value)
}, { immediate: true })

watch(() => state.value?.draft, () => intake.invalidatePreview(), { deep: true })
watch(() => state.value?.draft.kind, kind => {
  if (kind !== 'book' && state.value?.draft.sourceType === 'attachments') state.value.draft.sourceType = 'empty'
})
watch(() => state.value?.result, result => {
  const entry = state.value
  if (result && entry && !entry.completionEmitted) {
    entry.completionEmitted = true
    emit('completed', result)
  }
}, { immediate: true })

watch(() => props.open, async open => {
  if (open) {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    await nextTick()
    if (mounted && props.open) (dialog.value?.querySelector<HTMLElement>('#source-name, [data-testid="source-open-result"]') ?? dialog.value)?.focus()
  } else if (opener?.isConnected) opener.focus()
}, { immediate: true })
watch(() => state.value?.phase, async () => {
  await nextTick()
  if (mounted && props.open && dialog.value && !dialog.value.contains(document.activeElement)) dialog.value.focus()
})

function selectSource(type: SourceImportType) {
  if (state.value && !state.value.readingFiles) state.value.draft.sourceType = type
}

function onTabKeydown(event: KeyboardEvent, index: number) {
  if (isImeComposing(event) || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const tabs = sourceTabs.value
  const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
  const tab = tabs[nextIndex]!
  selectSource(tab.value)
  void nextTick(() => dialog.value?.querySelector<HTMLElement>(`#source-tab-${tab.value}`)?.focus())
}

async function chooseFolder() {
  const entry = state.value
  if (!entry || choosingFolder.value) return
  const draft = entry.draft
  choosingFolder.value = true
  try {
    const path = await AppService.OpenFolderDialog()
    if (mounted && state.value === entry && entry.draft === draft && path) entry.draft.source = path
  } catch (error) {
    if (mounted && state.value === entry && entry.draft === draft) entry.error = `无法打开文件夹选择器：${error instanceof Error ? error.message : String(error)}。也可以手动输入绝对路径。`
  } finally { choosingFolder.value = false }
}

async function onFileSelection(event: Event) {
  const input = event.target as HTMLInputElement
  await intake.addFiles(Array.from(input.files ?? []))
  input.value = ''
}

async function onDrop(event: DragEvent) {
  dragging.value = false
  await intake.addFiles(Array.from(event.dataTransfer?.files ?? []))
}

function guardComposition(event: KeyboardEvent) {
  if (isImeComposing(event)) event.preventDefault()
}

function submit() { void intake.start(aiConfig.value) }
function newDraft() { intake.prepare({ kind: state.value?.draft.kind ?? 'project' }) }

async function openResult() {
  const result = state.value?.result
  if (!result?.metadataPath) return
  if (result.metadataPath.endsWith('/book.md')) await router.push({ path: '/learning', query: { book: result.targetFolder } })
  else if (result.metadataPath.endsWith('/project.md')) await router.push({ path: '/projects', query: { project: result.targetFolder } })
  else {
    workspace.openFile(result.metadataPath)
    await router.push({ path: '/editor', query: { file: result.metadataPath } })
  }
  emit('close')
}

function onDialogKeydown(event: KeyboardEvent) {
  if (isImeComposing(event)) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); emit('close'); return }
  if (event.key !== 'Tab' || !dialog.value) return
  const controls = Array.from(dialog.value.querySelectorAll<HTMLElement>('button:not(:disabled):not([tabindex="-1"]), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, a[href]'))
    .filter(element => !element.closest('details:not([open])') || element.tagName === 'SUMMARY')
  const first = controls[0]
  const last = controls[controls.length - 1]
  if (!first || !last) { event.preventDefault(); dialog.value.focus(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) { event.preventDefault(); first.focus() }
}

onScopeDispose(() => { mounted = false; if (props.open && opener?.isConnected) opener.focus() })
</script>

<style scoped>
.source-import-overlay { position: fixed; inset: 0; z-index: 400; display: grid; place-items: center; padding: 20px; background: var(--overlay-bg, rgb(0 0 0 / .48)); }
.source-import-dialog { display: flex; flex-direction: column; gap: 20px; width: min(660px, 100%); max-height: min(90dvh, 900px); overflow-y: auto; overscroll-behavior: contain; padding: var(--panel-padding, 24px); border: 1px solid var(--border); border-radius: var(--panel-radius, 16px); background: var(--surface-panel, var(--bg-card)); color: var(--text-primary); box-shadow: 0 24px 80px rgb(0 0 0 / .24); }
.si-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; }
.si-header h2 { margin: 3px 0 8px; font-size: var(--text-xl, 20px); }
.si-eyebrow { margin: 0; color: var(--text-muted); font-size: var(--text-xs, 12px); }
.si-hint { margin: 0; color: var(--text-secondary); font-size: var(--text-sm, 13px); line-height: 1.6; }
.si-close, .si-files button { display: inline-flex; align-items: center; justify-content: center; padding: 6px; color: var(--text-secondary); background: transparent; border: 0; border-radius: var(--control-radius, 6px); cursor: pointer; }
.si-close:hover, .si-files button:hover { background: var(--bg-hover); }
.si-workspace { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; color: var(--text-secondary); font-size: var(--text-xs, 12px); }
.si-workspace strong { font-weight: 600; color: var(--text-primary); }
.si-workspace span { overflow-wrap: anywhere; }
.si-form, .si-task { display: flex; flex-direction: column; gap: 18px; }
.si-kind { max-width: 190px; }
.si-tabs { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 4px; }
.si-tab { min-height: var(--control-height, 36px); padding: 8px 5px; border: 1px solid transparent; border-radius: var(--control-radius, 8px); background: var(--surface-inset, var(--bg-input)); color: var(--text-secondary); font: inherit; font-size: var(--text-sm, 13px); cursor: pointer; }
.si-tab.active { background: var(--selection-bg, var(--accent-muted)); border-color: var(--selection-border, var(--accent)); color: var(--selection-text, var(--text-primary)); }
.si-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.si-field, .si-source { display: flex; flex-direction: column; gap: 7px; min-width: 0; }
.si-field label, .si-source > label:not(.si-dropzone) { color: var(--text-primary); font-size: var(--text-sm, 13px); font-weight: 500; }
.si-field label span { color: var(--text-muted); font-size: var(--text-xs, 12px); font-weight: 400; }
.source-import-dialog input:not([type="checkbox"]):not([type="file"]), .source-import-dialog select, .source-import-dialog textarea { box-sizing: border-box; width: 100%; min-width: 0; min-height: var(--control-height, 36px); padding: 8px 10px; background: var(--bg-input); color: var(--text-primary); border: 1px solid var(--border); border-radius: var(--control-radius, 8px); font: inherit; font-size: var(--text-sm, 13px); }
.source-import-dialog textarea { resize: vertical; min-height: 70px; }
.source-import-dialog :focus-visible { outline: 2px solid var(--focus-color, var(--accent)); outline-offset: 2px; }
.si-location-row { display: flex; gap: 8px; }
.si-location-row .si-btn { flex-shrink: 0; }
.si-dropzone { position: relative; display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 22px 14px; border: 1px dashed var(--border); border-radius: var(--control-radius, 8px); text-align: center; cursor: pointer; background: var(--surface-inset, var(--bg-input)); }
.si-dropzone span { color: var(--text-secondary); font-size: var(--text-xs, 12px); }
.si-dropzone.dragging, .si-dropzone:focus-within { border-color: var(--accent); background: var(--selection-bg, var(--bg-hover)); }
.si-file-input { position: absolute; width: 1px; height: 1px; padding: 0; opacity: 0; overflow: hidden; }
.si-files { display: flex; flex-direction: column; gap: 4px; margin: 0; padding: 0; list-style: none; max-height: 180px; overflow-y: auto; }
.si-files li { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: var(--text-sm, 13px); }
.si-files li span { min-width: 0; overflow-wrap: anywhere; }
.si-destination { display: flex; flex-direction: column; gap: 6px; font-size: var(--text-xs, 12px); color: var(--text-secondary); }
.source-import-dialog code { color: var(--text-primary); font-family: var(--font-metadata, var(--font-mono, monospace)); font-size: var(--text-xs, 12px); overflow-wrap: anywhere; }
.si-options { border-top: 1px solid var(--border); padding-top: 14px; }
.source-import-dialog summary { cursor: pointer; font-size: var(--text-sm, 13px); color: var(--text-secondary); }
.si-options > :not(summary) { margin-top: 12px; }
.si-checkbox { display: flex; align-items: center; gap: 8px; font-size: var(--text-sm, 13px); }
.si-checkbox input { accent-color: var(--accent); }
.si-preview, .si-progress, .si-result { padding: 16px; border: 1px solid var(--border); background: var(--surface-inset, var(--bg-input)); border-radius: var(--control-radius, 8px); }
.si-preview > :not(summary) { margin-top: 10px; }
.si-preview ul, .si-result ul { margin: 10px 0 0; padding-left: 20px; font-size: var(--text-sm, 13px); line-height: 1.7; overflow-wrap: anywhere; }
.si-warnings { color: var(--text-secondary); }
.si-error { margin: 0; padding: 10px 12px; border: 1px solid var(--danger, #dc6565); border-radius: var(--control-radius, 8px); color: var(--danger, #dc6565); font-size: var(--text-sm, 13px); line-height: 1.6; overflow-wrap: anywhere; }
.si-progress, .si-result { display: flex; flex-direction: column; gap: 12px; }
.si-progress-heading { display: flex; align-items: center; gap: 8px; }
.si-progress h3, .si-result h3 { margin: 0; font-size: var(--text-base, 15px); }
.si-progress > p { margin: 0; font-size: var(--text-sm, 13px); }
.si-progress-track { height: 6px; border-radius: 6px; overflow: hidden; background: var(--border); }
.si-progress-track > span { display: block; height: 100%; background: var(--accent); transition: width .2s ease; }
.si-progress-count { color: var(--text-secondary); font-size: var(--text-xs, 12px); }
.si-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
.si-stats > div { display: flex; flex-direction: column; gap: 5px; }
.si-stats strong { font-size: var(--text-xl, 20px); }
.si-stats span { color: var(--text-secondary); font-size: var(--text-xs, 12px); }
.si-actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; }
.si-btn { display: inline-flex; align-items: center; justify-content: center; min-height: var(--control-height, 36px); padding: 8px 13px; border: 1px solid var(--border); border-radius: var(--control-radius, 8px); background: var(--surface-panel, var(--bg-card)); color: var(--text-primary); font: inherit; font-size: var(--text-sm, 13px); cursor: pointer; }
.si-btn:hover:not(:disabled) { background: var(--bg-hover); }
.si-btn.primary { background: var(--accent); border-color: var(--accent); color: var(--inverse-text, white); }
.si-btn.primary:hover:not(:disabled) { background: var(--accent-hover, var(--accent)); }
.si-btn:disabled, .si-tab:disabled { opacity: .5; cursor: not-allowed; }
.si-spin { animation: si-spin 1s linear infinite; }
@keyframes si-spin { to { transform: rotate(360deg); } }
@media (max-width: 580px) { .source-import-overlay { padding: 10px; } .source-import-dialog { padding: 18px; max-height: 94dvh; gap: 16px; } .si-fields { grid-template-columns: 1fr; } .si-tabs { grid-template-columns: repeat(3, minmax(0, 1fr)); } .si-location-row { flex-wrap: wrap; } .si-location-row input { flex-basis: 100%; } }
@media (prefers-reduced-motion: reduce) { .si-spin { animation: none; } .si-progress-track > span { transition: none; } }
</style>
