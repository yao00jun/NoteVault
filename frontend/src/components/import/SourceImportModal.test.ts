// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { nextTick } from 'vue'
import type { SourceImportOpenRequest, SourceImportRequest } from '@/api/sourceImport'
import type { Workspace } from '@/types'

const backend = vi.hoisted(() => ({ preview: vi.fn(), start: vi.fn(), result: vi.fn(), task: vi.fn(), cancel: vi.fn(), folder: vi.fn() }))
vi.mock('@/api', () => ({
  TaskService: { GetTask: backend.task, Cancel: backend.cancel },
  AppService: { OpenFolderDialog: backend.folder },
  CredentialService: { GetCredential: vi.fn(async () => ''), SaveCredential: vi.fn(async () => undefined) },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))
vi.mock('@/api/sourceImport', () => ({ SourceImportService: { PreviewSourceImport: backend.preview, StartSourceImport: backend.start, GetSourceImportResult: backend.result } }))

import SourceImportModal from './SourceImportModal.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSourceImport } from '@/composables/useSourceImport'

const vault: Workspace = { id: 'a', name: 'My Vault', path: 'E:/Vault', createdAt: '', lastOpenedAt: '' }
const otherVault: Workspace = { id: 'b', name: 'Other Vault', path: 'E:/Other', createdAt: '', lastOpenedAt: '' }
let pinia: Pinia
let wrappers: VueWrapper[]
let submitted: SourceImportRequest[]
let taskStatus: 'running' | 'succeeded' | 'failed' | 'cancelled'

function preview(request: SourceImportRequest) {
  const name = request.name || 'Guide'
  const targetFolder = request.targetFolder || `${{ project: 'Projects', book: 'Learning', topic: 'Resources' }[request.kind]}/${name}`
  return { name, kind: request.kind, targetFolder, metadataPath: `${targetFolder}/${{ project: 'project.md', book: 'book.md', topic: 'index.md' }[request.kind]}`, fileCount: request.files.length || (request.sourceType === 'empty' ? 0 : 3), files: request.files.map(file => file.name), warnings: [], existing: false }
}

async function mountModal(request?: SourceImportOpenRequest, hasWorkspace = true) {
  const workspace = useWorkspaceStore()
  if (hasWorkspace) workspace.setCurrentWorkspace(vault)
  const router = createRouter({ history: createMemoryHistory(), routes: ['/today', '/projects', '/learning', '/editor'].map(path => ({ path, component: { template: '<div />' } })) })
  await router.push('/today')
  const wrapper = mount(SourceImportModal, { props: { open: true, request }, attachTo: document.body, global: { plugins: [pinia, router], stubs: { teleport: true } } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, workspace, router, intake: useSourceImport() }
}

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  localStorage.clear()
  wrappers = []
  submitted = []
  taskStatus = 'running'
  Object.values(backend).forEach(mock => mock.mockReset())
  backend.preview.mockImplementation(async (_path: string, request: SourceImportRequest) => preview(request))
  backend.start.mockImplementation(async (_path: string, request: SourceImportRequest) => { submitted.push(request); return `task-${submitted.length}` })
  backend.task.mockImplementation(async (id: string) => ({ id, name: 'source import', status: taskStatus, done: 2, total: 5, percent: 40, message: '正在保存第 2 个文件', startedAt: '', error: taskStatus === 'failed' ? '其中一个文件无法读取' : '' }))
  backend.result.mockImplementation(async (_path: string, id: string) => ({ ...preview(submitted[Number(id.split('-')[1]) - 1]!), taskId: id, imported: 2, skipped: 1, updated: 3, conflicts: ['existing.md'], warnings: ['保留已有笔记'], files: ['chapter.md'], cancelled: taskStatus === 'cancelled' }))
  backend.cancel.mockResolvedValue(true)
  backend.folder.mockResolvedValue('E:/Sources/Systems')
})

afterEach(() => {
  wrappers.forEach(wrapper => wrapper.unmount())
  disposePinia(pinia)
  vi.useRealTimers()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('SourceImportModal', () => {
  it('provides a labelled dialog and explains that a workspace is required', async () => {
    const { wrapper } = await mountModal(undefined, false)
    expect(wrapper.get('[role="dialog"]').attributes('aria-modal')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toContain('工作区')
    expect(wrapper.find('[data-testid="source-start"]').exists()).toBe(false)
  })

  it('creates an empty book, reports completion once, and opens its collection', async () => {
    taskStatus = 'succeeded'
    const { wrapper, router, workspace } = await mountModal({ kind: 'book', name: 'Design' })
    expect(wrapper.get('[data-testid="source-metadata-path"]').text()).toBe('Learning/Design/book.md')
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('创建完成')
    expect(wrapper.emitted('completed')).toHaveLength(1)
    expect(workspace.fileTreeVersion).toBe(1)
    await wrapper.get('[data-testid="source-open-result"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/learning?book=Learning/Design')
    expect(workspace.activeFile).toBeNull()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it.each([
    ['project', '/projects?project=Projects/Design', null],
    ['topic', '/editor?file=Resources/Design/index.md', 'Resources/Design/index.md'],
  ] as const)('opens a completed %s using its owning route', async (kind, route, activeFile) => {
    taskStatus = 'succeeded'
    const { wrapper, router, workspace } = await mountModal({ kind, name: 'Design' })
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="source-open-result"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe(route)
    expect(workspace.activeFile).toBe(activeFile)
  })

  it('lets a user choose a folder and inspect the exact destination before starting', async () => {
    const { wrapper } = await mountModal({ kind: 'project', sourceType: 'folder', name: 'Systems' })
    await wrapper.get('[data-testid="source-pick-folder"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="source-location"]').element as HTMLInputElement).value).toBe('E:/Sources/Systems')
    await wrapper.get('[data-testid="source-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.si-preview').text()).toContain('Projects/Systems/project.md')
    expect(wrapper.get('.si-preview').text()).toContain('3')
    expect(submitted).toHaveLength(0)
  })

  it('supports URL input, conflict policy, and optional AI without requiring credentials', async () => {
    const { wrapper } = await mountModal({ kind: 'topic', sourceType: 'url' })
    await wrapper.get('[data-testid="source-location"]').setValue('https://example.org/guide')
    await wrapper.get('[data-testid="source-conflict"]').setValue('copy')
    await wrapper.get('[data-testid="source-enrich"]').setValue(true)
    await wrapper.get('[data-testid="source-instruction"]').setValue('提取关键概念')
    expect(wrapper.text()).toContain('尚未配置 AI')
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    expect(submitted[0]).toMatchObject({ sourceType: 'url', source: 'https://example.org/guide', conflictStrategy: 'copy', enrich: true, instruction: '提取关键概念', ai: { apiKey: '' } })
    expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('40')
  })

  it('selects and drops files by reading their contents, and can remove a selection', async () => {
    const { wrapper } = await mountModal({ kind: 'book', sourceType: 'files', name: 'Design' })
    const input = wrapper.get('[data-testid="source-files"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['one'], 'one.md')] })
    await input.trigger('change')
    await vi.waitFor(() => expect(wrapper.text()).toContain('one.md'))
    await wrapper.get('[data-testid="source-dropzone"]').trigger('drop', { dataTransfer: { files: [new File(['two'], 'two.md')] } })
    await vi.waitFor(() => expect(wrapper.text()).toContain('two.md'))
    await wrapper.get('[aria-label="移除 one.md"]').trigger('click')
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    expect(submitted[0]!.files).toEqual([{ name: 'two.md', contentBase64: 'dHdv' }])
  })

  it('shows partial cancellation and lets the user explicitly retry while keeping background state', async () => {
    vi.useFakeTimers()
    const request = { kind: 'book', sourceType: 'folder', source: 'E:/Source', name: 'Design' } as const
    const { wrapper } = await mountModal(request)
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="source-cancel-task"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('正在取消')
    await wrapper.setProps({ open: false })
    taskStatus = 'cancelled'
    await vi.advanceTimersByTimeAsync(1500)
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.text()).toContain('已取消')
    expect(wrapper.get('[data-testid="source-result-imported"]').text()).toContain('2')
    expect(wrapper.get('[data-testid="source-result-updated"]').text()).toContain('3')
    expect(wrapper.get('[data-testid="source-result-skipped"]').text()).toContain('1')
    expect(wrapper.get('[data-testid="source-result-conflicts"]').text()).toContain('1')
    expect(submitted).toHaveLength(1)
    taskStatus = 'running'
    await wrapper.get('[data-testid="source-retry"]').trigger('click')
    await flushPromises()
    expect(submitted).toHaveLength(2)
    expect(wrapper.text()).toContain('正在保存第 2 个文件')
  })

  it('auto-starts a valid explicit source only once across dialog remounts', async () => {
    taskStatus = 'succeeded'
    const request: SourceImportOpenRequest = { kind: 'project', sourceType: 'url', source: 'https://example.org/guide', autoStart: true }
    const first = await mountModal(request)
    expect(submitted).toHaveLength(1)
    first.wrapper.unmount()
    wrappers = wrappers.filter(wrapper => wrapper !== first.wrapper)
    const second = await mountModal(request)
    expect(submitted).toHaveLength(1)
    expect(second.wrapper.text()).toContain('导入完成')
    expect(second.wrapper.emitted('completed')).toBeUndefined()
  })

  it('validates auto-start requests before any write', async () => {
    const { wrapper } = await mountModal({ kind: 'book', sourceType: 'folder', source: '../bad', autoStart: true })
    expect(wrapper.get('[role="alert"]').text()).toContain('绝对路径')
    expect(submitted).toHaveLength(0)
  })

  it('does not auto-start a source without an explicitly chosen kind', async () => {
    const { wrapper } = await mountModal({ sourceType: 'url', source: 'https://example.org/guide', autoStart: true })
    expect(wrapper.find('[data-testid="source-start"]').exists()).toBe(true)
    expect(submitted).toHaveLength(0)
  })

  it('shows existing-file and unsupported-source warnings in the optional preview', async () => {
    backend.preview.mockImplementationOnce(async (_path: string, request: SourceImportRequest) => ({ ...preview(request), existing: true, files: ['one.md', 'scanned.pdf'], warnings: ['scanned.pdf 需要 OCR，未生成正文'] }))
    const { wrapper } = await mountModal({ kind: 'book', sourceType: 'folder', source: 'E:/Source', name: 'Design' })
    await wrapper.get('[data-testid="source-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.si-preview').text()).toContain('已有目录')
    expect(wrapper.get('.si-preview').text()).toContain('需要 OCR')
    expect(submitted).toHaveLength(0)
  })

  it('does not claim partial completion when a failed task saved no content', async () => {
    taskStatus = 'failed'
    backend.result.mockImplementationOnce(async (_path: string, id: string) => ({ taskId: id, targetFolder: '', metadataPath: '', imported: 0, updated: 0, skipped: 0, conflicts: [], warnings: [], files: [], cancelled: false }))
    const { wrapper } = await mountModal({ kind: 'project', name: 'Compiler' })
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.si-result h3').text()).toBe('任务未完成')
    expect(wrapper.find('[data-testid="source-open-result"]').exists()).toBe(false)
  })

  it('allows an inspected unknown start to return to the form without automatically resubmitting', async () => {
    backend.start.mockRejectedValueOnce(new Error('lost response'))
    const { wrapper } = await mountModal({ kind: 'project', name: 'Compiler' })
    await wrapper.get('[data-testid="source-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('未能确认任务是否已启动')
    await wrapper.get('[data-testid="source-inspected-reset"]').trigger('click')
    expect((wrapper.get('[data-testid="source-name"]').element as HTMLInputElement).value).toBe('Compiler')
    expect(backend.start).toHaveBeenCalledTimes(1)
  })

  it('does not apply a delayed folder selection to another workspace', async () => {
    let choose!: (path: string) => void
    backend.folder.mockImplementationOnce(() => new Promise(resolve => { choose = resolve }))
    const { wrapper, workspace } = await mountModal({ kind: 'project', sourceType: 'folder' })
    await wrapper.get('[data-testid="source-pick-folder"]').trigger('click')
    workspace.setCurrentWorkspace(otherVault)
    choose('E:/Should not leak')
    await flushPromises()
    expect(wrapper.text()).toContain('Other Vault')
    expect(useSourceImport().current!.draft.source).toBe('')
  })

  it('keeps keyboard focus in the dialog, closes on Escape, and restores the opener', async () => {
    const opener = document.createElement('button')
    document.body.appendChild(opener)
    opener.focus()
    const { wrapper } = await mountModal({ kind: 'book' })
    const dialog = wrapper.get('[role="dialog"]')
    const close = wrapper.get('[aria-label="关闭新建窗口"]')
    const last = wrapper.get('[data-testid="source-preview"]')
    ;(last.element as HTMLElement).focus()
    await dialog.trigger('keydown', { key: 'Tab' })
    expect(document.activeElement).toBe(close.element)
    await dialog.trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(backend.cancel).not.toHaveBeenCalled()
    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.activeElement).toBe(opener)
  })
})
