// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import DocumentPropertiesPanel from './DocumentPropertiesPanel.vue'

enableAutoUnmount(afterEach)

function renderTags(tags: string[]) {
  return mount(DocumentPropertiesPanel, { props: { tags, visible: true }, global: { plugins: [i18n] } })
}

describe('DocumentPropertiesPanel', () => {
  it('shows only the add control for an empty tag set and restores the label while editing', async () => {
    const wrapper = renderTags([' ', '{{title}}'])
    expect(wrapper.find('.properties-label').exists()).toBe(false)
    expect(wrapper.find('.properties-icon').exists()).toBe(false)
    await wrapper.get('[data-testid="doc-tag-add"]').trigger('click')
    expect(wrapper.find('.properties-label').exists()).toBe(true)
    expect(wrapper.find('[data-testid="doc-tag-input"]').exists()).toBe(true)
  })

  it('trims and deduplicates existing tags while hiding empty values and template placeholders', () => {
    const wrapper = renderTags(['Go', ' Go ', '', '  ', '{{title}}', ' {{ topic }} ', 'Java'])
    expect(wrapper.findAll('[data-testid="doc-tag-chip"]').map(chip => chip.text())).toEqual(['Go', 'Java'])
  })

  it('removes all variants of a tag without writing placeholders back to the document', async () => {
    const wrapper = renderTags([' Go ', 'Go', '{{title}}', 'Java'])
    await wrapper.find('.chip-remove').trigger('click')
    expect(wrapper.emitted('update:tags')).toEqual([[['Java']]])
  })

  it('adds comma-separated tags to the cleaned set and preserves IME composition', async () => {
    const wrapper = renderTags([' Go ', 'Go', '{{title}}'])
    await wrapper.get('[data-testid="doc-tag-add"]').trigger('click')
    const input = wrapper.get('[data-testid="doc-tag-input"]')
    await input.setValue('Go，Rust; Rust；Java')
    await input.trigger('keydown', { key: 'Enter', isComposing: true })
    expect(wrapper.emitted('update:tags')).toBeUndefined()
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:tags')).toEqual([[['Go', 'Rust', 'Java']]])
  })
})
