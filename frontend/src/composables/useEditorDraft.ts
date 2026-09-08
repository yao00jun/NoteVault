/**
 * useEditorDraft - 编辑器草稿生命周期：自动保存 + 外部修改冲突保护
 *
 * 职责（蓝图专项 1）：
 *  - 自动保存：按用户设置的间隔 debounce 保存当前标签页；
 *  - 冲突监听：订阅后端 fsnotify 的 workspace:file-changed 事件，
 *    打开的 Tab 无草稿 → 静默重载磁盘内容；
 *    有未保存草稿 → 绝不自动覆盖，记录冲突路径，由宿主弹横幅供三选一
 *    （放弃重载 / 另存副本 / LCS Diff 对比）。
 *
 * 渐进降级：事件订阅失败（非 Wails 环境/运行时未就绪）只 console.warn，
 * 不阻断编辑器主流程。
 */
import { computed, ref, type ComputedRef, type Ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { FileService } from '@/api'
import { diffLines, type DiffRow } from '@/utils/textDiff'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'

export interface EditorTab {
  path: string
  name: string
  content: string
  isDirty: boolean
  lastSavedAt: string
}

export function useEditorDraft(options: {
  tabs: Ref<EditorTab[]>
  activeTabIndex: Ref<number>
  /** 当前工作区绝对路径（未选择工作区时为 undefined） */
  workspacePath: ComputedRef<string | undefined>
  /** 自动保存 debounce 间隔（ms），每次调度时实时读取 */
  autoSaveInterval: () => number
  /** 由相对路径定位标签页下标（找不到返回 -1） */
  findTabIndex: (path: string) => number
  /** 保存成功后的副作用（如标签缓存失效），失败不阻断保存 */
  onSaved?: () => Promise<void> | void
}) {
  const { t } = useI18n()
  const toast = useToast()
  // A cached editor owns one workspace for its whole lifetime. A delayed
  // disposal or timer must never reinterpret its relative paths in another.
  const sessionWorkspacePath = options.workspacePath.value
  const workspacePath = () => options.workspacePath.value === sessionWorkspacePath ? sessionWorkspacePath : undefined

  const isSaving = ref(false)
  let activeSaves = 0
  const pendingSaves = new WeakMap<EditorTab, Promise<boolean>>()
  const saveErrors = ref<Record<string, string>>({})
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  const reloadVersions = new WeakMap<EditorTab, number>()
  const externalChangePaths = ref(new Set<string>())
  const isApplyingExternalChanges = computed(() => externalChangePaths.value.size > 0)

  // Reserve case aliases together: Windows may resolve differently cased paths
  // to the same Markdown file. Keep each tab's original path for disk access.
  function externalPathKey(path: string) {
    return path.replace(/\\/g, '/').toLowerCase()
  }

  function activeTab(): EditorTab | null {
    const i = options.activeTabIndex.value
    return i >= 0 && i < options.tabs.value.length ? options.tabs.value[i]! : null
  }

  // 保存指定标签页
  async function saveTab(index: number): Promise<boolean> {
    return persistTab(index, false)
  }

  async function persistTab(index: number, ownsExternalChange: boolean): Promise<boolean> {
    const tab = options.tabs.value[index]
    const wsPath = workspacePath()
    if (!tab || !wsPath || conflictedPaths.value.has(tab.path)) return false
    if (externalChangePaths.value.has(externalPathKey(tab.path)) && !ownsExternalChange) return false

    const pending = pendingSaves.get(tab)
    if (pending) {
      await pending
      if (wsPath !== options.workspacePath.value || !options.tabs.value.includes(tab) || conflictedPaths.value.has(tab.path)) return false
      if (externalChangePaths.value.has(externalPathKey(tab.path)) && !ownsExternalChange) return false
      return !tab.isDirty || persistTab(options.tabs.value.indexOf(tab), ownsExternalChange)
    }
    const saving = writeTab(tab, wsPath)
    pendingSaves.set(tab, saving)
    try { return await saving }
    finally { if (pendingSaves.get(tab) === saving) pendingSaves.delete(tab) }
  }

  async function writeTab(tab: EditorTab, wsPath: string) {
    const content = tab.content
    activeSaves++
    isSaving.value = true
    try {
      await FileService.SaveFile(wsPath, tab.path, content)
      if (wsPath !== options.workspacePath.value || !options.tabs.value.includes(tab)) return false
      tab.isDirty = tab.content !== content
      tab.lastSavedAt = new Date().toLocaleTimeString()
      delete saveErrors.value[tab.path]
      try { await options.onSaved?.() }
      catch (error) { console.warn('Saved file, but its derived cache could not refresh:', error) }
      return !tab.isDirty
    } catch (e) {
      if (wsPath === options.workspacePath.value && options.tabs.value.includes(tab)) saveErrors.value[tab.path] = e instanceof Error ? e.message : String(e)
      console.error('Failed to save file:', e)
      return false
    } finally {
      activeSaves--
      isSaving.value = activeSaves > 0
    }
  }

  // 保存当前标签页
  async function saveCurrentTab() {
    if (options.activeTabIndex.value >= 0) {
      await saveTab(options.activeTabIndex.value)
    }
  }

  // 自动保存（debounce；间隔实时读用户设置，设置变更即时生效）
  function scheduleAutoSave() {
    if (saveTimer) clearTimeout(saveTimer)
    const delay = Math.max(200, options.autoSaveInterval() || 500)
    saveTimer = setTimeout(() => {
      void saveCurrentTab()
    }, delay)
  }

  // ---- 外部修改冲突保护 ----
  // Git/Syncthing 拉取远端改动时 fsnotify 推 file-change。
  const conflictedPaths = ref<Set<string>>(new Set())
  const diffModal = ref<{ path: string; rows: DiffRow[] } | null>(null)
  let stopFileChangeSub: (() => void) | null = null

  function markConflict(path: string) {
    conflictedPaths.value = new Set([...conflictedPaths.value, path])
  }

  function onExternalFileChange(payload: { type?: string; path?: string } | null) {
    if (!workspacePath()) return
    if (!payload?.path || !['modify', 'create'].includes(payload.type ?? '')) return
    const rel = payload.path
    // A known backend mutation owns these files until its final disk reload.
    if (externalChangePaths.value.has(externalPathKey(rel))) return
    const idx = options.findTabIndex(rel)
    if (idx < 0) return
    const tab = options.tabs.value[idx]!
    // Atomic Markdown writes emit create; every open tab must see them.
    // Pause pending saves before asynchronously checking a dirty draft.
    if (tab.isDirty) markConflict(rel)
    void reloadTabFromDisk(idx)
  }

  async function reloadTabFromDisk(idx: number, discardDraft = false) {
    const tab = options.tabs.value[idx]
    const wsPath = workspacePath()
    if (!tab || !wsPath) return
    const content = tab.content
    const version = (reloadVersions.get(tab) ?? 0) + 1
    reloadVersions.set(tab, version)
    try {
      const disk = await FileService.ReadFile(wsPath, tab.path)
      if (wsPath !== options.workspacePath.value || !options.tabs.value.includes(tab) || reloadVersions.get(tab) !== version) return
      if (disk === tab.content) { clearConflict(tab.path); return }
      if (tab.content !== content || (tab.isDirty && !discardDraft)) {
        markConflict(tab.path)
        return
      }
      tab.content = disk
      tab.isDirty = false
      clearConflict(tab.path)
    } catch (e) {
      if (wsPath === options.workspacePath.value && options.tabs.value.includes(tab) && reloadVersions.get(tab) === version) {
        markConflict(tab.path)
        saveErrors.value[tab.path] = `无法读取文件最新内容：${e instanceof Error ? e.message : String(e)}`
      }
      console.error('[conflict] reload failed:', e)
    }
  }

  function clearConflict(path: string) {
    const next = new Set(conflictedPaths.value)
    next.delete(path)
    conflictedPaths.value = next
    delete saveErrors.value[path]
  }

  /** Flush and reserve every open file a backend Markdown operation may change. */
  async function withExternalFileChanges(paths: string[], operation: () => Promise<void>): Promise<void> {
    const wsPath = workspacePath()
    if (!wsPath) throw new Error('工作区已变化，请重新打开文档')
    if (isApplyingExternalChanges.value) throw new Error('上一项文档操作正在保存，请稍候')
    const affected = new Set(paths.map(externalPathKey))
    if (!affected.size) throw new Error('没有需要更新的文档')
    externalChangePaths.value = affected
    if (saveTimer) { clearTimeout(saveTimer); saveTimer = null }
    const originals = options.tabs.value.filter(tab => affected.has(externalPathKey(tab.path)))
    let started = false
    try {
      for (const tab of originals) {
        // A disk read begun before the reservation cannot replace newer content.
        reloadVersions.set(tab, (reloadVersions.get(tab) ?? 0) + 1)
        if (conflictedPaths.value.has(tab.path)) throw new Error(`请先处理文档冲突：${tab.path}`)
        if ((tab.isDirty || pendingSaves.has(tab)) && !await persistTab(options.tabs.value.indexOf(tab), true)) {
          throw new Error(`文档保存未完成，请先处理草稿：${tab.path}`)
        }
      }
      if (workspacePath() !== wsPath || originals.some(tab => !options.tabs.value.includes(tab))) {
        throw new Error('工作区或文档已变化，请重新打开沉淀窗口')
      }
      if (originals.some(tab => tab.isDirty || conflictedPaths.value.has(tab.path))) {
        throw new Error('文档仍有未保存的修改或冲突，请保存后重试')
      }
      started = true
      await operation()
    } finally {
      // Failed multi-file writes may still have changed a file. Reconcile before
      // releasing the save lock; dirty text stays a conflict, never overwritten.
      if (started && workspacePath() === wsPath) {
        for (const tab of options.tabs.value.filter(tab => affected.has(externalPathKey(tab.path)))) {
          await reloadTabFromDisk(options.tabs.value.indexOf(tab))
        }
      }
      externalChangePaths.value = new Set()
      const current = activeTab()
      if (current?.isDirty && !affected.has(externalPathKey(current.path))) scheduleAutoSave()
    }
  }

  const activeConflictPath = computed(() => {
    const active = activeTab()?.path
    if (active && conflictedPaths.value.has(active)) return active
    for (const p of conflictedPaths.value) {
      if (options.findTabIndex(p) >= 0) return p
    }
    return null
  })

  function abandonDraftAndReload(path: string) {
    const idx = options.findTabIndex(path)
    if (idx >= 0) void reloadTabFromDisk(idx, true)
  }

  // 保留本地草稿，另存副本（副本落在原目录，天然可检索）
  async function saveDraftAsCopy(path: string) {
    const idx = options.findTabIndex(path)
    const tab = options.tabs.value[idx]
    const wsPath = workspacePath()
    if (!tab || !wsPath) return
    const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
    const ext = path.toLowerCase().endsWith('.markdown') ? '.markdown' : '.md'
    const base = path.slice(0, path.length - ext.length)
    const copyRel = `${base}.冲突副本-${ts}${ext}`
    const copiedContent = tab.content
    try {
      await FileService.SaveFile(wsPath, copyRel, copiedContent)
      if (wsPath !== options.workspacePath.value || !options.tabs.value.includes(tab)) return
      // Only the copy owns our draft. Keep the external original authoritative;
      // newer typing stays conflicted until the user resolves it separately.
      if (tab.content === copiedContent) await reloadTabFromDisk(options.tabs.value.indexOf(tab), true)
      toast.success(t('editor.conflict.copySaved', { path: copyRel }))
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  // 查看对比：磁盘内容 vs 本地草稿
  async function openConflictDiff(path: string) {
    const wsPath = workspacePath()
    if (!wsPath) return
    try {
      const disk = await FileService.ReadFile(wsPath, path)
      const idx = options.findTabIndex(path)
      const draft = options.tabs.value[idx]?.content ?? ''
      diffModal.value = { path, rows: diffLines(disk, draft) }
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  // 订阅后端文件变更推送；返回取消订阅函数
  function startConflictWatcher() {
    try {
      stopFileChangeSub = Events.On('workspace:file-changed', (ev: { data?: { path?: string; type?: string } }) => {
        onExternalFileChange(ev?.data ?? null)
      })
    } catch (e) {
      console.warn('[conflict] failed to subscribe file-change:', e)
    }
  }

  function stopConflictWatcher() {
    stopFileChangeSub?.()
    stopFileChangeSub = null
  }

  // flushDirtyTab：清掉挂起的自动保存定时器，并对未保存的当前标签页立即保存。
  // keep-alive 下路由切换触发的是 deactivated 而非 unmount，flush 必须挂在
  // onDeactivated 才能覆盖"切页面前最后 1 秒（一个 debounce 窗口）的编辑"；
  // dispose 保留作真卸载时的兜底。注意 deactivated 时不能清 saveTimer：
  // 组件仍保活，用户可能切回来继续编辑，定时器要照常工作。
  function flushDirtyTab() {
    const tab = activeTab()
    if (tab?.isDirty) {
      void saveTab(options.activeTabIndex.value)
    }
  }

  async function flushAllTabs(): Promise<boolean> {
    if (isApplyingExternalChanges.value) return false
    if (saveTimer) { clearTimeout(saveTimer); saveTimer = null }
    let saved = true
    for (let index = 0; index < options.tabs.value.length; index++) {
      if (options.tabs.value[index]?.isDirty && !await saveTab(index)) saved = false
    }
    return saved
  }

  // 真卸载兜底：停订阅 + 清定时器 + 冲出最后草稿
  function dispose() {
    stopConflictWatcher()
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    flushDirtyTab()
  }

  return {
    isSaving,
    saveErrors,
    saveTab,
    saveCurrentTab,
    scheduleAutoSave,
    flushDirtyTab,
    flushAllTabs,
    withExternalFileChanges,
    isApplyingExternalChanges,
    conflictedPaths,
    diffModal,
    activeConflictPath,
    reloadTabFromDisk,
    clearConflict,
    abandonDraftAndReload,
    saveDraftAsCopy,
    openConflictDiff,
    startConflictWatcher,
    stopConflictWatcher,
    dispose,
  }
}
