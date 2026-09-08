// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { undo } from '@codemirror/commands'
import { getActiveEditor } from '@/plugins/editorBridge'
import { useSettingsStore } from '@/stores/settings'
import { i18n } from '@/i18n'
import MarkdownEditor from './MarkdownEditor.vue'

vi.mock('@/api', () => ({
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))
vi.mock('@/stores/pluginRuntime', () => ({ usePluginRuntimeStore: () => ({
  editorDecorations: [], editorWidgets: [], editorKeymaps: [],
  registerEditorUiHandlers: vi.fn(), runCommand: vi.fn(),
}) }))
enableAutoUnmount(afterEach)
beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()) })

function mountEditor() {
  return mount(MarkdownEditor, {
    props: { modelValue: 'First document', documentId: 'workspace/A.md' },
    global: { plugins: [i18n], stubs: { EditorToolbar: true } },
  })
}

describe('document editor sessions', () => {
  it('keeps undo and cursor per document and cannot undo into another file', async () => {
    const wrapper = mountEditor()
    const first = getActiveEditor()!
    first.dispatch({ changes: { from: first.state.doc.length, insert: ' edited' }, selection: { anchor: 4 } })
    const draft = first.state.doc.toString()
    await wrapper.setProps({ modelValue: draft })
    await wrapper.setProps({ documentId: 'workspace/B.md', modelValue: 'Second document' })
    expect(undo(getActiveEditor()!)).toBe(false)
    expect(getActiveEditor()!.state.doc.toString()).toBe('Second document')

    await wrapper.setProps({ documentId: 'workspace/A.md', modelValue: draft })
    expect(getActiveEditor()!.state.selection.main.head).toBe(4)
    expect(undo(getActiveEditor()!)).toBe(true)
    expect(getActiveEditor()!.state.doc.toString()).toBe('First document')
  })

  it('applying disk content does not emit a new user edit or dirty the loaded document', async () => {
    const wrapper = mountEditor()
    await wrapper.setProps({ modelValue: 'Changed on disk' })
    expect(getActiveEditor()!.state.doc.toString()).toBe('Changed on disk')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('theme changes preserve typing history and selection', async () => {
    mountEditor()
    const first = getActiveEditor()!
    first.dispatch({ changes: { from: 0, insert: 'New ' }, selection: { anchor: 4 } })
    useSettingsStore().settings.theme = 'macos'
    useSettingsStore().settings.fontSize = 17
    await nextTick()
    expect(getActiveEditor()!.state.selection.main.head).toBe(4)
    expect(undo(getActiveEditor()!)).toBe(true)
    expect(getActiveEditor()!.state.doc.toString()).toBe('First document')
  })
})
