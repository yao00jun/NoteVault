<script setup lang="ts">
/**
 * ConfirmDialog - 全局确认弹框渲染层
 * 挂在 App.vue 根部一次，读 useConfirm 的模块级单例状态渲染。
 * 替代 window.confirm（WebView2 把它转成位置不可控的原生对话框）。
 */
import { watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConfirm } from '@/composables/useConfirm'

const { t } = useI18n()
const { state, accept, chooseAlt, dismiss } = useConfirm()

// 弹框期间接管 Esc（取消）与 Enter（确认），关闭后移除监听
function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    dismiss()
  } else if (e.key === 'Enter') {
    e.preventDefault()
    accept()
  }
}

watch(
  () => state.value.visible,
  (visible) => {
    if (visible) window.addEventListener('keydown', handleKeydown)
    else window.removeEventListener('keydown', handleKeydown)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<template>
  <div
    v-if="state.visible"
    class="confirm-mask"
    data-testid="confirm-mask"
    @click.self="dismiss"
  >
    <div
      class="confirm-box"
      role="dialog"
      aria-modal="true"
    >
      <p
        v-if="state.title"
        class="confirm-title"
      >
        {{ state.title }}
      </p>
      <p class="confirm-text">{{ state.message }}</p>
      <div class="confirm-actions">
        <button
          class="confirm-btn cancel"
          type="button"
          @click="dismiss"
        >
          {{ state.cancelText || t('common.cancel') }}
        </button>
        <button
          v-if="state.altText"
          class="confirm-btn alt"
          type="button"
          data-testid="confirm-alt"
          @click="chooseAlt"
        >
          {{ state.altText }}
        </button>
        <button
          class="confirm-btn ok"
          :class="{ danger: state.danger }"
          type="button"
          data-testid="confirm-ok"
          autofocus
          @click="accept"
        >
          {{ state.confirmText || t('common.confirm') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 样式与 TitleBar 退出确认弹框保持同一套视觉 */
.confirm-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
}
.confirm-box {
  background: var(--bg-window, #1e1f22);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  border-radius: 8px;
  padding: 20px 24px 16px;
  min-width: 320px;
  max-width: 80vw;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
}
.confirm-title {
  font-size: var(--text-base, 14px);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 8px;
}
.confirm-text {
  font-size: var(--text-sm, 13px);
  color: var(--text-secondary, #aaa);
  margin: 0 0 16px;
  line-height: 1.5;
  word-break: break-word;
}
.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.confirm-btn {
  padding: 6px 16px;
  border-radius: 4px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-sm, 13px);
  cursor: pointer;
  transition: background var(--transition-fast);
}
.confirm-btn:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.08));
}
.confirm-btn.alt {
  background: var(--bg-card, rgba(255, 255, 255, 0.08));
}
.confirm-btn.ok {
  background: var(--accent, #007aff);
  border-color: var(--accent, #007aff);
  color: #fff;
}
.confirm-btn.ok:hover {
  filter: brightness(1.1);
}
.confirm-btn.ok.danger {
  background: #dc2626;
  border-color: #dc2626;
}
.confirm-btn.ok.danger:hover {
  background: #b91c1c;
  filter: none;
}
</style>
