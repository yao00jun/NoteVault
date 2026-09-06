<script setup lang="ts" generic="T">
/**
 * VirtualList - 轻量虚拟滚动容器（蓝图 Phase 6）
 *
 * 固定行高窗口化：只渲染可视区 ± overscan 的行，万级数据 60fps。
 * - 固定行高（rowHeight prop），行内容用作用域插槽渲染
 * - 滚动用 rAF 节流，容器尺寸变化用 ResizeObserver 重算
 * - 无依赖、纯 Vue，不引入第三方虚拟滚动库
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    items: T[]
    rowHeight: number
    overscan?: number
  }>(),
  { overscan: 6 },
)

const emit = defineEmits<{
  (e: 'scroll-bottom'): void
}>()

const container = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const viewportH = ref(0)
let rafId = 0
let resizeObserver: ResizeObserver | null = null

function onScroll() {
  if (rafId) return
  rafId = requestAnimationFrame(() => {
    rafId = 0
    if (!container.value) return
    scrollTop.value = container.value.scrollTop
    // 触底检测（距底 < 2 行）交还父组件做加载更多
    const el = container.value
    if (el.scrollHeight - el.scrollTop - el.clientHeight < props.rowHeight * 2) {
      emit('scroll-bottom')
    }
  })
}

const totalH = computed(() => props.items.length * props.rowHeight)

const startIndex = computed(() => {
  const s = Math.max(0, Math.floor(scrollTop.value / props.rowHeight) - props.overscan)
  return Math.min(s, Math.max(0, props.items.length))
})

const visibleCount = computed(() => {
  const h = viewportH.value || 600
  return Math.ceil(h / props.rowHeight) + props.overscan * 2
})

const endIndex = computed(() => Math.min(props.items.length, startIndex.value + visibleCount.value))

const visibleItems = computed(() =>
  props.items.slice(startIndex.value, endIndex.value).map((item, i) => ({
    item,
    index: startIndex.value + i,
  })),
)

const offsetTop = computed(() => startIndex.value * props.rowHeight)

onMounted(() => {
  if (container.value) {
    viewportH.value = container.value.clientHeight
    // jsdom（测试环境）没有 ResizeObserver，静默跳过
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => {
        if (container.value) viewportH.value = container.value.clientHeight
      })
      resizeObserver.observe(container.value)
    }
  }
})

onBeforeUnmount(() => {
  if (rafId) cancelAnimationFrame(rafId)
  resizeObserver?.disconnect()
  resizeObserver = null
})
</script>

<template>
  <div
    ref="container"
    class="virtual-list"
    @scroll.passive="onScroll"
  >
    <div
      class="virtual-list-spacer"
      :style="{ height: totalH + 'px' }"
    >
      <div
        class="virtual-list-window"
        :style="{ transform: `translateY(${offsetTop}px)` }"
      >
        <slot
          v-for="{ item, index } in visibleItems"
          :key="index"
          :item="item"
          :index="index"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.virtual-list {
  overflow-y: auto;
  position: relative;
  height: 100%;
}
.virtual-list-spacer {
  position: relative;
}
.virtual-list-window {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
}
</style>
