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
              {{ t('import.source.eyebrow') }}
            </p>
            <h2 id="source-import-title">
              {{ intake.hasTask ? t('import.source.taskTitle') : t('import.source.title') }}
            </h2>
            <p
              id="source-import-description"
              class="si-hint"
            >
              {{ t('import.source.subtitle') }}
            </p>
          </div>
          <button
            type="button"
            class="si-close"
            :aria-label="t('import.source.closeAria')"
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
          {{ t('import.source.noWorkspace') }}
        </p>

        <template v-else>
          <form
            v-if="!intake.hasTask"
            class="si-form"
            @submit.prevent="submit"
            @keydown.enter="guardComposition"
          >
            <div class="si-field si-kind">
              <label for="source-kind">{{ t('import.source.kindLabel') }}</label>
              <select
                id="source-kind"
                v-model="state.draft.kind"
                :disabled="state.readingFiles"
                data-testid="source-kind"
              >
                <option value="project">
                  {{ t('import.source.kindProject') }}
                </option>
                <option value="book">
                  {{ t('import.source.kindBook') }}
                </option>
                <option value="topic">
                  {{ t('import.source.kindTopic') }}
                </option>
              </select>
            </div>

            <div
              class="si-tabs"
              role="tablist"
              :aria-label="t('import.source.tabsAria')"
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
                {{ t('import.source.emptyHint', { kind: kindLabel }) }}
              </p>
              <template v-else-if="state.draft.sourceType !== 'files'">
                <label for="source-location">{{ state.draft.sourceType === 'url' ? t('import.source.labelUrl') : ['adopt', 'attachments'].includes(state.draft.sourceType) ? t('import.source.labelAdopt') : t('import.source.labelFolder') }}</label>
                <div class="si-location-row">
                  <input
                    id="source-location"
                    v-model="state.draft.source"
                    data-testid="source-location"
                    type="text"
                    autocomplete="off"
                    spellcheck="false"
                    :placeholder="state.draft.sourceType === 'url' ? 'https://…' : ['adopt', 'attachments'].includes(state.draft.sourceType) ? t('import.source.placeholderAdopt', { space }) : t('import.source.placeholderFolder')"
                  >
                  <button
                    v-if="!['url', 'attachments'].includes(state.draft.sourceType)"
                    type="button"
                    class="si-btn"
                    data-testid="source-pick-folder"
                    :disabled="choosingFolder"
                    @click="chooseFolder"
                  >
                    {{ t('import.source.pickFolder') }}
                  </button>
                </div>
                <p class="si-hint">
                  {{ state.draft.sourceType === 'attachments' ? t('import.source.hintAttachments') : state.draft.sourceType === 'adopt' ? t('import.source.hintAdopt') : state.draft.sourceType === 'folder' ? t('import.source.hintFolder') : t('import.source.hintUrl') }}
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
                  <strong>{{ state.readingFiles ? t('import.source.readingFiles') : t('import.source.dropzoneTitle') }}</strong>
                  <span>{{ t('import.source.dropzoneTypes') }}</span>
                  <span>{{ t('import.source.dropzoneLimits') }}</span>
                  <input
                    id="source-files"
                    data-testid="source-files"
                    class="si-file-input"
                    type="file"
                    multiple
                    :aria-label="t('import.source.filesAria')"
                    :disabled="state.readingFiles"
                    @change="onFileSelection"
                  >
                </label>
                <p
                  v-if="state.draft.source"
                  class="si-hint"
                >
                  {{ t('import.source.pickedFrom', { source: state.draft.source }) }}
                </p>
                <ul
                  v-if="state.draft.files.length"
                  class="si-files"
                  :aria-label="t('import.source.chosenFilesAria')"
                >
                  <li
                    v-for="(file, index) in state.draft.files"
                    :key="file.name"
                  >
                    <span>{{ file.name }}</span>
                    <button
                      type="button"
                      :aria-label="t('import.source.removeFile', { name: file.name })"
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
                <label for="source-name">{{ t('import.source.nameLabel') }}<span v-if="state.draft.sourceType !== 'empty'">{{ t('import.source.nameOptional') }}</span></label>
                <input
                  id="source-name"
                  v-model="state.draft.name"
                  data-testid="source-name"
                  type="text"
                  :placeholder="t('import.source.namePlaceholder', { kind: kindLabel })"
                  :disabled="state.readingFiles"
                >
              </div>
              <div class="si-field">
                <label for="source-destination">{{ t('import.source.destLabel') }}<span>{{ t('import.source.destOptional') }}</span></label>
                <input
                  id="source-destination"
                  v-model="state.draft.targetFolder"
                  data-testid="source-destination"
                  type="text"
                  :placeholder="`${space}/${state.draft.name.trim() || t('import.source.collectionName')}`"
                  spellcheck="false"
                  :disabled="state.readingFiles"
                >
              </div>
            </div>
            <div class="si-destination">
              <span>{{ t('import.source.entryLabel') }}</span>
              <code
                v-if="metadataPath"
                data-testid="source-metadata-path"
              >{{ metadataPath }}</code>
              <span
                v-else
                class="si-hint"
              >{{ t('import.source.entryHint') }}</span>
            </div>

            <details class="si-options">
              <summary>{{ t('import.source.optionsTitle') }}</summary>
              <div class="si-field">
                <label for="source-conflict">{{ t('import.source.conflictLabel') }}</label>
                <select
                  id="source-conflict"
                  v-model="state.draft.conflictStrategy"
                  data-testid="source-conflict"
                >
                  <option value="skip">
                    {{ t('import.source.conflictSkip') }}
                  </option>
                  <option value="update">
                    {{ t('import.source.conflictUpdate') }}
                  </option>
                  <option value="copy">
                    {{ t('import.source.conflictCopy') }}
                  </option>
                </select>
              </div>
              <label class="si-checkbox"><input
                v-model="state.draft.enrich"
                type="checkbox"
                data-testid="source-enrich"
              >{{ t('import.source.enrichLabel') }}</label>
              <p
                v-if="state.draft.enrich && !aiConfigured"
                class="si-hint"
              >
                {{ t('import.source.noAIHint') }}
              </p>
              <div class="si-field">
                <label for="source-instruction">{{ t('import.source.instructionLabel') }}<span>{{ t('import.source.instructionOptional') }}</span></label>
                <textarea
                  id="source-instruction"
                  v-model="state.draft.instruction"
                  data-testid="source-instruction"
                  rows="2"
                  maxlength="4000"
                  :placeholder="t('import.source.instructionPlaceholder')"
                />
              </div>
            </details>

            <details
              v-if="state.preview"
              class="si-preview"
              open
            >
              <summary>{{ state.preview.existing ? t('import.source.existingDir') : t('import.source.willCreate') }} · {{ t('import.source.fileCount', { n: state.preview.fileCount }) }}</summary>
              <code>{{ state.preview.metadataPath }}</code>
              <p
                v-if="state.preview.existing"
                class="si-hint"
              >
                {{ t('import.source.existingHint') }}
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
                {{ t('import.source.moreFiles', { n: state.preview.files.length - 8 }) }}
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
                {{ state.previewing ? t('import.source.previewing') : t('import.source.preview') }}
              </button>
              <button
                type="submit"
                class="si-btn primary"
                data-testid="source-start"
                :disabled="!canSubmit || state.previewing || state.readingFiles"
              >
                {{ state.draft.sourceType === 'empty' ? t('import.source.createKind', { kind: kindLabel }) : t('import.source.importCreateKind', { kind: kindLabel }) }}
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
                /><h3>{{ state.phase === 'unknown' ? t('import.source.phaseUnknown') : state.cancelRequested ? t('import.source.phaseCancelling') : state.phase === 'starting' ? t('import.source.phaseStarting') : t('import.source.phaseImporting') }}</h3>
              </div>
              <p>{{ state.task?.message || (state.phase === 'starting' ? t('import.source.checkingSource') : t('import.source.waitingProgress')) }}</p>
              <div
                v-if="state.phase !== 'unknown'"
                class="si-progress-track"
                role="progressbar"
                :aria-label="t('import.source.progressAria')"
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
                {{ state.phase === 'unknown' ? t('import.source.unknownHint') : t('import.source.backgroundHint') }}
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
                {{ t('import.source.contentSummary', { readable: state.result.readable ?? 0, attachments: state.result.attachments ?? 0 }) }}
              </p>
              <code>{{ state.result.metadataPath }}</code>
              <div class="si-stats">
                <div data-testid="source-result-imported">
                  <strong>{{ state.result.imported }}</strong><span>{{ t('import.source.statImported') }}</span>
                </div>
                <div data-testid="source-result-updated">
                  <strong>{{ state.result.updated }}</strong><span>{{ t('import.source.statUpdated') }}</span>
                </div>
                <div data-testid="source-result-skipped">
                  <strong>{{ state.result.skipped }}</strong><span>{{ t('import.source.statSkipped') }}</span>
                </div>
                <div data-testid="source-result-conflicts">
                  <strong>{{ state.result.conflicts.length }}</strong><span>{{ t('import.source.statConflicts') }}</span>
                </div>
              </div>
              <details v-if="state.result.conflicts.length">
                <summary>{{ t('import.source.viewConflicts') }}</summary><ul>
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
                {{ t('import.source.recheck') }}
              </button>
              <button
                v-if="state.phase === 'unknown' && !state.taskId"
                type="button"
                class="si-btn"
                data-testid="source-inspected-reset"
                @click="intake.resetAfterInspection()"
              >
                {{ t('import.source.inspectedReset') }}
              </button>
              <button
                v-if="intake.busy && state.taskId && !['failed', 'cancelled', 'succeeded'].includes(state.task?.status ?? '')"
                type="button"
                class="si-btn"
                data-testid="source-cancel-task"
                :disabled="state.cancelRequested"
                @click="intake.cancel()"
              >
                {{ state.cancelRequested ? t('import.source.phaseCancelling') : t('import.source.cancelTask') }}
              </button>
              <button
                v-if="state.result && ['failed', 'cancelled'].includes(state.phase)"
                type="button"
                class="si-btn"
                data-testid="source-retry"
                @click="intake.retry(aiConfig)"
              >
                {{ t('import.source.retry') }}
              </button>
              <button
                v-if="state.result"
                type="button"
                class="si-btn"
                @click="newDraft"
              >
                {{ t('import.source.newDraft') }}
              </button>
              <button
                v-if="state.result?.metadataPath"
                type="button"
                class="si-btn primary"
                data-testid="source-open-result"
                @click="openResult"
              >
                {{ t('import.source.openKind', { kind: kindLabel }) }}
              </button>
              <button
                v-else
                type="button"
                class="si-btn primary"
                @click="emit('close')"
              >
                {{ state.phase === 'unknown' ? t('import.source.closeAndCheck') : t('import.source.backgroundContinue') }}
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
import { useI18n } from 'vue-i18n'
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
const { t } = useI18n()
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
  { value: 'empty', label: t('import.source.tabEmpty') }, { value: 'folder', label: t('import.source.tabFolder') },
  { value: 'files', label: t('import.source.tabFiles') }, { value: 'url', label: t('import.source.tabUrl') }, { value: 'adopt', label: t('import.source.tabAdopt') }, { value: 'attachments', label: t('import.source.tabAttachments') },
]
const sourceTabs = computed(() => sourceTabOptions.filter(tab => tab.value !== 'attachments' || state.value?.draft.kind === 'book'))
const kindLabel = computed(() => ({ project: t('import.source.kindProject'), book: t('import.source.kindBook'), topic: t('import.source.kindTopic') }[state.value?.draft.kind ?? 'project']))
const space = computed(() => collectionSpaces[state.value?.draft.kind ?? 'project'])
const aiConfig = computed(() => ({ apiKey: settings.settings.ai.apiKey ?? '', baseURL: settings.settings.ai.baseURL ?? '', model: settings.settings.ai.model ?? '' }))
const aiConfigured = computed(() => !!aiConfig.value.baseURL && !!aiConfig.value.model && (!!aiConfig.value.apiKey || isLocalBaseURL(aiConfig.value.baseURL)))
const progress = computed(() => Math.round(Math.max(0, Math.min(100, Number(state.value?.task?.percent) || 0))))
const resultTitle = computed(() => {
  const entry = state.value
  if (!entry?.result) return ''
  const hasSaved = !!entry.result.metadataPath || entry.result.files.length > 0 || entry.result.imported + entry.result.updated > 0
  if (entry.phase === 'cancelled' || entry.result.cancelled) return hasSaved ? t('import.source.resultCancelledSaved') : t('import.source.resultCancelled')
  if (entry.phase === 'failed') return hasSaved ? t('import.source.resultFailedSaved') : t('import.source.resultFailed')
  if (entry.result.attachments) return entry.result.readable ? t('import.source.resultPartial') : t('import.source.resultAttachmentsOnly')
  return entry.draft.sourceType === 'empty' ? t('import.source.resultCreated') : t('import.source.resultImported')
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
    if (mounted && state.value === entry && entry.draft === draft) entry.error = t('import.source.folderDialogFailed', { msg: error instanceof Error ? error.message : String(error) })
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
