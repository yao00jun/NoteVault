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
    vi.stubGlobal('confirm', vi.fn(() => true))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('确认退出后直接走后端 ForceQuit（绕开会死锁的 Wails Quit）', async () => {
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
    const wrapper = mount(TitleBar, {
      global: { plugins: [pinia, router, i18n] },
    })

    await wrapper.find('.win-btn.close').trigger('click')
    await flushPromises()

    expect(mockedForceQuit).toHaveBeenCalledOnce()
    // 不应触碰会三方死锁的 app.Quit()，也不应只关窗口
    expect(mockedQuit).not.toHaveBeenCalled()
    expect(mockedWindowClose).not.toHaveBeenCalled()
  })

  it('取消确认时不退出', async () => {
    vi.stubGlobal('confirm', vi.fn(() => false))
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    const wrapper = mount(TitleBar, {
      global: { plugins: [pinia, router, i18n] },
    })

    await wrapper.find('.win-btn.close').trigger('click')
    await flushPromises()

    expect(mockedForceQuit).not.toHaveBeenCalled()
    expect(mockedQuit).not.toHaveBeenCalled()
  })
})
