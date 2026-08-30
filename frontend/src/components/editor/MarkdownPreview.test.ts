// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MarkdownPreview from './MarkdownPreview.vue'
import { useSettingsStore } from '@/stores/settings'

enableAutoUnmount(afterEach)

describe('MarkdownPreview', () => {
  it('使用编辑器设置中的预览字号和行高', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const settingsStore = useSettingsStore()
    settingsStore.settings.editor.previewFontSize = 18
    settingsStore.settings.editor.lineHeight = 2

    const wrapper = mount(MarkdownPreview, {
      props: { content: '# Preview' },
      global: { plugins: [pinia] },
    })

    expect(wrapper.find('.markdown-preview').attributes('style')).toContain('font-size: 18px')
    expect(wrapper.find('.markdown-preview').attributes('style')).toContain('line-height: 2')
  })
})
