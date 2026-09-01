<script setup lang="ts">
/**
 * ToastHost - 全局通知渲染层
 * 挂在 App.vue 根部一次，读 useToast 的模块级单例列表渲染。
 * 纯展示 + 关闭动作，不含任何业务逻辑。
 */
import { CheckCircle2, XCircle, AlertTriangle, Info, X } from 'lucide-vue-next'
import { useToast } from '@/composables/useToast'
import type { ToastKind } from '@/composables/useToast'

const { toasts, dismiss } = useToast()

const ICONS = {
  success: CheckCircle2,
  error: XCircle,
  warning: AlertTriangle,
  info: Info,
} as const satisfies Record<ToastKind, unknown>
</script>

<template>
  <div
    class="toast-stack"
    data-testid="toast-stack"
  >
    <TransitionGroup name="toast">
      <div
        v-for="toast in toasts"
        :key="toast.id"
        class="toast"
        :class="toast.kind"
        :data-testid="`toast-${toast.kind}`"
        role="status"
      >
        <component
          :is="ICONS[toast.kind]"
          :size="15"
          class="toast-icon"
        />
        <span class="toast-msg">{{ toast.message }}</span>
        <button
          type="button"
          class="toast-close"
          :aria-label="'close'"
          @click="dismiss(toast.id)"
        >
          <X :size="13" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-stack {
  position: fixed;
  right: var(--space-4);
  /* 让开底部状态栏 */
  bottom: 40px;
  z-index: 10001;
  display: flex;
  flex-direction: column;
  gap: 8px;
  /* 空栈时不挡鼠标事件 */
  pointer-events: none;
}

.toast {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 260px;
  max-width: 400px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-window);
  color: var(--text-primary);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
  font-size: var(--text-sm);
  pointer-events: auto;
}

.toast-icon {
  flex-shrink: 0;
  margin-top: 1px;
}

.toast.success {
  border-color: rgba(22, 163, 74, 0.45);
}
.toast.success .toast-icon {
  color: #16a34a;
}

.toast.error {
  border-color: rgba(239, 68, 68, 0.45);
}
.toast.error .toast-icon {
  color: #ef4444;
}

.toast.warning {
  border-color: rgba(245, 158, 11, 0.45);
}
.toast.warning .toast-icon {
  color: #f59e0b;
}

.toast.info .toast-icon {
  color: var(--accent);
}

.toast-msg {
  flex: 1;
  word-break: break-word;
  line-height: 1.45;
}

.toast-close {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  padding: 0;
  color: var(--text-muted);
  background: none;
  border: none;
  cursor: pointer;
}

.toast-close:hover {
  color: var(--text-primary);
}

/* 进出场动画 */
.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translateX(12px);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(12px);
}
</style>
