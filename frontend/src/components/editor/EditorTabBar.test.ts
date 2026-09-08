// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import EditorTabBar from './EditorTabBar.vue'

enableAutoUnmount(afterEach)

function mountBar(path: string) {
  const tab = { path, name: '复盘.md', content: 'Project experience', isDirty: true, lastSavedAt: '' }
  return mount(EditorTabBar, {
    props: { tabs: [tab], activeTab: tab, activeTabIndex: 0, isSaving: false, isExporting: false, isCompiling: false, viewMode: 'split' },
    global: { plugins: [i18n] },
  })
}

describe('project distillation entry', () => {
  it('shows a labelled action for a project document and sends the intent to its editor', async () => {
    const wrapper = mountBar('Projects/A/复盘.md')
    expect(wrapper.get('[data-testid="editor-distill"]').text()).toContain('沉淀为知识')
    await wrapper.get('[data-testid="editor-distill"]').trigger('click')
    expect(wrapper.emitted('distill')).toHaveLength(1)
  })

  it.each(['Learning/Go/Note.md', 'Daily/Reports/2026-09-09-日报.md', 'Projects/A/map.canvas'])('does not offer project distillation for %s', (path) => {
    expect(mountBar(path).find('[data-testid="editor-distill"]').exists()).toBe(false)
  })

  it('disables repeat entry while the source is being prepared or saved', async () => {
    const wrapper = mountBar('Projects/A/复盘.md')
    await wrapper.setProps({ isDistilling: true })
    expect(wrapper.get('[data-testid="editor-distill"]').attributes('disabled')).toBeDefined()
  })
})
