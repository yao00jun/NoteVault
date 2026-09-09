// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { effectScope, nextTick } from 'vue'
import { flushPromises } from '@vue/test-utils'
import type { SourceImportRequest, SourceImportResult } from '@/api/sourceImport'
import type { Workspace } from '@/types'

const transport = vi.hoisted(() => ({ preview: vi.fn(), start: vi.fn(), result: vi.fn(), task: vi.fn(), cancel: vi.fn() }))
vi.mock('@wailsio/runtime', () => ({ Call: { ByName: vi.fn() } }))
vi.mock('@/api', () => ({ TaskService: { GetTask: transport.task, Cancel: transport.cancel } }))
vi.mock('@/api/sourceImport', () => ({
  SourceImportService: {
    PreviewSourceImport: transport.preview,
    StartSourceImport: transport.start,
    GetSourceImportResult: transport.result,
  },
}))

import { useWorkspaceStore } from '@/stores/workspace'
import { requestSourceImport, useSourceImport } from './useSourceImport'

const vaultA: Workspace = { id: 'a', name: 'A', path: 'E:/Vault A', createdAt: '', lastOpenedAt: '' }
const vaultB: Workspace = { id: 'b', name: 'B', path: 'E:/Vault B', createdAt: '', lastOpenedAt: '' }
const ai = { apiKey: '', baseURL: '', model: '' }
let pinia: Pinia
let submissions: { workspace: string; request: SourceImportRequest; id: string }[]
let taskStatus: Record<string, { status: string; percent: number; done: number; total: number; message: string; error?: string }>
let results: Record<string, SourceImportResult>

function previewFor(request: SourceImportRequest) {
  const name = request.name || 'Source'
  const space = { project: 'Projects', book: 'Learning', topic: 'Resources' }[request.kind]
  const metadata = { project: 'project.md', book: 'book.md', topic: 'index.md' }[request.kind]
  const targetFolder = request.targetFolder || `${space}/${name}`
  return { name, kind: request.kind, targetFolder, metadataPath: `${targetFolder}/${metadata}`, fileCount: request.files.length, files: request.files.map(file => file.name), warnings: [], existing: false }
}

function setup() {
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace(vaultA)
  const intake = useSourceImport()
  expect(intake.current).not.toBeNull()
  return { workspace, intake }
}

beforeEach(() => {
  vi.useFakeTimers()
  pinia = createPinia()
  setActivePinia(pinia)
  localStorage.clear()
  submissions = []
  taskStatus = {}
  results = {}
  Object.values(transport).forEach(mock => mock.mockReset())
  transport.preview.mockImplementation(async (_workspace: string, request: SourceImportRequest) => previewFor(request))
  transport.start.mockImplementation(async (workspace: string, request: SourceImportRequest) => {
    const id = `task-${submissions.length + 1}`
    submissions.push({ workspace, request: JSON.parse(JSON.stringify(request)), id })
    taskStatus[id] = { status: 'running', percent: 40, done: 2, total: 5, message: '正在导入笔记' }
    results[id] = { taskId: id, targetFolder: previewFor(request).targetFolder, metadataPath: previewFor(request).metadataPath, imported: 2, skipped: 1, updated: 1, conflicts: ['chapter.md'], warnings: [], files: ['chapter.md', 'notes.md'], cancelled: false }
    return id
  })
  transport.task.mockImplementation(async (id: string) => ({ id, name: 'source-import', startedAt: '', ...taskStatus[id] }))
  transport.result.mockImplementation(async (workspace: string, id: string) => {
    if (submissions.find(item => item.id === id)?.workspace !== workspace) throw new Error('wrong workspace')
    return { ...results[id], files: [...results[id]!.files] }
  })
  transport.cancel.mockResolvedValue(true)
})

afterEach(() => {
  disposePinia(pinia)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('shared source import state', () => {
  it('re-extracts stored PDFs with guarded updates so unchanged source bytes can receive improved formatting', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'attachments', source: 'Learning/Design', targetFolder: 'Learning/Design', name: 'Design' })
    await intake.start(ai)
    await flushPromises()
    expect(submissions).toHaveLength(1)
    expect(submissions[0]!.request).toMatchObject({ sourceType: 'attachments', conflictStrategy: 'update', enrich: false, targetFolder: 'Learning/Design' })
  })

  it('dispatches the shared new-source event with the supplied draft', () => {
    const received: unknown[] = []
    const listener = (event: Event) => received.push((event as CustomEvent).detail)
    window.addEventListener('notevault:source-import', listener)
    requestSourceImport({ kind: 'book', name: 'Design' })
    requestSourceImport()
    window.removeEventListener('notevault:source-import', listener)
    expect(received).toEqual([{ kind: 'book', name: 'Design' }, {}])
  })

  it('previews and creates an empty book with the complete backend DTO', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'empty', name: 'Design' })
    await intake.preview()
    expect(intake.current!.preview?.metadataPath).toBe('Learning/Design/book.md')
    await intake.start(ai)
    await flushPromises()
    expect(submissions).toEqual([{ workspace: 'E:/Vault A', id: 'task-1', request: {
      sourceType: 'empty', source: '', files: [], kind: 'book', name: 'Design', targetFolder: 'Learning/Design',
      conflictStrategy: 'skip', enrich: false, instruction: '', ai: { apiKey: '', baseURL: '', model: '' },
    } }])
    expect(intake.current!.task?.percent).toBe(40)
    expect(intake.busy).toBe(true)
  })

  it.each(['Learning/Design/Volume1', '../Learning/Design', '/Learning/Design', 'Projects/Design', 'Learning/../Design'])('rejects a destination the collection reader cannot safely discover: %s', async targetFolder => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', name: 'Design', targetFolder })
    await intake.start(ai)
    expect(submissions).toHaveLength(0)
    expect(intake.current!.error).toContain('Learning')
  })

  it('validates URLs and absolute folders before previewing or writing', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'project', sourceType: 'folder', source: '../outside' })
    await intake.start(ai)
    expect(intake.current!.error).toContain('绝对路径')
    intake.prepare({ kind: 'topic', sourceType: 'url', source: 'javascript:alert(1)' })
    await intake.start(ai)
    expect(intake.current!.error).toContain('HTTP')
    expect(submissions).toHaveLength(0)
  })

  it('does not require AI credentials to import a URL', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'topic', sourceType: 'url', source: 'https://example.org/guide' })
    intake.current!.draft.enrich = true
    await intake.start(ai)
    expect(submissions[0]!.request).toMatchObject({ sourceType: 'url', source: 'https://example.org/guide', enrich: true, ai })
    expect(intake.current!.taskId).toBe('task-1')
  })

  it('retains progress after a consumer closes and refreshes the finished workspace once', async () => {
    const { workspace, intake } = setup()
    const scope = effectScope()
    scope.run(() => useSourceImport())
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    await flushPromises()
    scope.stop()
    expect(intake.current!.task?.message).toBe('正在导入笔记')
    taskStatus['task-1'] = { status: 'succeeded', percent: 100, done: 5, total: 5, message: '完成' }
    await vi.advanceTimersByTimeAsync(1500)
    const reopened = useSourceImport()
    expect(reopened.current!.phase).toBe('succeeded')
    expect(reopened.current!.result).toMatchObject({ imported: 2, skipped: 1, updated: 1, conflicts: ['chapter.md'] })
    expect(workspace.fileTreeVersion).toBe(1)
    await vi.advanceTimersByTimeAsync(5000)
    await reopened.checkStatus()
    expect(workspace.fileTreeVersion).toBe(1)
  })

  it('waits for cancellation acknowledgement and offers an explicit retry of partial work', async () => {
    const { intake, workspace } = setup()
    intake.prepare({ kind: 'book', sourceType: 'folder', source: 'E:/Books', name: 'Systems' })
    await intake.start(ai)
    await flushPromises()
    await intake.cancel()
    expect(intake.current!.cancelRequested).toBe(true)
    expect(intake.current!.phase).toBe('running')
    taskStatus['task-1'] = { status: 'cancelled', percent: 40, done: 2, total: 5, message: '已取消' }
    results['task-1']!.cancelled = true
    await vi.advanceTimersByTimeAsync(1500)
    expect(intake.current!.result).toMatchObject({ cancelled: true, imported: 2, updated: 1 })
    expect(workspace.fileTreeVersion).toBe(1)
    await vi.advanceTimersByTimeAsync(3000)
    expect(submissions).toHaveLength(1)
    await intake.retry(ai)
    await flushPromises()
    expect(submissions).toHaveLength(2)
    expect(submissions[1]!.request.source).toBe('E:/Books')
    expect(intake.current!.taskId).toBe('task-2')
    expect(intake.current!.cancelRequested).toBe(false)
  })

  it('keeps a late task result in its original workspace and refreshes on return', async () => {
    const { intake, workspace } = setup()
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    await flushPromises()
    workspace.setCurrentWorkspace(vaultB)
    await nextTick()
    expect(intake.current!.taskId).toBe('')
    taskStatus['task-1'] = { status: 'succeeded', percent: 100, done: 5, total: 5, message: '完成' }
    await vi.advanceTimersByTimeAsync(1500)
    expect(intake.current!.result).toBeNull()
    expect(workspace.fileTreeVersion).toBe(0)
    intake.prepare({ kind: 'book', name: 'New workspace' })
    expect(intake.current!.draft.name).toBe('New workspace')
    workspace.setCurrentWorkspace(vaultA)
    await nextTick()
    expect(intake.current!.result?.metadataPath).toBe('Projects/Compiler/project.md')
    expect(workspace.fileTreeVersion).toBe(1)
    workspace.setCurrentWorkspace(vaultB)
    workspace.setCurrentWorkspace(vaultA)
    expect(workspace.fileTreeVersion).toBe(1)
  })

  it('does not start a write if the workspace changes during the read-only preview', async () => {
    const { intake, workspace } = setup()
    let resolvePreview!: (value: ReturnType<typeof previewFor>) => void
    transport.preview.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    intake.prepare({ kind: 'book', name: 'Design' })
    const request = { ...intake.current!.draft }
    const pending = intake.start(ai)
    workspace.setCurrentWorkspace(vaultB)
    resolvePreview(previewFor(request))
    await pending
    expect(submissions).toHaveLength(0)
    expect(intake.current!.taskId).toBe('')
  })

  it('discards a preview when the source draft changes before its response arrives', async () => {
    const { intake } = setup()
    let resolvePreview!: (value: ReturnType<typeof previewFor>) => void
    transport.preview.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    intake.prepare({ kind: 'book', name: 'Old name' })
    const request = { ...intake.current!.draft }
    const pending = intake.preview()
    intake.current!.draft.name = 'New name'
    intake.invalidatePreview()
    resolvePreview(previewFor(request))
    await pending
    expect(intake.current!.preview).toBeNull()
  })

  it('never automatically repeats a write after losing the start response', async () => {
    const { intake } = setup()
    transport.start.mockRejectedValueOnce(new Error('connection lost'))
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    expect(intake.current!.phase).toBe('unknown')
    expect(intake.current!.error).toContain('确认')
    await intake.retry(ai)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(transport.start).toHaveBeenCalledTimes(1)
  })

  it('recovers a status read failure by checking the same task without resubmitting', async () => {
    const { intake } = setup()
    transport.task.mockRejectedValueOnce(new Error('temporarily offline'))
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    await flushPromises()
    expect(intake.current!.phase).toBe('unknown')
    expect(intake.current!.taskId).toBe('task-1')
    await intake.checkStatus()
    expect(intake.current!.phase).toBe('running')
    expect(submissions).toHaveLength(1)
  })

  it('keeps known failed results honest and only retries after the user requests it', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    await flushPromises()
    taskStatus['task-1'] = { status: 'failed', percent: 40, done: 2, total: 5, message: '停止', error: '无法读取第三个文件' }
    await vi.advanceTimersByTimeAsync(1500)
    expect(intake.current!.phase).toBe('failed')
    expect(intake.current!.error).toContain('无法读取第三个文件')
    expect(intake.current!.result?.imported).toBe(2)
    expect(submissions).toHaveLength(1)
    await intake.retry(ai)
    expect(submissions).toHaveLength(2)
  })

  it('rechecks a terminal result read without repeating the completed write', async () => {
    const { intake, workspace } = setup()
    intake.prepare({ kind: 'book', name: 'Design' })
    await intake.start(ai)
    await flushPromises()
    taskStatus['task-1'] = { status: 'succeeded', percent: 100, done: 5, total: 5, message: '完成' }
    transport.result.mockRejectedValueOnce(new Error('offline'))
    await vi.advanceTimersByTimeAsync(1500)
    expect(intake.current!.phase).toBe('unknown')
    expect(intake.current!.result).toBeNull()
    await intake.retry(ai)
    expect(submissions).toHaveLength(1)
    await intake.checkStatus()
    expect(intake.current!.phase).toBe('succeeded')
    expect(workspace.fileTreeVersion).toBe(1)
  })

  it('retains the running task when another entry point requests a new collection', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    expect(intake.prepare({ kind: 'book', name: 'Replacement' })).toBe(false)
    expect(intake.current!.draft.name).toBe('Compiler')
    expect(intake.current!.taskId).toBe('task-1')
  })

  it('adopts an existing collection in the matching workspace space', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'adopt', source: 'E:/Vault A/Learning/Design', targetFolder: 'Learning/Design' })
    await intake.start(ai)
    expect(submissions[0]!.request).toMatchObject({ sourceType: 'adopt', source: 'E:/Vault A/Learning/Design', targetFolder: 'Learning/Design' })
  })

  it('rejects adopting an outside or mismatched existing directory', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'adopt', source: 'E:/Outside/Learning/Design' })
    await intake.start(ai)
    expect(intake.current!.error).toContain('当前工作区')
    intake.prepare({ kind: 'book', sourceType: 'adopt', source: 'Learning/Design', targetFolder: 'Learning/Other' })
    await intake.start(ai)
    expect(intake.current!.error).toContain('一致')
    expect(submissions).toHaveLength(0)
  })

  it('clears polling when its application store is disposed', async () => {
    const { intake, workspace } = setup()
    intake.prepare({ kind: 'project', name: 'Compiler' })
    await intake.start(ai)
    await flushPromises()
    disposePinia(pinia)
    taskStatus['task-1'] = { status: 'succeeded', percent: 100, done: 5, total: 5, message: '完成' }
    await vi.advanceTimersByTimeAsync(5000)
    expect(workspace.fileTreeVersion).toBe(0)
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('browser file intake', () => {
  it('discards file reads that finish after switching workspaces', async () => {
    const { intake, workspace } = setup()
    intake.prepare({ kind: 'book', sourceType: 'files', name: 'Design' })
    let complete!: (value: ArrayBuffer) => void
    const file = new File(['one'], 'one.md')
    Object.defineProperty(file, 'arrayBuffer', { value: () => new Promise<ArrayBuffer>(resolve => { complete = resolve }) })
    const reading = intake.addFiles([file])
    workspace.setCurrentWorkspace(vaultB)
    complete(new Uint8Array([111, 110, 101]).buffer)
    await reading
    expect(intake.current!.draft.files).toEqual([])
    workspace.setCurrentWorkspace(vaultA)
    expect(intake.current!.draft.files).toEqual([])
    expect(intake.current!.readingFiles).toBe(false)
  })

  it('reads selected bytes and filenames without relying on a local filesystem path', async () => {
    vi.useRealTimers()
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'files', name: 'Design' })
    await intake.addFiles([new File(['# 笔记'], 'chapter.md', { type: 'text/markdown' })])
    expect(intake.current!.draft.files).toEqual([{ name: 'chapter.md', contentBase64: 'IyDnrJTorrA=' }])
    await intake.start(ai)
    expect(submissions[0]!.request).toMatchObject({ sourceType: 'files', source: '', files: [{ name: 'chapter.md', contentBase64: 'IyDnrJTorrA=' }] })
  })

  it('rejects oversized files before allocating file contents', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'files', name: 'Design' })
    const file = new File([], 'large.pdf')
    Object.defineProperty(file, 'size', { value: 20 * 1024 * 1024 + 1 })
    await intake.addFiles([file])
    expect(intake.current!.draft.files).toEqual([])
    expect(intake.current!.error).toContain('20 MiB')
  })

  it('rejects oversized batches and too many files before reading them', async () => {
    const { intake } = setup()
    intake.prepare({ kind: 'book', sourceType: 'files', name: 'Design' })
    const files = Array.from({ length: 6 }, (_, index) => {
      const file = new File([], `part-${index}.pdf`)
      Object.defineProperty(file, 'size', { value: 20 * 1024 * 1024 })
      return file
    })
    await intake.addFiles(files)
    expect(intake.current!.error).toContain('100 MiB')
    await intake.addFiles(Array.from({ length: 501 }, (_, index) => new File([], `${index}.md`)))
    expect(intake.current!.error).toContain('500')
    expect(intake.current!.draft.files).toEqual([])
  })
})
