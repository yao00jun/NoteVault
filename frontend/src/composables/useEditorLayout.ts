import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from 'vue'

type Mode = 'split' | 'editor' | 'preview'
const storageKey = 'notevault:editor-layout:v1'
const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))

/** Layout preferences contain no document data and can be shared across workspaces. */
export function useEditorLayout() {
  let saved: { treeWidth?: number; treeOpen?: boolean; splitPercent?: number; viewMode?: Mode } = {}
  try { saved = JSON.parse(localStorage.getItem(storageKey) || '{}') ?? {} } catch { /* Defaults recover old/corrupt preferences. */ }
  const treeWidth = ref(clamp(Number(saved.treeWidth) || 224, 180, 360))
  const treeOpen = ref(saved.treeOpen !== false)
  const splitPercent = ref(clamp(Number(saved.splitPercent) || 50, 30, 70))
  const viewMode = ref<Mode>(['split', 'editor', 'preview'].includes(saved.viewMode || '') ? saved.viewMode! : 'split')
  const mainRef = ref<HTMLElement | null>(null)
  const panesRef = ref<HTMLElement | null>(null)
  const mainWidth = ref(window.innerWidth - 230)
  const compactTreeOpen = ref(false)
  const overlayTree = computed(() => mainWidth.value < 760)
  const showTree = computed(() => overlayTree.value ? compactTreeOpen.value : treeOpen.value)
  const contentWidth = computed(() => mainWidth.value - (!overlayTree.value && showTree.value ? treeWidth.value + 6 : 0))
  const effectiveViewMode = computed<Mode>(() => viewMode.value === 'split' && contentWidth.value < 720 ? 'editor' : viewMode.value)
  let observer: ResizeObserver | undefined
  let stopResize: (() => void) | undefined

  watch([treeWidth, treeOpen, splitPercent, viewMode], () => {
    try { localStorage.setItem(storageKey, JSON.stringify({ treeWidth: treeWidth.value, treeOpen: treeOpen.value, splitPercent: splitPercent.value, viewMode: viewMode.value })) } catch { /* A full browser store must not interrupt writing. */ }
  })

  function measure() { if (mainRef.value?.clientWidth) mainWidth.value = mainRef.value.clientWidth }
  function toggleTree() {
    if (overlayTree.value) compactTreeOpen.value = !compactTreeOpen.value
    else treeOpen.value = !treeOpen.value
  }
  function toggleViewMode() {
    // Compact windows have one useful writing pane; preview remains one click away.
    if (contentWidth.value < 720) viewMode.value = effectiveViewMode.value === 'preview' ? 'editor' : 'preview'
    else viewMode.value = viewMode.value === 'split' ? 'editor' : viewMode.value === 'editor' ? 'preview' : 'split'
  }
  function beginResize(event: PointerEvent, kind: 'tree' | 'split') {
    if (event.button !== 0) return
    const element = kind === 'tree' ? mainRef.value : panesRef.value
    if (!element) return
    event.preventDefault()
    stopResize?.()
    const rect = element.getBoundingClientRect()
    const move = (pointer: PointerEvent) => {
      if (kind === 'tree') treeWidth.value = clamp(pointer.clientX - rect.left, 180, Math.min(360, rect.width - 420))
      else splitPercent.value = clamp((pointer.clientX - rect.left) / rect.width * 100, 30, 70)
    }
    stopResize = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', finish)
      window.removeEventListener('pointercancel', finish)
      stopResize = undefined
    }
    const finish = () => stopResize?.()
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', finish)
    window.addEventListener('pointercancel', finish)
  }
  function adjust(kind: 'tree' | 'split', delta: number) {
    if (kind === 'tree') treeWidth.value = clamp(treeWidth.value + delta * 16, 180, 360)
    else splitPercent.value = clamp(splitPercent.value + delta * 5, 30, 70)
  }
  onMounted(() => {
    measure()
    observer = new ResizeObserver(measure)
    if (mainRef.value) observer.observe(mainRef.value)
  })
  onActivated(() => { measure(); if (mainRef.value) observer?.observe(mainRef.value) })
  onDeactivated(() => { observer?.disconnect(); compactTreeOpen.value = false; stopResize?.() })
  onBeforeUnmount(() => { observer?.disconnect(); stopResize?.() })
  return { mainRef, panesRef, treeWidth, showTree, overlayTree, splitPercent, viewMode, effectiveViewMode, toggleTree, toggleViewMode, beginResize, adjust }
}
