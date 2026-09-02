<script setup lang="ts">
import { ref, computed, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessageCircle, Send, Trash2, FileText, Loader2, AlertTriangle, Sparkles } from 'lucide-vue-next'
import { marked } from 'marked'
import { QnAService } from '@bindings/github.com/notevault/notevault/index.js'
import type { QnACitation } from '@bindings/github.com/notevault/notevault/models.js'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'

const { t } = useI18n()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const settingsStore = useSettingsStore()

// rerank 选了 Ollama 时显式提示（P1-3b 不静默）：Ollama 原生不支持 /api/rerank，
// 选它会让问答检索静默降级为纯 RRF，用户无感。这是确定事实，无需实时探测。
const rerankOllamaSelected = computed(
  () => settingsStore.settings.rerank.provider === 'ollama',
)

interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  citations?: QnACitation[]
  error?: boolean
}

const messages = ref<ChatMessage[]>([])
const question = ref('')
const isAsking = ref(false)
const messagesRef = ref<HTMLElement | null>(null)

const canAsk = computed(() => question.value.trim().length > 0 && !isAsking.value)

const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

async function ask() {
  const q = question.value.trim()
  if (!q || isAsking.value) return

  if (!currentWorkspace.value) {
    messages.value.push({ role: 'assistant', content: t('qna.noWorkspace'), error: true })
    return
  }
  const ai = settingsStore.settings.ai
  const emb = settingsStore.settings.embedding
  const rerank = settingsStore.settings.rerank
  if (!ai.apiKey || !ai.apiKey.trim()) {
    messages.value.push({ role: 'assistant', content: t('qna.noApiKey'), error: true })
    return
  }

  messages.value.push({ role: 'user', content: q })
  question.value = ''
  isAsking.value = true
  await scrollToBottom()
  try {
    const resp = await QnAService.Answer(
      ai.apiKey,
      ai.baseURL,
      ai.model,
      emb.baseURL,
      emb.model,
      emb.apiKey,
      {
        provider: rerank.provider,
        baseURL: rerank.baseURL,
        model: rerank.model,
        apiKey: rerank.apiKey,
      },
      currentWorkspace.value.path,
      q,
    )
    messages.value.push({
      role: 'assistant',
      content: (resp?.answer ?? '').trim() || t('qna.emptyTitle'),
      citations: resp?.citations ?? [],
    })
  } catch (e) {
    messages.value.push({
      role: 'assistant',
      content: t('qna.askFailed', { msg: (e as Error).message }),
      error: true,
    })
  } finally {
    isAsking.value = false
    await scrollToBottom()
  }
}

function clearConversation() {
  if (isAsking.value) return
  messages.value = []
}

async function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    await ask()
  }
}

function openCitation(path: string) {
  workspaceStore.openFile(path)
  router.push('/editor')
}

function renderMarkdown(content: string): string {
  return marked.parse(content, { async: false }) as string
}

async function scrollToBottom() {
  await nextTick()
  if (messagesRef.value) {
    messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  }
}

watch(messages, () => { scrollToBottom() }, { deep: true })
</script>

<template>
  <div class="qna-view">
    <div class="qna-header">
      <div class="header-left">
        <h2 class="qna-title">
          <MessageCircle :size="20" /> {{ t('qna.title') }}
        </h2>
        <p class="qna-subtitle">
          {{ t('qna.subtitle') }}
        </p>
      </div>
      <button
        v-if="messages.length > 0"
        class="btn-clear"
        :title="t('qna.clear')"
        @click="clearConversation"
      >
        <Trash2 :size="14" />
        <span>{{ t('qna.clear') }}</span>
      </button>
    </div>

    <div
      v-if="rerankOllamaSelected"
      class="qna-rerank-hint"
      data-testid="rerank-hint"
    >
      <AlertTriangle :size="14" />
      <span>{{ t('qna.rerankOllamaHint') }}</span>
    </div>

    <div
      ref="messagesRef"
      class="qna-messages"
    >
      <div
        v-if="messages.length === 0 && !isAsking"
        class="qna-empty"
      >
        <Sparkles :size="40" />
        <h3>{{ t('qna.emptyTitle') }}</h3>
        <p>{{ t('qna.emptyDesc') }}</p>
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
              {{ t('qna.citations') }}
            </div>
            <button
              v-for="c in msg.citations"
              :key="c.index"
              class="citation-chip"
              :title="c.path"
              @click="openCitation(c.path)"
            >
              <FileText :size="12" />
              <span class="cite-index">[{{ c.index }}]</span>
              <span class="cite-title">{{ c.title }}</span>
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
          <span>{{ t('qna.thinking') }}</span>
        </div>
      </div>
    </div>

    <div class="qna-input">
      <textarea
        v-model="question"
        :placeholder="t('qna.placeholder')"
        rows="2"
        @keydown="onKeydown"
      />
      <button
        class="btn-ask"
        :disabled="!canAsk"
        @click="ask"
      >
        <Send :size="14" />
        <span>{{ t('qna.ask') }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.qna-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

/* 头部 */
.qna-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 24px 12px;
  border-bottom: 1px solid var(--border-color, #2d2d30);
  flex-shrink: 0;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.qna-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary, #e8e8e8);
}

.qna-subtitle {
  margin: 0;
  font-size: 12px;
  color: var(--text-secondary, #9d9d9d);
}

.btn-clear {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: 1px solid var(--border-color, #2d2d30);
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary, #9d9d9d);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-clear:hover {
  color: var(--text-primary, #e8e8e8);
  border-color: var(--text-secondary, #9d9d9d);
}

/* 消息区 */
.qna-messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.qna-rerank-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 24px;
  padding: 8px 12px;
  border: 1px solid var(--warning-border, var(--warning, #f59e0b));
  border-radius: var(--radius-sm, 6px);
  background: color-mix(in srgb, var(--warning, #f59e0b) 12%, transparent);
  color: var(--text-secondary, #c8c8c8);
  font-size: var(--text-sm, 13px);
  line-height: 1.5;
}

.qna-rerank-hint :deep(svg) {
  flex-shrink: 0;
  color: var(--warning, #f59e0b);
}

.qna-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin: auto;
  color: var(--text-secondary, #9d9d9d);
  text-align: center;
}

.qna-empty h3 {
  margin: 8px 0 0;
  font-size: 16px;
  color: var(--text-primary, #e8e8e8);
}

.qna-empty p {
  margin: 0;
  font-size: 13px;
  max-width: 420px;
}

.msg {
  display: flex;
}

.msg.user {
  justify-content: flex-end;
}

.msg-bubble {
  max-width: 78%;
  padding: 10px 14px;
  border-radius: 12px;
  font-size: 14px;
  line-height: 1.6;
}

.msg.user .msg-bubble {
  background: var(--accent-color, #4f7cf7);
  color: #fff;
  border-bottom-right-radius: 4px;
  white-space: pre-wrap;
}

.msg.assistant .msg-bubble {
  background: var(--bg-secondary, #252526);
  color: var(--text-primary, #e8e8e8);
  border: 1px solid var(--border-color, #2d2d30);
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
  font-size: 13px;
}

.msg-content :deep(p) {
  margin: 0 0 8px;
}

.msg-content :deep(p:last-child) {
  margin-bottom: 0;
}

.msg-content :deep(ul),
.msg-content :deep(ol) {
  margin: 4px 0 8px;
  padding-left: 20px;
}

.msg-content :deep(code) {
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(127, 127, 127, 0.18);
  font-size: 13px;
}

.msg-content :deep(pre) {
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.3);
  overflow-x: auto;
}

.msg-content :deep(h1),
.msg-content :deep(h2),
.msg-content :deep(h3) {
  margin: 8px 0 6px;
  font-size: 15px;
}

/* 引用 */
.msg-citations {
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px dashed var(--border-color, #2d2d30);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.citations-label {
  font-size: 11px;
  color: var(--text-secondary, #9d9d9d);
}

.citation-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  align-self: flex-start;
  max-width: 100%;
  padding: 4px 10px;
  border: 1px solid var(--border-color, #2d2d30);
  border-radius: 999px;
  background: transparent;
  color: var(--text-secondary, #9d9d9d);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.citation-chip:hover {
  color: var(--text-primary, #e8e8e8);
  border-color: var(--accent-color, #4f7cf7);
}

.cite-index {
  color: var(--accent-color, #4f7cf7);
  font-weight: 600;
}

.cite-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 260px;
}

/* 输入区 */
.qna-input {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  padding: 14px 24px 18px;
  border-top: 1px solid var(--border-color, #2d2d30);
  flex-shrink: 0;
}

.qna-input textarea {
  flex: 1;
  padding: 10px 14px;
  border: 1px solid var(--border-color, #2d2d30);
  border-radius: 10px;
  background: var(--bg-secondary, #252526);
  color: var(--text-primary, #e8e8e8);
  font-size: 14px;
  font-family: inherit;
  line-height: 1.5;
  resize: none;
  outline: none;
  transition: border-color 0.15s ease;
}

.qna-input textarea:focus {
  border-color: var(--accent-color, #4f7cf7);
}

.btn-ask {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 10px 18px;
  border: none;
  border-radius: 10px;
  background: var(--accent-color, #4f7cf7);
  color: #fff;
  font-size: 14px;
  cursor: pointer;
  transition: opacity 0.15s ease;
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
