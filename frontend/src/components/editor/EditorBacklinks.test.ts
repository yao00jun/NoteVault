// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { i18n } from '@/i18n'
import EditorBacklinks from './EditorBacklinks.vue'

enableAutoUnmount(afterEach)
beforeEach(() => localStorage.clear())
afterEach(() => vi.restoreAllMocks())

describe('EditorBacklinks', () => {
  it('remembers collapse across remounts while keeping the count and reopen control available', async () => {
    const props = { backlinks: [{ name: '来源', path: 'Projects/来源.md' }] }
    const wrapper = mount(EditorBacklinks, { props, global: { plugins: [i18n] } })
    expect(wrapper.get('.backlinks-header').attributes('aria-expanded')).toBe('true')
    await wrapper.get('.backlinks-header').trigger('click')
    expect(wrapper.get('.backlinks-list').isVisible()).toBe(false)
    wrapper.unmount()

    const restored = mount(EditorBacklinks, { props, global: { plugins: [i18n] } })
    expect(restored.get('.backlinks-header').attributes('aria-expanded')).toBe('false')
    expect(restored.get('.backlinks-header').text()).toContain('1')
    await restored.get('.backlinks-header').trigger('click')
    expect(restored.get('.backlinks-list').isVisible()).toBe(true)
  })

  it('keeps the collapse control usable when browser storage is unavailable', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('Storage blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('Storage full') })
    const wrapper = mount(EditorBacklinks, {
      props: { backlinks: [{ name: '来源', path: 'Projects/来源.md' }] }, global: { plugins: [i18n] },
    })
    await wrapper.get('.backlinks-header').trigger('click')
    expect(wrapper.get('.backlinks-header').attributes('aria-expanded')).toBe('false')
  })

  it('distinguishes documents with the same title by path and opens the selected document', async () => {
    const backlinks = [
      { name: '资料来源', path: 'Projects/Go/资料来源.md' },
      { name: '资料来源', path: 'Learning/Go/资料来源.md' },
    ]
    const wrapper = mount(EditorBacklinks, { props: { backlinks }, global: { plugins: [i18n] } })

    const rows = wrapper.findAll('.backlink-item')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('Projects/Go/资料来源.md')
    expect(rows[1]!.text()).toContain('Learning/Go/资料来源.md')
    expect(rows[1]!.element.tagName).toBe('BUTTON')
    await rows[1]!.trigger('click')
    expect(wrapper.emitted('open')).toEqual([[backlinks[1]]])
  })
})
