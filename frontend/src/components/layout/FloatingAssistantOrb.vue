<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Sparkles } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { isAIAnswering } from '@/composables/useAIChat'

// AI-COPILOT-FLOATING-ORB 蓝图 Step 2：全局常驻微光悬浮球。
// 单例挂载在 App.vue 最外层，脱离路由页面生命周期——任何页面都能一键唤起抽屉。
// 支持任意拖动：按住拖走即换位，位置记忆在 localStorage；未拖动过保持右下默认位。
const { t } = useI18n()

const emit = defineEmits<{ toggle: [] }>()

// ---- 拖拽状态机 ----
const ORB_SIZE = 44
const EDGE_MARGIN = 8
const DRAG_THRESHOLD = 3 // px，超过视为拖动而非点击
const POS_KEY = 'notevault.aiOrbPos'

/** null = 未拖过，走 CSS 默认右下角；一旦拖动改用 left/top 定位 */
const pos = ref<{ x: number; y: number } | null>(null)
const dragging = ref(false)
let pressStart: { px: number; py: number; ox: number; oy: number; moved: boolean } | null = null
/** click 抑制标记：本次按压发生过真实拖动则吞掉 click */
let suppressClick = false

const dragStyle = computed(() =>
  pos.value
    ? { left: `${pos.value.x}px`, top: `${pos.value.y}px`, right: 'auto', bottom: 'auto' }
    : undefined,
)

function clamp(x: number, y: number): { x: number; y: number } {
  const maxX = window.innerWidth - ORB_SIZE - EDGE_MARGIN
  // 底部避开常驻状态栏
  const maxY = window.innerHeight - ORB_SIZE - EDGE_MARGIN - 28
  return {
    x: Math.min(Math.max(x, EDGE_MARGIN), Math.max(maxX, EDGE_MARGIN)),
    y: Math.min(Math.max(y, EDGE_MARGIN), Math.max(maxY, EDGE_MARGIN)),
  }
}

function loadPersistedPos() {
  try {
    const raw = localStorage.getItem(POS_KEY)
    if (!raw) return
    const saved = JSON.parse(raw) as { x: number; y: number }
    if (typeof saved?.x === 'number' && typeof saved?.y === 'number') {
      pos.value = clamp(saved.x, saved.y)
    }
  } catch {
    // 坏数据当作没拖过
  }
}

function onPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  pressStart = { px: e.clientX, py: e.clientY, ox: rect.left, oy: rect.top, moved: false }
  // 捕获后 move/up 都派发到球本身，拖出球体也不丢事件；无实现环境（jsdom）静默跳过
  ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
}

function onPointerMove(e: PointerEvent) {
  if (!pressStart) return
  const dx = e.clientX - pressStart.px
  const dy = e.clientY - pressStart.py
  if (!pressStart.moved && Math.abs(dx) + Math.abs(dy) <= DRAG_THRESHOLD) return
  pressStart.moved = true
  dragging.value = true
  pos.value = clamp(pressStart.ox + dx, pressStart.oy + dy)
}

function onPointerUp() {
  if (!pressStart) return
  suppressClick = pressStart.moved
  if (pressStart.moved && pos.value) {
    try {
      localStorage.setItem(POS_KEY, JSON.stringify(pos.value))
    } catch {
      /* 存不上就只在本次会话生效 */
    }
  }
  pressStart = null
  dragging.value = false
}

function onClick() {
  if (suppressClick) {
    suppressClick = false
    return
  }
  emit('toggle')
}

/** 窗口缩放后把球拉回可视范围 */
function onWindowResize() {
  if (pos.value) pos.value = clamp(pos.value.x, pos.value.y)
}

onMounted(() => {
  loadPersistedPos()
  window.addEventListener('resize', onWindowResize)
})
onBeforeUnmount(() => window.removeEventListener('resize', onWindowResize))
</script>

<template>
  <button
    type="button"
    class="ai-orb"
    :class="{ answering: isAIAnswering, dragging }"
    :style="dragStyle"
    :aria-label="t('copilot.orbTip')"
    :title="t('copilot.orbTip')"
    data-ai-orb
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @pointercancel="onPointerUp"
    @click="onClick"
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
  cursor: grab;
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

/* 拖动中：禁 hover 位移/过渡，光标变抓取态，跟手不弹跳 */
.ai-orb.dragging {
  cursor: grabbing;
  transition: none;
  transform: none;
  opacity: 0.92;
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
