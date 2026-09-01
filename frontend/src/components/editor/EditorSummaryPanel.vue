<script setup lang="ts">
/**
 * EditorSummaryPanel - 编辑器 AI 总结弹窗
 * 纯展示组件：打开/插入动作通过 emit 交还父组件处理。
 * 注：原 EditorView 的 .spin 缺少 @keyframes 定义，这里补上使加载动画真正旋转。
 */
import { useI18n } from 'vue-i18n'
import { Sparkles, X, Loader2, Download } from 'lucide-vue-next'

const { t } = useI18n()

defineProps<{
  open: boolean
  summary: string
  isSummarizing: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'insert'): void
}>()
</script>

<template>
  <div
    v-if="open"
    class="summary-overlay"
    @click.self="emit('close')"
  >
    <div class="summary-modal">
      <div class="summary-header">
        <span class="summary-title"><Sparkles :size="16" /> {{ t('editor.aiSummary') }}</span>
        <button
          class="summary-close"
          @click="emit('close')"
        >
          <X :size="16" />
        </button>
      </div>
      <div class="summary-body">
        <div
          v-if="isSummarizing"
          class="summary-loading"
        >
          <Loader2
            :size="18"
            class="spin"
          />
          <span>{{ t('editor.summarizing') }}</span>
        </div>
        <pre
          v-else
          class="summary-text"
        >{{ summary }}</pre>
      </div>
      <div class="summary-footer">
        <button
          class="btn-ghost"
          @click="emit('close')"
        >
          {{ t('common.close') }}
        </button>
        <button
          class="btn-primary"
          :disabled="!summary || isSummarizing"
          @click="emit('insert')"
        >
          <Download :size="14" /> {{ t('editor.insertToNote') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.summary-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
}

.summary-modal {
  width: min(640px, 90%);
  max-height: 80%;
  display: flex;
  flex-direction: column;
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.3);
  overflow: hidden;
}

.summary-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}

.summary-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-weight: 600;
  color: var(--accent);
}

.summary-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: all var(--transition-fast);
}
.summary-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.summary-body {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-4);
}

.summary-loading {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}

.summary-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: var(--text-sm);
  line-height: 1.7;
  color: var(--text-primary);
  margin: 0;
}

.summary-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
}

.btn-primary,
.btn-ghost {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.btn-primary {
  background: var(--accent);
  color: var(--text-inverse);
  border: 1px solid transparent;
}
.btn-primary:hover:not(:disabled) {
  background: var(--accent-hover);
}
.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-ghost {
  background: transparent;
  color: var(--text-secondary);
  border: 1px solid var(--border);
}
.btn-ghost:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
