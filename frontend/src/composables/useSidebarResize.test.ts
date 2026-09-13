// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest'
import { ref } from 'vue'
import { useSidebarResize, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from './useSidebarResize'

function pointerEvent(clientX: number, button = 0): PointerEvent {
  return { button, clientX, preventDefault: () => {} } as unknown as PointerEvent
}

describe('useSidebarResize', () => {
  beforeEach(() => localStorage.clear())

  it('drags to resize and clamps to the configured range', () => {
    const collapsed = ref(false)
    const { width, beginResize } = useSidebarResize({ collapsed, autoCollapse: () => {} })
    expect(width.value).toBe(240)

    beginResize(pointerEvent(0))
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 80 } as PointerEvent))
    expect(width.value).toBe(320)

    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 100000 } as PointerEvent))
    expect(width.value).toBe(SIDEBAR_MAX_WIDTH)

    window.dispatchEvent(new PointerEvent('pointerup', {} as PointerEvent))
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 0 } as PointerEvent))
    expect(width.value).toBe(SIDEBAR_MAX_WIDTH)
  })

  it('persists the width and restores it on next mount', () => {
    const first = useSidebarResize({ collapsed: ref(false), autoCollapse: () => {} })
    first.width.value = 333
    expect(JSON.parse(localStorage.getItem('notevault:sidebar-width:v1') || '0')).toBe(333)

    const second = useSidebarResize({ collapsed: ref(false), autoCollapse: () => {} })
    expect(second.width.value).toBe(333)
  })

  it('ignores non-primary button presses', () => {
    const { width, beginResize } = useSidebarResize({ collapsed: ref(false), autoCollapse: () => {} })
    beginResize(pointerEvent(100, 2))
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 300 } as PointerEvent))
    expect(width.value).toBe(240)
  })

  it('triggers auto-collapse when the window is narrow', () => {
    let collapsedCalls = 0
    const collapsed = ref(false)
    useSidebarResize({ collapsed, autoCollapse: () => { collapsedCalls++ } })
    const originalInnerWidth = window.innerWidth
    Object.defineProperty(window, 'innerWidth', { value: 800, configurable: true })
    window.dispatchEvent(new Event('resize'))
    expect(collapsedCalls).toBe(1)
    Object.defineProperty(window, 'innerWidth', { value: originalInnerWidth, configurable: true })
  })
})
