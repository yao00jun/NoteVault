import { describe, it, expect, beforeAll, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { useSettingsStore } from './settings'

// P2-5：settings store 会把 apiKey 同步进系统凭据库（经 Wails 服务），
// Node 环境下没有事件总线，mock 掉以便断言调用。
const saveCredential = vi.fn(async (..._args: unknown[]) => undefined)
const getCredential = vi.fn(async (..._args: unknown[]) => '')

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  CredentialService: {
    SaveCredential: (...args: unknown[]) => saveCredential(...args),
    GetCredential: (...args: unknown[]) => getCredential(...args),
    DeleteCredential: vi.fn(async () => undefined),
  },
}))

// 在 Node 环境下模拟 localStorage
class MemoryStorage {
  private store = new Map<string, string>()
  getItem(key: string) {
    return this.store.has(key) ? this.store.get(key)! : null
  }
  setItem(key: string, value: string) {
    this.store.set(key, value)
  }
  removeItem(key: string) {
    this.store.delete(key)
  }
  clear() {
    this.store.clear()
  }
}

beforeAll(() => {
  ;(globalThis as any).localStorage = new MemoryStorage()
  // Node 环境下没有 document，提供最小桩以便 setTheme 写入 data-theme
  const docEl: any = {
    _attrs: {} as Record<string, string>,
    _style: {} as Record<string, string>,
    setAttribute(key: string, value: string) {
      this._attrs[key] = value
    },
    getAttribute(key: string) {
      return this._attrs[key] ?? null
    },
  }
  docEl.style = {
    setProperty: (key: string, value: string) => {
      docEl._style[key] = value
    },
    removeProperty: (key: string) => {
      delete docEl._style[key]
    },
  }
  ;(globalThis as any).document = { documentElement: docEl }
})

beforeEach(() => {
  setActivePinia(createPinia())
  saveCredential.mockClear()
  getCredential.mockClear()
  getCredential.mockImplementation(async () => '')
  localStorage.clear()
})

describe('useSettingsStore', () => {
  it('默认值应包含 AI 与编辑器配置', () => {
    const store = useSettingsStore()
    expect(store.settings.ai).toBeDefined()
    expect(store.settings.ai.baseURL).toContain('v1')
    expect(store.settings.ai.model).toBeTruthy()
    expect(store.settings.editor).toBeDefined()
    expect(store.settings.editor.lineHeight).toBeGreaterThan(1)
    expect(store.settings.editor.previewFontSize).toBeGreaterThan(0)
  })

  it('setTheme 应更新主题并写入 document', () => {
    const store = useSettingsStore()
    store.setTheme('macos')
    expect(store.settings.theme).toBe('macos')
    expect(document.documentElement.getAttribute('data-theme')).toBe('macos')
  })

  it('toggleSidebar 应切换折叠状态', () => {
    const store = useSettingsStore()
    const before = store.settings.sidebarCollapsed
    store.toggleSidebar()
    expect(store.settings.sidebarCollapsed).toBe(!before)
  })

  it('setEditorMode 应更新编辑模式', () => {
    const store = useSettingsStore()
    store.setEditorMode('preview')
    expect(store.settings.editorMode).toBe('preview')
  })

  it('字体预设应内联覆盖 CSS 变量，theme 应移除覆盖', async () => {
    const store = useSettingsStore()
    const root: any = document.documentElement
    // 默认值是跟随主题：不产生内联覆盖
    expect(store.settings.uiFont).toBe('theme')
    expect(store.settings.monoFont).toBe('theme')
    store.settings.uiFont = 'inter'
    store.settings.monoFont = 'cascadia'
    await nextTick()
    expect(root._style['--font-sans']).toContain('Inter')
    expect(root._style['--font-mono']).toContain('Cascadia Code')
    store.settings.uiFont = 'theme'
    store.settings.monoFont = 'theme'
    await nextTick()
    expect(root._style['--font-sans']).toBeUndefined()
    expect(root._style['--font-mono']).toBeUndefined()
  })

  it('修改 AI 设置应在内存中生效并触发持久化', async () => {
    const store = useSettingsStore()
    store.settings.ai.model = 'deepseek-chat'
    expect(store.settings.ai.model).toBe('deepseek-chat')
    // 深度 watch 异步写入 localStorage
    await new Promise((r) => setTimeout(r, 10))
    const raw = localStorage.getItem('notevault-settings')
    expect(raw).toBeTruthy()
    expect(raw!).toContain('deepseek-chat')
  })

  it('旧版配置缺少嵌套字段时应回填默认设置', () => {
    localStorage.setItem('notevault-settings', JSON.stringify({
      theme: 'macos',
      ai: { model: 'legacy-model' },
      reminder: { doNotDisturb: { enabled: true } },
    }))

    const store = useSettingsStore()
    expect(store.settings.editor.lineHeight).toBe(1.6)
    expect(store.settings.editor.previewFontSize).toBe(14)
    expect(store.settings.ai.baseURL).toContain('v1')
    expect(store.settings.reminder.defaultTime).toBe('09:00')
    expect(store.settings.reminder.doNotDisturb.start).toBe('22:00')
  })

  it('apiKey 应同步进系统凭据库且不落 localStorage (P2-5)', async () => {
    const store = useSettingsStore()
    store.settings.ai.apiKey = 'sk-secret-value'
    await new Promise((r) => setTimeout(r, 10))

    expect(saveCredential).toHaveBeenCalledWith('ai.apiKey', 'sk-secret-value')
    const raw = localStorage.getItem('notevault-settings') ?? ''
    expect(raw).not.toContain('sk-secret-value')
  })

  it('apiKey 清空应触发凭据库删除语义（空值 → Delete）', async () => {
    const store = useSettingsStore()
    store.settings.ai.apiKey = 'sk-to-be-cleared'
    await new Promise((r) => setTimeout(r, 10))
    store.settings.ai.apiKey = ''
    await new Promise((r) => setTimeout(r, 10))

    expect(saveCredential).toHaveBeenLastCalledWith('ai.apiKey', '')
  })

  it('旧版 localStorage 里的明文 apiKey 应迁移进凭据库并清除 (P2-5)', async () => {
    localStorage.setItem('notevault-settings', JSON.stringify({
      theme: 'macos',
      ai: { apiKey: 'sk-legacy' },
    }))

    const store = useSettingsStore()
    await new Promise((r) => setTimeout(r, 10))

    expect(saveCredential).toHaveBeenCalledWith('ai.apiKey', 'sk-legacy')
    // localStorage 里不再有明文
    const raw = localStorage.getItem('notevault-settings') ?? ''
    expect(raw).not.toContain('sk-legacy')
    // 内存中也不恢复旧值（恢复走凭据库的 restoreApiKey 路径）
    expect(store.settings.ai.apiKey).toBe('')
  })
})
