// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { useToast, resetToasts } from './useToast'
import ToastHost from '@/components/layout/ToastHost.vue'

enableAutoUnmount(afterEach)

describe('useToast', () => {
  beforeEach(() => {
    resetToasts()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    resetToasts()
  })

  it('四种类型各自入栈，kind 与文案正确', () => {
    const toast = useToast()
    toast.success('保存成功')
    toast.error('保存失败')
    toast.warning('注意')
    toast.info('提示')

    const list = toast.toasts.value
    expect(list.map((t) => t.kind)).toEqual(['success', 'error', 'warning', 'info'])
    expect(list.map((t) => t.message)).toEqual(['保存成功', '保存失败', '注意', '提示'])
  })

  it('达到持续时间后自动消失', () => {
    const toast = useToast()
    toast.success('一秒后消失', 1000)
    expect(toast.toasts.value).toHaveLength(1)

    vi.advanceTimersByTime(999)
    expect(toast.toasts.value).toHaveLength(1)

    vi.advanceTimersByTime(1)
    expect(toast.toasts.value).toHaveLength(0)
  })

  it('dismiss 手动关闭，且清掉定时器', () => {
    const toast = useToast()
    const id = toast.info('手动关', 5000)
    toast.dismiss(id)
    expect(toast.toasts.value).toHaveLength(0)
    // 定时器已清：推进时间不应再报错也不应改变列表
    vi.advanceTimersByTime(10000)
    expect(toast.toasts.value).toHaveLength(0)
  })

  it('duration=0 常驻，不会自动消失', () => {
    const toast = useToast()
    toast.persistent('常驻提示')
    vi.advanceTimersByTime(60000)
    expect(toast.toasts.value).toHaveLength(1)

    toast.dismiss(toast.toasts.value[0].id)
    expect(toast.toasts.value).toHaveLength(0)
  })

  it('超过 5 条时挤掉最老的（防止批量操作刷屏）', () => {
    const toast = useToast()
    for (let i = 1; i <= 7; i++) {
      toast.info(`msg-${i}`, 0)
    }
    const list = toast.toasts.value
    expect(list).toHaveLength(5)
    expect(list[0].message).toBe('msg-3')
    expect(list[4].message).toBe('msg-7')
  })

  it('id 单调递增，不复用', () => {
    const toast = useToast()
    const a = toast.info('a', 0)
    const b = toast.info('b', 0)
    expect(b).toBeGreaterThan(a)
  })

  it('不同组件调用共享同一份列表（模块级单例）', () => {
    const a = useToast()
    const b = useToast()
    a.success('来自 A', 0)
    expect(b.toasts.value.map((t) => t.message)).toEqual(['来自 A'])
  })

  it('resetToasts 清空列表且 id 归位', () => {
    const toast = useToast()
    toast.info('x', 0)
    toast.info('y', 0)
    resetToasts()
    expect(toast.toasts.value).toHaveLength(0)
    expect(toast.info('z', 0)).toBe(1)
  })
})

describe('ToastHost', () => {
  beforeEach(() => {
    resetToasts()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    resetToasts()
  })

  it('渲染出每种类型的 toast 并带对应 class', async () => {
    const toast = useToast()
    const wrapper = mount(ToastHost)

    toast.success('ok')
    toast.error('bad')
    toast.warning('careful')
    toast.info('fyi')
    await nextTick()

    expect(wrapper.findAll('.toast')).toHaveLength(4)
    expect(wrapper.find('[data-testid="toast-success"]').text()).toContain('ok')
    expect(wrapper.find('[data-testid="toast-error"]').text()).toContain('bad')
    expect(wrapper.find('[data-testid="toast-warning"]').text()).toContain('careful')
    expect(wrapper.find('[data-testid="toast-info"]').text()).toContain('fyi')
    expect(wrapper.find('.toast.error').classes()).toContain('error')
  })

  it('点关闭按钮移除该条', async () => {
    const toast = useToast()
    const wrapper = mount(ToastHost)

    toast.error('关闭我', 0)
    await nextTick()
    expect(wrapper.findAll('.toast')).toHaveLength(1)

    await wrapper.find('.toast-close').trigger('click')
    expect(wrapper.findAll('.toast')).toHaveLength(0)
    expect(toast.toasts.value).toHaveLength(0)
  })

  it('空栈时容器仍在但不渲染任何 toast', () => {
    const wrapper = mount(ToastHost)
    expect(wrapper.find('[data-testid="toast-stack"]').exists()).toBe(true)
    expect(wrapper.findAll('.toast')).toHaveLength(0)
  })
})
