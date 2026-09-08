// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '@/i18n'
import { registerEditorFlush } from '@/composables/useEditorSession'
import { resetToasts, useToast } from '@/composables/useToast'
import { useWorkspaceStore } from '@/stores/workspace'
import { FileService, WorkspaceService } from '@/api'
import WelcomeView from './WelcomeView.vue'

vi.mock('@/api', () => ({
  WorkspaceService: { ListWorkspaces: vi.fn(), SetCurrentWorkspace: vi.fn(), CreateWorkspace: vi.fn() },
  FileService: { CreateFile: vi.fn() },
}))

const original = { id: 'a', name: '当前工作区', path: 'C:/notes-a', createdAt: '', lastOpenedAt: '' }
const destination = { id: 'b', name: '其他工作区', path: 'C:/notes-b', createdAt: '', lastOpenedAt: '' }
const piniaInstances: Pinia[] = []
let unregisterFlush = () => {}
enableAutoUnmount(afterEach)
beforeEach(() => {
  localStorage.clear()
  vi.resetAllMocks()
  resetToasts()
  vi.mocked(WorkspaceService.ListWorkspaces).mockResolvedValue([destination] as Awaited<ReturnType<typeof WorkspaceService.ListWorkspaces>>)
  vi.mocked(WorkspaceService.CreateWorkspace).mockResolvedValue(destination as Awaited<ReturnType<typeof WorkspaceService.CreateWorkspace>>)
})
afterEach(() => { unregisterFlush(); piniaInstances.splice(0).forEach(disposePinia) })

async function setup() {
  const pinia = createPinia()
  piniaInstances.push(pinia)
  setActivePinia(pinia)
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace({ ...original })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: WelcomeView },
    { path: '/knowledge', redirect: '/today' },
    ...['today', 'editor'].map(path => ({ path: '/' + path, component: { template: '<div />' } })),
  ] })
  await router.push('/')
  const wrapper = mount(WelcomeView, { global: { plugins: [pinia, router, i18n] } })
  await flushPromises()
  return { wrapper, workspace, router }
}

describe('Welcome workspace and document entry points', () => {
  it.each(['recent', 'new-doc', 'open-folder', 'demo'])('keeps unsaved drafts before %s changes the workspace', async action => {
    const { wrapper, workspace, router } = await setup()
    const flush = vi.fn(async () => false)
    unregisterFlush = registerEditorFlush(flush)
    if (action === 'recent') await wrapper.get('.recent-item').trigger('click')
    else if (action === 'demo') await wrapper.get('.demo-btn').trigger('click')
    else {
      await wrapper.get('[data-testid="welcome-' + action + '"]').trigger('click')
      const fields = wrapper.findAll('.dialog-body input')
      await fields[0]!.setValue(destination.name)
      await fields[1]!.setValue(destination.path)
      await wrapper.get('[data-testid="welcome-confirm"]').trigger('click')
    }
    await flushPromises()
    expect(flush).toHaveBeenCalledTimes(1)
    expect(WorkspaceService.SetCurrentWorkspace).not.toHaveBeenCalled()
    expect(WorkspaceService.CreateWorkspace).not.toHaveBeenCalled()
    expect(FileService.CreateFile).not.toHaveBeenCalled()
    expect(workspace.currentWorkspace?.path).toBe(original.path)
    expect(router.currentRoute.value.path).toBe('/')
    expect(useToast().toasts.value.some(item => item.message.includes('草稿'))).toBe(true)
  })

  it('waits for a successful draft flush before selecting the next workspace', async () => {
    const { wrapper, workspace, router } = await setup()
    let finish!: (saved: boolean) => void
    unregisterFlush = registerEditorFlush(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('.recent-item').trigger('click')
    expect(WorkspaceService.SetCurrentWorkspace).not.toHaveBeenCalled()
    finish(true)
    await flushPromises()
    expect(WorkspaceService.SetCurrentWorkspace).toHaveBeenCalledWith(destination.id)
    expect(workspace.currentWorkspace?.path).toBe(destination.path)
    expect(router.currentRoute.value.path).toBe('/today')
  })

  it('uses a raw file query for a new Unicode, percent and space filename', async () => {
    const { wrapper, router } = await setup()
    const name = '中文 100%25.md'
    vi.mocked(FileService.CreateFile).mockResolvedValue({ path: name } as Awaited<ReturnType<typeof FileService.CreateFile>>)
    await wrapper.get('[data-testid="welcome-new-doc"]').trigger('click')
    await wrapper.get('[data-testid="welcome-doc-name"]').setValue(name)
    await wrapper.get('[data-testid="welcome-confirm"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query.file).toBe(name)
  })
})
