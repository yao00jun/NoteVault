<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { BookOpen, Check, FileText, Link2, Loader2, MessageSquare, Sparkles, X, Zap } from '@lucide/vue'
import type { DistillRequest } from '@/api/workbench'
import { useAIChat } from '@/composables/useAIChat'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { splitFrontMatter } from '@/utils/frontmatter'
import { isImeComposing } from '@/utils/ime'
import { distillNameError, distillationPaths, distillTitleStem, interviewAnswerBody, isDistillableSource, isInboxMarkdown, parseDistillationDrafts, type DistillationDraft, type DistillationSource } from '@/utils/distillKnowledge'

const props = defineProps<{
  source: DistillationSource
  persist: (request: DistillRequest) => Promise<void>
}>()
const emit = defineEmits<{ close: []; saved: [path: string] }>()
const workspace = useWorkspaceStore()
const workbench = useWorkbenchStore()
const { t } = useI18n()
const mode = ref<DistillRequest['targetMode']>('book')
const bookFolder = ref('')
const newBook = ref('')
const title = ref(props.source.title)
const summary = ref(splitFrontMatter(props.source.content).body.trim())
const topic = ref('')
const question = ref(t('workbench.distill.defaultQuestion', { title: props.source.title }))
const answer = ref(interviewAnswerBody(summary.value.replace(/^#[ \t]+[^\r\n]*(?:\r?\n|$)/, '').trim()))
const saving = ref(false)
const error = ref('')
const aiError = ref('')
const aiDrafts = ref<Array<{ draft: DistillationDraft; included: boolean }>>([])
const relatedProject = ref('')
const saveProgress = ref<{ index: number; total: number } | null>(null)
const dialog = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null
let generation = 0
let disposed = false
let closed = false

const contextValid = computed(() => workspace.currentWorkspace?.path === props.source.workspacePath && isDistillableSource(props.source.path))
const isInbox = computed(() => isInboxMarkdown(props.source.path))
const projectNames = computed(() => [...new Set(workbench.projects.map(project => project.name))].sort((a, b) => a.localeCompare(b, 'zh-CN')))
const books = computed(() => workbench.books.filter(book => /^Learning\/[^/]+$/i.test(book.folder) && book.folder !== 'Learning/面试宝典'))
const topics = computed(() => [...new Set(workbench.documents.flatMap(document => {
  const match = document.path.match(/^Learning\/面试宝典\/([^/]+)\.md$/)
  return match && match[1]!.toLowerCase() !== 'book' ? [match[1]!] : []
}))].sort((a, b) => a.localeCompare(b, 'zh-CN')))

watch([books, topics, () => workbench.loading], () => {
  if (!bookFolder.value) bookFolder.value = books.value[0]?.folder || (workbench.loading ? '' : '__new__')
  if (!topic.value) topic.value = topics.value[0] || (workbench.loading ? '' : t('workbench.distill.defaultTopic'))
}, { immediate: true })

const request = computed<DistillRequest>(() => ({
  sourceFile: props.source.path,
  targetMode: mode.value,
  targetFolder: mode.value === 'interview' ? 'Learning/面试宝典' : bookFolder.value === '__new__' ? `Learning/${newBook.value.trim()}` : bookFolder.value,
  targetTitle: distillTitleStem(mode.value === 'book' ? title.value : topic.value),
  summary: mode.value === 'book' ? summary.value.trim() : '',
  question: mode.value === 'interview' ? question.value.trim() : '',
  answer: mode.value === 'interview' ? interviewAnswerBody(answer.value.trim()) : '',
  ...(isInbox.value ? { relatedProject: relatedProject.value.trim() } : {}),
}))
/** AI 草稿被纳入时循环提交各卡（书卡按标题去重），否则提交手工表单。 */
const pendingPayloads = computed<DistillRequest[]>(() => {
  const selected = aiDrafts.value.filter(item => item.included)
  if (!selected.length) return [request.value]
  const payloads: DistillRequest[] = []
  const seenBookTitles = new Set<string>()
  for (const { draft } of selected) {
    // 书卡：每张草稿一个标题一个文件；题卡：共用专题文件，一卡一条请求。
    const targetTitle = mode.value === 'book' ? distillTitleStem(draft.title || request.value.targetTitle) : request.value.targetTitle
    if (mode.value === 'book') {
      const key = targetTitle.toLowerCase()
      if (seenBookTitles.has(key)) continue
      seenBookTitles.add(key)
    }
    payloads.push({
      ...request.value,
      targetTitle,
      summary: mode.value === 'book' ? draft.summary : '',
      question: mode.value === 'interview' ? draft.question : '',
      answer: mode.value === 'interview' ? interviewAnswerBody(draft.answer) : '',
    })
  }
  return payloads.length ? payloads : [request.value]
})
const destination = computed(() => distillationPaths(pendingPayloads.value[0]!)[1]!)
const allIncluded = computed(() => aiDrafts.value.length > 0 && aiDrafts.value.every(item => item.included))
const validation = computed(() => {
  if (!contextValid.value) return t('workbench.distill.invalidContext')
  if (isInbox.value) {
    const projectIssue = distillNameError(relatedProject.value.trim())
    if (projectIssue) return t('workbench.distill.projectIssue', { issue: projectIssue })
  }
  if (mode.value === 'book') {
    if (!bookFolder.value) return t('workbench.distill.selectBook')
    if (bookFolder.value === '__new__') {
      const issue = distillNameError(newBook.value.trim())
      if (issue) return t('workbench.distill.nameIssue', { issue })
      if (newBook.value.trim() === '面试宝典') return t('workbench.distill.reservedInterviewMode')
    } else if (!books.value.some(book => book.folder === bookFolder.value)) return t('workbench.distill.bookChanged')
  }
  for (const payload of pendingPayloads.value) {
    const titleIssue = distillNameError(payload.targetTitle)
    if (titleIssue) return t('workbench.distill.fieldIssue', { label: mode.value === 'book' ? t('workbench.distill.titleLabel') : t('workbench.distill.interviewTopicLabel'), issue: titleIssue })
    const reserved = mode.value === 'book' ? ['book', 'sources', 'ai-plan'] : ['book']
    if (reserved.includes(payload.targetTitle.toLowerCase())) return t('workbench.distill.reservedName')
  }
  if (aiDrafts.value.some(item => item.included)) return ''
  if (mode.value === 'book' && !request.value.summary) return t('workbench.distill.needSummary')
  if (mode.value === 'interview' && (!request.value.question || !request.value.answer)) return t('workbench.distill.needQA')
  if (mode.value === 'interview' && /[\r\n]/.test(request.value.question)) return t('workbench.distill.singleLineQuestion')
  return ''
})

const { ask, messages, isAsking, clearConversation } = useAIChat({
  buildContext: () => t('workbench.distill.aiContext', { path: props.source.path, content: props.source.content.slice(0, 16000) }),
})

async function generateAIDraft() {
  if (!contextValid.value || isAsking.value || saving.value || closed) return
  const current = ++generation
  const selectedMode = mode.value
  aiError.value = ''
  aiDrafts.value = []
  clearConversation()
  const fields = selectedMode === 'book' ? '{"title":"简明笔记标题","summary":"Markdown 格式的技术要点与复盘"}' : '{"question":"单行面试题面","answer":"Markdown 格式的核心解答"}'
  await ask(t(selectedMode === 'book' ? 'workbench.distill.aiPromptBook' : 'workbench.distill.aiPromptInterview', { fields }), { forceContext: true })
  if (disposed || closed || current !== generation || !contextValid.value) return
  const response = messages.value.at(-1)
  if (!response || response.role !== 'assistant' || response.error) {
    aiError.value = response?.content || t('workbench.distill.aiNoDraft')
    return
  }
  try { aiDrafts.value = parseDistillationDrafts(response.content, selectedMode).map(draft => ({ draft, included: true })) }
  catch (cause) { aiError.value = cause instanceof Error ? cause.message : String(cause) }
}

function toggleAllDrafts() {
  const target = !allIncluded.value
  aiDrafts.value.forEach(item => { item.included = target })
}

async function submit() {
  if (saving.value || closed || validation.value) return
  const current = ++generation
  const payloads = pendingPayloads.value
  saving.value = true
  error.value = ''
  try {
    let savedPath = ''
    for (const [index, payload] of payloads.entries()) {
      saveProgress.value = payloads.length > 1 ? { index: index + 1, total: payloads.length } : null
      await props.persist(payload)
      if (disposed || closed || current !== generation || !contextValid.value) return
      if (!savedPath) savedPath = distillationPaths(payload)[1]!
    }
    emit('saved', savedPath)
    closed = true
    emit('close')
  } catch (cause) {
    if (!disposed && !closed && current === generation && contextValid.value) error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current === generation) {
      saving.value = false
      saveProgress.value = null
    }
  }
}

function close() {
  if (saving.value || closed) return
  closed = true
  generation++
  emit('close')
}
function onKeydown(event: KeyboardEvent) {
  if (isImeComposing(event)) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); close(); return }
  if (event.key !== 'Tab' || !dialog.value) return
  const elements = Array.from(dialog.value.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]'))
  const first = elements[0], last = elements.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}

watch(mode, () => { generation++; aiDrafts.value = []; aiError.value = ''; error.value = '' })
watch(contextValid, (valid) => {
  if (valid || closed) return
  generation++
  closed = true
  emit('close')
}, { flush: 'sync' })
onMounted(async () => {
  previousFocus = document.activeElement as HTMLElement | null
  window.addEventListener('keydown', onKeydown, true)
  await nextTick()
  dialog.value?.querySelector<HTMLElement>('select, input, textarea')?.focus()
})
onBeforeUnmount(() => {
  disposed = true
  generation++
  window.removeEventListener('keydown', onKeydown, true)
  previousFocus?.focus()
})
</script>

<template>
  <Teleport to="body">
    <div
      class="distill-overlay"
      @click.self="close"
    >
      <section
        ref="dialog"
        class="distill-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="distill-heading"
        aria-describedby="distill-description"
        @keydown.stop
      >
        <header class="distill-header">
          <div class="distill-symbol">
            <Zap :size="21" />
          </div>
          <div>
            <h2 id="distill-heading">
              {{ t('workbench.distill.title') }}
            </h2>
            <p id="distill-description">
              {{ t('workbench.distill.subtitle') }}
            </p>
          </div>
          <button
            type="button"
            class="icon-button"
            :aria-label="t('workbench.distill.closeAria')"
            :disabled="saving"
            @click="close"
          >
            <X :size="19" />
          </button>
        </header>
        <div class="source-line">
          <FileText :size="14" /><span>{{ source.path }}</span><span class="source-label">{{ t('workbench.distill.sourceLabel') }}</span>
        </div>

        <form @submit.prevent="submit">
          <fieldset :disabled="saving || !contextValid">
            <legend class="sr-only">
              {{ t('workbench.distill.settingsLegend') }}
            </legend>
            <label
              v-if="isInbox"
              class="related-project"
              for="distill-related-project"
            >{{ t('workbench.distill.relatedProjectLabel') }}
              <input
                id="distill-related-project"
                v-model="relatedProject"
                list="distill-projects"
                data-testid="distill-related-project"
                :placeholder="t('workbench.distill.relatedProjectPlaceholder')"
                maxlength="100"
              >
              <datalist id="distill-projects"><option
                v-for="name in projectNames"
                :key="name"
                :value="name"
              /></datalist>
              <small class="field-help">{{ t('workbench.distill.relatedProjectHint') }}</small>
            </label>
            <div
              class="distill-modes"
              role="tablist"
              :aria-label="t('workbench.distill.typeAria')"
            >
              <button
                id="distill-book-tab"
                type="button"
                role="tab"
                aria-controls="distill-book-panel"
                :aria-selected="mode === 'book'"
                :class="{ selected: mode === 'book' }"
                data-testid="distill-mode-book"
                @click="mode = 'book'"
              >
                <BookOpen :size="18" /><span><strong>{{ t('workbench.distill.modeBookTitle') }}</strong><small>{{ t('workbench.distill.modeBookDesc') }}</small></span>
              </button>
              <button
                id="distill-interview-tab"
                type="button"
                role="tab"
                aria-controls="distill-interview-panel"
                :aria-selected="mode === 'interview'"
                :class="{ selected: mode === 'interview' }"
                data-testid="distill-mode-interview"
                @click="mode = 'interview'"
              >
                <MessageSquare :size="18" /><span><strong>{{ t('workbench.distill.modeInterviewTitle') }}</strong><small>{{ t('workbench.distill.modeInterviewDesc') }}</small></span>
              </button>
            </div>

            <div class="draft-heading">
              <span>{{ t('workbench.distill.draftHeading') }}</span>
              <button
                type="button"
                class="ai-button"
                :disabled="isAsking"
                data-testid="distill-ai"
                @click="generateAIDraft"
              >
                <Loader2
                  v-if="isAsking"
                  :size="14"
                  class="spin"
                /><Sparkles
                  v-else
                  :size="14"
                />{{ isAsking ? t('workbench.distill.distilling') : t('workbench.distill.aiDraftAction') }}
              </button>
            </div>
            <p
              v-if="source.content.length > 16000"
              class="field-help"
            >
              {{ t('workbench.distill.longContentHint') }}
            </p>

            <div
              v-if="mode === 'book'"
              id="distill-book-panel"
              role="tabpanel"
              aria-labelledby="distill-book-tab"
              class="distill-fields"
            >
              <div class="field-grid">
                <label for="distill-book">{{ t('workbench.distill.targetBook') }}
                  <select
                    id="distill-book"
                    v-model="bookFolder"
                    data-testid="distill-book"
                  >
                    <option
                      v-if="!bookFolder"
                      value=""
                      disabled
                    >{{ t('workbench.distill.loadingBooks') }}</option>
                    <option
                      v-for="book in books"
                      :key="book.folder"
                      :value="book.folder"
                    >{{ book.name }}</option>
                    <option value="__new__">{{ t('workbench.distill.newBookOption') }}</option>
                  </select>
                </label>
                <label
                  v-if="bookFolder === '__new__'"
                  for="distill-new-book"
                >{{ t('workbench.distill.newBookLabel') }}
                  <input
                    id="distill-new-book"
                    v-model="newBook"
                    data-testid="distill-new-book"
                    :placeholder="t('workbench.distill.newBookPlaceholder')"
                    maxlength="100"
                  >
                </label>
              </div>
              <label for="distill-title">{{ t('workbench.distill.titleLabel') }}
                <input
                  id="distill-title"
                  v-model="title"
                  data-testid="distill-title"
                  maxlength="120"
                >
              </label>
              <label for="distill-summary">{{ t('workbench.distill.summaryLabel') }}
                <textarea
                  id="distill-summary"
                  v-model="summary"
                  data-testid="distill-summary"
                  rows="7"
                  :placeholder="t('workbench.distill.summaryPlaceholder')"
                />
              </label>
            </div>
            <div
              v-else
              id="distill-interview-panel"
              role="tabpanel"
              aria-labelledby="distill-interview-tab"
              class="distill-fields"
            >
              <label for="distill-topic">{{ t('workbench.distill.topicLabel') }}
                <input
                  id="distill-topic"
                  v-model="topic"
                  list="distill-topics"
                  data-testid="distill-topic"
                  :placeholder="t('workbench.distill.topicPlaceholder')"
                  maxlength="120"
                >
                <datalist id="distill-topics"><option
                  v-for="name in topics"
                  :key="name"
                  :value="name"
                /></datalist>
              </label>
              <label for="distill-question">{{ t('workbench.distill.questionLabel') }}
                <input
                  id="distill-question"
                  v-model="question"
                  data-testid="distill-question"
                  maxlength="500"
                >
              </label>
              <label for="distill-answer">{{ t('workbench.distill.answerLabel') }}
                <textarea
                  id="distill-answer"
                  v-model="answer"
                  data-testid="distill-answer"
                  rows="6"
                  :placeholder="t('workbench.distill.answerPlaceholder')"
                />
              </label>
              <p class="review-hint">
                <Check :size="13" />{{ t('workbench.distill.reviewHint') }}
              </p>
            </div>

            <section
              v-if="aiDrafts.length"
              class="ai-draft"
              data-testid="distill-ai-drafts"
              :aria-label="t('workbench.distill.aiDraftsAria')"
            >
              <div>
                <strong><Sparkles :size="13" />{{ t('workbench.distill.aiDraftsLabel', { count: aiDrafts.length }) }}</strong><button
                  type="button"
                  class="ai-button"
                  data-testid="distill-drafts-toggle-all"
                  @click="toggleAllDrafts"
                >
                  {{ allIncluded ? t('workbench.distill.deselectAll') : t('workbench.distill.selectAll') }}
                </button>
              </div>
              <label
                v-for="(item, index) in aiDrafts"
                :key="index"
                class="ai-draft-item"
              >
                <input
                  v-model="item.included"
                  type="checkbox"
                  :data-testid="`distill-draft-include-${index}`"
                >
                <span class="ai-draft-item-body">
                  <strong>{{ mode === 'book' ? item.draft.title : item.draft.question }}</strong>
                  <p>{{ mode === 'book' ? item.draft.summary : item.draft.answer }}</p>
                </span>
              </label>
              <small>{{ t('workbench.distill.aiDraftsHint') }}</small>
            </section>
          </fieldset>

          <p
            v-if="error || aiError"
            class="distill-error"
            role="alert"
          >
            {{ error || aiError }}
          </p>
          <p
            v-else-if="validation"
            class="field-help"
            role="status"
          >
            {{ validation }}
          </p>
          <div class="distill-destination">
            <Link2 :size="15" /><div><span>{{ pendingPayloads.length > 1 ? t('workbench.distill.destMulti', { count: pendingPayloads.length }) : t('workbench.distill.destLabel') }}</span><code data-testid="distill-destination">{{ destination }}</code><small>{{ t('workbench.distill.destHint') }}</small></div>
          </div>
          <footer class="distill-footer">
            <button
              type="button"
              class="cancel-button"
              :disabled="saving"
              @click="close"
            >
              {{ t('common.cancel') }}
            </button>
            <button
              type="submit"
              class="submit-button"
              :disabled="saving || !!validation"
              data-testid="distill-submit"
            >
              <Loader2
                v-if="saving"
                :size="15"
                class="spin"
              /><Zap
                v-else
                :size="15"
              />{{ saveProgress ? t('workbench.distill.savingProgress', saveProgress) : saving ? t('workbench.distill.saving') : t('workbench.distill.submit') }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.distill-overlay { position: fixed; inset: 0; z-index: 995; display: grid; place-items: center; padding: 24px; background: var(--overlay-bg, rgb(15 20 24 / .4)); backdrop-filter: blur(4px); }
.distill-dialog { width: min(680px, 100%); max-height: calc(100dvh - 48px); overflow: auto; border: 1px solid var(--border); border-radius: var(--radius-lg, 16px); background: var(--bg-card); color: var(--text-primary); box-shadow: 0 24px 72px rgb(0 0 0 / .2); }
.distill-header { display: flex; align-items: center; gap: 13px; padding: 24px 26px 16px; }
.distill-symbol { display: grid; place-items: center; width: 44px; height: 44px; flex-shrink: 0; border-radius: var(--radius-md, 12px); color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); }
.distill-header h2 { margin: 0; font-size: 19px; font-weight: 650; }
.distill-header p { margin: 5px 0 0; font-size: 12px; color: var(--text-secondary); line-height: 1.5; }
.icon-button { margin-left: auto; padding: 6px; color: var(--text-muted); border-radius: var(--radius-sm); }
.icon-button:hover { background: var(--bg-hover); }
.source-line { display: flex; align-items: center; gap: 8px; margin: 0 26px; padding: 10px 12px; background: var(--bg-content); border: 1px solid var(--border); border-radius: var(--radius-sm); font-size: 11px; color: var(--text-secondary); }
.source-line > span:first-of-type { overflow-wrap: anywhere; flex: 1; }
.source-label { white-space: nowrap; color: var(--text-muted); font-size: 10px; }
form { padding: 20px 26px 0; }
fieldset { padding: 0; margin: 0; border: 0; min-width: 0; }
.distill-modes { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.distill-modes button { display: flex; align-items: center; text-align: left; gap: 10px; padding: 13px 14px; background: var(--bg-content); border: 1px solid var(--border); border-radius: var(--radius-md, 10px); color: var(--text-secondary); }
.distill-modes button.selected { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 7%, var(--bg-card)); color: var(--accent); }
.distill-modes strong, .distill-modes small { display: block; }
.distill-modes strong { font-size: 13px; font-weight: 600; }
.distill-modes small { font-size: 11px; color: var(--text-muted); margin-top: 4px; }
.draft-heading { display: flex; justify-content: space-between; align-items: center; margin: 22px 0 12px; font-size: 12px; color: var(--text-secondary); }
.ai-button { display: inline-flex; align-items: center; gap: 5px; color: var(--accent); font-size: 12px; padding: 5px 0 5px 6px; }
.ai-button:hover { text-decoration: underline; }
.distill-fields { display: grid; gap: 13px; }
.distill-fields label { display: grid; gap: 7px; font-size: 12px; font-weight: 550; color: var(--text-secondary); }
.field-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; }
input, select, textarea { box-sizing: border-box; width: 100%; min-width: 0; border: 1px solid var(--border); border-radius: var(--radius-sm, 6px); background: var(--bg-content); color: var(--text-primary); padding: 9px 11px; font: inherit; font-size: 13px; font-weight: 400; }
textarea { resize: vertical; min-height: 115px; max-height: 300px; line-height: 1.7; }
button:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
button:disabled { cursor: default; opacity: .55; }
.review-hint { display: flex; align-items: center; gap: 6px; margin: 0; color: var(--text-secondary); font-size: 11px; }
.field-help, .distill-error { margin: 12px 0 0; font-size: 12px; line-height: 1.6; overflow-wrap: anywhere; }
.field-help { color: var(--text-muted); }
.distill-error { color: var(--danger, #dc4545); }
.ai-draft { margin-top: 16px; padding: 13px 15px; border: 1px solid color-mix(in srgb, var(--accent) 30%, var(--border)); border-radius: var(--radius-md, 10px); background: color-mix(in srgb, var(--accent) 5%, var(--bg-card)); }
.ai-draft > div, .ai-draft strong { display: flex; align-items: center; gap: 6px; }
.ai-draft > div { justify-content: space-between; }
.ai-draft > div strong { font-size: 12px; color: var(--accent); }
.ai-draft small { color: var(--text-muted); font-size: 11px; }
.ai-draft-item { display: flex; gap: 9px; align-items: flex-start; padding: 10px 0 9px; border-top: 1px dashed var(--border); font-weight: 400; color: var(--text-secondary); }
.ai-draft-item:first-of-type { border-top: 0; padding-top: 12px; }
.ai-draft-item input[type='checkbox'] { width: auto; flex-shrink: 0; margin: 3px 0 0; accent-color: var(--accent); }
.ai-draft-item-body { display: grid; gap: 4px; min-width: 0; }
.ai-draft-item-body strong { font-size: 12px; color: var(--text-primary); font-weight: 600; overflow-wrap: anywhere; }
.ai-draft-item-body p { margin: 0; max-height: 110px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 11px; line-height: 1.6; color: var(--text-secondary); }
.related-project { display: grid; gap: 7px; margin-top: 16px; font-size: 12px; font-weight: 550; color: var(--text-secondary); }
.related-project .field-help { margin: 0; }
.distill-destination { display: flex; align-items: flex-start; gap: 8px; margin-top: 18px; padding: 13px 0; color: var(--text-muted); }
.distill-destination div { display: grid; gap: 4px; min-width: 0; }
.distill-destination span, .distill-destination small { font-size: 11px; }
.distill-destination code { color: var(--text-secondary); font-size: 11px; overflow-wrap: anywhere; }
.distill-footer { display: flex; justify-content: flex-end; gap: 10px; padding: 15px 0 20px; border-top: 1px solid var(--border); }
.cancel-button, .submit-button { display: inline-flex; align-items: center; justify-content: center; gap: 7px; min-height: 35px; padding: 8px 15px; border-radius: var(--radius-sm); font-size: 12px; }
.cancel-button { border: 1px solid var(--border); color: var(--text-secondary); }
.submit-button { background: var(--accent); color: var(--accent-text, #fff); font-weight: 600; }
.spin { animation: distill-spin 1s linear infinite; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; }
@keyframes distill-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
@media (max-width: 560px) { .distill-overlay { padding: 10px; } .distill-dialog { max-height: calc(100dvh - 20px); } .distill-header { padding: 18px 16px 14px; } .source-line { margin: 0 16px; } form { padding: 16px 16px 0; } .distill-modes button { padding: 10px; gap: 7px; } .distill-modes small { font-size: 10px; } }
</style>
