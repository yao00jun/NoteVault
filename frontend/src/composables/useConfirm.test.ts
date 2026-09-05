// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { confirmDialog, resetConfirm, useConfirm } from './useConfirm'
import ConfirmDialog from '@/components/layout/ConfirmDialog.vue'
import { i18n } from '@/i18n'

enableAutoUnmount(afterEach)

function mountDialog() {
  return mount(ConfirmDialog, { global: { plugins: [i18n] } })
}

describe('useConfirm', () => {
  beforeEach(() => {
    resetConfirm()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('confirmDialog 弹出后渲染标题/正文/默认按钮文案，accept 结算为 true', async () => {
    const pending = confirmDialog({ title: '删除', message: '确定删除吗？' })
    const wrapper = mountDialog()
    await nextTick()

    expect(wrapper.find('[data-testid="confirm-mask"]').exists()).toBe(true)
    expect(wrapper.find('.confirm-title').text()).toBe('删除')
    expect(wrapper.find('.confirm-text').text()).toBe('确定删除吗？')
    expect(wrapper.find('.confirm-btn.cancel').text()).toBe('取消')
    expect(wrapper.find('.confirm-btn.ok').text()).toBe('确定')

    await wrapper.find('[data-testid="confirm-ok"]').trigger('click')
    await expect(pending).resolves.toBe(true)
  })

  it('cancel / 遮罩点击 / Esc 都结算为 false，弹框关闭', async () => {
    const pending = confirmDialog({ message: 'msg' })
    const wrapper = mountDialog()
    await nextTick()

    await wrapper.find('.confirm-btn.cancel').trigger('click')
    await expect(pending).resolves.toBe(false)

    const pending2 = confirmDialog({ message: 'msg2' })
    await nextTick()
    await wrapper.find('[data-testid="confirm-mask"]').trigger('click')
    await expect(pending2).resolves.toBe(false)

    const pending3 = confirmDialog({ message: 'msg3' })
    await nextTick()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await expect(pending3).resolves.toBe(false)
  })

  it('danger 时确认按钮带 danger 类；自定义按钮文案优先生效', async () => {
    const pending = confirmDialog({
      message: '清空回收站？',
      confirmText: '清空',
      cancelText: '留着',
      danger: true,
    })
    const wrapper = mountDialog()
    await nextTick()

    const ok = wrapper.find('[data-testid="confirm-ok"]')
    expect(ok.classes()).toContain('danger')
    expect(ok.text()).toBe('清空')
    expect(wrapper.find('.confirm-btn.cancel').text()).toBe('留着')

    await ok.trigger('click')
    await expect(pending).resolves.toBe(true)
  })

  it('重复弹出时，旧的等待 Promise 按取消结算，不悬挂', async () => {
    const first = confirmDialog({ message: '第一个' })
    const second = confirmDialog({ message: '第二个' })

    await expect(first).resolves.toBe(false)

    const { state, accept } = useConfirm()
    expect(state.value.message).toBe('第二个')
    accept()
    await expect(second).resolves.toBe(true)
  })
})
