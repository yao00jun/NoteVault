// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@wailsio/runtime', () => {
  // bindings/models.js 会调用 $Create.Map / $Create.Nullable / $Create.Pointer / $Create.Any ...
  // 用 Proxy 让任意 Create.X 都返回可调用占位，避免加载 bindings 时抛 TypeError。
  const createProxy = new Proxy({}, {
    get: () => vi.fn(() => vi.fn()),
  })
  return {
    Application: { Quit: vi.fn() },
    Create: createProxy,
    Dialogs: {},
    Window: { Close: vi.fn() },
  }
})

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  AppService: { ForceQuit: vi.fn(() => Promise.resolve()) },
}))

import TitleBar from './TitleBar.vue'
import { Application, Window } from '@wailsio/runtime'
import { AppService } from '@bindings/github.com/notevault/notevault/index.js'

const mockedQuit = vi.mocked(Application.Quit)
const mockedWindowClose = vi.mocked(Window.Close)
const mockedForceQuit = vi.mocked(AppService.ForceQuit)

describe('TitleBar', () => {
  beforeEach(() => {
    localStorage.clear()
    ;(i18n.global.locale as any).value = 'zh-CN'
    mockedQuit.mockReset()
    mockedWindowClose.mockReset()
    mockedForceQuit.mockReset()
  })

  // 挂载 TitleBar。退出确认已改成自定义弹框（不再用 window.confirm——
  // WebView2 会把它转成 native dialog 弹到窗口右上角），因此 mount 后
  // 需要「点关闭 → 点确认」两步才会真正触发退出。
  function mountTitleBar() {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/search', component: { template: '<div />' } },
        { path: '/settings', component: { template: '<div />' } },
      ],
    })
    return mount(TitleBar, {
      global: { plugins: [pinia, router, i18n] },
    })
  }

  it('点关闭只弹确认框，此时不能退出', async () => {
    const wrapper = mountTitleBar()

    await wrapper.find('.win-btn.close').trigger('click')
    await flushPromises()

    expect(wrapper.find('.exit-confirm').exists()).toBe(true)
    expect(mockedForceQuit).not.toHaveBeenCalled()
    expect(mockedQuit).not.toHaveBeenCalled()
  })

  it('确认退出后走后端 ForceQuit（绕开会死锁的 Wails Quit）', async () => {
    const wrapper = mountTitleBar()

    await wrapper.find('.win-btn.close').trigger('click')
    await flushPromises()
    await wrapper.find('.exit-btn.confirm').trigger('click')
    await flushPromises()

    expect(mockedForceQuit).toHaveBeenCalledOnce()
    // 不应触碰会三方死锁的 app.Quit()，也不应只关窗口
    expect(mockedQuit).not.toHaveBeenCalled()
    expect(mockedWindowClose).not.toHaveBeenCalled()
  })

  it('点取消按钮不退出', async () => {
    const wrapper = mountTitleBar()

    await wrapper.find('.win-btn.close').trigger('click')
    await flushPromises()
    await wrapper.find('.exit-btn.cancel').trigger('click')
    await flushPromises()

    expect(mockedForceQuit).not.toHaveBeenCalled()
    expect(mockedQuit).not.toHaveBeenCalled()
    // 确认框应当收起
    expect(wrapper.find('.exit-confirm').exists()).toBe(false)
  })

  it('点遮罩空白处不退出', async () => {
    const wrapper = mountTitleBar()

    await wrapper.find('.win-btn.close').trigger('click')
    await flushPromises()
    await wrapper.find('.exit-confirm-mask').trigger('click')
    await flushPromises()

    expect(mockedForceQuit).not.toHaveBeenCalled()
    expect(wrapper.find('.exit-confirm').exists()).toBe(false)
  })
})
