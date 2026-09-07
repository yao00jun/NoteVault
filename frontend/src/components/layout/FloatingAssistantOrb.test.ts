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
})
