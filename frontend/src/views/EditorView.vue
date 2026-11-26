<script setup lang="ts">
import { ref, shallowRef, computed, onMounted, onBeforeUnmount, onActivated, onDeactivated, watch, nextTick } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import {
  BookOpen,
  CalendarDays,
  FileText,
  Inbox as InboxIcon,
  Library,
  Plus,
  Rocket,
} from '@lucide/vue'
import EditorTabBar from '@/components/editor/EditorTabBar.vue'
import { isImeComposing } from '@/utils/ime'
import EditorContextDrawer from '@/components/editor/EditorContextDrawer.vue'
import type { OutlineItem } from '@/components/editor/EditorContextDrawer.vue'
import { getActiveEditor } from '@/plugins/editorBridge'
import EditorSummaryPanel from '@/components/editor/EditorSummaryPanel.vue'
import FileTree from '@/components/editor/FileTree.vue'
import type { FileNode } from '@/components/editor/FileTree.vue'
import MarkdownEditor from '@/components/editor/MarkdownEditor.vue'
import MarkdownPreview from '@/components/editor/MarkdownPreview.vue'
import DocumentPropertiesPanel from '@/components/editor/DocumentPropertiesPanel.vue'
import DistillKnowledgeModal from '@/components/workbench/DistillKnowledgeModal.vue'
import { buildContent, extractTags, splitFrontMatter } from '@/utils/frontmatter'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { toWorkspace, toWorkspaceList } from '@/utils/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useI18n } from 'vue-i18n'
import { FileService, WorkspaceService, TagService, SummarizeService, ExportService, CompileService } from '@/api'
import { arrayBufferToBase64, generateMarkdownImage } from '@/utils/image'
import { marked } from 'marked'
import { sanitizeHtml } from '@/utils/sanitize'
import { isLocalBaseURL } from '@/utils/localEndpoint'
import { toSplitPairs } from '@/utils/textDiff'
import { WIKI_LINK_RE, parseWikiLinkFile } from '@/utils/markdownText'
import { useToast } from '@/composables/useToast'
import { promptDialog } from '@/composables/usePrompt'
import { useEditorDraft } from '@/composables/useEditorDraft'
import { useEditorFileOperations } from '@/composables/useEditorFileOperations'
import { useEditorBacklinks } from '@/composables/useEditorBacklinks'
import { useEditorAnchorNavigation } from '@/composables/useEditorAnchorNavigation'
import { useEditorTabs } from '@/composables/useEditorTabs'
import { editorSession, registerEditorFlush } from '@/composables/useEditorSession'
import { findFileByName } from '@/utils/wikiLinkFiles'
import { useEditorLayout } from '@/composables/useEditorLayout'
import { distillationPaths, distillNoteTitle, isDistillableSource, type DistillationSource } from '@/utils/distillKnowledge'
import type { DistillRequest } from '@/api/workbench'

const workspaceStore = useWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const route = useRoute()
const router = useRouter()
const settingsStore = useSettingsStore()
const { t } = useI18n()
const toast = useToast()

// 状态
const fileTree = ref<FileNode[]>([])
const focusFolder = ref<string | null>(null)
const distillSource = ref<DistillationSource | null>(null)
const preparingDistillation = ref(false)
const { mainRef, panesRef, treeWidth, showTree, overlayTree, splitPercent, viewMode, effectiveViewMode, toggleTree, setTreeOpen, toggleViewMode, beginResize, adjust } = useEditorLayout()
let fileTreeRequest = 0

const sessionWorkspacePath = workspaceStore.currentWorkspace?.path
const currentWorkspace = computed(() => workspaceStore.currentWorkspace?.path === sessionWorkspacePath ? workspaceStore.currentWorkspace : null)

// 草稿层（useEditorDraft）要用 tabs，而标签层又要 saveTab / applyInProgress，
// 用一个延迟句柄打破先后顺序，避免两边的初始化顺序被固定死。
// 必须是 shallowRef 而不是普通变量：标签层的排队打开靠 watch(applyInProgress) 触发，
// 普通变量在 watch 建立后不会再被追踪，rename 结束时的排队请求就永远不执行。
const draftState = shallowRef<ReturnType<typeof useEditorDraft>>()

const {
  tabs, activeTabIndex, activeTab, findTabIndex,
  openFile, openFileByPath, switchToTab,
  removeTabAt, closeTab, closeOthers, closeRight, invalidatePendingOpens,
} = useEditorTabs({
  workspacePath: computed(() => currentWorkspace.value?.path),
  sessionWorkspacePath,
  route,
  router,
  workspace: {
    openFile: (path: string) => workspaceStore.openFile(path),
    closeFile: (path: string) => workspaceStore.closeFile(path),
    setActiveFile: (path: string | null) => workspaceStore.setActiveFile(path),
  },
  applyInProgress: () => draftState.value?.isApplyingExternalChanges.value ?? false,
  saveTab: (index: number) => draftState.value?.saveTab(index) ?? Promise.resolve(false),
})

// ---- Mybase 式知识库浏览模式：/editor 无 file 参数时，左侧文件树固定展开，右侧显示知识库概览 ----
const vaultBrowseMode = computed(() => route.path === '/editor' && typeof route.query.file !== 'string')
// 浏览模式强制显示左树（非 overlay）；编辑模式沿用用户偏好（可折叠、窄窗自动收起）
const effectiveShowTree = computed(() => vaultBrowseMode.value ? true : (showTree.value && !overlayTree.value))

// PARA 空间快捷卡片：目录名 → 递归文档数（从已加载的文件树统计）
interface VaultSpace { key: string; dir: string; icon: typeof BookOpen }
const vaultSpaces: VaultSpace[] = [
  { key: 'learning', dir: 'Learning', icon: BookOpen },
  { key: 'projects', dir: 'Projects', icon: Rocket },
  { key: 'resources', dir: 'Resources', icon: Library },
  { key: 'inbox', dir: 'Inbox', icon: InboxIcon },
  { key: 'daily', dir: 'Daily', icon: CalendarDays },
]

function countMarkdownNotes(children?: FileNode[]): number {
  if (!children) return 0
  let notes = 0
  for (const child of children) {
    if (child.isDir) notes += countMarkdownNotes(child.children)
    else if (/\.md$/i.test(child.name)) notes++
  }
  return notes
}

const spaceCounts = computed<Record<string, number>>(() => {
  const counts: Record<string, number> = {}
  for (const node of fileTree.value) {
    if (node.isDir) counts[node.name.toLowerCase()] = countMarkdownNotes(node.children)
  }
  return counts
})

// 点击空间卡片：在左树中展开并高亮该目录（重复点击同一目录可重新触发）
function focusSpace(dir: string) {
  focusFolder.value = null
  void nextTick(() => { focusFolder.value = dir })
}

const recentFiles = computed(() => workspaceStore.recentFiles.slice(0, 6))

// 计算属性
const canDistill = computed(() => !!activeTab.value && isDistillableSource(activeTab.value.path))

// 编辑器只显示正文（front matter 元数据不在正文区渲染）；
// 编辑/保存时透明地与 front matter 合并，文件中仍保留元数据（兼容 Obsidian / git）。
const fileContent = computed({
  get: () => splitFrontMatter(activeTab.value?.content || '').body,
  set: (val: string) => {
    if (isApplyingExternalChanges.value) return
    if (activeTab.value) {
      const parsed = splitFrontMatter(activeTab.value.content)
      // parsed.raw 以 `---\n...\n---\n` 结尾，直接拼接新正文
      activeTab.value.content = parsed.raw ? parsed.raw + val : val
      activeTab.value.isDirty = true
      scheduleAutoSave()
    }
  }
})

// 当前文档的 tags（从 front matter 解析，供属性栏展示/编辑）
const activeTags = computed<string[]>(() => {
  if (!activeTab.value) return []
  return extractTags(splitFrontMatter(activeTab.value.content))
})

function updateTags(newTags: string[]) {
  if (!activeTab.value || isApplyingExternalChanges.value) return
  const parsed = splitFrontMatter(activeTab.value.content)
  activeTab.value.content = buildContent(parsed, newTags)
  activeTab.value.isDirty = true
  scheduleAutoSave()
  invalidateTagCache()
}

const wordCount = computed(() => {
  const text = fileContent.value.trim()
  if (!text) return 0
  const chinese = (text.match(/[\u4e00-\u9fa5]/g) || []).length
  const english = (text.match(/[a-zA-Z]+/g) || []).length
  return chinese + english
})

const charCount = computed(() => fileContent.value.length)

// 标签服务带 30s TTL 缓存；文件变化后必须主动失效，避免标签页读到旧空结果。
async function invalidateTagCache() {
  if (!currentWorkspace.value?.path) return
  try {
    await TagService.InvalidateCache(currentWorkspace.value.path)
  } catch (e) {
    console.warn('Failed to invalidate tag cache:', e)
  }
}

// 加载文件树
async function loadFileTree() {
  const workspacePath = currentWorkspace.value?.path
  if (!workspacePath) return
  const request = ++fileTreeRequest
  try {
    const tree = await FileService.GetFileTree(workspacePath)
    if (currentWorkspace.value?.path === workspacePath && request === fileTreeRequest) fileTree.value = tree as FileNode[]
  } catch (e) {
    console.error('Failed to load file tree:', e)
  }
}

// ---- 草稿生命周期与冲突保护（自动保存/脏标记/fsnotify 冲突监听已抽到 useEditorDraft）----
const draft = useEditorDraft({
  tabs,
  activeTabIndex,
  workspacePath: computed(() => currentWorkspace.value?.path),
  autoSaveInterval: () => settingsStore.settings.autoSaveInterval,
  findTabIndex,
  onSaved: invalidateTagCache,
})
draftState.value = draft
const {
  isSaving, saveErrors, saveTab, saveCurrentTab, scheduleAutoSave,
  flushDirtyTab, flushAllTabs, withExternalFileChanges, isApplyingExternalChanges,
  conflictedPaths, diffModal, activeConflictPath,
  abandonDraftAndReload, saveDraftAsCopy, openConflictDiff,
  startConflictWatcher, dispose: disposeDraft,
} = draft

const unregisterFlush = registerEditorFlush(flushAllTabs)

async function openDistillModal() {
  const tab = activeTab.value
  const workspacePath = currentWorkspace.value?.path
  if (!tab || !workspacePath || !canDistill.value || preparingDistillation.value || isApplyingExternalChanges.value) return
  preparingDistillation.value = true
  try {
    // Synchronize the source before taking the immutable extraction snapshot.
    await withExternalFileChanges([tab.path], async () => undefined)
    if (currentWorkspace.value?.path !== workspacePath || activeTab.value !== tab || route.path !== '/editor') return
    if (conflictedPaths.value.has(tab.path)) throw new Error(t('editor.view.distillSourceConflict'))
    distillSource.value = { workspacePath, path: tab.path, title: distillNoteTitle(tab.name, tab.content), content: tab.content }
  } catch (cause) {
    if (currentWorkspace.value?.path === workspacePath) toast.warning(cause instanceof Error ? cause.message : String(cause))
  } finally {
    preparingDistillation.value = false
  }
}

async function persistDistillation(request: DistillRequest) {
  const source = distillSource.value
  if (!source || request.sourceFile !== source.path || currentWorkspace.value?.path !== source.workspacePath) {
    throw new Error(t('editor.view.distillSourceChanged'))
  }
  await withExternalFileChanges(distillationPaths(request), async () => {
    if (currentWorkspace.value?.path !== source.workspacePath) throw new Error(t('editor.view.workspaceChanged'))
    await workbenchStore.distillKnowledge(request)
  })
}

function onDistilled(path: string) {
  toast.success(t('editor.view.distilled', { path }))
  if (activeConflictPath.value) toast.warning(t('editor.view.distillDraftPending'))
}
watch(() => [activeTab.value?.path, activeTab.value?.isDirty, isSaving.value, saveErrors.value, activeConflictPath.value, wordCount.value, tabs.value.filter(tab => tab.isDirty).length], () => {
  if (workspaceStore.currentWorkspace?.path !== sessionWorkspacePath) return
  const tab = activeTab.value
  editorSession.workspacePath = currentWorkspace.value?.path || ''
  editorSession.path = tab?.path || ''
  editorSession.error = tab ? saveErrors.value[tab.path] || '' : ''
  editorSession.state = activeConflictPath.value ? 'conflict' : isSaving.value ? 'saving' : editorSession.error ? 'error' : tab?.isDirty ? 'dirty' : tab ? 'saved' : 'idle'
  editorSession.words = wordCount.value
  editorSession.dirtyCount = tabs.value.filter(item => item.isDirty).length
  editorSession.draftPath = tabs.value.find(item => item.isDirty)?.path || ''
}, { deep: true, immediate: true })
onBeforeRouteLeave(async () => {
  invalidatePendingOpens()
  if (!await flushAllTabs()) {
    toast.warning(t('editor.view.leaveWithUnsaved'))
    return false
  }
})

// 冲突对比改 Beyond Compare 式左右分栏：统一 diff 行 → 对齐行对
const diffPairs = computed(() => (diffModal.value ? toSplitPairs(diffModal.value.rows) : []))

// ---- 反向链接（提取与跳转已抽到 useEditorBacklinks）----
const { backlinks, loadBacklinks, openBacklink } = useEditorBacklinks({
  workspacePath: computed(() => currentWorkspace.value?.path),
  activeTabName: computed(() => activeTab.value?.name),
  activeTabPath: computed(() => activeTab.value?.path),
  activeTabIndex,
  fileTree,
  openFile,
})

// ---- 出链（本文 [[引用]] 的目标文档）----
// WIKI_LINK_RE / parseWikiLinkFile 与 MarkdownPreview 共用 utils/markdownText，
// 避免两处对 [[a#b^c|d]] 的拆解口径漂移（那会导致出链与点击跳转对不上）。
interface DrawerOutlink {
  path: string
  name: string
  exists: boolean
}

const outlinks = computed<DrawerOutlink[]>(() => {
  const content = fileContent.value
  if (!content) return []
  const seen = new Set<string>()
  const result: DrawerOutlink[] = []
  for (const match of content.matchAll(WIKI_LINK_RE)) {
    const file = parseWikiLinkFile(match[1]!)
    if (!file || seen.has(file)) continue
    seen.add(file)
    const node = findFileByName(fileTree.value, file)
    if (node) {
      result.push({ path: node.path, name: node.name.replace(/\.(md|markdown)$/i, ''), exists: true })
    } else {
      // 未创建的引用目标也列出（虚链），点击后由 open-path 的兜底逻辑处理
      result.push({ path: file.endsWith('.md') || file.endsWith('.canvas') ? file : `${file}.md`, name: file, exists: false })
    }
  }
  return result
})

const { busy: fileOperationsBusy, handleNewFile, handleNewFolder, handleRename, handleMoveEntry, handleDeleteFile, handleArchiveFile, handleTrashFile } = useEditorFileOperations({
  tabs, activeTabIndex, focusFolder,
  workspacePath: computed(() => currentWorkspace.value?.path),
  withExternalFileChanges,
  // 文件树新建：新文档打开时确保不在纯预览模式（新建是为了写）
  openFile: async (node) => { await openFile(node); ensureWritableMode() },
  onBegin: invalidatePendingOpens,
  refresh: async () => {
    await invalidateTagCache()
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
  },
})

// 切换视图模式

/**
 * 处理图片粘贴 / 拖拽 / 工具栏选择（同一管线）
 * 1. 立即在光标处插入"上传中..."占位文本（乐观更新）
 * 2. 上传到 assets/ 后替换为真实路径
 * 3. 失败时回退并提示
 */
async function handlePasteImage(payload: { file: File; insertText: (text: string) => void }) {
  await saveAndInsertImage(payload.file, payload.insertText)
}

async function saveAndInsertImage(file: File, insertText: (text: string) => void) {
  if (!currentWorkspace.value) return

  const fileName = file.name || 'pasted-image.png'
  const altText = fileName.replace(/\.[^.]+$/, '') || 'image'
  // 临时占位 Markdown 图片语法（alt 标记为"上传中"）
  const placeholder = `\n![${altText}](uploading-${Date.now()})\n`

  // 立即插入占位，让用户看到响应
  insertText(placeholder)

  try {
    const arrayBuffer = await file.arrayBuffer()
    const base64Data = arrayBufferToBase64(arrayBuffer)

    // 调用后端保存图片（data 参数为 base64 字符串，Wails 会序列化为 []byte）
    const imagePath = await FileService.SaveImage(
      currentWorkspace.value.path,
      fileName,
      base64Data,
    )

    // 把占位文本替换为真实 Markdown 图片链接
    if (activeTab.value) {
      const content = activeTab.value.content
      const realImage = generateMarkdownImage(imagePath as string, altText)
      // 替换最近一次插入的占位（更稳健：替换所有 uploading- 开头的链接）
      const updated = content.replace(/!\[[^\]]*\]\(uploading-\d+\)/, realImage.trimStart())
      if (updated !== content) {
        fileContent.value = updated
      } else {
        // 没匹配到占位，回退到追加
        fileContent.value = content + realImage
      }
    }
  } catch (e) {
    console.error('Failed to save pasted image:', e)
    // 回滚占位
    if (activeTab.value) {
      fileContent.value = activeTab.value.content.replace(/\n*!\[[^\]]*\]\(uploading-\d+\)\n*/g, '')
    }
    toast.error(t('editor.imagePasteFailed', { msg: (e as Error).message }))
  }
}

// ---- 锚点跳转（wiki-link / ?anchor= 引用 / 大纲）----
// 「等预览渲染完再滚」的两帧重试与 pendingAnchor 的清空规则收敛在 composable 内，
// 视图只负责接线；时序契约由 EditorView.anchorTiming.test.ts 锁定。
const { handleWikiLinkClick, handleAnchorCopy, openFileWithAnchor, scrollToAnchor } = useEditorAnchorNavigation({
  workspacePath: computed(() => currentWorkspace.value?.path),
  fileTree,
  activePath: computed(() => activeTab.value?.path),
  findTabIndex,
  openFile,
  openFileByPath,
  createLinkedNote: handleNewFileWithName,
  route,
  router,
})

// 新建指定名称的文件
async function handleNewFileWithName(fileName: string) {
  if (!currentWorkspace.value) return
  try {
    const node = await FileService.CreateFile(
      currentWorkspace.value.path,
      fileName,
      `# ${fileName.replace(/\.md$/, '')}\n\n`
    )
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
    if (node) {
      openFile(node as FileNode)
      ensureWritableMode()
    }
  } catch (e) {
    console.error('Failed to create file:', e)
  }
}

// 新建文档是为了写，不是为了读：纯预览模式的会话里打开刚创建的文档时
// 自动回到分屏（用户手动选过预览的既有文档不受影响，只干预"新建"这一次）。
function ensureWritableMode() {
  if (viewMode.value === 'preview') viewMode.value = 'split'
}

// ---- 右侧辅助抽屉（Context Drawer）：大纲 + 反向链接 ----
const drawerOpen = ref(false)
const drawerTab = ref<'outline' | 'backlinks' | 'outlinks'>('outline')
const userManuallyToggledDrawer = ref(false)
// Use the same Markdown parser as preview and count body lines, excluding frontmatter.
const outline = computed<OutlineItem[]>(() => {
  const items: OutlineItem[] = []
  let line = 0
  for (const token of marked.lexer(fileContent.value)) {
    if (token.type === 'heading') items.push({ level: token.depth, text: token.text, line })
    line += (token.raw.match(/\n/g) || []).length
  }
  return items
})

watch([effectiveViewMode, () => outline.value.length], ([mode, count]) => {
  if (userManuallyToggledDrawer.value) return
  drawerOpen.value = mode === 'preview' && count > 0
  if (drawerOpen.value) drawerTab.value = 'outline'
}, { immediate: true })

// 从新建入口（侧栏 createNewDoc）带来的 created 标记：纯预览会话下自动回到分屏
watch(() => route.query.created, (flag) => {
  if (flag === '1') ensureWritableMode()
}, { immediate: true })

function setDrawerOpen(open: boolean) {
  userManuallyToggledDrawer.value = true
  drawerOpen.value = open
}

function selectDrawerTab(tab: 'outline' | 'backlinks' | 'outlinks') {
  userManuallyToggledDrawer.value = true
  drawerTab.value = tab
}

function jumpToLine(line: number) {
  if (effectiveViewMode.value !== 'editor') {
    const index = outline.value.findIndex(item => item.line === line)
    // Only parsed top-level Markdown headings belong to this document's outline.
    const headings = mainRef.value?.querySelectorAll<HTMLElement>('.preview-pane .markdown-preview > :is(h1, h2, h3, h4, h5, h6)[data-markdown-heading="true"]')
    headings?.[index]?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    if (effectiveViewMode.value === 'preview') return
  }
  const view = getActiveEditor()
  if (!view) return
  const target = Math.max(0, Math.min(line, view.state.doc.lines - 1))
  const pos = view.state.doc.line(target + 1).from
  view.dispatch({ selection: { anchor: pos }, scrollIntoView: true })
  view.focus()
}

function openDrawerPath(path: string) {
  void openFileByPath(path)
}

// 反向链接的加载由 useEditorBacklinks 内部的 watch 负责；
// 锚点重试随 pendingAnchor 一起搬进了 useEditorAnchorNavigation。
watch(() => workspaceStore.activeFile, async (requestedPath) => {
  if (!requestedPath || route.path !== '/editor' || requestedPath === activeTab.value?.path) return
  await openFileByPath(requestedPath)
})
watch(() => workspaceStore.fileTreeVersion, () => {
  loadBacklinks()
})

// 工作台空间卡直达（?folder=Learning 等）：路由 query 变化时更新聚焦目录，
// 由 FileTree 展开祖先链并短暂高亮。keep-alive 下本组件不重挂载，
// query 变化只能靠 watcher 接住。
const newDocumentFolder = computed(() => focusFolder.value || (activeTab.value?.path.includes('/') ? activeTab.value.path.slice(0, activeTab.value.path.lastIndexOf('/')) : 'Inbox'))
watch(
  () => route.query.folder,
  (val) => {
    focusFolder.value = typeof val === 'string' && val.trim() ? val : null
  },
  { immediate: true },
)

// 文件拖拽支持
const isDragOver = ref(false)

function handleDragOver(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = true
}

function handleDragLeave(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = false
}

async function handleDrop(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = false

  if (!e.dataTransfer?.files || !currentWorkspace.value) return

  for (const file of Array.from(e.dataTransfer.files)) {
    const ext = file.name.toLowerCase().split('.').pop()
    if (ext !== 'md' && ext !== 'markdown') continue

    try {
      const text = await file.text()
      // 保存到工作区
      const fileName = file.name
      try {
      const node = await FileService.CreateFile(currentWorkspace.value.path, fileName, text)
      await invalidateTagCache()
        await loadFileTree()
        workspaceStore.incrementFileTreeVersion()
        if (node) {
          openFile(node as FileNode)
          ensureWritableMode()
        }
      } catch {
        // 文件已存在，直接打开
        const existingNode = findFileByName(fileTree.value, fileName)
        if (existingNode) openFile(existingNode)
      }
    } catch (err) {
      console.error('Failed to read dropped file:', err)
    }
  }
}

// ============ AI 总结 ============
const summary = ref('')
const summaryOpen = ref(false)
const isSummarizing = ref(false)

// ============ 知识编译（单篇）============
const isCompiling = ref(false)

async function handleCompile() {
  if (!activeTab.value) {
    toast.warning(t('editor.openNoteFirst'))
    return
  }
  // 编译流水线只处理 Inbox/ 下的笔记（与 CompileView 一致）
  if (activeTab.value.path.split('/')[0].toLowerCase() !== 'inbox') {
    toast.warning(t('editor.compileOnlyInbox'))
    return
  }
  const ai = settingsStore.settings.ai
  if (!isLocalBaseURL(ai.baseURL) && (!ai.apiKey || !ai.apiKey.trim())) {
    toast.warning(t('editor.compileNoKey'))
    return
  }
  // 先把未保存的编辑落盘，确保编译的是当前内容（后端按磁盘文件编译）
  if (activeTab.value.isDirty) {
    await saveTab(activeTabIndex.value)
  }
  isCompiling.value = true
  const oldIndex = activeTabIndex.value
  const oldPath = activeTab.value.path
  try {
    const result = (await CompileService.CompileNote(
      currentWorkspace.value!.path,
      oldPath,
      ai.apiKey,
      ai.baseURL,
      ai.model,
      ai.protocol,
    )) as { Dest?: string; SnapshotID?: string } | null
    if (!result || !result.Dest) {
      toast.error(t('editor.compileFailed', { msg: t('editor.compileEmptyResult') }))
      return
    }
    // 文件已移动到 Compiled/，关闭旧标签页并跟随到新位置，保持编辑器一致
    removeTabAt(oldIndex)
    await openFileByPath(result.Dest)
    await invalidateTagCache()
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
    toast.success(t('editor.compiled', { dest: result.Dest, snapshot: result.SnapshotID || '' }))
  } catch (e) {
    toast.error(t('editor.compileFailed', { msg: (e as Error).message }))
  } finally {
    isCompiling.value = false
  }
}

async function handleSummarize() {
  if (!activeTab.value) {
    toast.warning(t('editor.openNoteFirst'))
    return
  }
  const ai = settingsStore.settings.ai
  if (!isLocalBaseURL(ai.baseURL) && (!ai.apiKey || !ai.apiKey.trim())) {
    toast.warning(t('editor.apiKeyMissing'))
    return
  }
  isSummarizing.value = true
  summaryOpen.value = true
  summary.value = ''
  try {
    const result = await SummarizeService.Summarize(
      ai.apiKey,
      ai.baseURL,
      ai.model,
      ai.protocol,
      activeTab.value.content,
    )
    summary.value = result as string
  } catch (e) {
    summary.value = t('editor.summaryFailed', { msg: (e as Error).message })
    console.error('Summarize failed:', e)
  } finally {
    isSummarizing.value = false
  }
}

function insertSummaryToNote() {
  if (!activeTab.value || !summary.value) return
  const block = `\n\n## ${t('editor.summaryBlockTitle')}\n\n${summary.value.trim()}\n`
  const updated = activeTab.value.content + block
  fileContent.value = updated
  summaryOpen.value = false
  toast.success(t('editor.summaryInserted'))
}

// ============ 导出 ============
const isExporting = ref(false)

async function pickSavePath(defaultName: string, ext: string): Promise<string | null> {
  const runtime = await import('@wailsio/runtime')
  const result = await runtime.Dialogs.SaveFile({
    Title: t('editor.chooseExportLocation'),
    Filename: defaultName,
    Filters: [{ DisplayName: t('editor.fileFilter', { ext: ext.toUpperCase() }), Pattern: '*' + ext }],
  })
  if (!result) return null
  let p = Array.isArray(result) ? result[0] : result
  if (!p.toLowerCase().endsWith(ext)) p += ext
  return p
}

async function exportMarkdown() {
  if (!activeTab.value || !currentWorkspace.value) return
  const dest = await pickSavePath(activeTab.value.name, '.md')
  if (!dest) return
  isExporting.value = true
  try {
    await ExportService.ExportNoteMarkdown(
      currentWorkspace.value.path,
      activeTab.value.path,
      dest,
    )
    toast.success(t('editor.exportedMd', { path: dest }))
  } catch (e) {
    toast.error(t('editor.exportFailed', { msg: (e as Error).message }))
  } finally {
    isExporting.value = false
  }
}

async function exportSingleHTML() {
  if (!activeTab.value) return
  const content = activeTab.value.content
  const html = buildStandaloneHTML(activeTab.value.name, content)
  const dest = await pickSavePath(activeTab.value.name.replace(/\.md$/, '') + '.html', '.html')
  if (!dest) return
  isExporting.value = true
  try {
    await ExportService.SaveText(dest, html)
    toast.success(t('editor.exportedHtml', { path: dest }))
  } catch (e) {
    toast.error(t('editor.exportFailed', { msg: (e as Error).message }))
  } finally {
    isExporting.value = false
  }
}

// ---- 文件树右键导出/复制（Mybase 式：对任意节点操作，不要求先打开） ----
async function exportFileMarkdown(node: FileNode) {
  if (node.isDir || !currentWorkspace.value) return
  const dest = await pickSavePath(node.name, '.md')
  if (!dest) return
  isExporting.value = true
  try {
    await ExportService.ExportNoteMarkdown(currentWorkspace.value.path, node.path, dest)
    toast.success(t('editor.exportedMd', { path: dest }))
  } catch (e) {
    toast.error(t('editor.exportFailed', { msg: (e as Error).message }))
  } finally {
    isExporting.value = false
  }
}

async function exportFileHTML(node: FileNode) {
  if (node.isDir || !currentWorkspace.value) return
  const workspacePath = currentWorkspace.value.path
  let content: string
  try {
    content = await FileService.ReadFile(workspacePath, node.path)
  } catch (e) {
    toast.error(t('editor.view.readFailed', { msg: e instanceof Error ? e.message : String(e) }))
    return
  }
  const name = node.name.replace(/\.md$/i, '')
  const dest = await pickSavePath(name + '.html', '.html')
  if (!dest) return
  isExporting.value = true
  try {
    await ExportService.SaveText(dest, buildStandaloneHTML(name, content))
    toast.success(t('editor.exportedHtml', { path: dest }))
  } catch (e) {
    toast.error(t('editor.exportFailed', { msg: (e as Error).message }))
  } finally {
    isExporting.value = false
  }
}

async function handleCopyFile(node: FileNode) {
  if (node.isDir || !currentWorkspace.value) return
  const workspacePath = currentWorkspace.value.path
  const defaultName = t('editor.view.copyDefaultName', { name: node.name.replace(/\.md$/i, '') })
  const name = await promptDialog({ message: t('editor.view.copyPrompt', { name: node.name }), defaultValue: defaultName })
  if (!name?.trim()) return
  const fileName = name.trim().endsWith('.md') ? name.trim() : `${name.trim()}.md`
  const parent = node.path.includes('/') ? node.path.slice(0, node.path.lastIndexOf('/')) : ''
  const target = parent ? `${parent}/${fileName}` : fileName
  try {
    const content = await FileService.ReadFile(workspacePath, node.path)
    await FileService.CreateFile(workspacePath, target, content)
    workspaceStore.incrementFileTreeVersion()
    toast.success(t('editor.view.copied', { target }))
  } catch (e) {
    toast.error(t('editor.view.copyFailed', { msg: e instanceof Error ? e.message : String(e) }))
  }
}

// 把 Markdown 渲染为内联样式的独立 HTML 文件
function buildStandaloneHTML(title: string, markdown: string): string {  const body = sanitizeHtml(marked.parse(markdown) as string)
  return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(title)}</title>
<style>
  :root { color-scheme: light; }
  body { font-family: -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
         max-width: 800px; margin: 40px auto; padding: 0 20px; line-height: 1.8; color: #1a1a1a; }
  h1, h2, h3 { line-height: 1.3; }
  h1 { border-bottom: 2px solid #eaecef; padding-bottom: .3em; }
  h2 { border-bottom: 1px solid #eaecef; padding-bottom: .3em; }
  code { background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-family: "JetBrains Mono", Consolas, monospace; }
  pre { background: #f3f4f6; padding: 16px; border-radius: 8px; overflow-x: auto; }
  pre code { background: transparent; padding: 0; }
  blockquote { border-left: 4px solid #4f9cf0; margin: 0; padding: 8px 16px; background: #f8fafc; color: #555; }
  img { max-width: 100%; border-radius: 8px; }
  table { border-collapse: collapse; width: 100%; }
  th, td { border: 1px solid #ddd; padding: 8px 12px; text-align: left; }
  a { color: #2563eb; }
  .wiki-link { color: #2563eb; background: #eff6ff; padding: 1px 6px; border-radius: 4px; text-decoration: none; }
</style>
</head>
<body>
<article>
${body}
</article>
</body>
</html>`
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] || c))
}

function toggleSidebar() {
  if (typeof settingsStore.toggleSidebar === 'function') {
    settingsStore.toggleSidebar()
  }
}

function handleEditorKeydown(e: KeyboardEvent) {
  if (isImeComposing(e)) return
  if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'b') {
    e.preventDefault()
    toggleSidebar()
  }
}

// flushDirtyTab / conflictWatcher / dispose 均由 useEditorDraft 提供：
// keep-alive 下路由切换触发的是 deactivated 而非 unmount，flush 必须挂在
// onDeactivated 才能覆盖"切页面前最后 1 秒（一个 debounce 窗口）的编辑"；
// dispose 保留作真卸载时的兜底。注意 deactivated 时不能清 saveTimer：
// 组件仍保活，用户可能切回来继续编辑，定时器要照常工作。
onDeactivated(() => {
  window.removeEventListener('keydown', handleEditorKeydown)
  distillSource.value = null
  flushDirtyTab()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleEditorKeydown)
  unregisterFlush()
  disposeDraft()
})

/**
 * 打开请求的文件：优先 route.query.file（欢迎页「新建文档」经此跳转），
 * 其次 store 里的 activeFile。keep-alive 下 onMounted 只跑一次，
 * 第二次从欢迎页带 query 跳转必须由 watcher 接住，否则新文件静默不打开。
 */
async function openRequestedFile() {
  const requestedPath = route.query.file
  if (typeof requestedPath === 'string' && requestedPath.trim()) {
    await openFileWithAnchor(requestedPath, true)
  } else if (workspaceStore.activeFile && !route.query.folder) {
    await openFileByPath(workspaceStore.activeFile, true)
  }
}

// 同一个 query 值连续两次跳转（如重复打开同一文件）不会再触发 watch，
// 用 activated 钩子兜底：每次回到编辑器页都检查一次
onActivated(() => {
  window.addEventListener('keydown', handleEditorKeydown)
  void openRequestedFile()
})

// P3-9：file 与 anchor 一起监听——只改 anchor（已在目标文件上点别的引用）也要触发，
// 否则「当前文件恰好是引用目标」时 query.file 不变、跳转静默失效。
watch(
  () => [route.query.file, route.query.anchor] as const,
  ([file, anchor]) => {
    if (route.path !== '/editor') return
    const target = typeof file === 'string' ? file.trim() : ''
    const heading = typeof anchor === 'string' ? anchor.trim() : ''
    if (target) {
      // 摘除 anchor 引发的二次触发：文件已是当前标签且无锚点，无需重复打开
      if (!heading && target === activeTab.value?.path) return
      void openFileWithAnchor(target, true)
      return
    }
    if (heading) void scrollToAnchor(heading)
  },
)

onMounted(async () => {
  window.addEventListener('keydown', handleEditorKeydown)
  // 外部修改冲突保护：订阅后端 fsnotify 推送（蓝图专项 1，订阅在 useEditorDraft 内）
  startConflictWatcher()
  if (!currentWorkspace.value) {
    try {
      const ws = await WorkspaceService.GetCurrentWorkspace()
      if (ws) {
        workspaceStore.setCurrentWorkspace(toWorkspace(ws))
      }
    } catch (e) {
      console.error('Failed to get current workspace:', e)
    }
  }
  await loadFileTree()
  await openRequestedFile()
})

// 工作区变化时重新加载文件树并清空标签页
watch(() => currentWorkspace.value?.path, () => {
  invalidatePendingOpens()
  distillSource.value = null
  tabs.value = []
  activeTabIndex.value = -1
  loadFileTree()
})

// 文件树版本号变化时重新加载
watch(() => workspaceStore.fileTreeVersion, () => {
  loadFileTree()
})
</script>

<template>
  <div
    class="editor-view"
    :class="{ 'drag-over': isDragOver }"
    @dragover="handleDragOver"
    @dragleave="handleDragLeave"
    @drop="handleDrop"
  >
    <!-- 拖拽提示 -->
    <div
      v-if="isDragOver"
      class="drag-overlay"
    >
      <div class="drag-hint">
        <div class="drag-icon">
          📄
        </div>
        <p>{{ t('editor.dropHint') }}</p>
      </div>
    </div>
    <!-- 标签页栏 -->
    <EditorTabBar
      :tabs="tabs"
      :active-tab-index="activeTabIndex"
      :is-saving="isSaving"
      :active-tab="activeTab"
      :view-mode="effectiveViewMode"
      :tree-open="effectiveShowTree"
      :drawer-open="drawerOpen"
      :is-exporting="isExporting"
      :is-compiling="isCompiling"
      :is-distilling="preparingDistillation || isApplyingExternalChanges"
      @distill="openDistillModal"
      @switch-tab="switchToTab"
      @close-tab="closeTab"
      @close-others="closeOthers"
      @close-right="closeRight"
      @new-file="handleNewFile(newDocumentFolder)"
      @summarize="handleSummarize"
      @compile="handleCompile"
      @export-md="exportMarkdown"
      @export-html="exportSingleHTML"
      @save="saveCurrentTab"
      @toggle-view="toggleViewMode"
      @toggle-tree="toggleTree"
      @toggle-drawer="setDrawerOpen(!drawerOpen)"
    />

    <!-- 外部修改冲突横幅（蓝图专项 1）：有未保存草稿时禁止自动覆盖 -->
    <div
      v-if="activeConflictPath"
      class="conflict-banner"
      data-testid="conflict-banner"
    >
      <span class="conflict-text">
        ⚠️ {{ t('editor.conflict.banner', { name: activeConflictPath }) }}
      </span>
      <div class="conflict-actions">
        <button
          class="conflict-btn"
          data-testid="conflict-reload"
          @click="abandonDraftAndReload(activeConflictPath)"
        >
          {{ t('editor.conflict.reload') }}
        </button>
        <button
          class="conflict-btn"
          data-testid="conflict-copy"
          @click="saveDraftAsCopy(activeConflictPath)"
        >
          {{ t('editor.conflict.saveCopy') }}
        </button>
        <button
          class="conflict-btn"
          data-testid="conflict-diff"
          @click="openConflictDiff(activeConflictPath)"
        >
          {{ t('editor.conflict.viewDiff') }}
        </button>
      </div>
    </div>

    <!-- 编辑器主区域（方案 A：独享屏幕剩余宽幅） -->
    <div
      ref="mainRef"
      class="editor-main"
      :class="{ 'overlay-tree': overlayTree }"
    >
      <!-- 左侧文件树（Mybase 式左树右文：知识库浏览模式固定展开，编辑模式可折叠/拖宽） -->
      <div
        v-show="effectiveShowTree"
        class="file-tree-pane"
        :style="{ width: `${treeWidth}px` }"
        :inert="fileOperationsBusy || isApplyingExternalChanges ? true : undefined"
      >
        <FileTree
          :nodes="fileTree"
          :active-file-path="activeTab?.path"
          :focus-folder="focusFolder"
          :folder-display-names="settingsStore.settings.editor.folderDisplayNames"
          :collapsible="!vaultBrowseMode"
          @collapse-tree="setTreeOpen(false)"
          @open-file="openFile"
          @new-file="handleNewFile"
          @new-folder="handleNewFolder"
          @rename="handleRename"
          @delete="handleDeleteFile"
          @archive="handleArchiveFile"
          @trash="handleTrashFile"
          @copy="handleCopyFile"
          @move-entry="handleMoveEntry"
          @export-md="exportFileMarkdown"
          @export-html="exportFileHTML"
        />
      </div>
      <!-- 树宽拖拽手柄 -->
      <span
        v-if="effectiveShowTree"
        class="tree-resize-handle"
        role="separator"
        aria-orientation="vertical"
        :aria-label="t('editor.view.treeResizeAria')"
        @pointerdown="beginResize($event, 'tree')"
      />

      <!-- 编辑/预览区域 -->
      <div
        class="editor-content"
        :inert="isApplyingExternalChanges ? true : undefined"
      >
        <!-- 知识库浏览模式概览：无打开文件时作为「右文」默认页 -->
        <div
          v-if="!activeTab && vaultBrowseMode"
          class="vault-overview"
        >
          <header class="vault-hero">
            <div class="vault-hero-text">
              <p class="vault-eyebrow">
                KNOWLEDGE BASE
              </p>
              <h2>{{ t('editor.view.vault.title') }}</h2>
              <p class="vault-sub">
                {{ t('editor.view.vault.subtitle') }}
              </p>
            </div>
            <button
              class="vault-new-btn"
              data-testid="vault-overview-new"
              @click="handleNewFile(newDocumentFolder)"
            >
              <Plus :size="15" />
              {{ t('editor.newDoc') }}
            </button>
          </header>

          <section class="vault-section">
            <h3>{{ t('editor.view.vault.spaces') }}</h3>
            <div class="vault-space-grid">
              <button
                v-for="space in vaultSpaces"
                :key="space.key"
                class="vault-space-card"
                :data-testid="`vault-space-${space.key}`"
                :title="t('editor.view.vault.locateSpace', { dir: space.dir })"
                @click="focusSpace(space.dir)"
              >
                <component
                  :is="space.icon"
                  :size="16"
                  class="vault-space-icon"
                />
                <span class="vault-space-name">{{ t(`editor.view.space.${space.key}`) }}</span>
                <span class="vault-space-count">{{ t('editor.view.vault.noteCount', { count: spaceCounts[space.dir.toLowerCase()] || 0 }) }}</span>
              </button>
            </div>
          </section>

          <section
            v-if="recentFiles.length"
            class="vault-section"
          >
            <h3>{{ t('editor.view.vault.recent') }}</h3>
            <div class="vault-recent-list">
              <button
                v-for="file in recentFiles"
                :key="file.path"
                class="vault-recent-item"
                data-testid="vault-recent-file"
                :title="file.path"
                @click="openFileByPath(file.path)"
              >
                <FileText
                  :size="14"
                  class="vault-recent-icon"
                />
                <span class="vault-recent-name">{{ file.title }}</span>
                <span class="vault-recent-path">{{ file.path }}</span>
              </button>
            </div>
          </section>

          <footer class="vault-footer">
            <button
              class="vault-dashboard-link"
              data-testid="vault-overview-dashboard"
              @click="router.push('/vault')"
            >
              {{ t('editor.view.vault.dashboard') }}
            </button>
          </footer>
        </div>

        <div
          v-else-if="!activeTab"
          class="empty-state"
        >
          <div class="empty-icon">
            📝
          </div>
          <h3>{{ t('editor.emptyTitle') }}</h3>
          <p>{{ t('editor.emptyDesc') }}</p>
          <p
            v-if="tabs.length > 0"
            class="hint"
          >
            {{ t('editor.tabsOpen', { count: tabs.length }) }}
          </p>
        </div>

        <div
          v-else
          class="editor-with-backlinks"
        >
          <!-- 文档属性栏：tags 以 chip 形式展示/编辑（front matter 不再占正文首屏） -->
          <DocumentPropertiesPanel
            :tags="activeTags"
            :visible="!!activeTab"
            @update:tags="updateTags"
          />
          <div
            ref="panesRef"
            class="editor-panes"
          >
            <!-- 编辑器 -->
            <div
              v-show="effectiveViewMode !== 'preview'"
              class="pane editor-pane"
              :style="effectiveViewMode === 'split' ? { flex: `0 0 calc(${splitPercent}% - 3px)` } : {}"
            >
              <div class="pane-header">
                <span>{{ t('editor.editPane') }}</span>
                <span class="pane-stats">{{ t('editor.wordCount', { words: wordCount, chars: charCount }) }}</span>
              </div>
              <div class="pane-body">
                <MarkdownEditor
                  v-model="fileContent"
                  :document-id="`${currentWorkspace?.path}/${activeTab.path}`"
                  :can-distill="canDistill"
                  :is-distilling="preparingDistillation || isApplyingExternalChanges"
                  :readonly="isApplyingExternalChanges"
                  data-testid="editor-input"
                  @distill="openDistillModal"
                  @save="saveCurrentTab"
                  @paste-image="handlePasteImage"
                />
              </div>
            </div>
            <div
              v-if="effectiveViewMode === 'split'"
              class="pane-resizer"
              role="separator"
              :aria-label="t('editor.view.splitResizeAria')"
              aria-orientation="vertical"
              :aria-valuenow="splitPercent"
              aria-valuemin="30"
              aria-valuemax="70"
              tabindex="0"
              @pointerdown="beginResize($event, 'split')"
              @keydown.left.prevent="adjust('split', -1)"
              @keydown.right.prevent="adjust('split', 1)"
            />

            <!-- 预览 -->
            <div
              v-show="effectiveViewMode !== 'editor'"
              class="pane preview-pane"
            >
              <div class="pane-header">
                <span>{{ t('editor.previewPane') }}</span>
              </div>
              <div class="pane-body">
                <MarkdownPreview
                  :key="activeTab.path"
                  :content="fileContent"
                  :workspace-path="currentWorkspace?.path"
                  :current-file-name="activeTab?.name"
                  @wiki-link-click="handleWikiLinkClick"
                  @anchor-copy="handleAnchorCopy"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 右侧辅助抽屉（大纲 / 反向链接） -->
      <EditorContextDrawer
        :tab="drawerTab"
        :open="drawerOpen"
        :outline="outline"
        :backlinks="backlinks"
        :outlinks="outlinks"
        @update:tab="selectDrawerTab"
        @jump-line="jumpToLine"
        @open-path="openDrawerPath"
        @close="setDrawerOpen(false)"
      />

      <!-- AI 总结面板 -->
      <EditorSummaryPanel
        :open="summaryOpen"
        :summary="summary"
        :is-summarizing="isSummarizing"
        @close="summaryOpen = false"
        @insert="insertSummaryToNote"
      />
    </div>
    <DistillKnowledgeModal
      v-if="distillSource"
      :source="distillSource"
      :persist="persistDistillation"
      @saved="onDistilled"
      @close="distillSource = null"
    />
    <!-- 冲突对比弹窗 -->
    <div
      v-if="diffModal"
      class="conflict-diff-mask"
      @click.self="diffModal = null"
    >
      <div
        class="conflict-diff-modal"
        role="dialog"
        aria-modal="true"
      >
        <div class="diff-header">
          <span class="diff-title">{{ t('editor.conflict.diffTitle', { path: diffModal.path }) }}</span>
          <button
            class="diff-close"
            @click="diffModal = null"
          >
            ✕
          </button>
        </div>
        <div class="diff-legend">
          <span class="legend-side disk">{{ t('editor.conflict.legendDisk') }}</span>
          <span class="legend-side draft">{{ t('editor.conflict.legendDraft') }}</span>
        </div>
        <div class="diff-body">
          <div
            v-for="(pair, i) in diffPairs"
            :key="i"
            class="diff-row"
          >
            <span
              class="diff-cell"
              :class="{ removed: pair.left?.type === 'removed' }"
            >{{ pair.left?.text ?? '' }}</span>
            <span
              class="diff-cell"
              :class="{ added: pair.right?.type === 'added' }"
            >{{ pair.right?.text ?? '' }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.editor-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 编辑器主区域 */
.editor-main {
  position: relative;
  min-height: 0;
  flex: 1;
  display: flex;
  overflow: hidden;
}

/* 文件树面板 */
.file-tree-pane {
  width: 280px;
  border-right: 1px solid var(--border);
  background: var(--bg-sidebar);
  flex-shrink: 0;
  overflow: hidden;
}

/* 树宽拖拽手柄：贴在树窗格右缘 */
.tree-resize-handle {
  width: 5px;
  margin-left: -3px;
  cursor: col-resize;
  flex-shrink: 0;
  touch-action: none;
  z-index: 5;
}
.tree-resize-handle:hover { background: var(--accent); opacity: .35; }

.overlay-tree .file-tree-pane {
  position: absolute;
  inset: 0 auto 0 0;
  z-index: 20;
  box-shadow: var(--shadow-lg);
}
.overlay-tree :deep(.ctx-drawer) {
  position: absolute;
  inset: 0 0 0 auto;
  z-index: 19;
  max-width: calc(100% - 36px);
  background: var(--surface-panel, var(--bg-card));
  box-shadow: var(--shadow-lg);
}
.pane-resizer {
  flex: 0 0 6px;
  cursor: col-resize;
  touch-action: none;
  background: var(--bg-secondary);
  border-inline: 1px solid var(--border);
}
.pane-resizer:hover, .pane-resizer:focus-visible {
  background: var(--accent);
  outline: none;
}

/* 编辑内容区域 */
.editor-content {
  min-width: 0;
  flex: 1;
  display: flex;
  overflow: hidden;
}

/* ---- 知识库浏览模式概览（右文默认页） ---- */
.vault-overview {
  flex: 1;
  overflow-y: auto;
  padding: 28px 32px;
  color: var(--text-primary);
}
.vault-hero {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}
.vault-eyebrow { margin: 0 0 6px; font-size: 10px; letter-spacing: .12em; font-weight: 650; color: var(--text-muted); }
.vault-hero h2 { margin: 0 0 8px; font-size: 22px; font-weight: 600; letter-spacing: -.03em; }
.vault-sub { margin: 0; font-size: 12px; color: var(--text-muted); line-height: 1.6; }
.vault-new-btn {
  display: inline-flex; align-items: center; gap: 6px; padding: 8px 14px;
  border-radius: 7px; font-size: 12px; white-space: nowrap;
  color: var(--text-inverse, #fff); background: var(--accent); border: 1px solid var(--accent);
}
.vault-new-btn:hover { background: var(--accent-hover, var(--accent)); }
.vault-section { margin-bottom: 24px; }
.vault-section h3 { margin: 0 0 10px; font-size: 13px; font-weight: 600; color: var(--text-secondary); }
.vault-space-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 10px;
}
.vault-space-card {
  display: flex; align-items: center; gap: 8px; padding: 12px 14px;
  border: 1px solid var(--border); border-radius: 10px; background: var(--bg-card);
  color: var(--text-secondary); text-align: left;
  transition: background var(--transition-fast), border-color var(--transition-fast);
}
.vault-space-card:hover { background: var(--bg-hover); border-color: var(--border-accent, var(--accent)); color: var(--text-primary); }
.vault-space-icon { color: var(--accent); flex-shrink: 0; }
.vault-space-name { flex: 1; min-width: 0; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.vault-space-count { font-size: 10px; color: var(--text-muted); font-variant-numeric: tabular-nums; }
.vault-recent-list { display: flex; flex-direction: column; gap: 2px; }
.vault-recent-item {
  display: flex; align-items: center; gap: 8px; padding: 7px 10px;
  border: none; border-radius: 6px; background: transparent;
  color: var(--text-secondary); text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.vault-recent-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.vault-recent-icon { color: var(--text-muted); flex-shrink: 0; }
.vault-recent-name { flex-shrink: 0; font-size: 12px; max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.vault-recent-path { flex: 1; min-width: 0; font-size: 11px; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.vault-footer { padding-top: 4px; }
.vault-dashboard-link {
  padding: 4px 0; border: none; background: transparent;
  color: var(--text-muted); font-size: 12px; cursor: pointer;
  transition: color var(--transition-fast);
}
.vault-dashboard-link:hover { color: var(--accent); }

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  color: var(--text-muted);
}

.empty-icon {
  font-size: 48px;
  opacity: 0.5;
}

.empty-state h3 {
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0;
}

.empty-state p {
  font-size: var(--text-sm);
  margin: 0;
}

.empty-state .hint {
  font-size: var(--text-xs);
  color: var(--text-muted);
  opacity: 0.7;
}

/* 编辑/预览面板 */
.editor-panes {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.pane {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.editor-pane {
  border-right: 1px solid var(--border);
}

.pane-header {
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  background: var(--bg-window);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.pane-stats {
  font-weight: 400;
  text-transform: none;
  letter-spacing: 0;
}

.pane-body {
  flex: 1;
  overflow: hidden;
}

/* 编辑器+反向链接容器 */
.editor-with-backlinks {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 文件拖拽 */
.editor-view.drag-over {
  position: relative;
}

.drag-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  pointer-events: none;
}

.drag-hint {
  background: var(--bg-window);
  border: 2px dashed var(--accent);
  border-radius: var(--radius-lg);
  padding: var(--space-8) var(--space-12);
  text-align: center;
  color: var(--text-primary);
}

.drag-icon {
  font-size: 48px;
  margin-bottom: var(--space-3);
}

.drag-hint p {
  font-size: var(--text-lg);
  font-weight: 600;
  margin: 0;
}


/* 外部修改冲突横幅（蓝图专项 1） */
.conflict-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-4);
  background: rgba(245, 158, 11, 0.14);
  border-bottom: 1px solid rgba(245, 158, 11, 0.5);
  color: var(--text-primary);
  flex-shrink: 0;
}
.conflict-text {
  font-size: var(--text-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conflict-actions {
  display: flex;
  gap: var(--space-2);
  flex-shrink: 0;
}
.conflict-btn {
  padding: 4px 12px;
  border: 1px solid rgba(245, 158, 11, 0.6);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: background var(--transition-fast);
}
.conflict-btn:hover {
  background: rgba(245, 158, 11, 0.22);
}

/* 冲突对比弹窗 */
.conflict-diff-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
}
.conflict-diff-modal {
  width: min(1100px, 94vw);
  max-height: 82vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-window, #1e1f22);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}
.diff-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}
.diff-title {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.diff-close {
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: var(--text-sm);
}
/* 双栏表头：与 diff-row 的 1fr 1fr 栅格严格对齐 */
.diff-legend {
  display: grid;
  grid-template-columns: 1fr 1fr;
  border-bottom: 1px solid var(--border);
}
.legend-side {
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  text-align: center;
}
.legend-side.disk {
  color: #fca5a5;
  background: rgba(239, 68, 68, 0.08);
  border-right: 1px solid var(--border);
}
.legend-side.draft {
  color: #86efac;
  background: rgba(34, 197, 94, 0.08);
}
.diff-body {
  flex: 1;
  overflow: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}
.diff-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
}
.diff-cell {
  padding: 1px var(--space-3);
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--text-secondary);
}
.diff-cell:first-child {
  border-right: 1px solid var(--border);
}
.diff-cell.removed {
  background: rgba(239, 68, 68, 0.14);
  color: #fca5a5;
}
.diff-cell.added {
  background: rgba(34, 197, 94, 0.14);
  color: #86efac;
}
</style>
