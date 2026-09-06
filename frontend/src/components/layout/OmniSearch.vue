<script setup lang="ts">
/**
 * OmniSearch - 全局检索与 AI 问答浮层（蓝图 2.4 / Phase 3）
 *
 * 任意界面 Ctrl+K 唤起，居中毛玻璃浮层：
 *  - 检索模式：BM25（gse 分词）毫秒级返回标题与高亮片段，点击直达编辑器
 *  - 问答模式（点击 Tab 或输入以 ? 开头）：对全库发起 RAG 提问，
 *    返回带引用来源的回答；引用可点击跳转
 * AI 未配置 / 无网络时平滑降级为纯检索，绝不阻断。
 */
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, MessageCircle, X, FileText, ArrowRight } from '@lucide/vue'
import { SearchService, QnAService } from '@/api'
import type { RerankProvider } from '@/api'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { isImeComposing } from '@/utils/ime'

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const settingsStore = useSettingsStore()

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'open-file', path: string): void
}>()

type Mode = 'search' | 'ask'
const mode = ref<Mode>('search')
const query = ref('')
interface OmniResult {
  path: string
  title: string
  snippet: string
}
const results = ref<OmniResult[]>([])
const searching = ref(false)
const answer = ref('')
const citations = ref<{ index: number; path: string; title: string; snippet: string }[]>([])
const asking = ref(false)
const asked = ref(false)
const errorMsg = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

const wsPath = computed(() => workspaceStore.currentWorkspace?.path ?? '')

// 打开时聚焦输入框
watch(
  () => props.visible,
  async (v) => {
    if (v) {
      await nextTick()
      inputEl.value?.focus()
    }
  },
)

// 问答模式快捷入口：输入 ? 开头自动切换
function detectModePrefix() {
  if (query.value.startsWith('?')) {
    if (mode.value !== 'ask') {
      mode.value = 'ask'
      query.value = query.value.slice(1)
    }
  }
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(query, () => {
  detectModePrefix()
  if (mode.value !== 'search') return
  if (searchTimer) clearTimeout(searchTimer)
  if (!query.value.trim()) {
    results.value = []
    return
  }
  searchTimer = setTimeout(() => void doSearch(), 180)
})

async function doSearch() {
  const q = query.value.trim()
  if (!q || !wsPath.value) {
    results.value = []
    return
  }
  searching.value = true
  try {
    const data = await SearchService.Search(wsPath.value, q)
    // 绑定层对可空切片统一标可空：先过滤 null 再裁剪到 30 条
    const raw = (Array.isArray(data) ? data : []) as Array<Partial<OmniResult> | null>
    results.value = raw
      .filter((x): x is Partial<OmniResult> & { path: string } => !!x && typeof x.path === 'string')
      .slice(0, 30)
      .map((x) => ({ path: x.path, title: x.title ?? '', snippet: x.snippet ?? '' }))
  } catch {
    results.value = [] // 静默降级：搜索失败不留错误弹窗
  } finally {
    searching.value = false
  }
}

async function doAsk() {
  const q = query.value.trim().replace(/^\?/, '')
  if (!q || !wsPath.value || asking.value) return
  const ai = settingsStore.settings.ai
  const emb = settingsStore.settings.embedding
  const rerank = settingsStore.settings.rerank
  asking.value = true
  asked.value = true
  errorMsg.value = ''
  answer.value = ''
  citations.value = []
  try {
    const resp = await QnAService.Answer(
      ai.apiKey,
      ai.baseURL,
      ai.model,
      ai.protocol,
      emb.baseURL,
      emb.model,
      emb.apiKey,
      {
        provider: rerank.provider as unknown as RerankProvider,
        baseURL: rerank.baseURL,
        model: rerank.model,
        apiKey: rerank.apiKey,
      },
      wsPath.value,
      q,
    )
    answer.value = (resp?.answer ?? '').trim() || t('omni.emptyAnswer')
    citations.value = resp?.citations ?? []
  } catch (e) {
    // 平滑降级：问答失败给出可行动提示而非阻断
    errorMsg.value = t('omni.askFailed', { msg: (e as Error).message })
  } finally {
    asking.value = false
  }
}

function submit() {
  if (mode.value === 'ask') {
    void doAsk()
    return
  }
  // 检索模式 Enter：打开第一条结果
  if (results.value.length > 0) {
    openResult(results.value[0]!.path)
  }
}

function openResult(path: string) {
  emit('open-file', path)
  emit('close')
}

function openCitation(path: string) {
  openResult(path)
}

function switchMode() {
  mode.value = mode.value === 'search' ? 'ask' : 'search'
  if (mode.value === 'ask' && query.value.trim()) void doAsk()
}

function onKeydown(e: KeyboardEvent) {
  if (isImeComposing(e)) return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
  } else if (e.key === 'Enter') {
    e.preventDefault()
    submit()
  }
}

defineExpose({ switchMode })
</script>

<template>
  <div
    v-if="visible"
    class="omni-mask"
    data-testid="omni-mask"
    @click.self="emit('close')"
  >
    <div
      class="omni-box"
      role="dialog"
      aria-modal="true"
    >
      <div class="omni-input-row">
        <component
          :is="mode === 'ask' ? MessageCircle : Search"
          :size="16"
          class="omni-mode-icon"
        />
        <input
          ref="inputEl"
          v-model="query"
          class="omni-input"
          type="text"
          :placeholder="mode === 'ask' ? t('omni.askPlaceholder') : t('omni.searchPlaceholder')"
          data-testid="omni-input"
          @keydown="onKeydown"
        >
        <button
          class="omni-mode-btn"
          :class="{ active: mode === 'ask' }"
          :title="t('omni.switchMode')"
          @click="switchMode"
        >
          {{ mode === 'ask' ? t('omni.modeAsk') : t('omni.modeSearch') }}
        </button>
        <button
          class="omni-close"
          @click="emit('close')"
        >
          <X :size="15" />
        </button>
      </div>

      <!-- 检索模式 -->
      <div
        v-if="mode === 'search'"
        class="omni-body"
      >
        <div
          v-if="searching"
          class="omni-hint"
        >
          {{ t('common.loading') }}
        </div>
        <div
          v-else-if="results.length === 0"
          class="omni-hint"
        >
          {{ query.trim() ? t('omni.noResults') : t('omni.searchHint') }}
        </div>
        <div
          v-else
          class="omni-results"
        >
          <button
            v-for="r in results"
            :key="r.path"
            class="omni-result"
            :data-testid="`omni-result-${r.path}`"
            @click="openResult(r.path)"
          >
            <FileText :size="14" />
            <span class="omni-result-title">{{ r.title || r.path }}</span>
            <span class="omni-result-snippet">{{ r.snippet }}</span>
            <ArrowRight :size="12" />
          </button>
        </div>
      </div>

      <!-- 问答模式 -->
      <div
        v-else
        class="omni-body"
      >
        <div
          v-if="asking"
          class="omni-hint"
        >
          {{ t('omni.thinking') }}
        </div>
        <template v-else>
          <div
            v-if="errorMsg"
            class="omni-error"
          >
            {{ errorMsg }}
          </div>
          <div
            v-else-if="!asked"
            class="omni-hint"
          >
            {{ t('omni.askHint') }}
          </div>
          <div
            v-else
            class="omni-answer"
            data-testid="omni-answer"
          >
            {{ answer }}
          </div>
          <div
            v-if="citations.length > 0"
            class="omni-citations"
          >
            <div class="omni-citations-label">
              {{ t('omni.citations') }}
            </div>
            <button
              v-for="c in citations"
              :key="c.index"
              class="omni-citation"
              :title="c.path"
              @click="openCitation(c.path)"
            >
              [{{ c.index }}] {{ c.title || c.path }}
            </button>
          </div>
        </template>
      </div>

      <div class="omni-footer">
        <span>Enter {{ t('omni.footerAction') }}</span>
        <span>Esc {{ t('omni.footerClose') }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.omni-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 12vh;
  z-index: 10020;
}
.omni-box {
  width: min(640px, 90vw);
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-window, #1e1f22);
  border: 1px solid var(--border);
  border-radius: var(--radius-md, 10px);
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
  overflow: hidden;
}
.omni-input-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}
.omni-mode-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}
.omni-input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-base);
}
.omni-input::placeholder {
  color: var(--text-muted);
}
.omni-mode-btn {
  padding: 3px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.omni-mode-btn.active,
.omni-mode-btn:hover {
  background: var(--bg-active);
  color: var(--text-primary);
}
.omni-close {
  display: flex;
  align-items: center;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 3px;
}
.omni-close:hover {
  color: var(--text-primary);
}
.omni-body {
  flex: 1;
  overflow: auto;
  padding: var(--space-2);
  min-height: 120px;
}
.omni-hint {
  padding: var(--space-5) var(--space-3);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.omni-error {
  padding: var(--space-3);
  color: var(--error, #ef4444);
  font-size: var(--text-sm);
}
.omni-results {
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.omni-result {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  transition: background var(--transition-fast);
}
.omni-result:hover {
  background: var(--bg-hover);
}
.omni-result-title {
  font-size: var(--text-sm);
  font-weight: 500;
  flex-shrink: 0;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.omni-result-snippet {
  flex: 1;
  font-size: var(--text-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.omni-answer {
  padding: var(--space-3);
  font-size: var(--text-sm);
  line-height: 1.7;
  color: var(--text-primary);
  white-space: pre-wrap;
}
.omni-citations {
  padding: var(--space-2) var(--space-3) var(--space-3);
  border-top: 1px solid var(--border);
}
.omni-citations-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-bottom: var(--space-1);
}
.omni-citation {
  display: inline-block;
  margin: 2px 6px 2px 0;
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color var(--transition-fast), border-color var(--transition-fast);
}
.omni-citation:hover {
  color: var(--accent);
  border-color: var(--border-accent, var(--accent));
}
.omni-footer {
  display: flex;
  gap: var(--space-4);
  padding: var(--space-2) var(--space-4);
  border-top: 1px solid var(--border);
  font-size: var(--text-xs);
  color: var(--text-muted);
}
</style>
