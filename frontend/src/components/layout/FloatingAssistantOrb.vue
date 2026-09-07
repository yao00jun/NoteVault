<script setup lang="ts">
import { Sparkles } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { isAIAnswering } from '@/composables/useAIChat'

// AI-COPILOT-FLOATING-ORB 蓝图 Step 2：全局常驻微光悬浮球。
// 单例挂载在 App.vue 最外层，脱离路由页面生命周期——任何页面都能一键唤起抽屉。
const { t } = useI18n()

defineEmits<{ toggle: [] }>()
</script>

<template>
  <button
    type="button"
    class="ai-orb"
    :class="{ answering: isAIAnswering }"
    :aria-label="t('copilot.orbTip')"
    :title="t('copilot.orbTip')"
    data-ai-orb
    @click="$emit('toggle')"
  >
    <span class="orb-halo" />
    <Sparkles
      :size="20"
      :stroke-width="1.8"
    />
  </button>
</template>

<style scoped>
.ai-orb {
  position: fixed;
  right: 28px;
  /* 状态栏（--statusbar-height）常驻在应用底部，球要避开它 */
  bottom: calc(var(--statusbar-height, 28px) + 24px);
  z-index: 990;
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(139, 92, 246, 0.45);
  border-radius: 50%;
  background: color-mix(in srgb, var(--bg-window, #16181d) 60%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  color: #c4b5fd;
  cursor: pointer;
  box-shadow:
    0 4px 18px rgba(0, 0, 0, 0.35),
    0 0 10px rgba(139, 92, 246, 0.25);
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease,
    border-color 0.18s ease;
}

.ai-orb:hover {
  transform: scale(1.08) translateY(-2px);
  border-color: rgba(167, 139, 250, 0.75);
  box-shadow:
    0 8px 24px rgba(0, 0, 0, 0.4),
    0 0 18px rgba(139, 92, 246, 0.45);
}

.ai-orb:active {
  transform: scale(0.98);
}

.ai-orb:focus-visible {
  outline: 2px solid rgba(167, 139, 250, 0.8);
  outline-offset: 2px;
}

/* AI 回答中：外圈流光旋转（Halo）+ 本体呼吸 */
.orb-halo {
  position: absolute;
  inset: -4px;
  border-radius: 50%;
  background: conic-gradient(
    from 0deg,
    transparent 0%,
    rgba(139, 92, 246, 0.9) 22%,
    rgba(56, 189, 248, 0.6) 40%,
    transparent 62%
  );
  -webkit-mask: radial-gradient(farthest-side, transparent calc(100% - 2.5px), #000 calc(100% - 2px));
  mask: radial-gradient(farthest-side, transparent calc(100% - 2.5px), #000 calc(100% - 2px));
  opacity: 0;
  pointer-events: none;
}

.ai-orb.answering .orb-halo {
  opacity: 1;
  animation: orb-halo-spin 1.6s linear infinite;
}

.ai-orb.answering {
  animation: orb-breathe 2.2s ease-in-out infinite;
}

@keyframes orb-halo-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes orb-breathe {
  0%,
  100% {
    box-shadow:
      0 4px 18px rgba(0, 0, 0, 0.35),
      0 0 10px rgba(139, 92, 246, 0.3);
  }
  50% {
    box-shadow:
      0 4px 22px rgba(0, 0, 0, 0.35),
      0 0 22px rgba(139, 92, 246, 0.65);
  }
}
</style>
