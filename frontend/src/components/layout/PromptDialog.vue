<script setup lang="ts">
/**
 * PromptDialog - 全局输入弹框渲染层
 * 挂在 App.vue 根部一次，读 usePrompt 的模块级单例状态渲染。
 * 替代 window.prompt（WebView2 原生对话框位置不可控、无法应用主题）。
 */
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePrompt } from '@/composables/usePrompt'

const { t } = useI18n()
const { state, submit, dismiss } = usePrompt()

const inputEl = ref<HTMLInputElement | null>(null)

// 弹出时聚焦输入框并全选预填值（对齐原生 prompt 的覆盖输入习惯）。
// immediate：组件可能在弹框已可见的状态下挂载（测试/复用场景），此时也要接好键盘监听
watch(
  () => state.value.visible,
  async (visible) => {
    if (visible) {
      window.addEventListener('keydown', handleGlobalKeydown)
      await nextTick()
      inputEl.value?.focus()
      inputEl.value?.select()
    } else {
      window.removeEventListener('keydown', handleGlobalKeydown)
    }
  },
  { immediate: true },
)

function handleGlobalKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    dismiss()
  } else if (e.key === 'Enter') {
    e.preventDefault()
    submit()
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
})
</script>

<template>
  <div
    v-if="state.visible"
    class="prompt-mask"
    data-testid="prompt-mask"
    @click.self="dismiss"
  >
    <div
      class="prompt-box"
      role="dialog"
      aria-modal="true"
    >
      <label
        class="prompt-text"
        for="prompt-dialog-input"
      >{{ state.message }}</label>
      <input
        id="prompt-dialog-input"
        ref="inputEl"
        v-model="state.value"
        class="prompt-input"
        type="text"
        :placeholder="state.placeholder"
        data-testid="prompt-input"
      >
      <div class="prompt-actions">
        <button
          class="prompt-btn cancel"
          type="button"
          @click="dismiss"
        >
          {{ state.cancelText || t('common.cancel') }}
        </button>
        <button
          class="prompt-btn ok"
          type="button"
          data-testid="prompt-ok"
          @click="submit"
        >
          {{ state.okText || t('common.confirm') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 与 ConfirmDialog 保持同一套视觉 */
.prompt-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
}
.prompt-box {
  background: var(--bg-window, #1e1f22);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  border-radius: 8px;
  padding: 20px 24px 16px;
  min-width: 340px;
  max-width: 80vw;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
}
.prompt-text {
  display: block;
  font-size: var(--text-sm, 13px);
  color: var(--text-secondary, #aaa);
  margin: 0 0 10px;
  line-height: 1.5;
}
.prompt-input {
  width: 100%;
  box-sizing: border-box;
  height: 32px;
  padding: 0 var(--space-2, 8px);
  background: var(--bg-input, rgba(255, 255, 255, 0.06));
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  border-radius: var(--radius-sm, 4px);
  color: var(--text-primary);
  font-size: var(--text-sm, 13px);
  margin-bottom: 16px;
  outline: none;
}
.prompt-input:focus {
  border-color: var(--border-accent, var(--accent, #007aff));
}
.prompt-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.prompt-btn {
  padding: 6px 16px;
  border-radius: 4px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-sm, 13px);
  cursor: pointer;
  transition: background var(--transition-fast);
}
.prompt-btn:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.08));
}
.prompt-btn.ok {
  background: var(--accent, #007aff);
  border-color: var(--accent, #007aff);
  color: #fff;
}
.prompt-btn.ok:hover {
  filter: brightness(1.1);
}
</style>
