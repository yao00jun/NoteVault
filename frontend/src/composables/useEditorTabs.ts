/**
 * useEditorTabs - 标签页集合：打开 / 切换 / 关闭 / 与路由同步
 *
 * 抽出来的理由不是行数，而是**索引不变式只该有一份实现**：
 * 「删掉第 i 个标签后 activeTabIndex 怎么变」原先在 closeTab 与「编译后标签跟随」
 * 两处各写了一遍（同样的三行 if/else），任何一处漏改就是关错标签。
 * 现在统一走 removeTabAt()。
 *
 * 另外两个容易被改坏的隐式契约：
 *  - **openFileVersion**：异步 ReadFile 返回时，用户可能已经切到别的文件。
 *    只有版本号仍是自己那一次、工作区没换、路由还在 /editor，才允许把结果写进 tabs。
 *  - **deferredOpen**：文件操作（重命名/移动）进行中会锁住编辑器，
 *    期间到达的打开请求要排队，等锁释放后只执行最新那一个——
 *    否则 rename 结束会把用户刚点开的另一个文件顶掉。
 */
import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import type { RouteLocationNormalizedLoaded, Router } from 'vue-router'
import { i18n } from '@/i18n'
import { FileService } from '@/api'
import { useToast } from '@/composables/useToast'
import type { FileNode } from '@/components/editor/FileTree.vue'
import type { EditorTab } from '@/composables/useEditorDraft'
import { normalizeNotePath } from '@/utils/navigation'
import { isEntryPath, windowsWorkspace } from '@/utils/filePaths'

export function useEditorTabs(options: {
  workspacePath: ComputedRef<string | undefined>
  /** 会话创建时的工作区路径（Windows 需要大小写不敏感比较） */
  sessionWorkspacePath: string | undefined
  route: RouteLocationNormalizedLoaded
  router: Router
  workspace: {
    openFile: (path: string) => void
    closeFile: (path: string) => void
    setActiveFile: (path: string | null) => void
  }
  /** 文件操作（重命名/移动）是否正占用编辑器；锁住期间打开请求排队 */
  applyInProgress: () => boolean
  /** 切换标签前落盘：由 useEditorDraft 提供，晚于本 composable 创建 */
  saveTab: (index: number) => Promise<boolean>
}) {
  const t = i18n.global.t
  const toast = useToast()

  const tabs = ref<EditorTab[]>([])
  const activeTabIndex = ref(-1)
  const activeTab = computed<EditorTab | null>(() => {
    const i = activeTabIndex.value
    return i >= 0 && i < tabs.value.length ? tabs.value[i]! : null
  })

  // 异步打开的代际号：只有最后一次请求能落地
  let openVersion = 0
  let deferredOpen: string | null = null

  function invalidatePendingOpens() {
    openVersion++
  }

  function findTabIndex(path: string): number {
    return tabs.value.findIndex((tab) =>
      isEntryPath(tab.path, path, false, windowsWorkspace(options.sessionWorkspacePath || '')),
    )
  }

  async function openFile(node: FileNode) {
    if (node.isDir) return
    // Canvas 白板是文档类型而非编辑器内容：直达画布视图
    if (node.name.toLowerCase().endsWith('.canvas')) {
      await options.router.push({ path: '/canvas', query: { file: node.path } })
      return
    }
    await openFileByPath(node.path)
  }

  async function openFileByPath(filePath: string, fromRoute = false) {
    const workspacePath = options.workspacePath.value
    if (!workspacePath || options.route.path !== '/editor') return
    if (options.applyInProgress()) {
      deferredOpen = filePath
      if (!fromRoute && options.route.query.file !== filePath) {
        void options.router.push({ path: '/editor', query: { ...options.route.query, file: filePath, folder: undefined } })
      }
      return
    }
    filePath = normalizeNotePath(filePath)
    const version = ++openVersion
    // 切走前先把当前草稿落盘（保存失败不阻断切换，错误由 saveErrors 呈现）
    if (activeTab.value?.isDirty && activeTab.value.path !== filePath) void options.saveTab(activeTabIndex.value)
    try {
      const existingIndex = findTabIndex(filePath)
      if (existingIndex >= 0) {
        activeTabIndex.value = existingIndex
        filePath = tabs.value[existingIndex]!.path
      } else {
        const content = await FileService.ReadFile(workspacePath, filePath)
        if (version !== openVersion || options.workspacePath.value !== workspacePath || options.route.path !== '/editor') return
        tabs.value.push({
          path: filePath,
          name: filePath.split('/').pop() || filePath,
          content,
          isDirty: false,
          lastSavedAt: new Date().toLocaleTimeString(),
        })
        activeTabIndex.value = tabs.value.length - 1
      }
      options.workspace.openFile(filePath)
      if (options.route.query.file !== filePath) {
        const location = { path: '/editor', query: { ...options.route.query, file: filePath, folder: undefined } }
        if (fromRoute) await options.router.replace(location)
        else await options.router.push(location)
      }
    } catch (e) {
      console.error('Failed to open file:', e)
      toast.error(t('editor.view.openFailed', { path: filePath, msg: e instanceof Error ? e.message : String(e) }))
    }
  }

  function switchToTab(index: number) {
    if (index < 0 || index >= tabs.value.length) return
    void openFileByPath(tabs.value[index]!.path)
  }

  /**
   * 从集合里摘掉第 index 个并修正 activeTabIndex。
   * 所有「关掉一个标签」的路径都必须走这里（含编译后标签跟随新文件）。
   */
  function removeTabAt(index: number): EditorTab | null {
    const removed = tabs.value[index]
    if (!removed) return null
    tabs.value.splice(index, 1)
    if (tabs.value.length === 0) activeTabIndex.value = -1
    else if (index <= activeTabIndex.value) activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
    return removed
  }

  async function closeTab(index: number, event?: Event) {
    if (event) event.stopPropagation()
    const tab = tabs.value[index]
    if (!tab) return
    if (tab.isDirty && !await options.saveTab(index)) {
      toast.warning(t('editor.view.saveUnfinished'))
      return
    }
    removeTabAt(index)
    options.workspace.closeFile(tab.path)
    if (options.route.query.file === tab.path) {
      options.workspace.setActiveFile(activeTab.value?.path || null)
      await options.router.replace({ path: '/editor', query: { ...options.route.query, file: activeTab.value?.path } })
    }
  }

  function closeOthers(index: number) {
    for (let i = tabs.value.length - 1; i >= 0; i--) {
      if (i !== index) void closeTab(i)
    }
  }

  function closeRight(index: number) {
    for (let i = tabs.value.length - 1; i > index; i--) {
      void closeTab(i)
    }
  }

  // 文件操作结束后执行排队中最新的打开请求。
  // 优先用当前 URL 的 file：同编辑器里的侧栏链接可能在锁住期间又改了一次路由。
  watch(options.applyInProgress, applying => {
    if (applying || !deferredOpen) return
    const pending = deferredOpen
    deferredOpen = null
    if (options.route.path !== '/editor') return
    void openFileByPath(typeof options.route.query.file === 'string' ? options.route.query.file : pending, true)
  })

  return {
    tabs,
    activeTabIndex,
    activeTab,
    findTabIndex,
    openFile,
    openFileByPath,
    switchToTab,
    removeTabAt,
    closeTab,
    closeOthers,
    closeRight,
    invalidatePendingOpens,
  }
}
