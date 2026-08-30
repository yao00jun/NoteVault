import { describe, it, expect, beforeAll, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useSettingsStore } from './settings'

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
  ;(globalThis as any).document = {
    documentElement: {
      _attrs: {} as Record<string, string>,
      setAttribute(key: string, value: string) {
        this._attrs[key] = value
      },
      getAttribute(key: string) {
        return this._attrs[key] ?? null
      },
    },
  }
})

beforeEach(() => {
  setActivePinia(createPinia())
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
})
