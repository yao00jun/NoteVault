// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  LLMConfigService: {
    Presets: vi.fn(),
    Probe: vi.fn(),
  },
  CredentialService: {
    SaveCredential: vi.fn(async () => undefined),
    GetCredential: vi.fn(async () => ''),
    DeleteCredential: vi.fn(async () => undefined),
  },
}))

import SettingsView from './SettingsView.vue'
import { useSettingsStore } from '@/stores/settings'
import { LLMConfigService } from '@bindings/github.com/notevault/notevault/index.js'

const mockedPresets = vi.mocked(LLMConfigService.Presets)
const mockedProbe = vi.mocked(LLMConfigService.Probe)

enableAutoUnmount(afterEach)

function mountSettings() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/settings', component: { template: '<div />' } },
    ],
  })
  const settingsStore = useSettingsStore()
  const wrapper = mount(SettingsView, {
    global: { plugins: [pinia, router, i18n] },
  })
  return { wrapper, settingsStore }
}

describe('SettingsView · P1-6 LLM 端点配置', () => {
  beforeEach(() => {
    localStorage.clear()
    ;(i18n.global.locale as any).value = 'zh-CN'
    mockedPresets.mockReset()
    mockedProbe.mockReset()
    mockedPresets.mockResolvedValue([
      {
        id: 'ollama',
        label: 'Ollama（本机）',
        baseURL: 'http://localhost:11434/v1',
        model: 'qwen2.5:7b',
        requiresKey: false,
        hint: '需先运行 ollama serve',
      },
      {
        id: 'openai',
        label: 'OpenAI',
        baseURL: 'https://api.openai.com/v1',
        model: 'gpt-4o-mini',
        requiresKey: true,
        hint: '',
      },
    ] as any)
  })

  it('渲染后端返回的端点预设', async () => {
    const { wrapper } = mountSettings()
    await flushPromises()

    // 选择器必须限定在 AI 区块内：Embedding / Rerank 区块各有一组同名 .preset-btn
    const btns = wrapper.findAll('#settings-section-ai .preset-btn')
    expect(btns).toHaveLength(2)
    expect(btns[0].text()).toContain('Ollama')
    // 免鉴权的预设要显式打标，否则用户不知道 Key 能留空
    expect(btns[0].text()).toContain('免 Key')
    expect(btns[1].text()).not.toContain('免 Key')
  })

  it('点选本机预设会填入地址与模型，并清掉旧的云端 Key', async () => {
    const { wrapper, settingsStore } = mountSettings()
    await flushPromises()

    settingsStore.settings.ai.apiKey = 'sk-cloud-leftover'
    await wrapper.findAll('#settings-section-ai .preset-btn')[0].trigger('click')

    expect(settingsStore.settings.ai.baseURL).toBe('http://localhost:11434/v1')
    expect(settingsStore.settings.ai.model).toBe('qwen2.5:7b')
    // 切到本机端点必须清 Key：否则会把云端凭据发给本机服务
    expect(settingsStore.settings.ai.apiKey).toBe('')
  })

  it('点选云端预设时保留已填的 Key', async () => {
    const { wrapper, settingsStore } = mountSettings()
    await flushPromises()

    settingsStore.settings.ai.apiKey = 'sk-keep-me'
    await wrapper.findAll('#settings-section-ai .preset-btn')[1].trigger('click')

    expect(settingsStore.settings.ai.baseURL).toBe('https://api.openai.com/v1')
    expect(settingsStore.settings.ai.apiKey).toBe('sk-keep-me')
  })

  it('自检成功后展示模型列表，点击可切换当前模型', async () => {
    mockedProbe.mockResolvedValue({
      ok: true,
      endpoint: 'http://localhost:11434/v1/models',
      isLocal: true,
      models: ['llama3.2', 'qwen2.5:7b'],
      latencyMs: 12,
      message: '',
    } as any)

    const { wrapper, settingsStore } = mountSettings()
    await flushPromises()

    await wrapper.find('[data-testid="probe-btn"]').trigger('click')
    await flushPromises()

    const box = wrapper.find('[data-testid="probe-result"]')
    expect(box.exists()).toBe(true)
    expect(box.classes()).toContain('ok')
    expect(box.text()).toContain('本机端点')
    expect(box.text()).toContain('12 ms')

    const chips = wrapper.findAll('.model-chip')
    expect(chips).toHaveLength(2)
    await chips[0].trigger('click')
    expect(settingsStore.settings.ai.model).toBe('llama3.2')
  })

  it('自检失败时标红并显示后端给出的排查指引', async () => {
    mockedProbe.mockResolvedValue({
      ok: false,
      endpoint: 'http://localhost:11434/v1/models',
      isLocal: true,
      models: [],
      latencyMs: 3,
      message: '连接不上本机服务：dial tcp: connection refused\n请确认服务已启动（Ollama：ollama serve）',
    } as any)

    const { wrapper } = mountSettings()
    await flushPromises()

    await wrapper.find('[data-testid="probe-btn"]').trigger('click')
    await flushPromises()

    const box = wrapper.find('[data-testid="probe-result"]')
    expect(box.classes()).toContain('fail')
    // 指引是多行文本，必须完整透出而不是只显示第一行
    expect(box.text()).toContain('ollama serve')
    expect(wrapper.findAll('.model-chip')).toHaveLength(0)
  })

  it('绑定调用抛异常时降级为失败提示，不让页面崩掉', async () => {
    mockedProbe.mockRejectedValue(new Error('bridge down'))

    const { wrapper } = mountSettings()
    await flushPromises()

    await wrapper.find('[data-testid="probe-btn"]').trigger('click')
    await flushPromises()

    const box = wrapper.find('[data-testid="probe-result"]')
    expect(box.classes()).toContain('fail')
    expect(box.text()).toContain('bridge down')
  })

  it('预设拉取失败时静默降级，仍可手填地址', async () => {
    mockedPresets.mockRejectedValue(new Error('offline'))

    const { wrapper } = mountSettings()
    await flushPromises()

    expect(wrapper.findAll('#settings-section-ai .preset-btn')).toHaveLength(0)
    // 手填输入框必须还在，否则拉取失败就等于功能不可用
    expect(wrapper.find('.setting-input').exists()).toBe(true)
  })
})
