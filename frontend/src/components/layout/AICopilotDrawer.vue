<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { X, Trash2, Send, Loader2, FileText, Search, ListTodo, WandSparkles, Inbox } from '@lucide/vue'
import { FileService } from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { isLocalBaseURL } from '@/utils/localEndpoint'
import { useAIChat } from '@/composables/useAIChat'
import { insertAtCursor } from '@/plugins/editorBridge'
import { useToast } from '@/composables/useToast'
import type { CopilotRequest } from '@/composables/useCopilotRequest'
import { usePageContext } from '@/composables/usePageContext'
import { useWorkbenchStore } from '@/stores/workbench'
import { useRoute } from 'vue-router'
import { getActiveEditor } from '@/plugins/editorBridge'
import { projectSummaryContext, tasksForProject } from '@/utils/workbenchCollections'
import { requestSourceImport } from '@/composables/useSourceImport'
import { parseSourceIntent } from '@/utils/sourceIntent'
import { isImeComposing } from '@/utils/ime'
import { editorSession } from '@/composables/useEditorSession'

// AI-COPILOT-FLOATING-ORB 蓝图 Step 3：右侧伴生 AI 抽屉。
// 非阻塞覆盖：无遮罩层，主工作区（左侧编辑器）照常可交互，
// 由「点外部 / Esc / 右上角 X」三种方式关闭。
const props = defineProps<{ visible: boolean; request?: CopilotRequest | null }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const settingsStore = useSettingsStore()
const toast = useToast()
const route = useRoute()
const workbench = useWorkbenchStore()
const { context: page } = usePageContext()
const activeRequest = ref<CopilotRequest | null>(null)
const requestAnswerStart = ref(0)
let consumedRequest = ''

/** 注入 Prompt 的文档内容上限：防止超长文档把请求撑爆 */
const CONTEXT_LIMIT = 8000

/** 当前活动文档内容缓存：打开抽屉 / 切换活动文件时惰性拉取 */
const docContent = ref('')
const docContentLoaded = ref(false)
let documentGeneration = 0
let disposed = false

function invalidateDocContent() {
  documentGeneration++
  docContent.value = ''
  docContentLoaded.value = false
}

watch(() => [workspaceStore.currentWorkspace?.path, route.fullPath], () => {
  invalidateDocContent()
  activeRequest.value = null
  // Pending domain actions belong to the page that created them. Finishing an
  // older answer after navigation must not resurrect that page's context.
  consumedRequest = props.request?.id ?? ''
  requestAnswerStart.value = 0
}, { flush: 'sync' })

async function loadDocContent() {
  const workspacePath = workspaceStore.currentWorkspace?.path
  const path = page.value.file
  const location = route.fullPath
  invalidateDocContent()
  const generation = documentGeneration
  const isCurrent = () => !disposed && generation === documentGeneration && workspacePath === workspaceStore.currentWorkspace?.path && location === route.fullPath
  if (!workspacePath) return
  try {
    let content = ''
    if (path) {
      const editor = route.path === '/editor' && editorSession.workspacePath === workspacePath && editorSession.path === path ? getActiveEditor() : null
      content = editor ? editor.state.doc.toString() : await FileService.ReadFile(workspacePath, path)
      content = `当前文档：${path}\n\n${content}`
    } else if (page.value.section === 'projects') {
      const project = workbench.projects.find(item => item.folder === page.value.entityFolder)
      content = project ? projectSummaryContext(project, tasksForProject(project, workbench.tasks), project.notes, workbench.today)
        : `项目组合：\n${workbench.projects.map(item => `${item.name} · ${item.status} · 下一步 ${item.nextStep || '未记录'}`).join('\n')}`
    } else if (page.value.section === 'learning') {
      const book = workbench.books.find(item => item.folder === page.value.entityFolder)
      content = book ? `技术分册：${book.name}\n状态：${book.status}，用户记录的进度：${book.progress}%\n章节：\n${book.chapters.map(chapter => `${chapter.title} (${chapter.path})`).join('\n')}`
        : `当前学习页面：${page.value.title}\n书架：${workbench.books.map(item => item.name).join('、')}\n待复习：${workbench.reviewQueue.length} 题`
    } else if (page.value.section === 'today') {
      content = `今日工作台 ${workbench.today}\n${workbench.todayTasks.map(task => `[${task.completed ? '完成' : '待办'}] [${task.type}] ${task.title}${task.blocker ? `（阻塞：${task.blocker}）` : ''}`).join('\n')}`
    } else content = `当前位置：${page.value.title}\n当前工作区文档 ${workbench.documents.length} 篇，当前目录 ${page.value.folder || '全部空间'}。根据用户明确的问题检索知识库。`
    if (!isCurrent()) return
    docContent.value = content.length > CONTEXT_LIMIT ? `${content.slice(0, CONTEXT_LIMIT)}\n…` : content
    docContentLoaded.value = true
  } catch (e) {
    if (!isCurrent()) return
    console.warn('[AICopilotDrawer] 读取当前文档失败:', e)
    docContent.value = ''
  }
}

const activeFileName = computed(() => {
  if (activeRequest.value) return ({ 'daily-report': '今日日报', project: '项目进展', learning: '面试与学习' } as Record<string, string>)[activeRequest.value.source || ''] || '工作台上下文'
  return page.value.title
})

const hasActiveDoc = computed(() => workspaceStore.hasWorkspace || !!activeRequest.value)

const attachContext = ref(true)

const {
  messages,
  question,
  isAsking,
  canAsk,
  ask: askAI,
  clearConversation,
  openCitation,
  renderMarkdown,
} = useAIChat({
  buildContext: async (force) => {
    if (activeRequest.value && (attachContext.value || force)) return activeRequest.value.context
    // 勾选「关联当前文档上下文」或快捷指令强制注入时，带上文档内容
    if (!attachContext.value && !force) return null
    await loadDocContent()
    if (!docContentLoaded.value) return null
    return docContent.value
  },
})

const pendingSourceIntent = computed(() => parseSourceIntent(question.value))
const canSubmit = computed(() => !isAsking.value && (canAsk.value || (workspaceStore.hasWorkspace && !!pendingSourceIntent.value)))
async function ask(questionOverride?: string, options?: { forceContext?: boolean }) {
  const intent = questionOverride === undefined ? pendingSourceIntent.value : null
  if (intent) { requestSourceImport(intent); question.value = ''; return }
  return askAI(questionOverride, options)
}
function onKeydown(event: KeyboardEvent) {
  if (isImeComposing(event)) return
  if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void ask() }
}
function createFromSources() {
  requestSourceImport({ sourceType: 'folder', kind: page.value.section === 'projects' ? 'project' : page.value.section === 'learning' ? 'book' : 'topic' })
}

// Queue domain requests while a previous answer is finishing instead of losing clicks.
watch(() => [props.request?.id, props.visible, isAsking.value, workspaceStore.currentWorkspace?.path], () => {
  const request = props.request
  if (!props.visible || !request || request.id === consumedRequest || isAsking.value) return
  if (request.workspacePath && request.workspacePath !== workspaceStore.currentWorkspace?.path) return
  consumedRequest = request.id
  activeRequest.value = request
  attachContext.value = true
  requestAnswerStart.value = messages.value.length
  void ask(request.prompt, { forceContext: true })
}, { immediate: true })

watch(() => workspaceStore.currentWorkspace?.path, () => {
  activeRequest.value = null
  requestAnswerStart.value = 0
  // A request queued before the switch belongs to the previous workspace, even
  // if its caller did not supply an explicit workspacePath.
  consumedRequest = props.request?.id ?? ''
  attachContext.value = true
}, { flush: 'sync' })

function clearChat() {
  if (isAsking.value) return
  clearConversation()
  requestAnswerStart.value = 0
}

function applyToReport(content: string) {
  const request = activeRequest.value
  if (!request?.onApply || (request.workspacePath && request.workspacePath !== workspaceStore.currentWorkspace?.path)) return
  request.onApply(content)
  emit('close')
}

// 4 个高频快捷胶囊（蓝图 3.2）：needsDoc 的指令点击时强制携带文档上下文
interface QuickChip {
  key: string
  icon: typeof FileText
  question: string
  needsDoc: boolean
}

const quickChips = computed<QuickChip[]>(() => [
  { key: 'summary', icon: FileText, question: t('copilot.chipSummaryPrompt'), needsDoc: true },
  { key: 'related', icon: Search, question: t('copilot.chipRelatedPrompt'), needsDoc: false },
  { key: 'todos', icon: ListTodo, question: t('copilot.chipTodosPrompt'), needsDoc: true },
  { key: 'polish', icon: WandSparkles, question: t('copilot.chipPolishPrompt'), needsDoc: true },
])

async function runChip(chip: QuickChip) {
  if (chip.needsDoc && !hasActiveDoc.value) {
    toast.warning(t('copilot.noActiveDoc'))
    return
  }
  await ask(chip.question, { forceContext: chip.needsDoc })
}

/** 头部供应商徽标：严格继承本机端点免 Key 口径的展示 */
const providerBadge = computed(() => {
  const ai = settingsStore.settings.ai
  return isLocalBaseURL(ai.baseURL)
    ? `${ai.model || 'Local'} · ${t('copilot.badgeLocal')}`
    : `${ai.model || 'Cloud'} · ${t('copilot.badgeCloud')}`
})

function insertToNote(content: string) {
  if (route.path !== '/editor' || !page.value.file || editorSession.path !== page.value.file || editorSession.workspacePath !== workspaceStore.currentWorkspace?.path) { toast.warning(t('copilot.noEditor')); return }
  const ok = insertAtCursor(content)
  if (ok) {
    toast.success(t('copilot.inserted'))
    emit('close')
  } else {
    toast.warning(t('copilot.noEditor'))
  }
}

// ---------------------------------------------------------------------------
// 打开 / 关闭生命周期
// ---------------------------------------------------------------------------

const inputRef = ref<HTMLTextAreaElement | null>(null)
const drawerRef = ref<HTMLElement | null>(null)
const messagesRef = ref<HTMLElement | null>(null)

async function scrollToBottom() {
  await nextTick()
  if (messagesRef.value) {
    messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  }
}

watch(messages, () => { void scrollToBottom() }, { deep: true })

function onDocMousedown(e: MouseEvent) {
  const target = e.target as HTMLElement
  // 点在抽屉或悬浮球上不算「外部」
  if (target.closest('[data-ai-drawer]') || target.closest('[data-ai-orb]')) return
  emit('close')
}

function onWindowKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

watch(
  () => props.visible,
  (visible) => {
    if (visible) {
      document.addEventListener('mousedown', onDocMousedown, true)
      window.addEventListener('keydown', onWindowKeydown)
      void nextTick(() => inputRef.value?.focus())
    } else {
      document.removeEventListener('mousedown', onDocMousedown, true)
      window.removeEventListener('keydown', onWindowKeydown)
    }
  },
  // 初始即打开（visible 首值 true）也要挂监听
  { immediate: true },
)

watch(() => [props.visible, workspaceStore.currentWorkspace?.path, route.fullPath], ([visible]) => {
  if (visible) void loadDocContent()
  else invalidateDocContent()
}, { immediate: true })

onBeforeUnmount(() => {
  disposed = true
  invalidateDocContent()
  document.removeEventListener('mousedown', onDocMousedown, true)
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<template>
  <aside
    ref="drawerRef"
    class="copilot-drawer"
    :class="{ open: visible }"
    data-ai-drawer
    :aria-hidden="!visible"
  >
    <!-- 头部：标题 + 供应商徽标 + 清空 / 关闭 -->
    <header class="copilot-header">
      <div class="header-titles">
        <h3 class="drawer-title">
          <Inbox
            :size="16"
            class="title-icon"
          />
          {{ t('copilot.title') }}
        </h3>
        <span class="provider-badge">{{ providerBadge }}</span>
      </div>
      <div class="header-actions">
        <button
          v-if="messages.length > 0"
          type="button"
          class="icon-btn"
          :title="t('copilot.clear')"
          :disabled="isAsking"
          @click="clearChat"
        >
          <Trash2 :size="15" />
        </button>
        <button
          type="button"
          class="icon-btn"
          :title="t('copilot.close')"
          @click="emit('close')"
        >
          <X :size="16" />
        </button>
      </div>
    </header>

    <!-- 上下文感知条 -->
    <div
      class="context-pill"
      :class="{ empty: !hasActiveDoc }"
    >
      <FileText :size="13" />
      <span
        v-if="hasActiveDoc"
        class="context-name"
        :title="activeFileName"
      >当前上下文: {{ activeFileName }}</span>
      <span
        v-else
        class="context-name"
      >{{ t('copilot.contextWorkspace') }}: {{ workspaceStore.currentWorkspace?.name ?? t('qna.noWorkspace') }}</span>
      <label
        v-if="hasActiveDoc"
        class="attach-toggle"
      >
        <input
          v-model="attachContext"
          type="checkbox"
        >
        <span>{{ t('copilot.attachContext') }}</span>
      </label>
      <button
        v-if="activeRequest"
        type="button"
        class="icon-btn"
        aria-label="回到当前文档上下文"
        title="回到当前文档上下文"
        @click="activeRequest = null"
      >
        <X :size="12" />
      </button>
    </div>

    <!-- 快捷行动胶囊 -->
    <div class="chips-row">
      <button
        v-for="chip in quickChips"
        :key="chip.key"
        type="button"
        class="chip"
        :disabled="isAsking || (chip.needsDoc && !hasActiveDoc)"
        :title="chip.question"
        @click="runChip(chip)"
      >
        <component
          :is="chip.icon"
          :size="13"
        />
        <span>{{ t(`copilot.chip_${chip.key}`) }}</span>
      </button>
    </div>

    <!-- 消息流 -->
    <div
      ref="messagesRef"
      class="copilot-messages"
    >
      <div
        v-if="messages.length === 0 && !isAsking"
        class="copilot-empty"
      >
        <Inbox :size="28" />
        <h4>{{ t('copilot.emptyTitle') }}</h4>
        <p>{{ t('copilot.emptyDesc') }}</p>
      </div>

      <div
        v-for="(msg, i) in messages"
        :key="i"
        class="msg"
        :class="msg.role"
      >
        <div
          class="msg-bubble"
          :class="{ error: msg.error }"
        >
          <div
            v-if="msg.role === 'assistant'"
            class="msg-content"
            v-html="renderMarkdown(msg.content)"
          />
          <div
            v-else
            class="msg-content"
          >
            {{ msg.content }}
          </div>

          <div
            v-if="msg.citations && msg.citations.length > 0"
            class="msg-citations"
          >
            <div class="citations-label">
              {{ t('copilot.citations') }}
            </div>
            <button
              v-for="c in msg.citations"
              :key="c.index"
              type="button"
              class="citation-chip"
              :title="c.path"
              @click="openCitation(c.path)"
            >
              <FileText :size="12" />
              <span class="cite-index">[{{ c.index }}]</span>
              <span class="cite-title">{{ c.title }}</span>
            </button>
          </div>

          <!-- 一键就地插入：把 AI 产物写进当前活动编辑器光标处 -->
          <div
            v-if="msg.role === 'assistant' && !msg.error && msg.content"
            class="msg-actions"
          >
            <button
              v-if="activeRequest?.source === 'daily-report' && activeRequest.onApply && i >= requestAnswerStart"
              type="button"
              class="insert-btn"
              data-testid="apply-report-answer"
              :disabled="isAsking"
              @click="applyToReport(msg.content)"
            >
              <FileText :size="13" />
              <span>应用到日报</span>
            </button>
            <button
              type="button"
              class="insert-btn"
              @click="insertToNote(msg.content)"
            >
              <Inbox :size="13" />
              <span>{{ t('copilot.insert') }}</span>
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="isAsking"
        class="msg assistant"
      >
        <div class="msg-bubble thinking">
          <Loader2
            :size="14"
            class="spin"
          />
          <span>{{ t('copilot.thinking') }}</span>
        </div>
      </div>
    </div>

    <!-- 输入区 -->
    <div class="copilot-source-action">
      <button
        type="button"
        data-testid="copilot-source-import"
        :disabled="!workspaceStore.hasWorkspace"
        @click="createFromSources"
      >
        <Inbox :size="14" />从资料创建
      </button>
      <span>目录、链接或文件 → 项目 / 技术分册</span>
    </div>
    <div class="copilot-input">
      <textarea
        ref="inputRef"
        v-model="question"
        :placeholder="t('copilot.inputPlaceholder')"
        rows="2"
        @keydown="onKeydown"
      />
      <button
        type="button"
        class="btn-ask"
        :disabled="!canSubmit"
        @click="ask()"
      >
        <Send :size="14" />
        <span>{{ t('copilot.send') }}</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.copilot-source-action { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 8px 14px; border-top: 1px solid var(--border); }
.copilot-source-action button { display: flex; align-items: center; gap: 6px; background: var(--bg-hover); border: 1px solid var(--border); border-radius: var(--control-radius, 6px); color: var(--text-primary); font-size: 12px; padding: 6px 9px; }
.copilot-source-action span { color: var(--text-secondary); font-size: 11px; }
.copilot-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  z-index: 980;
  width: 380px;
  display: flex;
  flex-direction: column;
  background: var(--bg-sidebar, var(--bg-window));
  border-left: 1px solid var(--border);
  box-shadow: -12px 0 32px rgba(0, 0, 0, 0.28);
  transform: translateX(105%);
  transition: transform 0.2s ease;
  visibility: hidden;
}

.copilot-drawer.open {
  transform: translateX(0);
  visibility: visible;
}

/* 头部 */
.copilot-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.header-titles {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.drawer-title {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary, #e8e8e8);
}

.title-icon {
  color: var(--accent, #8b5cf6);
}

.provider-badge {
  font-size: 11px;
  color: var(--text-secondary, #9d9d9d);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary, #9d9d9d);
  cursor: pointer;
  transition: all 0.15s ease;
}

.icon-btn:hover:not(:disabled) {
  color: var(--text-primary, #e8e8e8);
  background: rgba(127, 127, 127, 0.15);
}

.icon-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* 上下文感知条 */
.context-pill {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 10px 14px 0;
  padding: 6px 10px;
  border: 1px solid color-mix(in srgb, var(--accent, #8b5cf6) 35%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent, #8b5cf6) 10%, transparent);
  color: var(--text-secondary, #c8c8c8);
  font-size: 12px;
  flex-shrink: 0;
}

.context-pill.empty {
  border-color: var(--border);
  background: transparent;
}

.context-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.attach-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  flex-shrink: 0;
  color: var(--text-secondary, #9d9d9d);
}

.attach-toggle input {
  accent-color: var(--accent, #8b5cf6);
  margin: 0;
}

/* 快捷胶囊 */
.chips-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 10px 14px;
  flex-shrink: 0;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-card, transparent);
  color: var(--text-secondary, #c8c8c8);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.chip:hover:not(:disabled) {
  color: var(--text-primary, #e8e8e8);
  border-color: var(--accent, #8b5cf6);
  background: color-mix(in srgb, var(--accent, #8b5cf6) 12%, transparent);
}

.chip:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

/* 消息区 */
.copilot-messages {
  flex: 1;
  overflow-y: auto;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.copilot-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin: auto;
  color: var(--text-secondary, #9d9d9d);
  text-align: center;
}

.copilot-empty h4 {
  margin: 6px 0 0;
  font-size: 14px;
  color: var(--text-primary, #e8e8e8);
}

.copilot-empty p {
  margin: 0;
  font-size: 12px;
  max-width: 280px;
}

.msg {
  display: flex;
}

.msg.user {
  justify-content: flex-end;
}

.msg-bubble {
  max-width: 88%;
  padding: 8px 12px;
  border-radius: 12px;
  font-size: 13px;
  line-height: 1.6;
}

.msg.user .msg-bubble {
  background: var(--accent);
  color: #fff;
  border-bottom-right-radius: 4px;
  white-space: pre-wrap;
}

.msg.assistant .msg-bubble {
  background: var(--bg-card);
  color: var(--text-primary, #e8e8e8);
  border: 1px solid var(--border);
  border-bottom-left-radius: 4px;
}

.msg-bubble.error {
  border-color: #e5534b;
  color: #f48b84;
}

.msg-bubble.thinking {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-secondary, #9d9d9d);
  font-size: 12px;
}

.msg-content :deep(p) {
  margin: 0 0 6px;
}

.msg-content :deep(p:last-child) {
  margin-bottom: 0;
}

.msg-content :deep(ul),
.msg-content :deep(ol) {
  margin: 4px 0 6px;
  padding-left: 18px;
}

.msg-content :deep(code) {
  padding: 1px 4px;
  border-radius: 4px;
  background: rgba(127, 127, 127, 0.18);
  font-size: 12px;
}

.msg-content :deep(pre) {
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.3);
  overflow-x: auto;
}

.msg-content :deep(h1),
.msg-content :deep(h2),
.msg-content :deep(h3) {
  margin: 6px 0 4px;
  font-size: 13px;
}

/* 引用 */
.msg-citations {
  margin-top: 8px;
  padding-top: 6px;
  border-top: 1px dashed var(--border);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.citations-label {
  font-size: 11px;
  color: var(--text-secondary, #9d9d9d);
}

.citation-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  align-self: flex-start;
  max-width: 100%;
  padding: 3px 9px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: transparent;
  color: var(--text-secondary, #9d9d9d);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.citation-chip:hover {
  color: var(--text-primary, #e8e8e8);
  border-color: var(--accent);
}

.cite-index {
  color: var(--accent);
  font-weight: 600;
}

.cite-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 220px;
}

/* 插入按钮 */
.msg-actions {
  margin-top: 8px;
  display: flex;
}

.insert-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  border: 1px solid color-mix(in srgb, var(--accent, #8b5cf6) 45%, transparent);
  border-radius: 8px;
  background: color-mix(in srgb, var(--accent, #8b5cf6) 12%, transparent);
  color: var(--text-primary, #e8e8e8);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.insert-btn:hover {
  background: color-mix(in srgb, var(--accent, #8b5cf6) 24%, transparent);
}

/* 输入区 */
.copilot-input {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  padding: 10px 14px 12px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.copilot-input textarea {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-card);
  color: var(--text-primary, #e8e8e8);
  font-size: 13px;
  font-family: inherit;
  line-height: 1.5;
  resize: none;
  outline: none;
  transition: border-color 0.15s ease;
}

.copilot-input textarea:focus {
  border-color: var(--accent);
}

.btn-ask {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 8px 14px;
  border: none;
  border-radius: 10px;
  background: var(--accent);
  color: #fff;
  font-size: 13px;
  cursor: pointer;
  transition: opacity 0.15s ease;
  flex-shrink: 0;
}

.btn-ask:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
