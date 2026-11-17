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
import EditorToolbar from './EditorToolbar.vue'

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
  it('forwards the project distillation action from its Markdown toolbar without editing the note', async () => {
    const wrapper = mountEditor()
    useSettingsStore().settings.toolbar.mode = 'top'
    await wrapper.setProps({ canDistill: true, isDistilling: false })
    const toolbar = wrapper.findComponent(EditorToolbar)
    expect(toolbar.props('canDistill')).toBe(true)
    toolbar.vm.$emit('distill')
    expect(wrapper.emitted('distill')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ canDistill: false })
    expect(toolbar.props('canDistill')).toBe(false)
  })

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

describe('image paste and drop', () => {
  const shot = () => new File([new Uint8Array([1, 2, 3])], 'shot.png', { type: 'image/png' })

  function pasteEvent(items: Array<{ type: string; getAsFile: () => File | null }>) {
    const event = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', { value: { items } })
    return event
  }

  function dropEvent(values: { files?: File[]; items?: Array<{ type: string }> }) {
    const event = new Event('drop', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'dataTransfer', { value: values })
    return event
  }

  it('intercepts an image on the clipboard and emits the file', async () => {
    const wrapper = mountEditor()
    const file = shot()
    const event = pasteEvent([{ type: 'image/png', getAsFile: () => file }])

    wrapper.get('.editor-cm').element.dispatchEvent(event)
    await nextTick()

    expect(event.defaultPrevented).toBe(true)
    const payload = wrapper.emitted('paste-image')?.[0]?.[0] as
      | { file: File; insertText: (text: string) => void }
      | undefined
    expect(payload?.file).toBe(file)

    payload!.insertText('![](attachments/shot.png)')
    expect(getActiveEditor()!.state.doc.toString()).toContain('![](attachments/shot.png)')
  })

  it('leaves plain-text paste untouched', async () => {
    const wrapper = mountEditor()
    const event = pasteEvent([{ type: 'text/plain', getAsFile: () => null }])

    wrapper.get('.editor-cm').element.dispatchEvent(event)
    await nextTick()

    expect(event.defaultPrevented).toBe(false)
    expect(wrapper.emitted('paste-image')).toBeUndefined()
  })

  it('accepts a dropped image file and ignores other drops', async () => {
    const wrapper = mountEditor()
    const file = shot()
    const root = wrapper.get('.editor-cm').element

    const plain = dropEvent({ files: [new File(['x'], 'a.txt', { type: 'text/plain' })] })
    root.dispatchEvent(plain)
    await nextTick()
    expect(wrapper.emitted('paste-image')).toBeUndefined()

    const event = dropEvent({ files: [file] })
    root.dispatchEvent(event)
    await nextTick()
    expect(event.defaultPrevented).toBe(true)
    const payload = wrapper.emitted('paste-image')?.[0]?.[0] as { file: File } | undefined
    expect(payload?.file).toBe(file)
  })

  it('only blocks dragover when an image is being dragged', async () => {
    const wrapper = mountEditor()
    const root = wrapper.get('.editor-cm').element

    const textDrag = new Event('dragover', { bubbles: true, cancelable: true })
    Object.defineProperty(textDrag, 'dataTransfer', { value: { items: [{ type: 'text/plain' }] } })
    root.dispatchEvent(textDrag)
    expect(textDrag.defaultPrevented).toBe(false)

    const imageDrag = new Event('dragover', { bubbles: true, cancelable: true })
    Object.defineProperty(imageDrag, 'dataTransfer', { value: { items: [{ type: 'image/png' }] } })
    root.dispatchEvent(imageDrag)
    expect(imageDrag.defaultPrevented).toBe(true)
  })
})
