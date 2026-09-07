<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import { BookOpen, CheckCircle2, Eye, Sparkles, ArrowUpRight, Star } from '@lucide/vue'
import { useWorkbenchStore } from '@/stores/workbench'
import { useWorkspaceStore } from '@/stores/workspace'
import { sanitizeHtml } from '@/utils/sanitize'
import { requestCopilot } from '@/composables/useCopilotRequest'

withDefaults(defineProps<{ compact?: boolean; limit?: number }>(), { compact: false, limit: 5 })
const store = useWorkbenchStore()
const workspace = useWorkspaceStore()
const router = useRouter()
const revealed = ref(false)
const error = ref('')
const card = computed(() => store.reviewQueue[0])
const answer = computed(() => sanitizeHtml(marked.parse(card.value?.answer || '这道题还没有答案，打开笔记补充你的理解。', { async: false }) as string))
const completed = computed(() => store.reviewedToday > 0 && store.reviewedToday >= store.reviewTarget)
watch(() => card.value?.id, () => { revealed.value = false; error.value = '' })

async function rate(level: '掌握' | '模糊' | '不会') {
  if (!card.value || !revealed.value || store.busy) return
  error.value = ''
  try { await store.reviewCard(card.value, level) } catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
}
function openNote() {
  if (!card.value) return
  workspace.openFile(card.value.filePath)
  void router.push({ path: '/editor', query: { file: card.value.filePath, line: card.value.lineIndex + 1 } })
}
function askAI() {
  if (!card.value) return
  requestCopilot({ prompt: '请解释这道面试题的底层原理，举一个实际开发中的例子，并提出一道追问题。', context: `${card.value.question}\n\n${card.value.answer}`, source: 'learning', workspacePath: workspace.currentWorkspace?.path })
}
</script>

<template>
  <section
    class="interview-review"
    :class="{ compact }"
    aria-label="今日面试复习"
  >
    <header class="review-header">
      <div><BookOpen :size="16" /><h3>每日五题</h3></div>
      <span class="review-count">{{ Math.min(store.reviewedToday, store.reviewTarget) }} / {{ store.reviewTarget }} 已复习</span>
    </header>
    <div class="review-track">
      <span :style="{ width: `${store.reviewTarget ? Math.min(100, store.reviewedToday / store.reviewTarget * 100) : 0}%` }" />
    </div>
    <div
      v-if="card"
      class="question-card"
    >
      <div class="question-meta">
        <span>先回想，再看答案</span><span
          v-if="card.weak"
          class="weak"
        ><Star :size="11" />核心薄弱点</span>
      </div>
      <h4>{{ card.question }}</h4>
      <button
        v-if="!revealed"
        type="button"
        class="reveal-button"
        data-testid="reveal-answer"
        @click="revealed = true"
      >
        <Eye :size="14" />显示答案
      </button>
      <div
        v-if="revealed"
        class="answer"
        v-html="answer"
      />
      <div
        v-if="revealed"
        class="ratings"
      >
        <button
          type="button"
          class="failed"
          :disabled="store.busy"
          data-testid="rate-failed"
          @click="rate('不会')"
        >
          完全不会<small>1 天后复习</small>
        </button>
        <button
          type="button"
          class="vague"
          :disabled="store.busy"
          data-testid="rate-vague"
          @click="rate('模糊')"
        >
          有些模糊<small>3 天后复习</small>
        </button>
        <button
          type="button"
          class="mastered"
          :disabled="store.busy"
          data-testid="rate-mastered"
          @click="rate('掌握')"
        >
          熟练掌握<small>7 天后复习</small>
        </button>
      </div>
      <p
        v-if="error"
        class="review-error"
        role="alert"
      >
        {{ error }}
      </p>
      <footer class="question-footer">
        <button
          type="button"
          @click="openNote"
        >
          打开笔记<ArrowUpRight :size="12" />
        </button><button
          type="button"
          @click="askAI"
        >
          <Sparkles :size="12" />向 Copilot 追问
        </button>
      </footer>
    </div>
    <div
      v-else
      class="review-empty"
      :class="{ finished: completed }"
    >
      <CheckCircle2 :size="28" />
      <h4>{{ completed ? '今日复习已完成' : '今天没有到期题目' }}</h4>
      <p>{{ completed ? '保持每天一点积累，明天继续。' : '面试宝典中到期的题卡会出现在这里。' }}</p>
    </div>
  </section>
</template>

<style scoped>
.interview-review { background: var(--bg-card); border: 1px solid var(--border); border-radius: 14px; overflow: hidden; }
.review-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 18px 20px 13px; }
.review-header>div { display: flex; gap: 8px; align-items: center; color: var(--accent); }
.review-header h3 { margin: 0; font-size: 14px; font-weight: 600; color: var(--text-primary); }
.review-count { color: var(--text-muted); font-size: 11px; white-space: nowrap; }
.review-track { height: 3px; background: var(--bg-hover); margin: 0 20px; border-radius: 4px; overflow: hidden; }
.review-track span { display: block; height: 100%; background: #65a687; transition: width .3s; }
.question-card { padding: 20px; }
.question-meta { display: flex; justify-content: space-between; gap: 8px; color: var(--text-muted); font-size: 11px; }
.question-meta .weak { color: #c19242; display: inline-flex; align-items: center; gap: 3px; }
.question-card h4 { font-size: 17px; font-weight: 550; line-height: 1.7; margin: 13px 0 20px; color: var(--text-primary); overflow-wrap: anywhere; }
.compact .question-card h4 { font-size: 15px; }
.reveal-button { display: flex; gap: 7px; align-items: center; justify-content: center; width: 100%; padding: 11px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg-content); font-size: 12px; color: var(--text-secondary); }
.reveal-button:hover { color: var(--accent); border-color: var(--accent); }
.answer { color: var(--text-secondary); font-size: 13px; line-height: 1.85; margin-bottom: 20px; overflow-wrap: anywhere; max-height: 440px; overflow: auto; }
.answer :deep(ul), .answer :deep(ol) { padding-left: 20px; }
.answer :deep(pre) { white-space: pre-wrap; background: var(--bg-content); border-radius: 6px; padding: 10px; }
.answer :deep(p) { margin: 8px 0; }
.ratings { display: grid; grid-template-columns: repeat(3, minmax(0,1fr)); gap: 7px; }
.ratings button { display: flex; flex-direction: column; align-items: center; padding: 10px 3px; gap: 4px; border-radius: 7px; font-size: 12px; border: 1px solid currentColor; }
.ratings small { font-size: 10px; opacity: .85; }
.failed { color: #c96963; background: color-mix(in srgb, #c96963 7%, transparent); }
.vague { color: #ae8838; background: color-mix(in srgb, #ae8838 7%, transparent); }
.mastered { color: #508f6c; background: color-mix(in srgb, #508f6c 7%, transparent); }
.ratings button:disabled { opacity: .45; cursor: wait; }
.question-footer { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; margin-top: 18px; }
.question-footer button { display: inline-flex; align-items: center; gap: 5px; color: var(--text-muted); font-size: 11px; }
.question-footer button:hover { color: var(--accent); }
.review-empty { text-align: center; padding: 34px 20px; color: var(--text-muted); }
.review-empty h4 { margin: 13px 0 8px; color: var(--text-secondary); font-size: 14px; }
.review-empty p { font-size: 12px; line-height: 1.7; }
.review-empty.finished { color: #65a687; }
.review-error { color: #cf5a56; font-size: 12px; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
</style>
