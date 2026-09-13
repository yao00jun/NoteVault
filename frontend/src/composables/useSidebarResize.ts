import { getCurrentInstance, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

const STORAGE_KEY = 'notevault:sidebar-width:v1'
export const SIDEBAR_MIN_WIDTH = 200
export const SIDEBAR_MAX_WIDTH = 420
/** 无保存宽度时的默认值：与原 --sidebar-width 保持一致 */
export const SIDEBAR_DEFAULT_WIDTH = 240
/** 窗口窄于此值时自动折叠侧栏（图标模式），避免内容区被挤没 */
export const AUTO_COLLAPSE_WINDOW_WIDTH = 1000

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))

function loadSavedWidth(): number {
  try {
    const saved = Number(JSON.parse(localStorage.getItem(STORAGE_KEY) || '0'))
    if (saved > 0) return clamp(saved, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH)
  } catch { /* 损坏数据回退默认宽度 */ }
  return 0
}

/**
 * 侧边栏宽度拖拽调宽：右缘拖拽手柄 + 持久化 + 窄窗口自动折叠。
 * 折叠状态本身仍由 settingsStore.sidebarCollapsed 管理，本组合式函数
 * 只负责「展开时的宽度」和「窄窗口时的自动折叠触发」。
 */
export function useSidebarResize(options: {
  collapsed: Ref<boolean>
  /** 窄窗口自动折叠要调用的回调（toggleSidebar） */
  autoCollapse: () => void
}) {
  const width = ref(loadSavedWidth() || SIDEBAR_DEFAULT_WIDTH)
  const resizing = ref(false)
  let stopResize: (() => void) | undefined

  // 同步持久化：拖拽中每次 move 都写一次 localStorage，量小且保证退出即存
  watch(width, (value) => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(value)) } catch { /* 存储满不中断拖拽 */ }
  }, { flush: 'sync' })

  function beginResize(event: PointerEvent) {
    if (event.button !== 0) return
    event.preventDefault()
    stopResize?.()
    resizing.value = true
    const startX = event.clientX
    const startWidth = width.value
    const move = (pointer: PointerEvent) => {
      width.value = clamp(startWidth + (pointer.clientX - startX), SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH)
    }
    const finish = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', finish)
      window.removeEventListener('pointercancel', finish)
      resizing.value = false
      stopResize = undefined
    }
    stopResize = finish
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', finish)
    window.addEventListener('pointercancel', finish)
  }

  function handleWindowResize() {
    if (window.innerWidth < AUTO_COLLAPSE_WINDOW_WIDTH && !options.collapsed.value) {
      options.autoCollapse()
    }
  }

  // 组件外（单测）直接挂监听；组件内走生命周期，避免 HMR 重复挂载
  if (getCurrentInstance()) {
    onMounted(() => window.addEventListener('resize', handleWindowResize))
    onBeforeUnmount(() => {
      window.removeEventListener('resize', handleWindowResize)
      stopResize?.()
    })
  } else {
    window.addEventListener('resize', handleWindowResize)
  }

  return { width, resizing, beginResize }
}
