import { computed, onScopeDispose, reactive, toRaw, watch } from 'vue'
import { defineStore } from 'pinia'
import { TaskService, type TaskInfo } from '@/api'
import { SourceImportService } from '@/api/sourceImport'
import type { CollectionKind, SourceImportFile, SourceImportOpenRequest, SourceImportPreview, SourceImportRequest, SourceImportResult } from '@/api/sourceImport'
import { useWorkspaceStore } from '@/stores/workspace'

export type { SourceImportOpenRequest } from '@/api/sourceImport'
export type SourceImportPhase = 'idle' | 'starting' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'unknown'

export const collectionSpaces: Record<CollectionKind, string> = { project: 'Projects', book: 'Learning', topic: 'Resources' }
export const collectionMetadata: Record<CollectionKind, string> = { project: 'project.md', book: 'book.md', topic: 'index.md' }

export interface SourceImportState {
  workspacePath: string
  draft: SourceImportRequest
  preview: SourceImportPreview | null
  previewing: boolean
  readingFiles: boolean
  taskId: string
  task: TaskInfo | null
  phase: SourceImportPhase
  result: SourceImportResult | null
  submitted: SourceImportRequest | null
  error: string
  cancelRequested: boolean
  refreshed: boolean
  completionEmitted: boolean
}

interface Runtime {
  generation: number
  previewSequence: number
  pollingGeneration: number | null
  timer?: ReturnType<typeof setTimeout>
}

const POLL_INTERVAL = 1200
const FILE_LIMIT = 20 * 1024 * 1024
const BATCH_LIMIT = 100 * 1024 * 1024
const normalizePath = (path: string) => path.replace(/\\/g, '/').replace(/\/+$/, '')
const workspaceKey = (path: string) => /^[a-z]:|^\\\\|^\/\//i.test(path) ? normalizePath(path).toLowerCase() : normalizePath(path)
const errorText = (error: unknown) => error instanceof Error ? error.message : String(error)
const active = (entry: SourceImportState) => ['starting', 'running', 'unknown'].includes(entry.phase)
const terminal = (status: string) => ['succeeded', 'failed', 'cancelled'].includes(status)

function blankDraft(request: SourceImportOpenRequest = {}): SourceImportRequest {
  return {
    kind: request.kind ?? 'project', sourceType: request.sourceType ?? 'empty',
    source: request.source ?? '', name: request.name ?? '', targetFolder: request.targetFolder ?? '',
    files: [], conflictStrategy: request.sourceType === 'attachments' ? 'update' : 'skip', enrich: false, instruction: request.instruction ?? '',
    ai: { apiKey: '', baseURL: '', model: '' },
  }
}

function blankState(path: string, request?: SourceImportOpenRequest): SourceImportState {
  return { workspacePath: path, draft: blankDraft(request), preview: null, previewing: false, readingFiles: false, taskId: '', task: null, phase: 'idle', result: null, submitted: null, error: '', cancelRequested: false, refreshed: false, completionEmitted: false }
}

function cloneRequest(draft: SourceImportRequest): SourceImportRequest {
  return {
    ...draft, name: draft.name.trim(), targetFolder: normalizePath(draft.targetFolder.trim()),
    source: ['empty', 'files'].includes(draft.sourceType) ? '' : draft.source.trim(),
    files: draft.sourceType === 'files' ? draft.files.map(file => ({ ...file })) : [],
    instruction: draft.instruction.trim(), ai: { ...draft.ai },
  }
}

function sameDraft(request: SourceImportRequest, draft: SourceImportRequest): boolean {
  const other = cloneRequest(draft)
  return request.kind === other.kind && request.sourceType === other.sourceType
    && request.name === other.name && request.source === other.source && request.targetFolder === other.targetFolder
    && request.conflictStrategy === other.conflictStrategy && request.enrich === other.enrich && request.instruction === other.instruction
    && request.files.length === other.files.length && request.files.every((file, index) => file.name === other.files[index]!.name && file.contentBase64 === other.files[index]!.contentBase64)
}

function safeFolderName(value: string): boolean {
  return !!value && value !== '.' && value !== '..' && !/[<>:"/\\|?*]/.test(value)
    && !Array.from(value).some(character => character.charCodeAt(0) < 32)
    && !/[. ]$/.test(value) && !/^(?:CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\.|$)/i.test(value)
}

function destinationValid(path: string, kind: CollectionKind): boolean {
  const segments = path.split('/')
  return segments.length === 2 && segments[0] === collectionSpaces[kind] && safeFolderName(segments[1]!)
}

function absoluteSource(path: string): boolean {
  return /^(?:[a-z]:[\\/]|\\\\[^\\/]+[\\/]|\/)/i.test(path) && !/[\r\n\0]/.test(path)
}

function validate(request: SourceImportRequest, workspace: string): string {
  if (!collectionSpaces[request.kind] || !['empty', 'folder', 'adopt', 'files', 'url', 'attachments'].includes(request.sourceType)) return '请选择有效的来源和类型'
  if (request.sourceType === 'empty' && !request.name) return '请填写名称'
  if (request.name && !safeFolderName(request.name)) return '名称不能包含路径分隔符、特殊字符或保留的文件名'
  if (request.targetFolder && !destinationValid(request.targetFolder, request.kind)) return `保存位置应为 ${collectionSpaces[request.kind]}/集合名称，集合必须直接位于该空间下`
  if (request.sourceType === 'folder' && !absoluteSource(request.source)) return '请输入文件夹的绝对路径，或使用选择文件夹'
  if (request.sourceType === 'adopt') {
    let relative = normalizePath(request.source)
    if (absoluteSource(relative)) {
      const root = normalizePath(workspace)
      if (!workspaceKey(relative).startsWith(`${workspaceKey(root)}/`)) return '已有目录必须位于当前工作区内'
      relative = relative.slice(root.length + 1)
    }
    if (!destinationValid(relative, request.kind)) return `已有目录应为当前工作区的 ${collectionSpaces[request.kind]}/集合名称`
    if (request.targetFolder && request.targetFolder !== relative) return '接入已有目录时，保存位置必须与来源目录一致'
  }
  if (request.sourceType === 'url') {
    try {
      const url = new URL(request.source)
      if (!/^https?:$/.test(url.protocol) || !url.hostname || url.username || url.password) return '请输入有效的 HTTP(S) 链接，不要在链接中包含账号或密码'
    } catch { return '请输入有效的 HTTP(S) 链接' }
  }
  if (request.sourceType === 'files' && request.files.length === 0) return '请先选择或拖入文件'
  if (request.sourceType === 'attachments' && (request.kind !== 'book' || !destinationValid(request.source, 'book') || (request.targetFolder && request.targetFolder !== request.source))) return '请选择当前工作区 Learning 下的原分册目录'
  return ''
}

export function requestSourceImport(request?: SourceImportOpenRequest): void {
  window.dispatchEvent(new CustomEvent('notevault:source-import', { detail: request ?? {} }))
}

/** Pinia owns the polling lifetime; closing any dialog only disposes that dialog's observers. */
export const useSourceImport = defineStore('sourceImport', () => {
  const workspace = useWorkspaceStore()
  const states = reactive(new Map<string, SourceImportState>())
  const runtimes = new Map<string, Runtime>()
  const openedRequests = new WeakSet<SourceImportOpenRequest>()
  let disposed = false

  function ensureState(path: string): SourceImportState {
    const key = workspaceKey(path)
    if (!states.has(key)) {
      states.set(key, blankState(path))
      runtimes.set(key, { generation: 0, previewSequence: 0, pollingGeneration: null })
    }
    return states.get(key)!
  }

  const current = computed(() => workspace.currentWorkspace?.path ? ensureState(workspace.currentWorkspace.path) : null)
  const busy = computed(() => current.value ? active(current.value) : false)
  const hasTask = computed(() => !!current.value && (busy.value || !!current.value.taskId))
  const runtime = (entry: SourceImportState) => runtimes.get(workspaceKey(entry.workspacePath))!
  const valid = (entry: SourceImportState, generation: number) => !disposed && runtime(entry).generation === generation

  function refreshCompleted(entry: SourceImportState) {
    if (entry.result && !entry.refreshed && current.value === entry && !disposed) {
      entry.refreshed = true
      // Workbench and file-tree subscribers already share this invalidation signal.
      workspace.incrementFileTreeVersion()
    }
  }
  watch(() => workspace.currentWorkspace?.path, path => {
    if (path) refreshCompleted(ensureState(path))
  }, { immediate: true, flush: 'sync' })

  function prepare(request: SourceImportOpenRequest = {}, once = false): boolean {
    const entry = current.value
    if (!entry || disposed) return false
    if (once && openedRequests.has(toRaw(request))) return false
    if (Object.keys(request).length === 0) return true
    if (active(entry) || entry.readingFiles) return false
    if (once) openedRequests.add(toRaw(request))
    const stateRuntime = runtime(entry)
    stateRuntime.generation++
    stateRuntime.previewSequence++
    clearTimeout(stateRuntime.timer)
    Object.assign(entry, blankState(entry.workspacePath, request))
    return true
  }

  function invalidatePreview() {
    const entry = current.value
    if (!entry || active(entry)) return
    runtime(entry).previewSequence++
    entry.preview = null
    entry.previewing = false
    entry.error = ''
  }

  async function readPreview(entry: SourceImportState, request: SourceImportRequest): Promise<SourceImportPreview | null> {
    const stateRuntime = runtime(entry)
    const generation = stateRuntime.generation
    const sequence = ++stateRuntime.previewSequence
    entry.previewing = true
    entry.error = ''
    try {
      const value = await SourceImportService.PreviewSourceImport(entry.workspacePath, request)
      if (!valid(entry, generation) || sequence !== stateRuntime.previewSequence || !sameDraft(request, entry.draft)) return null
      if (value.kind !== request.kind || !destinationValid(value.targetFolder, value.kind) || value.metadataPath !== `${value.targetFolder}/${collectionMetadata[value.kind]}`) throw new Error('来源预览返回了无法识别的保存位置，请重新检查')
      entry.preview = { ...value, files: value.files ?? [], warnings: value.warnings ?? [] }
      return entry.preview
    } catch (error) {
      if (valid(entry, generation) && sequence === stateRuntime.previewSequence) entry.error = `预览失败：${errorText(error)}`
      return null
    } finally {
      if (valid(entry, generation) && sequence === stateRuntime.previewSequence) entry.previewing = false
    }
  }

  async function preview(): Promise<SourceImportPreview | null> {
    const entry = current.value
    if (!entry || active(entry) || entry.readingFiles || disposed) return null
    const request = cloneRequest(entry.draft)
    entry.error = validate(request, entry.workspacePath)
    if (entry.error) return null
    return readPreview(entry, request)
  }

  async function poll(entry: SourceImportState): Promise<void> {
    const stateRuntime = runtime(entry)
    const generation = stateRuntime.generation
    if (!entry.taskId || disposed || stateRuntime.pollingGeneration === generation || (terminal(entry.phase) && entry.result)) return
    const taskId = entry.taskId
    clearTimeout(stateRuntime.timer)
    stateRuntime.pollingGeneration = generation
    try {
      const task = await TaskService.GetTask(taskId)
      if (!valid(entry, generation) || entry.taskId !== taskId) return
      if (!task || task.id !== taskId) throw new Error('找不到任务，结果尚未确认')
      entry.task = task
      if (!terminal(task.status)) {
        if (!['pending', 'running'].includes(task.status)) throw new Error('任务状态暂时无法识别')
        entry.phase = 'running'
        entry.error = ''
        stateRuntime.timer = setTimeout(() => { void poll(entry) }, POLL_INTERVAL)
        return
      }
      const result = await SourceImportService.GetSourceImportResult(entry.workspacePath, taskId)
      if (!valid(entry, generation) || entry.taskId !== taskId) return
      if (!result || result.taskId !== taskId) throw new Error('任务结果尚未确认')
      entry.result = { ...result, files: result.files ?? [], warnings: result.warnings ?? [], conflicts: result.conflicts ?? [] }
      entry.phase = task.status as SourceImportPhase
      entry.error = task.status === 'failed' ? (task.error || '任务未完成，已保存的文件会保留') : ''
      entry.cancelRequested = false
      if (entry.submitted) entry.submitted.ai.apiKey = ''
      refreshCompleted(entry)
    } catch (error) {
      if (valid(entry, generation)) {
        entry.phase = 'unknown'
        entry.error = `任务结果尚未确认：${errorText(error)}。请重新检查状态，不要重复导入。`
      }
    } finally {
      if (stateRuntime.pollingGeneration === generation) stateRuntime.pollingGeneration = null
    }
  }

  async function start(ai: SourceImportRequest['ai'] = { apiKey: '', baseURL: '', model: '' }): Promise<void> {
    const entry = current.value
    if (!entry || active(entry) || entry.readingFiles || disposed) return
    const request = cloneRequest(entry.draft)
    entry.error = validate(request, entry.workspacePath)
    if (entry.error) return
    const stateRuntime = runtime(entry)
    const generation = ++stateRuntime.generation
    clearTimeout(stateRuntime.timer)
    entry.phase = 'starting'
    entry.result = null
    entry.task = null
    entry.taskId = ''
    entry.cancelRequested = false
    entry.refreshed = false
    entry.completionEmitted = false
    const plan = await readPreview(entry, request)
    if (!valid(entry, generation)) return
    // Preview is read-only. A workspace switch before submission cancels this local action.
    if (!plan || current.value !== entry || !sameDraft(request, entry.draft)) {
      entry.phase = 'idle'
      return
    }
    request.name = plan.name
    request.targetFolder = plan.targetFolder
    request.ai = { apiKey: ai.apiKey ?? '', baseURL: ai.baseURL ?? '', model: ai.model ?? '' }
    entry.submitted = cloneRequest(request)
    try {
      const id = await SourceImportService.StartSourceImport(entry.workspacePath, request)
      if (!valid(entry, generation)) return
      if (!id) throw new Error('未收到任务编号')
      entry.taskId = id
      entry.phase = 'running'
      void poll(entry)
    } catch (error) {
      if (valid(entry, generation)) {
        entry.phase = 'unknown'
        entry.error = `未能确认任务是否已启动：${errorText(error)}。请先检查工作区，避免重复写入。`
      }
    }
  }

  async function cancel(): Promise<void> {
    const entry = current.value
    if (!entry?.taskId || !active(entry) || entry.cancelRequested || terminal(entry.task?.status ?? '')) return
    const generation = runtime(entry).generation
    entry.cancelRequested = true
    try {
      const sent = await TaskService.Cancel(entry.taskId)
      if (!valid(entry, generation)) return
      if (!sent) entry.cancelRequested = false
      await poll(entry)
    } catch (error) {
      if (valid(entry, generation)) {
        entry.cancelRequested = false
        entry.error = `取消请求尚未确认：${errorText(error)}`
      }
    }
  }

  async function checkStatus(): Promise<void> {
    if (current.value) await poll(current.value)
  }

  async function retry(ai?: SourceImportRequest['ai']): Promise<void> {
    const entry = current.value
    if (!entry?.result || !entry.submitted || !['failed', 'cancelled'].includes(entry.phase)) return
    entry.draft = cloneRequest(entry.submitted)
    entry.phase = 'idle'
    await start(ai)
  }

  function resetAfterInspection(): void {
    const entry = current.value
    if (!entry || entry.phase !== 'unknown' || entry.taskId || disposed) return
    const draft = cloneRequest(entry.draft)
    const stateRuntime = runtime(entry)
    stateRuntime.generation++
    stateRuntime.previewSequence++
    Object.assign(entry, blankState(entry.workspacePath), { draft })
  }

  async function addFiles(files: readonly File[]): Promise<void> {
    const entry = current.value
    if (!entry || active(entry) || entry.readingFiles || !files.length) return
    entry.error = ''
    if (files.length + entry.draft.files.length > 500) { entry.error = '每次最多选择 500 个文件'; return }
    if (files.some(file => file.size > FILE_LIMIT)) { entry.error = '单个文件不能超过 20 MiB'; return }
    const existingBytes = entry.draft.files.reduce((sum, file) => sum + file.contentBase64.length * 3 / 4 - (file.contentBase64.endsWith('==') ? 2 : file.contentBase64.endsWith('=') ? 1 : 0), 0)
    if (files.reduce((sum, file) => sum + file.size, existingBytes) > BATCH_LIMIT) { entry.error = '每次文件总大小不能超过 100 MiB'; return }
    const names = files.map(file => (file.webkitRelativePath || file.name).replace(/\\/g, '/'))
    if (names.some(name => name.split('/').some(segment => !safeFolderName(segment)))) { entry.error = '文件名包含无效路径，请重新选择'; return }
    const allNames = [...entry.draft.files.map(file => file.name), ...names]
    if (new Set(allNames).size !== allNames.length) { entry.error = '已选择同名文件，请先移除重复项'; return }
    const generation = runtime(entry).generation
    entry.readingFiles = true
    try {
      const selected: SourceImportFile[] = []
      for (let index = 0; index < files.length; index++) {
        const buffer = await fileBytes(files[index]!)
        if (!valid(entry, generation) || current.value !== entry) return
        if (buffer.byteLength > FILE_LIMIT) throw new Error('单个文件不能超过 20 MiB')
        const bytes = new Uint8Array(buffer)
        let binary = ''
        for (let offset = 0; offset < bytes.length; offset += 32768) binary += String.fromCharCode(...bytes.subarray(offset, offset + 32768))
        selected.push({ name: names[index]!, contentBase64: btoa(binary) })
      }
      entry.draft.files.push(...selected)
      entry.draft.sourceType = 'files'
      invalidatePreview()
    } catch (error) {
      if (valid(entry, generation)) entry.error = `文件读取失败：${errorText(error)}`
    } finally {
      if (valid(entry, generation)) entry.readingFiles = false
    }
  }

  function removeFile(index: number) {
    const entry = current.value
    if (!entry || active(entry) || entry.readingFiles) return
    entry.draft.files.splice(index, 1)
    invalidatePreview()
  }

  onScopeDispose(() => {
    disposed = true
    for (const stateRuntime of runtimes.values()) clearTimeout(stateRuntime.timer)
  })

  return { current, busy, hasTask, prepare, invalidatePreview, preview, start, cancel, retry, checkStatus, resetAfterInspection, addFiles, removeFile }
})

function fileBytes(file: File): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === 'function') return file.arrayBuffer()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => reader.result instanceof ArrayBuffer ? resolve(reader.result) : reject(new Error('无法读取文件内容'))
    reader.onerror = () => reject(reader.error ?? new Error('无法读取文件'))
    reader.onabort = () => reject(new Error('文件读取已取消'))
    reader.readAsArrayBuffer(file)
  })
}
