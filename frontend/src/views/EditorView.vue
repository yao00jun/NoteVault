<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, onActivated, onDeactivated, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import EditorTabBar from '@/components/editor/EditorTabBar.vue'
import EditorBacklinks from '@/components/editor/EditorBacklinks.vue'
import EditorSummaryPanel from '@/components/editor/EditorSummaryPanel.vue'
import FileTree from '@/components/editor/FileTree.vue'
import type { FileNode } from '@/components/editor/FileTree.vue'
import MarkdownEditor from '@/components/editor/MarkdownEditor.vue'
import MarkdownPreview from '@/components/editor/MarkdownPreview.vue'
import DocumentPropertiesPanel from '@/components/editor/DocumentPropertiesPanel.vue'
import { buildContent, extractTags, splitFrontMatter } from '@/utils/frontmatter'
import { useWorkspaceStore } from '@/stores/workspace'
import { toWorkspace, toWorkspaceList } from '@/utils/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useI18n } from 'vue-i18n'
import { FileService, WorkspaceService, SearchService, TagService, ArchiveService, TrashService, SummarizeService, ExportService, CompileService } from '@bindings/github.com/notevault/notevault/index.js'
import { arrayBufferToBase64, generateMarkdownImage } from '@/utils/image'
import { marked } from 'marked'
import { sanitizeHtml } from '@/utils/sanitize'
import { isLocalBaseURL } from '@/utils/localEndpoint'
import { useToast } from '@/composables/useToast'
import { confirmDialog } from '@/composables/useConfirm'

const workspaceStore = useWorkspaceStore()
const route = useRoute()
const settingsStore = useSettingsStore()
const { t } = useI18n()
const toast = useToast()

// 标签页数据结构
interface Tab {
  path: string
  name: string
  content: string
  isDirty: boolean
  lastSavedAt: string
}

// 状态
const fileTree = ref<FileNode[]>([])
const tabs = ref<Tab[]>([])
const activeTabIndex = ref(-1)
const isSaving = ref(false)
const viewMode = ref<'split' | 'editor' | 'preview'>('split')
let saveTimer: ReturnType<typeof setTimeout> | null = null

// 计算属性
const activeTab = computed(() => {
  if (activeTabIndex.value >= 0 && activeTabIndex.value < tabs.value.length) {
    return tabs.value[activeTabIndex.value]
  }
  return null
})

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
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

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

  // 如果已经在标签页中，直接切换
  const existingIndex = findTabIndex(node.path)
  if (existingIndex >= 0) {
    activeTabIndex.value = existingIndex
    workspaceStore.openFile(node.path)
    return
  }

  // 读取文件内容并添加新标签页
  try {
    const content = await FileService.ReadFile(currentWorkspace.value!.path, node.path)
    tabs.value.push({
      path: node.path,
      name: node.name,
      content,
      isDirty: false,
      lastSavedAt: new Date().toLocaleTimeString(),
    })
    activeTabIndex.value = tabs.value.length - 1
    workspaceStore.openFile(node.path)
  } catch (e) {
    console.error('Failed to open file:', e)
  }
}

async function openFileByPath(filePath: string) {
  if (!currentWorkspace.value?.path) return
  try {
    const content = await FileService.ReadFile(currentWorkspace.value.path, filePath)
    const existingIndex = findTabIndex(filePath)
    if (existingIndex >= 0) {
      activeTabIndex.value = existingIndex
      workspaceStore.openFile(filePath)
      return
    }
    tabs.value.push({
      path: filePath,
      name: filePath.split(/[\\/]/).pop() || filePath,
      content,
      isDirty: false,
      lastSavedAt: new Date().toLocaleTimeString(),
    })
    activeTabIndex.value = tabs.value.length - 1
    workspaceStore.openFile(filePath)
  } catch (e) {
    console.error('Failed to open file:', e)
  }
}

// 切换标签页
function switchToTab(index: number) {
  if (index < 0 || index >= tabs.value.length) return
  activeTabIndex.value = index
}

// 关闭标签页
async function closeTab(index: number, event?: Event) {
  if (event) event.stopPropagation()

  const tab = tabs.value[index]
  if (!tab) return

  // 如果有未保存的更改，提示保存
  if (tab.isDirty) {
    const shouldSave = await confirmDialog({ message: t('editor.unsavedConfirm', { name: tab.name }) })
    if (shouldSave) {
      await saveTab(index)
    }
  }

  // 从标签页列表中移除
  tabs.value.splice(index, 1)

  // 调整当前激活的标签页索引
  if (tabs.value.length === 0) {
    activeTabIndex.value = -1
  } else if (index <= activeTabIndex.value) {
    activeTabIndex.value = Math.max(0, activeTabIndex.value - 1)
  }
}

// 保存指定标签页
async function saveTab(index: number) {
  const tab = tabs.value[index]
  if (!tab || !currentWorkspace.value) return

  isSaving.value = true
  try {
    await FileService.SaveFile(currentWorkspace.value.path, tab.path, tab.content)
    await invalidateTagCache()
    tab.isDirty = false
    tab.lastSavedAt = new Date().toLocaleTimeString()
  } catch (e) {
    console.error('Failed to save file:', e)
  } finally {
    isSaving.value = false
  }
}

// 保存当前标签页
async function saveCurrentTab() {
  if (activeTabIndex.value >= 0) {
    await saveTab(activeTabIndex.value)
  }
}

// 自动保存（debounce；间隔读用户设置，不再硬编码）
function scheduleAutoSave() {
  if (saveTimer) clearTimeout(saveTimer)
  const delay = Math.max(200, settingsStore.settings.autoSaveInterval || 500)
  saveTimer = setTimeout(() => {
    saveCurrentTab()
  }, delay)
}

// 新建文件
async function handleNewFile(parentPath: string) {
  const name = prompt(t('editor.promptFileName'), t('editor.untitledDoc'))
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
      alert(t('editor.fileExists'))
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
    alert(t('editor.archiveFailed', { msg: (e as Error).message }))
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
    alert(t('editor.moveToTrashFailed', { msg: (e as Error).message }))
  }
}

// 切换视图模式
function toggleViewMode() {
  if (viewMode.value === 'split') viewMode.value = 'editor'
  else if (viewMode.value === 'editor') viewMode.value = 'preview'
  else viewMode.value = 'split'
}

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
    alert(t('editor.imagePasteFailed', { msg: (e as Error).message }))
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

// 按文件名查找文件
function findFileByName(nodes: FileNode[], name: string): FileNode | null {
  for (const node of nodes) {
    const nodeName = node.name.replace(/\.md$/, '').replace(/\.markdown$/, '')
    if (nodeName === name || node.name === name) {
      return node
    }
    if (node.children) {
      const found = findFileByName(node.children, name)
      if (found) return found
    }
  }
  return null
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

// 反向链接
const backlinks = ref<{ path: string; name: string }[]>([])

async function loadBacklinks() {
  if (!currentWorkspace.value || !activeTab.value) {
    backlinks.value = []
    return
  }
  const currentName = activeTab.value.name.replace(/\.md$/, '').replace(/\.markdown$/, '')
  try {
    // 搜索包含 [[当前文档名]] 的文档
    const results = await SearchService.Search(currentWorkspace.value.path, `[[${currentName}]]`)
    backlinks.value = (Array.isArray(results) ? results : []).map((r: any) => ({
      path: r.path,
      name: r.title,
    })).filter((r: any) => r.path !== activeTab.value?.path)
  } catch (e) {
    console.error('Failed to load backlinks:', e)
    backlinks.value = []
  }
}

// 打开反向链接文档
function openBacklink(link: { path: string; name: string }) {
  // 在文件树中找到并打开
  const node = findFileByPath(fileTree.value, link.path)
  if (node) {
    openFile(node)
  }
}

function findFileByPath(nodes: FileNode[], path: string): FileNode | null {
  for (const node of nodes) {
    if (node.path === path) return node
    if (node.children) {
      const found = findFileByPath(node.children, path)
      if (found) return found
    }
  }
  return null
}

// 监听当前标签页变化，加载反向链接
watch(activeTabIndex, () => {
  loadBacklinks()
})
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
  if (!requestedPath) return
  const existingIndex = findTabIndex(requestedPath)
  if (existingIndex >= 0) {
    activeTabIndex.value = existingIndex
    return
  }
  await openFileByPath(requestedPath)
})
watch(() => workspaceStore.fileTreeVersion, () => {
  loadBacklinks()
})

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
    alert(t('editor.openNoteFirst'))
    return
  }
  const ai = settingsStore.settings.ai
  if (!isLocalBaseURL(ai.baseURL) && (!ai.apiKey || !ai.apiKey.trim())) {
    alert(t('editor.apiKeyMissing'))
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
  alert(t('editor.summaryInserted'))
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
    alert(t('editor.exportedMd', { path: dest }))
  } catch (e) {
    alert(t('editor.exportFailed', { msg: (e as Error).message }))
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
    alert(t('editor.exportedHtml', { path: dest }))
  } catch (e) {
    alert(t('editor.exportFailed', { msg: (e as Error).message }))
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
// flushDirtyTab：清掉挂起的自动保存定时器，并对未保存的当前标签页立即保存。
// keep-alive 下路由切换触发的是 deactivated 而非 unmount，flush 必须挂在
// onDeactivated 才能覆盖"切页面前最后 1 秒（一个 debounce 窗口）的编辑"；
// onBeforeUnmount 保留作真卸载时的兜底。注意 deactivated 时不能清 saveTimer：
// 组件仍保活，用户可能切回来继续编辑，定时器要照常工作。
function flushDirtyTab() {
  const tab = tabs.value[activeTabIndex.value]
  if (tab?.isDirty) {
    void saveTab(activeTabIndex.value)
  }
}
onDeactivated(flushDirtyTab)
onBeforeUnmount(() => {
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  flushDirtyTab()
})

// 打开请求的文件：优先 route.query.file（欢迎页"新建文档"经此跳转），
// 其次 store 里的 activeFile。keep-alive 下 onMounted 只跑一次，
// 第二次从欢迎页带 query 跳转必须由 watcher 接住，否则新文件静默不打开。
async function openRequestedFile() {
  const requestedPath = route.query.file
  if (typeof requestedPath === 'string' && requestedPath.trim()) {
    await openFileByPath(decodeURIComponent(requestedPath))
  } else if (workspaceStore.activeFile) {
    await openFileByPath(workspaceStore.activeFile)
  }
}

// 同一个 query 值连续两次跳转（如重复打开同一文件）不会再触发 watch，
// 用 activated 钩子兜底：每次回到编辑器页都检查一次
onActivated(() => {
  void openRequestedFile()
})

watch(() => route.query.file, (val) => {
  if (typeof val === 'string' && val.trim()) {
    void openFileByPath(decodeURIComponent(val))
  }
})

onMounted(async () => {
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
watch(() => currentWorkspace.value?.id, () => {
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
      :view-mode="viewMode"
      :is-exporting="isExporting"
      :is-compiling="isCompiling"
      @switch-tab="switchToTab"
      @close-tab="closeTab"
      @new-file="handleNewFile('')"
      @summarize="handleSummarize"
      @compile="handleCompile"
      @export-md="exportMarkdown"
      @export-html="exportSingleHTML"
      @save="saveCurrentTab"
      @toggle-view="toggleViewMode"
    />

    <!-- 编辑器主区域 -->
    <div class="editor-main">
      <!-- 左侧文件树 -->
      <div class="file-tree-pane">
        <FileTree
          :nodes="fileTree"
          :active-file-path="activeTab?.path"
          @open-file="openFile"
          @new-file="handleNewFile"
          @delete="handleDeleteFile"
          @archive="handleArchiveFile"
          @trash="handleTrashFile"
        />
      </div>

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
          <div class="editor-panes">
            <!-- 编辑器 -->
            <div
              v-if="viewMode !== 'preview'"
              class="pane editor-pane"
            >
              <div class="pane-header">
                <span>{{ t('editor.editPane') }}</span>
                <span class="pane-stats">{{ t('editor.wordCount', { words: wordCount, chars: charCount }) }}</span>
              </div>
              <div class="pane-body">
                <MarkdownEditor
                  v-model="fileContent"
                  data-testid="editor-input"
                  @save="saveCurrentTab"
                  @paste-image="handlePasteImage"
                />
              </div>
            </div>

            <!-- 预览 -->
            <div
              v-if="viewMode !== 'editor'"
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

/* 编辑内容区域 */
.editor-content {
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

</style>
