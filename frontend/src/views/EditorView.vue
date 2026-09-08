<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, onActivated, onDeactivated, watch, nextTick } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import EditorTabBar from '@/components/editor/EditorTabBar.vue'
import EditorBacklinks from '@/components/editor/EditorBacklinks.vue'
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
import { FileService, WorkspaceService, TagService, ArchiveService, TrashService, SummarizeService, ExportService, CompileService } from '@/api'
import { arrayBufferToBase64, generateMarkdownImage } from '@/utils/image'
import { marked } from 'marked'
import { sanitizeHtml } from '@/utils/sanitize'
import { isLocalBaseURL } from '@/utils/localEndpoint'
import { toSplitPairs } from '@/utils/textDiff'
import { useToast } from '@/composables/useToast'
import { confirmDialog } from '@/composables/useConfirm'
import { promptDialog } from '@/composables/usePrompt'
import { useEditorDraft, type EditorTab } from '@/composables/useEditorDraft'
import { useEditorBacklinks } from '@/composables/useEditorBacklinks'
import { editorSession, registerEditorFlush } from '@/composables/useEditorSession'
import { normalizeNotePath } from '@/utils/navigation'
import { findFileByName } from '@/utils/wikiLinkFiles'
import { useEditorLayout } from '@/composables/useEditorLayout'
import { distillationPaths, distillNoteTitle, isProjectMarkdown, type DistillationSource } from '@/utils/distillKnowledge'
import type { DistillRequest } from '@/api/workbench'

const workspaceStore = useWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const route = useRoute()
const router = useRouter()
const settingsStore = useSettingsStore()
const { t } = useI18n()
const toast = useToast()

// 标签页数据结构（字段定义与草稿/冲突逻辑同源，见 useEditorDraft）
type Tab = EditorTab

// 状态
const fileTree = ref<FileNode[]>([])
const tabs = ref<Tab[]>([])
const activeTabIndex = ref(-1)
const distillSource = ref<DistillationSource | null>(null)
const preparingDistillation = ref(false)
const { mainRef, panesRef, treeWidth, showTree, overlayTree, splitPercent, effectiveViewMode, toggleTree, toggleViewMode, beginResize, adjust } = useEditorLayout()
let openFileVersion = 0

// 计算属性
const activeTab = computed(() => {
  if (activeTabIndex.value >= 0 && activeTabIndex.value < tabs.value.length) {
    return tabs.value[activeTabIndex.value]
  }
  return null
})
const canDistill = computed(() => !!activeTab.value && isProjectMarkdown(activeTab.value.path))

// 编辑器只显示正文（front matter 元数据不在正文区渲染）；
// 编辑/保存时透明地与 front matter 合并，文件中仍保留元数据（兼容 Obsidian / git）。
const fileContent = computed({
  get: () => splitFrontMatter(activeTab.value?.content || '').body,
  set: (val: string) => {
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
  if (!activeTab.value) return
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
const sessionWorkspacePath = workspaceStore.currentWorkspace?.path
const currentWorkspace = computed(() => workspaceStore.currentWorkspace?.path === sessionWorkspacePath ? workspaceStore.currentWorkspace : null)

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
  if (!currentWorkspace.value?.path) return
  try {
    const tree = await FileService.GetFileTree(currentWorkspace.value.path)
    fileTree.value = tree as FileNode[]
  } catch (e) {
    console.error('Failed to load file tree:', e)
  }
}

// 查找文件是否已在标签页中
function findTabIndex(path: string): number {
  return tabs.value.findIndex((tab) => tab.path === path)
}

// 打开文件
async function openFile(node: FileNode) {
  if (node.isDir) return

  // Canvas 白板是文档类型而非编辑器内容：直达画布视图
  if (node.name.toLowerCase().endsWith('.canvas')) {
    router.push({ path: '/canvas', query: { file: node.path } })
    return
  }

  await openFileByPath(node.path)
}

async function openFileByPath(filePath: string, fromRoute = false) {
  const workspacePath = currentWorkspace.value?.path
  if (!workspacePath || route.path !== '/editor') return
  filePath = normalizeNotePath(filePath)
  const version = ++openFileVersion
  if (activeTab.value?.isDirty && activeTab.value.path !== filePath) void saveTab(activeTabIndex.value)
  try {
    const existingIndex = findTabIndex(filePath)
    if (existingIndex >= 0) {
      activeTabIndex.value = existingIndex
    } else {
      const content = await FileService.ReadFile(workspacePath, filePath)
      if (version !== openFileVersion || currentWorkspace.value?.path !== workspacePath || route.path !== '/editor') return
      tabs.value.push({
        path: filePath,
        name: filePath.split('/').pop() || filePath,
        content,
        isDirty: false,
        lastSavedAt: new Date().toLocaleTimeString(),
      })
      activeTabIndex.value = tabs.value.length - 1
    }
    workspaceStore.openFile(filePath)
    if (route.query.file !== filePath) {
      const location = { path: '/editor', query: { ...route.query, file: filePath, folder: undefined } }
      if (fromRoute) await router.replace(location)
      else await router.push(location)
    }
  } catch (e) {
    console.error('Failed to open file:', e)
    toast.error(`无法打开 ${filePath}：${e instanceof Error ? e.message : String(e)}`)
  }
}

// 切换标签页
function switchToTab(index: number) {
  if (index < 0 || index >= tabs.value.length) return
  void openFileByPath(tabs.value[index]!.path)
}

// ---- 草稿生命周期与冲突保护（自动保存/脏标记/fsnotify 冲突监听已抽到 useEditorDraft）----
const {
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
  abandonDraftAndReload,
  saveDraftAsCopy,
  openConflictDiff,
  startConflictWatcher,
  dispose: disposeDraft,
} = useEditorDraft({
  tabs,
  activeTabIndex,
  workspacePath: computed(() => currentWorkspace.value?.path),
  autoSaveInterval: () => settingsStore.settings.autoSaveInterval,
  findTabIndex,
  onSaved: invalidateTagCache,
})

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
    if (conflictedPaths.value.has(tab.path)) throw new Error('请先处理来源文档的保存错误或冲突，再沉淀知识。')
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
    throw new Error('来源文档或工作区已变化，请重新打开沉淀窗口')
  }
  await withExternalFileChanges(distillationPaths(request), async () => {
    if (currentWorkspace.value?.path !== source.workspacePath) throw new Error('工作区已变化，请重新打开文档')
    await workbenchStore.distillKnowledge(request)
  })
}

function onDistilled(path: string) {
  toast.success(`知识已保存到 ${path}`)
  if (activeConflictPath.value) toast.warning('文件已更新，另有编辑器草稿需要处理，请查看冲突提示。')
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
  openFileVersion++
  if (!await flushAllTabs()) {
    toast.warning('草稿尚未保存，请先处理保存错误或外部冲突再离开编辑器。')
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

// 关闭标签页
async function closeTab(index: number, event?: Event) {
  if (event) event.stopPropagation()

  const tab = tabs.value[index]
  if (!tab) return

  if (tab.isDirty && !await saveTab(index)) {
    toast.warning('保存未完成，草稿已保留；请处理保存错误或外部冲突。')
    return
  }

  // 从标签页列表中移除
  tabs.value.splice(index, 1)
  workspaceStore.closeFile(tab.path)

  // 调整当前激活的标签页索引
  if (tabs.value.length === 0) {
    activeTabIndex.value = -1
  } else if (index <= activeTabIndex.value) {
    activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
  }
  if (route.query.file === tab.path) {
    workspaceStore.setActiveFile(activeTab.value?.path || null)
    await router.replace({ path: '/editor', query: { ...route.query, file: activeTab.value?.path } })
  }
}

// 新建文件
async function handleNewFile(parentPath: string) {
  const name = await promptDialog({ message: t('editor.promptFileName'), defaultValue: t('editor.untitledDoc') })
  if (!name) return
  const fullPath = parentPath ? `${parentPath}/${name}` : name
  try {
    const node = await FileService.CreateFile(currentWorkspace.value!.path, fullPath, `# ${name.replace('.md', '')}\n\n`)
    await invalidateTagCache()
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
    if (node) openFile(node as FileNode)
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      toast.warning(t('editor.fileExists'))
    } else {
      console.error('Failed to create file:', e)
    }
  }
}

// 删除文件
async function handleDeleteFile(node: FileNode) {
  if (!(await confirmDialog({ message: t('editor.confirmDelete', { name: node.name }), danger: true }))) return
  try {
    await FileService.DeleteFile(currentWorkspace.value!.path, node.path)
    await invalidateTagCache()
    // 如果删除的文件在标签页中，关闭该标签页
    const tabIndex = findTabIndex(node.path)
    if (tabIndex >= 0) {
      tabs.value.splice(tabIndex, 1)
      if (tabs.value.length === 0) {
        activeTabIndex.value = -1
      } else if (tabIndex <= activeTabIndex.value) {
        activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
      }
    }
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
  } catch (e) {
    console.error('Failed to delete file:', e)
  }
}

// 归档文件
async function handleArchiveFile(node: FileNode) {
  try {
    await ArchiveService.ArchiveFile(currentWorkspace.value!.path, node.path)
    await invalidateTagCache()
    // 如果归档的文件在标签页中，关闭该标签页
    const tabIndex = findTabIndex(node.path)
    if (tabIndex >= 0) {
      tabs.value.splice(tabIndex, 1)
      if (tabs.value.length === 0) {
        activeTabIndex.value = -1
      } else if (tabIndex <= activeTabIndex.value) {
        activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
      }
    }
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
  } catch (e) {
    console.error('Failed to archive file:', e)
    toast.error(t('editor.archiveFailed', { msg: (e as Error).message }))
  }
}

// 移动到回收站
async function handleTrashFile(node: FileNode) {
  try {
    await TrashService.MoveToTrash(currentWorkspace.value!.path, node.path)
    await invalidateTagCache()
    // 如果删除的文件在标签页中，关闭该标签页
    const tabIndex = findTabIndex(node.path)
    if (tabIndex >= 0) {
      tabs.value.splice(tabIndex, 1)
      if (tabs.value.length === 0) {
        activeTabIndex.value = -1
      } else if (tabIndex <= activeTabIndex.value) {
        activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
      }
    }
    await loadFileTree()
    workspaceStore.incrementFileTreeVersion()
  } catch (e) {
    console.error('Failed to move to trash:', e)
    toast.error(t('editor.moveToTrashFailed', { msg: (e as Error).message }))
  }
}

// 切换视图模式

/**
 * 处理图片粘贴
 * 1. 立即在光标处插入"上传中..."占位文本（乐观更新）
 * 2. 上传到 assets/ 后替换为真实路径
 * 3. 失败时回退并提示
 */
async function handlePasteImage(payload: { file: File; insertText: (text: string) => void }) {
  if (!currentWorkspace.value) return

  const fileName = payload.file.name || 'pasted-image.png'
  const altText = fileName.replace(/\.[^.]+$/, '') || 'image'
  // 临时占位 Markdown 图片语法（alt 标记为"上传中"）
  const placeholder = `\n![${altText}](uploading-${Date.now()})\n`

  // 立即插入占位，让用户看到响应
  payload.insertText(placeholder)

  try {
    const arrayBuffer = await payload.file.arrayBuffer()
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

// 待跳转锚点：打开目标文件后用它滚动到对应 heading / 块
// - 当 wiki-link 指向的文件还没在标签页里时，先 openFile，加载完后 watch activeTab 触发滚动
// - file 为空时由 MarkdownPreview 自己处理同文件锚点跳转，不会进到这里
const pendingAnchor = ref<{ anchor: string; block: string } | null>(null)

// 处理 wiki-link 点击（接收结构化对象，支持锚点 / 块跳转）
async function handleWikiLinkClick(target: { file: string; anchor: string; block: string; raw: string }) {
  if (!currentWorkspace.value) return
  const { file, anchor, block, raw } = target

  // 同文件锚点：MarkdownPreview 自己已经处理了滚动，不会传到这层。
  // 但保险起见，如果 file 为空且 anchor/block 非空，记下 pending 让 watch 触发
  if (!file) {
    if (anchor || block) {
      pendingAnchor.value = { anchor, block }
      tryScrollToPendingAnchor()
    }
    return
  }

  // 跨文件：在文件树里找匹配
  const targetFile = findFileByName(fileTree.value, file)
  if (targetFile) {
    // 已在标签页 → 切换后立即滚动；否则 openFile 异步加载，watch 触发滚动
    const existing = findTabIndex(targetFile.path) >= 0
    if (anchor || block) {
      pendingAnchor.value = { anchor, block }
    }
    await openFile(targetFile)
    if (existing && (anchor || block)) {
      // 已打开：预览 DOM 早已渲染，同步尝试即可，失败就是锚点真不存在
      resolvePendingAnchor()
    }
  } else {
    // 找不到，询问是否创建新文档（用 file 名，不带锚点）
    if (await confirmDialog({ message: t('editor.createLinkDoc', { name: file }) })) {
      const fileName = file.endsWith('.md') ? file : file + '.md'
      await handleNewFileWithName(fileName)
    }
  }
}

/**
 * ¶ 复制块 / 标题链接后的回调。
 * 剪贴板不可用（非安全上下文或权限被拒）时也要有反馈，
 * 否则用户点了 ¶ 什么都没发生会以为是 bug。
 */
function handleAnchorCopy(info: { text: string; ok: boolean }) {
  if (info.ok) {
    toast.success(t('editor.linkCopied', { text: info.text }))
    return
  }
  // 剪贴板写不进去：文本打进 console 兜底，避免内容彻底丢失
  console.warn('[anchor-copy] clipboard unavailable, link text:', info.text)
  toast.error(t('editor.copyFallbackPrompt'), 6000)
}

/**
 * 尝试滚动到 pendingAnchor，失败即认为目标锚点不存在。
 * 必须清空 pending——否则它会一直挂着，下次切标签页时误触发跳转。
 */
function resolvePendingAnchor() {
  if (!pendingAnchor.value) return
  if (tryScrollToPendingAnchor()) return
  const { anchor, block } = pendingAnchor.value
  pendingAnchor.value = null
  const label = block ? `^${block}` : anchor
  if (label) {
    toast.warning(t('editor.anchorNotFound', { anchor: label }))
  }
}

// 在当前预览中尝试滚动到 pendingAnchor
// 失败时保留 pending，等 watch(activeTab) 或下个 tick 重试一次
function tryScrollToPendingAnchor(): boolean {
  if (!pendingAnchor.value) return false
  const { anchor, block } = pendingAnchor.value
  // 找到当前可见的预览容器
  const root = document.querySelector('.markdown-preview') as HTMLElement | null
  if (!root) return false
  let ok = false
  if (block) {
    const el = root.querySelector(`[data-block-id="${cssEscape(block)}"]`) as HTMLElement | null
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      ok = true
    }
  }
  if (!ok && anchor) {
    const slug = slugifyHeading(anchor)
    let el: HTMLElement | null = slug ? (root.querySelector(`#${cssEscape(slug)}`) as HTMLElement | null) : null
    if (!el) {
      const headings = Array.from(root.querySelectorAll('h1, h2, h3, h4, h5, h6'))
      for (const h of headings) {
        // 优先读 data-heading-text（预览注入 ¶ 后 textContent 会带 "¶"）
        const raw = (h as HTMLElement).dataset.headingText ?? (h.textContent || '').replace(/¶$/, '')
        if (raw.trim() === anchor.trim()) {
          el = h as HTMLElement
          break
        }
      }
    }
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      ok = true
    }
  }
  if (ok) {
    pendingAnchor.value = null
  }
  return ok
}

function slugifyHeading(text: string): string {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
}

function cssEscape(s: string): string {
  try {
    return CSS.escape(s)
  } catch {
    return s.replace(/["\\]/g, '\\$&')
  }
}

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
    if (node) openFile(node as FileNode)
  } catch (e) {
    console.error('Failed to create file:', e)
  }
}

// ---- 右侧辅助抽屉（Context Drawer）：大纲 + 反向链接 ----
const drawerOpen = ref(false)
const drawerTab = ref<'outline' | 'backlinks'>('outline')
// 大纲：解析当前活动 Tab 的 Markdown 标题树（H1~H6），记录行号供跳转
const outline = computed<OutlineItem[]>(() => {
  const tab = tabs.value[activeTabIndex.value]
  if (!tab) return []
  const items: OutlineItem[] = []
  let inCode = false
  tab.content.split('\n').forEach((line, i) => {
    if (/^\s*(```|~~~)/.test(line)) inCode = !inCode
    if (inCode) return
    const m = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line)
    if (m) items.push({ level: m[1].length, text: m[2].trim(), line: i })
  })
  return items
})

function jumpToLine(line: number) {
  const view = getActiveEditor()
  if (!view) return
  const target = Math.min(line, view.state.doc.lines - 1)
  const pos = view.state.doc.line(target + 1).from
  view.dispatch({ selection: { anchor: pos }, scrollIntoView: true })
  view.focus()
}

function openDrawerPath(path: string) {
  void openFileByPath(path)
}

// 监听当前标签页变化，加载反向链接（watch 在 useEditorBacklinks 内注册）
// 文件加载完成后尝试滚动到待跳转锚点
// 跨文件 [[note#heading]] 点击后：先 openFile 异步加载，加载完 activeTab 变化触发此 watch
watch(activeTab, () => {
  if (pendingAnchor.value && activeTab.value) {
    nextTick(() => {
      // 给 MarkdownPreview 一帧时间渲染 HTML
      requestAnimationFrame(() => {
        if (tryScrollToPendingAnchor()) return
        // 第一次失败（DOM 还没渲染完），再等一帧；第二帧仍失败就提示并放弃
        requestAnimationFrame(() => resolvePendingAnchor())
      })
    })
  }
})
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
const focusFolder = ref<string | null>(null)
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
        if (node) openFile(node as FileNode)
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
    tabs.value.splice(oldIndex, 1)
    if (tabs.value.length === 0) activeTabIndex.value = -1
    else if (oldIndex <= activeTabIndex.value) activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
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

// 把 Markdown 渲染为内联样式的独立 HTML 文件
function buildStandaloneHTML(title: string, markdown: string): string {
  const body = sanitizeHtml(marked.parse(markdown) as string)
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

// 初始化
// flushDirtyTab / conflictWatcher / dispose 均由 useEditorDraft 提供：
// keep-alive 下路由切换触发的是 deactivated 而非 unmount，flush 必须挂在
// onDeactivated 才能覆盖"切页面前最后 1 秒（一个 debounce 窗口）的编辑"；
// dispose 保留作真卸载时的兜底。注意 deactivated 时不能清 saveTimer：
// 组件仍保活，用户可能切回来继续编辑，定时器要照常工作。
onDeactivated(() => { distillSource.value = null; flushDirtyTab() })
onBeforeUnmount(() => {
  unregisterFlush()
  disposeDraft()
})

// 打开请求的文件：优先 route.query.file（欢迎页"新建文档"经此跳转），
// 其次 store 里的 activeFile。keep-alive 下 onMounted 只跑一次，
// 第二次从欢迎页带 query 跳转必须由 watcher 接住，否则新文件静默不打开。
async function openRequestedFile() {
  const requestedPath = route.query.file
  if (typeof requestedPath === 'string' && requestedPath.trim()) {
    await openFileByPath(requestedPath, true)
  } else if (workspaceStore.activeFile && !route.query.folder) {
    await openFileByPath(workspaceStore.activeFile, true)
  }
}

// 同一个 query 值连续两次跳转（如重复打开同一文件）不会再触发 watch，
// 用 activated 钩子兜底：每次回到编辑器页都检查一次
onActivated(() => {
  void openRequestedFile()
})

watch(() => route.query.file, (val) => {
  if (route.path === '/editor' && typeof val === 'string' && val.trim()) {
    void openFileByPath(val, true)
  }
})

onMounted(async () => {
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
  openFileVersion++
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
      :tree-open="showTree"
      :is-exporting="isExporting"
      :is-compiling="isCompiling"
      :is-distilling="preparingDistillation || isApplyingExternalChanges"
      @distill="openDistillModal"
      @switch-tab="switchToTab"
      @close-tab="closeTab"
      @new-file="handleNewFile(newDocumentFolder)"
      @summarize="handleSummarize"
      @compile="handleCompile"
      @export-md="exportMarkdown"
      @export-html="exportSingleHTML"
      @save="saveCurrentTab"
      @toggle-view="toggleViewMode"
      @toggle-tree="toggleTree"
      @toggle-drawer="drawerOpen = !drawerOpen"
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

    <!-- 编辑器主区域 -->
    <div
      ref="mainRef"
      class="editor-main"
      :class="{ 'overlay-tree': overlayTree }"
    >
      <!-- 右侧辅助抽屉（大纲 / 反向链接） -->
      <EditorContextDrawer
        v-model:tab="drawerTab"
        :open="drawerOpen"
        :outline="outline"
        :backlinks="backlinks"
        @jump-line="jumpToLine"
        @open-path="openDrawerPath"
        @close="drawerOpen = false"
      />
      <!-- 左侧文件树 -->
      <div
        v-show="showTree"
        class="file-tree-pane"
        :style="{ width: `${treeWidth}px` }"
      >
        <FileTree
          :nodes="fileTree"
          :active-file-path="activeTab?.path"
          :focus-folder="focusFolder"
          @open-file="openFile"
          @new-file="handleNewFile"
          @delete="handleDeleteFile"
          @archive="handleArchiveFile"
          @trash="handleTrashFile"
        />
      </div>
      <div
        v-if="showTree && !overlayTree"
        class="pane-resizer"
        role="separator"
        aria-label="调整文件目录宽度"
        aria-orientation="vertical"
        :aria-valuenow="treeWidth"
        aria-valuemin="180"
        aria-valuemax="360"
        tabindex="0"
        @pointerdown="beginResize($event, 'tree')"
        @keydown.left.prevent="adjust('tree', -1)"
        @keydown.right.prevent="adjust('tree', 1)"
      />

      <!-- 编辑/预览区域 -->
      <div class="editor-content">
        <div
          v-if="!activeTab"
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
              aria-label="调整编辑与预览比例"
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
                  :content="fileContent"
                  :workspace-path="currentWorkspace?.path"
                  :current-file-name="activeTab?.name"
                  @wiki-link-click="handleWikiLinkClick"
                  @anchor-copy="handleAnchorCopy"
                />
              </div>
            </div>
          </div>

          <!-- 反向链接面板 -->
          <EditorBacklinks
            :backlinks="backlinks"
            @open="openBacklink"
          />
        </div>
      </div>

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
  width: 240px;
  border-right: 1px solid var(--border);
  background: var(--bg-sidebar);
  flex-shrink: 0;
  overflow: hidden;
}

.overlay-tree .file-tree-pane {
  position: absolute;
  inset: 0 auto 0 0;
  z-index: 20;
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
