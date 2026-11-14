import { onScopeDispose, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import { ArchiveService, FileService, TrashService } from '@/api'
import type { FileNode } from '@/components/editor/FileTree.vue'
import { i18n } from '@/i18n'
import { useWorkspaceStore } from '@/stores/workspace'
import { isEntryPath, renamedEntryPath, windowsWorkspace } from '@/utils/filePaths'
import { normalizeNotePath } from '@/utils/navigation'
import { promptDialog } from './usePrompt'
import { confirmDialog } from './useConfirm'
import { useToast } from './useToast'
import type { EditorTab } from './useEditorDraft'

export function useEditorFileOperations(options: {
  tabs: Ref<EditorTab[]>
  activeTabIndex: Ref<number>
  workspacePath: ComputedRef<string | undefined>
  focusFolder: Ref<string | null>
  withExternalFileChanges: (paths: string[], operation: () => Promise<void>) => Promise<void>
  refresh: () => Promise<void>
  openFile: (node: FileNode) => Promise<void>
  onBegin?: () => void
}) {
  const store = useWorkspaceStore()
  const route = useRoute()
  const router = useRouter()
  const t = i18n.global.t
  const toast = useToast()
  const busy = ref(false)
  let generation = 0
  let disposed = false
  watch(options.workspacePath, () => { generation++ }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true })

  async function run(label: string, operation: (workspace: string, current: () => boolean) => Promise<void>) {
    const workspace = options.workspacePath.value
    if (!workspace || busy.value) return
    const version = generation
    const current = () => !disposed && version === generation && options.workspacePath.value === workspace && route.path === '/editor'
    if (!current()) return
    busy.value = true
    try { await operation(workspace, current) }
    catch (cause) {
      if (current()) toast.error(t('editor.fileOps.failed', { label, msg: cause instanceof Error ? cause.message : String(cause) }))
    } finally { busy.value = false }
  }

  function entryName(input: string | null) {
    if (input === null || !input.trim()) return null
    const name = input.trim()
    if (name === '.' || name === '..' || /[\\/:*?"<>|]/.test(name) || [...name].some(char => char.charCodeAt(0) < 32) || name.endsWith('.')) {
      throw new Error(t('editor.fileOps.invalidName'))
    }
    return name
  }

  function joined(parent: string, name: string) {
    parent = normalizeNotePath(parent).replace(/\/$/, '')
    return parent ? `${parent}/${name}` : name
  }

  async function create(parent: string, directory: boolean) {
    await run(directory ? t('editor.fileOps.createFolderLabel') : t('editor.fileOps.createFileLabel'), async (workspace, current) => {
      const name = entryName(await promptDialog({
        message: directory ? t('editor.fileOps.promptFolderName') : t('editor.promptFileName'),
        defaultValue: directory ? t('editor.fileOps.defaultFolderName') : t('editor.untitledDoc'),
      }))
      if (!name || !current()) return
      options.onBegin?.()
      const path = joined(parent, name)
      const node = directory ? await FileService.CreateFolder(workspace, path)
        : await FileService.CreateFile(workspace, path, `# ${name.replace(/\.(md|markdown)$/i, '')}\n\n`)
      if (!current()) return
      await options.refresh()
      if (!current()) return
      if (directory) {
        options.focusFolder.value = node?.path || path
        toast.success(t('editor.fileOps.createdFolder', { name }))
      } else if (node) await options.openFile(node as FileNode)
    })
  }

  async function handleRename(node: FileNode) {
    const source = { ...node, path: normalizeNotePath(node.path) }
    await run(t('editor.fileOps.renameLabel'), async (workspace, current) => {
      let name = entryName(await promptDialog({ message: source.isDir ? t('editor.fileOps.promptNewFolderName') : t('editor.fileOps.promptNewFileName'), defaultValue: source.name }))
      if (!name || !current()) return
      const extension = source.name.match(/\.[^.]+$/)?.[0]
      if (!source.isDir && extension && !/\.[^.]+$/.test(name)) name += extension
      if (name === source.name) return
      const destination = joined(source.path.slice(0, Math.max(0, source.path.lastIndexOf('/'))), name)
      const caseInsensitive = windowsWorkspace(workspace)
      const remap = (path: string) => renamedEntryPath(path, source.path, destination, source.isDir, caseInsensitive)
      const affected = options.tabs.value.filter(tab => isEntryPath(tab.path, source.path, source.isDir, caseInsensitive))
      const paths = [source.path, destination, ...affected.flatMap(tab => [tab.path, remap(tab.path)])]
      options.onBegin?.()
      await options.withExternalFileChanges(paths, async () => {
        if (!current()) throw new Error(t('editor.fileOps.workspaceChanged'))
        await FileService.RenameFile(workspace, source.path, name)
        if (!current()) return
        // Update identities before releasing the draft lock, so the final reload
        // and every subsequent autosave use the renamed file's location.
        for (const tab of affected) {
          tab.path = remap(tab.path)
          tab.name = tab.path.split('/').pop() || tab.path
        }
        store.renameFilePaths(source.path, destination, source.isDir)
        if (options.focusFolder.value) options.focusFolder.value = remap(options.focusFolder.value)
        if (source.isDir) options.focusFolder.value = destination
        const query: LocationQueryRaw = { ...route.query }
        for (const key of ['file', 'folder']) if (typeof query[key] === 'string') query[key] = remap(query[key])
        await router.replace({ path: route.path, query })
      })
      if (!current()) return
      await options.refresh()
      if (current()) toast.success(t('editor.fileOps.renamed', { name }))
    })
  }

  async function remove(node: FileNode, kind: 'delete' | 'archive' | 'trash') {
    const path = normalizeNotePath(node.path)
    const directory = node.isDir
    const label = kind === 'delete' ? t('editor.fileOps.deleteLabel') : kind === 'archive' ? t('editor.fileOps.archiveLabel') : t('editor.fileOps.trashLabel')
    await run(label, async (workspace, current) => {
      if (kind === 'delete' && !await confirmDialog({ message: t('editor.confirmDelete', { name: node.name }), danger: true })) return
      if (!current()) return
      const matches = (value: string) => isEntryPath(value, path, directory, windowsWorkspace(workspace))
      const affected = options.tabs.value.filter(tab => matches(tab.path))
      options.onBegin?.()
      await options.withExternalFileChanges([path, ...affected.map(tab => tab.path)], async () => {
        if (!current()) throw new Error(t('editor.fileOps.workspaceChanged'))
        const saved = new Map(affected.map(tab => [tab, tab.content]))
        if (kind === 'delete') await FileService.DeleteFile(workspace, path)
        else if (kind === 'archive') await ArchiveService.ArchiveFile(workspace, path)
        else await TrashService.MoveToTrash(workspace, path)
        if (!current()) return
        const active = options.tabs.value[options.activeTabIndex.value]
        const recovered = affected.filter(tab => tab.isDirty || tab.content !== saved.get(tab))
        // UI edits are locked during this operation. Still retain late plugin or
        // async edits if any slip through; never close an unpersisted draft.
        for (const tab of recovered) {
          tab.path = `Inbox/恢复草稿-${crypto.randomUUID()}-${tab.name}`
          tab.name = tab.path.split('/').pop()!
          const content = tab.content
          tab.isDirty = true
          try {
            await FileService.CreateFile(workspace, tab.path, content)
            tab.isDirty = tab.content !== content
            toast.warning(t('editor.fileOps.changedDuring', { label, path: tab.path }))
          } catch (cause) {
            toast.warning(t('editor.fileOps.keptAsDraft', { label, msg: cause instanceof Error ? cause.message : String(cause) }))
          }
        }
        if (!current()) return
        options.tabs.value = options.tabs.value.filter(tab => !affected.includes(tab) || recovered.includes(tab))
        options.activeTabIndex.value = active && options.tabs.value.includes(active) ? options.tabs.value.indexOf(active) : options.tabs.value.length - 1
        store.removeFilePaths(path, directory)
        for (const tab of recovered) store.openFile(tab.path)
        store.setActiveFile(options.tabs.value[options.activeTabIndex.value]?.path || null)
        const query: LocationQueryRaw = { ...route.query }
        if (typeof query.file === 'string' && matches(query.file)) query.file = store.activeFile || undefined
        if (typeof query.folder === 'string' && matches(query.folder)) query.folder = undefined
        if (options.focusFolder.value && matches(options.focusFolder.value)) options.focusFolder.value = null
        await router.replace({ path: route.path, query })
      })
      if (current()) await options.refresh()
    })
  }

  // 拖拽移动的受保护根目录：与 FileTree 的系统/模板收纳分组保持同一口径（根段匹配，大小写不敏感）
  const PROTECTED_ROOTS = new Set(['assets', 'templates', '.templates', '.trash', '.notevault'])
  const protectedRoot = (path: string) => PROTECTED_ROOTS.has(normalizeNotePath(path).split('/')[0]!.toLowerCase())

  async function handleMoveEntry(dragged: FileNode, target: FileNode) {
    if (!target.isDir) return
    const source = { ...dragged, path: normalizeNotePath(dragged.path).replace(/\/$/, '') }
    const destinationDir = normalizeNotePath(target.path).replace(/\/$/, '')
    // no-op：拖到自身、拖回所在目录，或把目录拖进自己的子树
    if (source.path === destinationDir) return
    const parent = source.path.includes('/') ? source.path.slice(0, source.path.lastIndexOf('/')) : ''
    if (parent === destinationDir) return
    if (source.isDir && (destinationDir + '/').startsWith(source.path + '/')) return
    // 系统目录既不能移动，也不能作为落点
    if (protectedRoot(source.path) || protectedRoot(destinationDir)) return
    const destination = joined(destinationDir, source.name)
    return run(t('editor.fileOps.moveLabel'), async (workspace, current) => {
      const caseInsensitive = windowsWorkspace(workspace)
      const remap = (path: string) => renamedEntryPath(path, source.path, destination, source.isDir, caseInsensitive)
      const affected = options.tabs.value.filter(tab => isEntryPath(tab.path, source.path, source.isDir, caseInsensitive))
      const paths = [source.path, destination, ...affected.flatMap(tab => [tab.path, remap(tab.path)])]
      options.onBegin?.()
      await options.withExternalFileChanges(paths, async () => {
        if (!current()) throw new Error(t('editor.fileOps.workspaceChanged'))
        await FileService.MoveEntry(workspace, source.path, destination)
        if (!current()) return
        // 与重命名同口径：先迁移打开的标签页路径，再释放草稿锁
        for (const tab of affected) {
          tab.path = remap(tab.path)
          tab.name = tab.path.split('/').pop() || tab.path
        }
        store.renameFilePaths(source.path, destination, source.isDir)
        if (options.focusFolder.value) options.focusFolder.value = remap(options.focusFolder.value)
        if (source.isDir) options.focusFolder.value = destination
        const query: LocationQueryRaw = { ...route.query }
        for (const key of ['file', 'folder']) if (typeof query[key] === 'string') query[key] = remap(query[key])
        await router.replace({ path: route.path, query })
      })
      if (!current()) return
      await options.refresh()
      if (current()) toast.success(t('editor.fileOps.movedTo', { path: destinationDir }))
    })
  }

  return {
    busy,
    handleNewFile: (parent: string) => create(parent, false),
    handleNewFolder: (parent: string) => create(parent, true),
    handleRename,
    handleDeleteFile: (node: FileNode) => remove(node, 'delete'),
    handleArchiveFile: (node: FileNode) => remove(node, 'archive'),
    handleTrashFile: (node: FileNode) => remove(node, 'trash'),
    handleMoveEntry,
  }
}
