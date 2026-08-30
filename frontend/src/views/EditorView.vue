<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { FileText, Plus, X, Save, Columns, Edit3, Eye, Sparkles, Download, FileDown, FileCode, Loader2 } from 'lucide-vue-next'
import FileTree from '@/components/editor/FileTree.vue'
import type { FileNode } from '@/components/editor/FileTree.vue'
import MarkdownEditor from '@/components/editor/MarkdownEditor.vue'
import MarkdownPreview from '@/components/editor/MarkdownPreview.vue'
import DocumentPropertiesPanel from '@/components/editor/DocumentPropertiesPanel.vue'
import { buildContent, extractTags, splitFrontMatter } from '@/utils/frontmatter'
import { useWorkspaceStore } from '@/stores/workspace'
import { useSettingsStore } from '@/stores/settings'
import { useI18n } from 'vue-i18n'
import { FileService, WorkspaceService, SearchService, TagService, ArchiveService, TrashService, SummarizeService, ExportService } from '@bindings/github.com/notevault/notevault/index.js'
import { arrayBufferToBase64, generateMarkdownImage } from '@/utils/image'
import { marked } from 'marked'

const workspaceStore = useWorkspaceStore()
const route = useRoute()
const settingsStore = useSettingsStore()
const { t } = useI18n()

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
    const shouldSave = confirm(t('editor.unsavedConfirm', { name: tab.name }))
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

// 自动保存（debounce）
function scheduleAutoSave() {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    saveCurrentTab()
  }, 1000)
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
  if (!confirm(t('editor.confirmDelete', { name: node.name }))) return
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

// 处理 wiki-link 点击
async function handleWikiLinkClick(link: string) {
  if (!currentWorkspace.value) return
  // 尝试在文件树中找到匹配的文件
  const targetFile = findFileByName(fileTree.value, link)
  if (targetFile) {
    openFile(targetFile)
  } else {
    // 如果找不到，询问是否创建新文档
    if (confirm(t('editor.createLinkDoc', { name: link }))) {
      const fileName = link.endsWith('.md') ? link : link + '.md'
      handleNewFileWithName(fileName)
    }
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

async function handleSummarize() {
  if (!activeTab.value) {
    alert(t('editor.openNoteFirst'))
    return
  }
  const ai = settingsStore.settings.ai
  if (!ai.apiKey || !ai.apiKey.trim()) {
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
  const body = marked.parse(markdown) as string
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
onMounted(async () => {
  if (!currentWorkspace.value) {
    try {
      const ws = await WorkspaceService.GetCurrentWorkspace()
      if (ws) {
        workspaceStore.setCurrentWorkspace(ws as any)
      }
    } catch (e) {
      console.error('Failed to get current workspace:', e)
    }
  }
  await loadFileTree()

  const requestedPath = route.query.file
  if (typeof requestedPath === 'string' && requestedPath.trim()) {
    await openFileByPath(decodeURIComponent(requestedPath))
  } else if (workspaceStore.activeFile) {
    await openFileByPath(workspaceStore.activeFile)
  }
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
    <div class="tab-bar">
      <div class="tabs-container">
        <div
          v-for="(tab, index) in tabs"
          :key="tab.path"
          class="tab-item"
          :data-testid="`tab-${tab.name}`"
          :class="{ active: index === activeTabIndex }"
          @click="switchToTab(index)"
        >
          <FileText :size="13" />
          <span class="tab-name">{{ tab.name }}</span>
          <span
            v-if="tab.isDirty"
            class="tab-dirty"
          >●</span>
          <button
            class="tab-close"
            @click="closeTab(index, $event)"
          >
            <X :size="12" />
          </button>
        </div>
      </div>

      <button
        class="tab-new"
        :title="t('editor.newDoc')"
        @click="handleNewFile('')"
      >
        <Plus :size="14" />
      </button>

      <!-- 右侧工具栏 -->
      <div class="tab-tools">
        <span
          v-if="isSaving"
          class="save-status"
          data-testid="save-status"
        >{{ t('editor.saving') }}</span>
        <span
          v-else-if="activeTab?.lastSavedAt"
          class="save-status"
          data-testid="save-status"
        >{{ t('editor.savedAt', { time: activeTab.lastSavedAt }) }}</span>
        <button
          class="tool-btn"
          :title="t('editor.aiSummary')"
          @click="handleSummarize"
        >
          <Sparkles :size="14" />
        </button>
        <button
          class="tool-btn"
          :title="t('editor.exportMd')"
          :disabled="isExporting"
          @click="exportMarkdown"
        >
          <FileDown :size="14" />
        </button>
        <button
          class="tool-btn"
          :title="t('editor.exportHtml')"
          :disabled="isExporting"
          @click="exportSingleHTML"
        >
          <FileCode :size="14" />
        </button>
        <button
          class="tool-btn"
          :title="t('editor.save')"
          @click="saveCurrentTab"
          data-testid="save-button"
        >
          <Save :size="14" />
        </button>
        <button
          class="tool-btn"
          :title="t('editor.viewMode', { mode: viewMode })"
          @click="toggleViewMode"
        >
          <Columns
            v-if="viewMode === 'split'"
            :size="14"
          />
          <Edit3
            v-else-if="viewMode === 'editor'"
            :size="14"
          />
          <Eye
            v-else
            :size="14"
          />
        </button>
      </div>
    </div>

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
                  @wiki-link-click="handleWikiLinkClick"
                />
              </div>
            </div>
          </div>

          <!-- 反向链接面板 -->
          <div
            v-if="backlinks.length > 0"
            class="backlinks-panel"
          >
            <div class="backlinks-header">
              <span>🔗 {{ t('editor.backlinks', { count: backlinks.length }) }}</span>
            </div>
            <div class="backlinks-list">
              <div
                v-for="link in backlinks"
                :key="link.path"
                class="backlink-item"
                @click="openBacklink(link)"
              >
                <FileText :size="14" />
                <span>{{ link.name }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- AI 总结面板 -->
      <div
        v-if="summaryOpen"
        class="summary-overlay"
        @click.self="summaryOpen = false"
      >
        <div class="summary-modal">
          <div class="summary-header">
            <span class="summary-title"><Sparkles :size="16" /> {{ t('editor.aiSummary') }}</span>
            <button
              class="summary-close"
              @click="summaryOpen = false"
            >
              <X :size="16" />
            </button>
          </div>
          <div class="summary-body">
            <div
              v-if="isSummarizing"
              class="summary-loading"
            >
              <Loader2
                :size="18"
                class="spin"
              />
              <span>{{ t('editor.summarizing') }}</span>
            </div>
            <pre
              v-else
              class="summary-text"
            >{{ summary }}</pre>
          </div>
          <div class="summary-footer">
            <button
              class="btn-ghost"
              @click="summaryOpen = false"
            >
              {{ t('common.close') }}
            </button>
            <button
              class="btn-primary"
              :disabled="!summary || isSummarizing"
              @click="insertSummaryToNote"
            >
              <Download :size="14" /> {{ t('editor.insertToNote') }}
            </button>
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

/* 标签页栏 */
.tab-bar {
  display: flex;
  align-items: center;
  height: 38px;
  background: var(--bg-sidebar);
  border-bottom: 1px solid var(--border);
  padding: 0 var(--space-2);
  gap: 2px;
  flex-shrink: 0;
}

.tabs-container {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: 1;
  overflow-x: auto;
  overflow-y: hidden;
}

.tabs-container::-webkit-scrollbar {
  height: 3px;
}

.tabs-container::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 2px;
}

.tab-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 30px;
  padding: 0 var(--space-2) 0 var(--space-3);
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  color: var(--text-secondary);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
  max-width: 200px;
  flex-shrink: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-bottom: none;
}

.tab-item:hover {
  background: var(--bg-hover);
}

.tab-item.active {
  background: var(--bg-content);
  color: var(--text-primary);
}

.tab-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tab-dirty {
  color: var(--accent);
  font-size: 10px;
  flex-shrink: 0;
}

.tab-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 3px;
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
  flex-shrink: 0;
}

.tab-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tab-new {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
  flex-shrink: 0;
}

.tab-new:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tab-tools {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  margin-left: var(--space-2);
}

.save-status {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.tool-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.tool-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
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

/* 反向链接面板 */
.backlinks-panel {
  border-top: 1px solid var(--border);
  background: var(--bg-sidebar);
  max-height: 150px;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.backlinks-header {
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  border-bottom: 1px solid var(--border);
}

.backlinks-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  overflow-y: auto;
}

.backlink-item {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.backlink-item:hover {
  background: var(--accent-alpha);
  border-color: var(--accent);
  color: var(--accent);
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

/* AI 总结面板 */
.summary-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
}

.summary-modal {
  width: min(640px, 90%);
  max-height: 80%;
  display: flex;
  flex-direction: column;
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.3);
  overflow: hidden;
}

.summary-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}

.summary-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-weight: 600;
  color: var(--accent);
}

.summary-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: all var(--transition-fast);
}
.summary-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.summary-body {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-4);
}

.summary-loading {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}

.summary-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: var(--text-sm);
  line-height: 1.7;
  color: var(--text-primary);
  margin: 0;
}

.summary-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
}

.btn-primary,
.btn-ghost {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.btn-primary {
  background: var(--accent);
  color: var(--text-inverse);
  border: 1px solid transparent;
}
.btn-primary:hover:not(:disabled) {
  background: var(--accent-hover);
}
.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-ghost {
  background: transparent;
  color: var(--text-secondary);
  border: 1px solid var(--border);
}
.btn-ghost:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.spin {
  animation: spin 0.8s linear infinite;
}
</style>
