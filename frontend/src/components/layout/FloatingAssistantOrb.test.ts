// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { i18n } from '@/i18n'

// 悬浮球只依赖 useAIChat 模块级导出的 isAIAnswering；
// mock 掉整个模块，避免连带真实 QnAService 绑定与 store 初始化。
// 注意必须是真实 ref（computed 读普通对象不会建立依赖，动画状态永远不更新）。
vi.mock('@/composables/useAIChat', async () => {
  const { ref } = await import('vue')
  return { isAIAnswering: ref(false) }
})

import FloatingAssistantOrb from './FloatingAssistantOrb.vue'
import { isAIAnswering } from '@/composables/useAIChat'

// 真实模块里 isAIAnswering 是只读 ComputedRef；测试里拿到的是 mock 的可写 ref，
// 统一经 answerFlag 别名写入，绕开类型层的 readonly 限制
const answerFlag = isAIAnswering as unknown as { value: boolean }

enableAutoUnmount(afterEach)

describe('FloatingAssistantOrb', () => {
  beforeEach(() => {
    localStorage.clear()
    answerFlag.value = false
  })

  function mountOrb() {
    return mount(FloatingAssistantOrb, {
      global: { plugins: [i18n] },
    })
  }

  // jsdom 把 pointer 事件映射为 button 只读的 MouseEvent，VTU trigger 塞不进
  // clientX/button——直接构造基础 Event（无只读属性冲突）手动派发。
  function fire(
    el: Element,
    type: string,
    opts: { button?: number; clientX?: number; clientY?: number } = {},
  ) {
    const ev = new Event(type, { bubbles: true, cancelable: true })
    Object.assign(ev, opts)
    el.dispatchEvent(ev)
  }

  it('渲染 44px 圆形悬浮球，携带 aria-label 与 data-ai-orb 标记', () => {
    const wrapper = mountOrb()
    const orb = wrapper.find('[data-ai-orb]')
    expect(orb.exists()).toBe(true)
    expect(orb.classes()).toContain('ai-orb')
    expect(orb.attributes('aria-label')).toContain('Ctrl+J')
  })

  it('点击触发 toggle', async () => {
    const wrapper = mountOrb()
    await wrapper.find('[data-ai-orb]').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
  })

  it('AI 回答中切换为 answering 状态（呼吸/流光动画挂载点）', async () => {
    const wrapper = mountOrb()
    expect(wrapper.find('[data-ai-orb]').classes()).not.toContain('answering')

    answerFlag.value = true
    await nextTick()
    expect(wrapper.find('[data-ai-orb]').classes()).toContain('answering')

    answerFlag.value = false
    await nextTick()
    expect(wrapper.find('[data-ai-orb]').classes()).not.toContain('answering')
  })

  it('拖动超过阈值：位置更新并持久化，且不触发 toggle', async () => {
    const wrapper = mountOrb()
    const orb = wrapper.find('[data-ai-orb]')
    // jsdom 没有 setPointerCapture 实现
    ;(orb.element as HTMLElement).setPointerCapture = vi.fn()

    fire(orb.element, 'pointerdown', { button: 0, clientX: 100, clientY: 100 })
    fire(orb.element, 'pointermove', { clientX: 160, clientY: 140 })
    fire(orb.element, 'pointerup')
    await nextTick()

    expect(orb.classes()).not.toContain('dragging')
    // jsdom 的 getBoundingClientRect 全 0：拖拽原点即 (0,0)，位移即新位置
    expect(JSON.parse(localStorage.getItem('notevault.aiOrbPos')!)).toEqual({ x: 60, y: 40 })
    // 拖动后的 click 被抑制
    await orb.trigger('click')
    expect(wrapper.emitted('toggle')).toBeUndefined()
  })

  it('微动（阈值内）视为点击，toggle 照常触发且不落盘位置', async () => {
    const wrapper = mountOrb()
    const orb = wrapper.find('[data-ai-orb]')
    ;(orb.element as HTMLElement).setPointerCapture = vi.fn()

    fire(orb.element, 'pointerdown', { button: 0, clientX: 100, clientY: 100 })
    fire(orb.element, 'pointermove', { clientX: 101, clientY: 100 })
    fire(orb.element, 'pointerup')
    await orb.trigger('click')

    expect(wrapper.emitted('toggle')).toHaveLength(1)
    expect(localStorage.getItem('notevault.aiOrbPos')).toBeNull()
  })

  it('重启恢复上次拖动位置（localStorage 回放并夹紧到视口内）', async () => {
    localStorage.setItem('notevault.aiOrbPos', JSON.stringify({ x: 99999, y: 50 }))
    const wrapper = mountOrb()
    await nextTick()
    const style = wrapper.find('[data-ai-orb]').attributes('style') ?? ''
    expect(style).toContain('left:')
    expect(style).not.toContain('left: 99999px')
    // jsdom 视口 1024 宽：夹紧后 left ≤ 1024-44-8
    const left = Number(/left:\s*(\d+(?:\.\d+)?)px/.exec(style)?.[1])
    expect(left).toBeLessThanOrEqual(1024 - 44 - 8)
  })
})
