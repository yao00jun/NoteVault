// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, onUnmounted, ref } from 'vue'
import { i18n } from '@/i18n'
import { FileService } from '@/api'
import { useEditorDraft, type EditorTab } from './useEditorDraft'

const events = vi.hoisted(() => ({ changed: null as ((event: { data: { type: string; path: string } }) => void) | null }))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn((_name, handler) => { events.changed = handler; return () => { events.changed = null } }) },
}))
vi.mock('@/api', () => ({ FileService: { ReadFile: vi.fn(), SaveFile: vi.fn().mockResolvedValue(undefined) } }))

enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks(); vi.mocked(FileService.ReadFile).mockReset(); vi.mocked(FileService.SaveFile).mockReset().mockResolvedValue(undefined) })
afterEach(() => { vi.useRealTimers() })

function mountDraft() {
  const workspace = ref('/workspace-a')
  const tabs = ref<EditorTab[]>([
    { path: 'Projects/A/Tasks.md', name: 'Tasks.md', content: '- [ ] [US] Ship', isDirty: false, lastSavedAt: '' },
    { path: 'Daily/2026-09-08.md', name: 'Daily', content: '# Daily', isDirty: false, lastSavedAt: '' },
  ])
  const activeTabIndex = ref(1)
  let draft!: ReturnType<typeof useEditorDraft>
  mount(defineComponent({
    setup() {
      draft = useEditorDraft({
        tabs,
        activeTabIndex,
        workspacePath: computed(() => workspace.value),
        autoSaveInterval: () => 250,
        findTabIndex: (path) => tabs.value.findIndex(tab => tab.path === path),
      })
      draft.startConflictWatcher()
      onUnmounted(draft.dispose)
      return () => null
    },
  }), { global: { plugins: [i18n] } })
  return { draft, tabs, activeTabIndex, workspace }
}

describe('editor and Markdown workbench integration', () => {
  it('flushes inactive dirty tabs and retains failed drafts until a successful retry', async () => {
    const { draft, tabs } = mountDraft()
    for (const tab of tabs.value) tab.isDirty = true
    vi.mocked(FileService.SaveFile).mockRejectedValueOnce(new Error('disk full'))

    expect(await draft.flushAllTabs()).toBe(false)
    expect(tabs.value[0]?.isDirty).toBe(true)
    expect(tabs.value[1]?.isDirty).toBe(false)
    expect(draft.saveErrors.value['Projects/A/Tasks.md']).toBe('disk full')
    expect(await draft.flushAllTabs()).toBe(true)
    expect(draft.saveErrors.value).toEqual({})
    expect(FileService.SaveFile).toHaveBeenLastCalledWith('/workspace-a', 'Projects/A/Tasks.md', '- [ ] [US] Ship')
  })

  it('serializes saves of the same document so a late write cannot overwrite newer typing', async () => {
    const { draft, tabs } = mountDraft()
    let finishFirst!: () => void
    vi.mocked(FileService.SaveFile).mockImplementationOnce(() => new Promise<void>(resolve => { finishFirst = resolve }) as ReturnType<typeof FileService.SaveFile>)
    tabs.value[0]!.isDirty = true
    const first = draft.saveTab(0)
    tabs.value[0]!.content = 'Newer draft'
    const second = draft.saveTab(0)
    expect(FileService.SaveFile).toHaveBeenCalledTimes(1)
    expect(draft.isSaving.value).toBe(true)
    finishFirst()
    await Promise.all([first, second])
    expect(FileService.SaveFile).toHaveBeenLastCalledWith('/workspace-a', 'Projects/A/Tasks.md', 'Newer draft')
    expect(tabs.value[0]?.isDirty).toBe(false)
    expect(draft.isSaving.value).toBe(false)
  })

  it('refuses to flush conflicts without writing over the external file', async () => {
    const { draft, tabs } = mountDraft()
    tabs.value[0]!.isDirty = true
    draft.conflictedPaths.value.add(tabs.value[0]!.path)
    expect(await draft.flushAllTabs()).toBe(false)
    expect(FileService.SaveFile).not.toHaveBeenCalled()
    expect(tabs.value[0]?.isDirty).toBe(true)
  })

  it('saving a conflict copy restores the external original instead of scheduling its overwrite', async () => {
    const { draft, tabs } = mountDraft()
    const path = tabs.value[0]!.path
    tabs.value[0]!.content = 'My local draft'
    tabs.value[0]!.isDirty = true
    draft.conflictedPaths.value.add(path)
    vi.mocked(FileService.ReadFile).mockResolvedValue('External original')
    await draft.saveDraftAsCopy(path)
    await flushPromises()
    expect(FileService.SaveFile).toHaveBeenCalledTimes(1)
    expect(vi.mocked(FileService.SaveFile).mock.calls[0]?.[1]).toContain('.冲突副本-')
    expect(vi.mocked(FileService.SaveFile).mock.calls[0]?.[2]).toBe('My local draft')
    expect(tabs.value[0]?.content).toBe('External original')
    expect(tabs.value[0]?.isDirty).toBe(false)
    expect(await draft.flushAllTabs()).toBe(true)
    expect(FileService.SaveFile).toHaveBeenCalledTimes(1)
  })

  it('reloads an inactive clean tab after an atomic replacement create event', async () => {
    vi.mocked(FileService.ReadFile).mockResolvedValue('- [x] [US] Ship #done/2026-09-08')
    const { tabs } = mountDraft()
    events.changed?.({ data: { type: 'create', path: 'Projects/A/Tasks.md' } })
    await flushPromises()
    expect(tabs.value[0]?.content).toBe('- [x] [US] Ship #done/2026-09-08')
    expect(tabs.value[1]?.content).toBe('# Daily')
  })

  it('pauses autosave and deactivation flush while a dirty draft conflicts with a workbench edit', async () => {
    vi.useFakeTimers()
    const { draft, tabs, activeTabIndex } = mountDraft()
    activeTabIndex.value = 0
    tabs.value[0]!.content = '- [ ] [US] Local unsaved edit'
    tabs.value[0]!.isDirty = true
    draft.scheduleAutoSave()
    events.changed?.({ data: { type: 'create', path: 'Projects/A/Tasks.md' } })
    await vi.advanceTimersByTimeAsync(300)
    draft.flushDirtyTab()
    await flushPromises()
    expect(draft.conflictedPaths.value.has('Projects/A/Tasks.md')).toBe(true)
    expect(tabs.value[0]?.content).toContain('Local unsaved edit')
    expect(FileService.SaveFile).not.toHaveBeenCalled()
  })

  it('keeps a draft typed while an external reload was pending', async () => {
    let resolveRead!: (value: string) => void
    vi.mocked(FileService.ReadFile).mockReturnValue(new Promise<string>(resolve => { resolveRead = resolve }) as ReturnType<typeof FileService.ReadFile>)
    const { draft, tabs, activeTabIndex } = mountDraft()
    activeTabIndex.value = 0
    events.changed?.({ data: { type: 'modify', path: 'Projects/A/Tasks.md' } })
    tabs.value[0]!.content = '- [ ] [US] New typing'
    tabs.value[0]!.isDirty = true
    resolveRead('- [x] [US] Ship')
    await flushPromises()
    expect(tabs.value[0]?.content).toBe('- [ ] [US] New typing')
    expect(tabs.value[0]?.isDirty).toBe(true)
    expect(draft.conflictedPaths.value.has('Projects/A/Tasks.md')).toBe(true)
  })

  it('discards a pending disk response after the workspace changes', async () => {
    let resolveRead!: (value: string) => void
    vi.mocked(FileService.ReadFile).mockReturnValue(new Promise<string>(resolve => { resolveRead = resolve }) as ReturnType<typeof FileService.ReadFile>)
    const { tabs, activeTabIndex, workspace } = mountDraft()
    activeTabIndex.value = 0
    events.changed?.({ data: { type: 'modify', path: 'Projects/A/Tasks.md' } })
    workspace.value = '/workspace-b'
    tabs.value[0]!.content = '# Workspace B'
    resolveRead('# Workspace A')
    await flushPromises()
    expect(tabs.value[0]?.content).toBe('# Workspace B')
  })

  it('never flushes an old session into a newly selected workspace during disposal', async () => {
    const { draft, tabs, workspace, activeTabIndex } = mountDraft()
    activeTabIndex.value = 0
    tabs.value[0]!.content = 'Unsaved workspace A draft'
    tabs.value[0]!.isDirty = true
    workspace.value = '/workspace-b'
    draft.dispose()
    expect(await draft.flushAllTabs()).toBe(false)
    await flushPromises()
    expect(FileService.SaveFile).not.toHaveBeenCalled()
    expect(tabs.value[0]?.isDirty).toBe(true)
  })
})
