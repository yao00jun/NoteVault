// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { promptDialog, resetPrompt, usePrompt } from './usePrompt'
import PromptDialog from '@/components/layout/PromptDialog.vue'
import { i18n } from '@/i18n'

enableAutoUnmount(afterEach)

function mountDialog() {
  return mount(PromptDialog, { global: { plugins: [i18n] } })
}

describe('usePrompt', () => {
  beforeEach(() => {
    resetPrompt()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('弹出后渲染标签/预填值/默认按钮文案，submit 返回输入值', async () => {
    const pending = promptDialog({ message: '请输入文件名：', defaultValue: '未命名.md' })
    const wrapper = mountDialog()
    await nextTick()
    await nextTick()

    expect(wrapper.find('[data-testid="prompt-mask"]').exists()).toBe(true)
    expect(wrapper.find('.prompt-text').text()).toBe('请输入文件名：')
    expect((wrapper.find('[data-testid="prompt-input"]').element as HTMLInputElement).value).toBe('未命名.md')
    expect(wrapper.find('.prompt-btn.cancel').text()).toBe('取消')
    expect(wrapper.find('.prompt-btn.ok').text()).toBe('确定')

    await wrapper.find('[data-testid="prompt-input"]').setValue('hello.md')
    await wrapper.find('[data-testid="prompt-ok"]').trigger('click')
    await expect(pending).resolves.toBe('hello.md')
  })

  it('取消 / 遮罩点击 / Esc 都返回 null', async () => {
    const first = promptDialog({ message: 'a' })
    const wrapper = mountDialog()
    await nextTick()
    await nextTick()

    await wrapper.find('.prompt-btn.cancel').trigger('click')
    await expect(first).resolves.toBe(null)

    const second = promptDialog({ message: 'b' })
    await nextTick()
    await nextTick()
    await wrapper.find('[data-testid="prompt-mask"]').trigger('click')
    await expect(second).resolves.toBe(null)

    const third = promptDialog({ message: 'c' })
    await nextTick()
    await nextTick()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await expect(third).resolves.toBe(null)
  })

  it('Enter 键提交当前输入值', async () => {
    const pending = promptDialog({ message: 'a', defaultValue: 'draft.md' })
    const wrapper = mountDialog()
    await nextTick()
    await nextTick()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await expect(pending).resolves.toBe('draft.md')
  })

  it('重复弹出时，旧的等待 Promise 按取消结算，不悬挂', async () => {
    const first = promptDialog({ message: '第一个' })
    const second = promptDialog({ message: '第二个', defaultValue: 'x' })

    await expect(first).resolves.toBe(null)

    const { state, submit } = usePrompt()
    expect(state.value.message).toBe('第二个')
    submit()
    await expect(second).resolves.toBe('x')
  })
})
