<script setup lang="ts">
/**
 * KnowledgeView - NoteVault 知识库主页
 *
 * 设计理念（参考 Obsidian 主页）：
 *  - 一屏总览：当前工作区信息、文档数量、未完成任务、近期活动
 *  - 快速入口：新建文档、日记、打开文件
 *  - 最近编辑：最近打开/修改的文档（可固定）
 *  - 知识脉络：双向链接、标签云、待办摘要
 *  - 文档网格：所有文档的卡片视图（支持搜索、筛选、排序）
 */
import { ref, computed, onMounted, watch } from 'vue'
import {
  Library,
  FileText,
  Search,
  Clock,
  Calendar,
  Star,
  StarOff,
  ChevronRight,
  ChevronDown,
  Folder,
  FolderPlus,
  FolderOpen,
  Sparkles,
  FilePlus,
  Edit3,
  Download,
  Loader2,
} from 'lucide-vue-next'
import KnowledgeStats from '@/components/knowledge/KnowledgeStats.vue'
import TodayPanel from '@/components/knowledge/TodayPanel.vue'
import KnowledgeTodoPanel from '@/components/knowledge/KnowledgeTodoPanel.vue'
import KnowledgeTagCloud from '@/components/knowledge/KnowledgeTagCloud.vue'
import TemplateCreateDialog from '@/components/knowledge/TemplateCreateDialog.vue'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import {
  FileService,
  WorkspaceService,
  TodoService,
  TagService,
  ExportService,
  TemplateService,
} from '@bindings/github.com/notevault/notevault/index.js'
import type { TodoItem, TagInfo } from '@bindings/github.com/notevault/notevault/models.js'

interface FileNode {
  name: string
  path: string
  fullPath?: string
  isDir: boolean
  size?: number
  modTime?: string
  children?: FileNode[]
}

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const { t, locale } = useI18n()

// 状态
const allFiles = ref<FileNode[]>([])
const recentFiles = ref<{ path: string; title: string; modifiedAt: string }[]>([])
const todos = ref<TodoItem[]>([])
const tags = ref<TagInfo[]>([])
const searchKeyword = ref('')
const showOnlyStarred = ref(false)
const sortBy = ref<'modified' | 'name' | 'created'>('modified')
const selectedFolder = ref('')
const expandedFolders = ref<Record<string, boolean>>({})
const isLoading = ref(false)
const errorMsg = ref('')

// 固定的文档（保存在 localStorage）
const STARRED_KEY = 'notevault_starred'
const starredPaths = ref<string[]>([])

function loadStarred() {
  try {
    const raw = localStorage.getItem(STARRED_KEY)
    starredPaths.value = raw ? (JSON.parse(raw) as string[]) : []
  } catch {
    starredPaths.value = []
  }
}

function saveStarred() {
  localStorage.setItem(STARRED_KEY, JSON.stringify(starredPaths.value))
}

function toggleStar(path: string) {
  const idx = starredPaths.value.indexOf(path)
  if (idx >= 0) starredPaths.value.splice(idx, 1)
  else starredPaths.value.unshift(path)
  saveStarred()
}

function isStarred(path: string): boolean {
  return starredPaths.value.includes(path)
}

const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

// 把树形文件扁平化为路径数组
function flattenFiles(nodes: FileNode[], depth = 0): { path: string; name: string; fullPath: string; depth: number; isDir: boolean; size?: number; modTime?: string }[] {
  const out: { path: string; name: string; fullPath: string; depth: number; isDir: boolean; size?: number; modTime?: string }[] = []
  for (const node of nodes) {
    out.push({
      path: node.path,
      name: node.name,
      fullPath: node.fullPath || node.path,
      depth,
      isDir: node.isDir,
      size: node.size,
      modTime: node.modTime,
    })
    if (node.children && node.children.length) {
      out.push(...flattenFiles(node.children, depth + 1))
    }
  }
  return out
}

const flatFiles = computed(() => flattenFiles(allFiles.value))

function isMarkdownFile(file: { name: string; isDir: boolean }): boolean {
  return !file.isDir && /\.(md|markdown)$/i.test(file.name)
}

function normalizePath(path: string): string {
  return path.replaceAll('\\', '/')
}

function parentFolderPath(path: string): string {
  const normalized = normalizePath(path)
  const separator = normalized.lastIndexOf('/')
  return separator >= 0 ? normalized.slice(0, separator) : ''
}

const documentFiles = computed(() => flatFiles.value.filter(isMarkdownFile))

// 文件夹导航使用后端返回的目录树，文档列表则按父目录重新分组。
const folderEntries = computed(() => flatFiles.value
  .filter((file) => file.isDir)
  .map((folder) => ({
    path: normalizePath(folder.path),
    name: folder.name,
    depth: normalizePath(folder.path).split('/').length - 1,
  }))
  .sort((a, b) => a.path.localeCompare(b.path)))

const folderDocumentCounts = computed(() => {
  const counts: Record<string, number> = { '': documentFiles.value.length }
  for (const file of documentFiles.value) {
    const parts = parentFolderPath(file.path).split('/').filter(Boolean)
    let path = ''
    for (const part of parts) {
      path = path ? `${path}/${part}` : part
      counts[path] = (counts[path] || 0) + 1
    }
  }
  return counts
})

const visibleFolderEntries = computed(() => folderEntries.value.filter((folder) => {
  const segments = folder.path.split('/')
  for (let i = 1; i < segments.length; i++) {
    const parent = segments.slice(0, i).join('/')
    if (expandedFolders.value[parent] === false) return false
  }
  return true
}))

const selectedFolderLabel = computed(() => {
  if (!selectedFolder.value) return t('knowledge.allDocs')
  return folderEntries.value.find((folder) => folder.path === selectedFolder.value)?.name || selectedFolder.value
})

// 统计卡片
const stats = computed(() => {
  const noteFiles = documentFiles.value
  const mdCount = noteFiles.length
  const starredCount = noteFiles.filter((f) => isStarred(f.path)).length
  const todoAll = todos.value
  const pendingTodos = todoAll.filter((todo) => !todo.completed).length
  const highTodos = todoAll.filter((todo) => !todo.completed && todo.priority === 'high').length
  const completedTodos = todoAll.filter((todo) => todo.completed).length
  return {
    notes: mdCount,
    starred: starredCount,
    folders: flatFiles.value.filter((f) => f.isDir).length,
    todos: pendingTodos,
    high: highTodos,
    done: completedTodos,
    tags: tags.value.length,
  }
})

// 最近文件（来自工作区 store + 文件树最新 8 个）
const recentDisplayed = computed(() => {
  const files = flatFiles.value
    .filter((f) => !f.isDir && (f.name.endsWith('.md') || f.name.endsWith('.markdown')))
    .sort((a, b) => (b.modTime || '').localeCompare(a.modTime || ''))
    .slice(0, 8)
  return files
})

// 待办摘要：未完成优先 + 高优先级在前
const urgentTodos = computed(() => {
  return todos.value
    .filter((todo) => !todo.completed)
    .slice()
    .sort((a, b) => {
      // 高优先级在前
      const priorityOrder: Record<string, number> = { high: 0, medium: 1, low: 2 }
      return (priorityOrder[a.priority] ?? 1) - (priorityOrder[b.priority] ?? 1)
    })
    .slice(0, 6)
})

// 标签云（最多 18 个）
const tagCloud = computed(() =>
  [...tags.value]
    .sort((a, b) => b.count - a.count)
    .slice(0, 18)
)

// 文档列表（搜索/筛选/排序）
const filteredFiles = computed(() => {
  let list = documentFiles.value.slice()
  if (selectedFolder.value) {
    const folderPrefix = `${selectedFolder.value}/`
    list = list.filter((file) => normalizePath(file.path).startsWith(folderPrefix))
  }
  if (showOnlyStarred.value) {
    list = list.filter((f) => isStarred(f.path))
  }
  if (searchKeyword.value.trim()) {
    const kw = searchKeyword.value.toLowerCase().trim()
    list = list.filter(
      (f) =>
        f.name.toLowerCase().includes(kw) ||
        f.path.toLowerCase().includes(kw) ||
        f.name.toLowerCase().includes(kw.replace(/\.md$/, '')),
    )
  }
  // 排序
  if (sortBy.value === 'name') {
    list = list.sort((a, b) => a.name.localeCompare(b.name))
  } else if (sortBy.value === 'created') {
    list = list.sort((a, b) => (a.modTime || '').localeCompare(b.modTime || ''))
  } else {
    list = list.sort((a, b) => (b.modTime || '').localeCompare(a.modTime || ''))
  }
  return list.slice(0, 50)
})

const groupedFiles = computed(() => {
  const groups = new Map<string, { path: string; name: string; files: typeof filteredFiles.value }>()
  for (const file of filteredFiles.value) {
    const path = parentFolderPath(file.path)
    if (!groups.has(path)) {
      groups.set(path, {
        path,
        name: path ? path.split('/').pop() || path : t('knowledge.rootFolder'),
        files: [],
      })
    }
    groups.get(path)!.files.push(file)
  }
  return [...groups.values()].sort((a, b) => a.path.localeCompare(b.path))
})

function toggleFolder(path: string) {
  expandedFolders.value[path] = expandedFolders.value[path] === false
}

function selectFolder(path: string) {
  selectedFolder.value = path
}

function syncFolderExpansion() {
  for (const folder of folderEntries.value) {
    if (!(folder.path in expandedFolders.value)) {
      expandedFolders.value[folder.path] = true
    }
  }
}

async function ensureWorkspace(): Promise<boolean> {
  if (!currentWorkspace.value) {
    try {
      const ws = await WorkspaceService.GetCurrentWorkspace()
      if (ws) {
        workspaceStore.setCurrentWorkspace(ws as any)
      } else {
        router.push('/')
        return false
      }
    } catch (e) {
      console.error('Failed to get workspace:', e)
      router.push('/')
      return false
    }
  }
  return true
}

async function loadAll() {
  errorMsg.value = ''
  if (!await ensureWorkspace()) return
  isLoading.value = true
  try {
    // 并行加载
    const [tree, todoList, tagList] = await Promise.all([
      FileService.GetFileTree(currentWorkspace.value!.path),
      TodoService.GetAllTodos(currentWorkspace.value!.path),
      TagService.GetAllTags(currentWorkspace.value!.path),
    ])
    allFiles.value = (tree as FileNode[]) || []
    syncFolderExpansion()
    todos.value = ((todoList as TodoItem[]) || []).filter((item) => !!item)
    tags.value = ((tagList as TagInfo[]) || []).filter((item) => !!item)
  } catch (e) {
    console.error('Failed to load knowledge view:', e)
    errorMsg.value = t('knowledge.loadFailed', { msg: (e as Error).message })
  } finally {
    isLoading.value = false
  }
}

function openFile(file: { path: string; name: string }) {
  workspaceStore.openFile(file.path)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

async function createNewDoc(folderPath = selectedFolder.value) {
  if (!currentWorkspace.value) {
    router.push('/')
    return
  }
  const name = prompt(t('knowledge.promptFileName'), t('knowledge.untitledDoc'))
  if (!name) return
  try {
    const cleanName = name.trim()
    const relativePath = folderPath && !cleanName.includes('/')
      ? `${folderPath}/${cleanName}`
      : cleanName
    const node = await FileService.CreateFile(
      currentWorkspace.value.path,
      relativePath,
      `# ${cleanName.replace(/\.(md|markdown)$/i, '')}\n\n`,
    )
    if (node) {
      workspaceStore.incrementFileTreeVersion()
      workspaceStore.openFile((node as any).path)
      router.push('/editor')
    }
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      alert(t('knowledge.fileExists'))
    } else {
      alert(t('knowledge.createFailed', { msg: (e as Error).message }))
    }
  }
}

function handleCreateNewDoc() {
  void createNewDoc()
}

async function createFolder() {
  if (!currentWorkspace.value) {
    router.push('/')
    return
  }
  const name = prompt(t('knowledge.promptFolderName'), t('knowledge.untitledFolder'))
  if (!name?.trim()) return
  const folderName = name.trim()
  const relativePath = selectedFolder.value ? `${selectedFolder.value}/${folderName}` : folderName
  try {
    await FileService.CreateFolder(currentWorkspace.value.path, relativePath)
    expandedFolders.value[relativePath] = true
    selectedFolder.value = relativePath
    workspaceStore.incrementFileTreeVersion()
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      alert(t('knowledge.folderExists'))
    } else {
      alert(t('knowledge.createFolderFailed', { msg: (e as Error).message }))
    }
  }
}

// 导出整个工作区为 zip（Markdown 打包）
const isExporting = ref(false)

// P2-2：模板创建对话框
const showTemplateDialog = ref(false)
async function exportWorkspace() {
  if (!currentWorkspace.value) {
    alert(t('knowledge.selectWorkspaceFirst'))
    return
  }
  const runtime = await import('@wailsio/runtime')
  const defaultName = `${currentWorkspace.value.name || 'notevault'}-export.zip`
  let dest: string | null = null
  try {
    const result = await runtime.Dialogs.SaveFile({
      Title: t('knowledge.chooseExportLocation'),
      Filename: defaultName,
      Filters: [{ DisplayName: t('knowledge.zipFile'), Pattern: '*.zip' }],
    })
    dest = Array.isArray(result) ? result[0] : result
    if (dest && !dest.toLowerCase().endsWith('.zip')) dest += '.zip'
  } catch (e) {
    console.error('SaveFile dialog failed:', e)
    return
  }
  if (!dest) return

  isExporting.value = true
  try {
    await ExportService.ExportWorkspaceMarkdown(currentWorkspace.value.path, dest)
    alert(t('knowledge.exported', { path: dest }))
  } catch (e) {
    alert(t('knowledge.exportFailed', { msg: (e as Error).message }))
  } finally {
    isExporting.value = false
  }
}

/** 创建今日日记（Obsidian 风格：文件名格式 "Daily/2026-08-26.md"）。
 * P2-2：若工作区提供 Templates/Daily.md 则优先用模板渲染（可使用 {{date}} 等占位符），
 * 没有模板时回退到内置默认结构。 */
async function createDailyNote() {
  if (!currentWorkspace.value) {
    router.push('/')
    return
  }
  const now = new Date()
  const dateStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const fileName = `Daily/${dateStr}.md`
  try {
    let node: unknown
    try {
      node = await TemplateService.CreateFromTemplate(
        currentWorkspace.value.path,
        'Daily',
        fileName,
        {},
      )
    } catch {
      // 无 Daily 模板 → 内置默认结构
      node = await FileService.CreateFile(
        currentWorkspace.value.path,
        fileName,
        `# ${dateStr}\n\n## 📅 今日计划\n\n- [ ] \n\n## 📝 笔记\n\n## 💭 想法\n\n`,
      )
    }
    if (node) {
      workspaceStore.incrementFileTreeVersion()
      workspaceStore.openFile((node as any).path)
      router.push('/editor')
    }
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      // 文件已存在，直接打开
      workspaceStore.openFile(fileName)
      router.push('/editor')
    } else {
      console.error('Failed to create daily note:', e)
      alert(t('knowledge.dailyFailed', { msg: (e as Error).message }))
    }
  }
}

/** P2-2：模板创建成功后打开新笔记 */
function onTemplateCreated(path: string) {
  showTemplateDialog.value = false
  workspaceStore.incrementFileTreeVersion()
  workspaceStore.openFile(path)
  router.push('/editor')
}

/** 切换待办完成状态 */
async function toggleTodo(todo: TodoItem) {
  if (!currentWorkspace.value) return
  try {
    await TodoService.ToggleTodo(currentWorkspace.value.path, todo.filePath, todo.lineIndex)
    // 重新加载
    const updated = await TodoService.GetAllTodos(currentWorkspace.value.path)
    todos.value = ((updated as TodoItem[]) || []).filter((item) => !!item)
  } catch (e) {
    console.error('Failed to toggle todo:', e)
  }
}

function openTodoFile(todo: TodoItem) {
  workspaceStore.openFile(todo.filePath)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

function formatRelativeTime(modTime?: string): string {
  if (!modTime) return ''
  const date = new Date(modTime)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)
  if (minutes < 1) return t('knowledge.time.justNow')
  if (minutes < 60) return t('knowledge.time.minutesAgo', { count: minutes })
  if (hours < 24) return t('knowledge.time.hoursAgo', { count: hours })
  if (days < 7) return t('knowledge.time.daysAgo', { count: days })
  return date.toLocaleDateString(locale.value)
}

onMounted(() => {
  loadStarred()
  loadAll()
})

watch(() => currentWorkspace.value?.id, () => {
  loadAll()
})

watch(() => workspaceStore.fileTreeVersion, () => {
  loadAll()
})
</script>

<template>
  <div class="knowledge-view">
    <!-- 顶部 Banner -->
    <header class="kv-banner">
      <div class="kv-banner-left">
        <div class="kv-banner-icon">
          <Library :size="24" />
        </div>
        <div>
          <h1 class="kv-banner-title">
            {{ currentWorkspace?.name || t('knowledge.defaultTitle') }}
          </h1>
          <p class="kv-banner-sub">
            <span v-if="currentWorkspace">
              <FolderOpen :size="12" />
              {{ currentWorkspace.path }}
            </span>
            <span v-else>{{ t('knowledge.noWorkspace') }}</span>
          </p>
        </div>
      </div>
      <div class="kv-banner-actions">
        <button
          class="kv-btn-primary"
          @click="handleCreateNewDoc"
        >
          <FolderPlus
            v-if="selectedFolder"
            :size="14"
          />
          <FilePlus
            v-else
            :size="14"
          />
          <span>{{ selectedFolder ? t('knowledge.newDocInFolder') : t('knowledge.newDoc') }}</span>
        </button>
        <button
          class="kv-btn-secondary"
          @click="createDailyNote"
        >
          <Calendar :size="14" />
          <span>{{ t('knowledge.dailyNote') }}</span>
        </button>
        <button
          class="kv-btn-secondary"
          @click="showTemplateDialog = true"
        >
          <FileText :size="14" />
          <span>{{ t('knowledge.newFromTemplate') }}</span>
        </button>
        <button
          class="kv-btn-ghost"
          :disabled="isExporting"
          @click="exportWorkspace"
        >
          <Download
            v-if="!isExporting"
            :size="14"
          />
          <Loader2
            v-else
            :size="14"
            class="spin"
          />
          <span>{{ t('knowledge.exportWorkspace') }}</span>
        </button>
      </div>
    </header>

    <!-- 错误条 -->
    <div
      v-if="errorMsg"
      class="kv-error"
    >
      ⚠️ {{ errorMsg }} <router-link to="/">
        {{ t('knowledge.goToSelect') }}
      </router-link>
    </div>

    <!-- 今日工作台条带：今日编辑/连续记录/待办/到期提醒 + 继续上次 -->
    <TodayPanel />

    <!-- 统计卡片 -->
    <KnowledgeStats :stats="stats" />

    <!-- 双列内容区 -->
    <div class="kv-grid">
      <!-- 主区：文档列表 -->
      <section class="kv-section kv-section-main">
        <div class="kv-section-header">
          <h2>
            <FileText :size="16" />
            <span>{{ t('knowledge.myDocs') }}</span>
            <span class="kv-section-count">{{ filteredFiles.length }}</span>
          </h2>
          <div class="kv-section-tools">
            <div class="kv-search">
              <Search :size="14" />
              <input
                v-model="searchKeyword"
                :placeholder="t('knowledge.searchPlaceholder')"
              >
            </div>
            <select
              v-model="sortBy"
              class="kv-sort"
            >
              <option value="modified">
                {{ t('knowledge.sortRecent') }}
              </option>
              <option value="name">
                {{ t('knowledge.sortName') }}
              </option>
              <option value="created">
                {{ t('knowledge.sortCreated') }}
              </option>
            </select>
            <button
              class="kv-icon-toggle"
              :class="{ active: showOnlyStarred }"
              :title="t('knowledge.onlyStarred')"
              @click="showOnlyStarred = !showOnlyStarred"
            >
              <Star :size="14" />
            </button>
          </div>
        </div>

        <div
          v-if="isLoading"
          class="kv-loading"
        >
          <div class="kv-spinner" />
          {{ t('common.loading') }}
        </div>

        <div
          v-else-if="filteredFiles.length === 0"
          class="kv-empty"
        >
          <Sparkles :size="32" />
          <h3>{{ searchKeyword ? t('knowledge.noMatchTitle') : t('knowledge.emptyTitle') }}</h3>
          <p v-if="!searchKeyword">
            {{ t('knowledge.emptyDescCreate') }}
          </p>
          <p v-else>
            {{ t('knowledge.emptyDescSearch') }}
          </p>
          <button
            v-if="!searchKeyword"
            class="kv-btn-primary"
            @click="handleCreateNewDoc"
          >
            <FilePlus :size="14" />
            <span>{{ t('knowledge.newDoc') }}</span>
          </button>
        </div>

        <div
          v-else
          class="kv-library-layout"
        >
          <aside class="kv-folder-nav">
            <div class="kv-folder-nav-header">
              <span>{{ t('knowledge.folderNav') }}</span>
              <button
                class="kv-folder-action"
                :title="t('knowledge.newFolder')"
                @click="createFolder"
              >
                <FolderPlus :size="14" />
              </button>
            </div>
            <button
              class="kv-folder-item"
              :class="{ active: selectedFolder === '' }"
              @click="selectFolder('')"
            >
              <Library :size="14" />
              <span class="kv-folder-name">{{ t('knowledge.allDocs') }}</span>
              <span class="kv-folder-count">{{ folderDocumentCounts[''] || 0 }}</span>
            </button>
            <div class="kv-folder-list">
              <div
                v-for="folder in visibleFolderEntries"
                :key="folder.path"
                class="kv-folder-row"
              >
                <button
                  class="kv-folder-toggle"
                  :title="expandedFolders[folder.path] === false ? t('knowledge.expandFolder') : t('knowledge.collapseFolder')"
                  @click.stop="toggleFolder(folder.path)"
                >
                  <ChevronRight
                    v-if="expandedFolders[folder.path] === false"
                    :size="12"
                  />
                  <ChevronDown
                    v-else
                    :size="12"
                  />
                </button>
                <button
                  class="kv-folder-item"
                  data-testid="folder-item"
                  :class="{ active: selectedFolder === folder.path }"
                  :style="{ paddingLeft: `${folder.depth * 12 + 4}px` }"
                  @click="selectFolder(folder.path)"
                >
                  <FolderOpen
                    v-if="selectedFolder === folder.path"
                    :size="14"
                  />
                  <Folder
                    v-else
                    :size="14"
                  />
                  <span class="kv-folder-name">{{ folder.name }}</span>
                  <span class="kv-folder-count">{{ folderDocumentCounts[folder.path] || 0 }}</span>
                </button>
              </div>
            </div>
          </aside>

          <div class="kv-document-pane">
            <div class="kv-document-context">
              <div>
                <FolderOpen :size="15" />
                <strong>{{ selectedFolderLabel }}</strong>
                <span>{{ filteredFiles.length }} {{ t('knowledge.stats.docs') }}</span>
              </div>
              <button
                class="kv-context-new"
                :title="t('knowledge.newDocInFolder')"
                @click="handleCreateNewDoc"
              >
                <FilePlus :size="14" />
                <span>{{ t('knowledge.newDocInFolder') }}</span>
              </button>
            </div>
            <div
              v-for="group in groupedFiles"
              :key="group.path || 'root'"
              class="kv-doc-group"
            >
              <button
                v-if="group.path !== selectedFolder"
                class="kv-doc-group-header"
                @click="selectFolder(group.path)"
              >
                <FolderOpen :size="14" />
                <span>{{ group.name }}</span>
                <span class="kv-folder-count">{{ group.files.length }}</span>
              </button>
              <div class="kv-doc-list">
                <div
                  v-for="file in group.files"
                  :key="file.path"
                  class="kv-doc-item"
                  data-testid="document-item"
                  @click="openFile(file)"
                >
                  <FileText
                    :size="16"
                    class="kv-doc-icon"
                  />
                  <div class="kv-doc-body">
                    <div class="kv-doc-title">
                      {{ file.name.replace(/\.(md|markdown)$/, '') }}
                    </div>
                    <div class="kv-doc-meta">
                      <span class="kv-doc-path">{{ file.path }}</span>
                      <span class="kv-doc-time">{{ formatRelativeTime(file.modTime) }}</span>
                    </div>
                  </div>
                  <button
                    class="kv-doc-star"
                    :class="{ active: isStarred(file.path) }"
                    :title="isStarred(file.path) ? t('knowledge.unstar') : t('knowledge.star')"
                    @click.stop="toggleStar(file.path)"
                  >
                    <Star
                      v-if="isStarred(file.path)"
                      :size="14"
                    />
                    <StarOff
                      v-else
                      :size="14"
                    />
                  </button>
                  <ChevronRight
                    :size="14"
                    class="kv-doc-arrow"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- 侧栏：待办 + 标签 -->
      <aside class="kv-section kv-section-side">
        <!-- 待办 -->
        <KnowledgeTodoPanel
          :todos="urgentTodos"
          @toggle="toggleTodo"
          @open="openTodoFile"
        />

        <!-- 标签云 -->
        <KnowledgeTagCloud :tags="tagCloud" />

        <!-- 最近编辑 -->
        <div class="kv-card">
          <div class="kv-card-header">
            <h3>
              <Clock :size="14" />
              <span>{{ t('knowledge.recentEdits') }}</span>
            </h3>
          </div>
          <div
            v-if="recentDisplayed.length === 0"
            class="kv-card-empty"
          >
            {{ t('knowledge.noRecent') }}
          </div>
          <ul
            v-else
            class="kv-recent-list"
          >
            <li
              v-for="file in recentDisplayed"
              :key="file.path"
              class="kv-recent-item"
              @click="openFile(file)"
            >
              <FileText :size="13" />
              <span class="kv-recent-name">{{ file.name.replace(/\.(md|markdown)$/, '') }}</span>
              <span class="kv-recent-time">{{ formatRelativeTime(file.modTime) }}</span>
            </li>
          </ul>
        </div>

        <!-- 快速入口 -->
        <div class="kv-card">
          <div class="kv-card-header">
            <h3>
              <Edit3 :size="14" />
              <span>{{ t('knowledge.quickAccess') }}</span>
            </h3>
          </div>
          <div class="kv-quick-grid">
            <button
              class="kv-quick-btn"
              @click="router.push('/editor')"
            >
              <FileText :size="16" />
              <span>{{ t('knowledge.allDocs') }}</span>
            </button>
            <button
              class="kv-quick-btn"
              @click="router.push('/search')"
            >
              <Search :size="16" />
              <span>{{ t('knowledge.globalSearch') }}</span>
            </button>
            <button
              class="kv-quick-btn"
              @click="router.push('/reminders')"
            >
              <Clock :size="16" />
              <span>{{ t('knowledge.reminders') }}</span>
            </button>
            <button
              class="kv-quick-btn"
              @click="router.push('/archive')"
            >
              <FolderOpen :size="16" />
              <span>{{ t('knowledge.archive') }}</span>
            </button>
          </div>
        </div>
      </aside>
    </div>

    <!-- P2-2：从模板新建 -->
    <TemplateCreateDialog
      v-if="showTemplateDialog"
      @close="showTemplateDialog = false"
      @created="onTemplateCreated"
    />
  </div>
</template>

// 标签字体大小计算在 setup script 内

<style scoped>
.knowledge-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  background: var(--bg-content);
}

/* Banner */
.kv-banner {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--space-6) var(--space-8);
  background: var(--bg-window);
  border-bottom: 1px solid var(--border);
}

.kv-banner-left {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  min-width: 0;
}

.kv-banner-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  border-radius: var(--radius-md);
  background: var(--accent);
  color: var(--text-inverse);
  flex-shrink: 0;
}

.kv-banner-title {
  font-size: var(--text-2xl);
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 480px;
}

.kv-banner-sub {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin: 4px 0 0 0;
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 480px;
}

.kv-banner-actions {
  display: flex;
  gap: var(--space-2);
  flex-shrink: 0;
  flex-wrap: wrap;
}

.kv-btn-ghost {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  font-weight: 500;
  flex-shrink: 0;
  transition: background var(--transition-fast), color var(--transition-fast), border-color var(--transition-fast);
}
.kv-btn-ghost:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--accent);
  border-color: var(--border-accent);
}
.kv-btn-ghost:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.spin {
  animation: kv-spin 0.8s linear infinite;
}
@keyframes kv-spin {
  to { transform: rotate(360deg); }
}

.kv-error {
  margin: var(--space-3) var(--space-8);
  padding: var(--space-3) var(--space-4);
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid #ef4444;
  border-radius: var(--radius-sm);
  color: #ef4444;
  font-size: var(--text-sm);
}

.kv-btn-primary {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--accent);
  color: var(--text-inverse);
  font-size: var(--text-sm);
  font-weight: 500;
  transition: background var(--transition-fast);
}

.kv-btn-primary:hover {
  background: var(--accent-hover);
}

.kv-btn-secondary {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-primary);
  border: 1px solid var(--border);
  font-size: var(--text-sm);
  font-weight: 500;
  transition: background var(--transition-fast);
}

.kv-btn-secondary:hover {
  background: var(--bg-hover);
}

/* 主内容双列网格 */
.kv-grid {
  display: grid;
  grid-template-columns: 1fr 320px;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-8);
  flex: 1;
}

.kv-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.kv-section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
  flex-shrink: 0;
}

.kv-section-header h2 {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.kv-section-count {
  font-size: var(--text-xs);
  color: var(--text-muted);
  background: var(--bg-sidebar);
  padding: 1px 8px;
  border-radius: 10px;
}

.kv-section-tools {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.kv-search {
  position: relative;
  display: flex;
  align-items: center;
}

.kv-search > svg {
  position: absolute;
  left: 8px;
  color: var(--text-muted);
}

.kv-search input {
  width: 180px;
  height: 28px;
  padding: 0 var(--space-2) 0 28px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-sm);
  outline: none;
  transition: border-color var(--transition-fast);
}

.kv-search input:focus {
  border-color: var(--accent);
}

.kv-sort {
  height: 28px;
  padding: 0 var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-xs);
  outline: none;
}

.kv-icon-toggle {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: all var(--transition-fast);
}

.kv-icon-toggle:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kv-icon-toggle.active {
  background: rgba(234, 179, 8, 0.12);
  color: #eab308;
}

.kv-loading,
.kv-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-12) var(--space-4);
  color: var(--text-muted);
  text-align: center;
}

.kv-empty h3 {
  font-size: var(--text-base);
  color: var(--text-secondary);
  font-weight: 600;
  margin: 0;
}

.kv-empty p {
  font-size: var(--text-sm);
  margin: 0;
}

.kv-spinner {
  width: 24px;
  height: 24px;
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: kv-spin 0.8s linear infinite;
}

@keyframes kv-spin {
  to { transform: rotate(360deg); }
}

/* 文件夹导航 + 文档分组 */
.kv-library-layout {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  min-height: 420px;
  flex: 1;
  min-width: 0;
}

.kv-folder-nav {
  min-width: 0;
  padding: var(--space-2);
  border-right: 1px solid var(--border);
  background: var(--bg-window);
  overflow-y: auto;
}

.kv-folder-nav-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-2) var(--space-2) var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-xs);
  font-weight: 600;
}

.kv-folder-action,
.kv-folder-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  border-radius: var(--radius-sm);
}

.kv-folder-action {
  width: 24px;
  height: 24px;
}

.kv-folder-action:hover,
.kv-folder-toggle:hover {
  background: var(--bg-hover);
  color: var(--accent);
}

.kv-folder-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: var(--space-1);
}

.kv-folder-row {
  display: flex;
  align-items: center;
  min-width: 0;
}

.kv-folder-toggle {
  width: 20px;
  height: 28px;
  flex-shrink: 0;
}

.kv-folder-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex: 1;
  min-height: 30px;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  text-align: left;
  white-space: nowrap;
}

.kv-folder-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kv-folder-item.active {
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
  color: var(--accent);
  font-weight: 600;
}

.kv-folder-name {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kv-folder-count {
  flex-shrink: 0;
  color: var(--text-muted);
  font-size: 10px;
}

.kv-document-pane {
  min-width: 0;
  overflow-y: auto;
}

.kv-document-context {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-xs);
}

.kv-document-context > div {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.kv-document-context strong {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv-context-new {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  padding: 5px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-xs);
}

.kv-context-new:hover {
  border-color: var(--border-accent);
  color: var(--accent);
  background: var(--bg-hover);
}

.kv-doc-group {
  border-bottom: 1px solid var(--border);
}

.kv-doc-group:last-child {
  border-bottom: 0;
}

.kv-doc-group-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-4) var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-xs);
  font-weight: 600;
  text-align: left;
}

.kv-doc-group-header:hover {
  color: var(--accent);
}

/* 文档列表 */
.kv-doc-list {
  display: flex;
  flex-direction: column;
  padding: var(--space-2);
  overflow-y: auto;
}

.kv-doc-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--transition-fast);
}

.kv-doc-item:hover {
  background: var(--bg-hover);
}

.kv-doc-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.kv-doc-body {
  flex: 1;
  min-width: 0;
}

.kv-doc-title {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv-doc-meta {
  display: flex;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-top: 2px;
}

.kv-doc-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.kv-doc-star {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: all var(--transition-fast);
  opacity: 0;
}

.kv-doc-item:hover .kv-doc-star {
  opacity: 1;
}

.kv-doc-star.active {
  color: #eab308;
  opacity: 1;
}

.kv-doc-star:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kv-doc-arrow {
  color: var(--text-muted);
  opacity: 0;
  transition: opacity var(--transition-fast);
}

.kv-doc-item:hover .kv-doc-arrow {
  opacity: 1;
}

/* 侧栏卡片 */
.kv-section-side {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  background: transparent;
  border: none;
  padding: 0;
}

.kv-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
  flex-shrink: 0;
}

.kv-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
}

.kv-card-header h3 {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.kv-card-link {
  display: flex;
  align-items: center;
  gap: 2px;
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-decoration: none;
}

.kv-card-link:hover {
  color: var(--accent);
}

.kv-card-empty {
  padding: var(--space-4);
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-align: center;
}

/* 最近编辑 */
.kv-recent-list {
  list-style: none;
  margin: 0;
  padding: var(--space-1);
}

.kv-recent-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--transition-fast);
  font-size: var(--text-sm);
}

.kv-recent-item:hover {
  background: var(--bg-hover);
}

.kv-recent-name {
  flex: 1;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv-recent-time {
  font-size: var(--text-xs);
  color: var(--text-muted);
  white-space: nowrap;
}

/* 快速入口 */
.kv-quick-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1px;
  background: var(--border);
}

.kv-quick-btn {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-4);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  transition: all var(--transition-fast);
}

.kv-quick-btn:hover {
  background: var(--bg-hover);
  color: var(--accent);
}

@media (max-width: 980px) {
  .kv-grid {
    grid-template-columns: 1fr;
  }
  .kv-banner-title {
    font-size: var(--text-xl);
    max-width: 240px;
  }
  .kv-library-layout {
    grid-template-columns: 1fr;
  }
  .kv-folder-nav {
    max-height: 190px;
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }
}

@media (max-width: 640px) {
  .kv-banner,
  .kv-grid {
    padding-left: var(--space-3);
    padding-right: var(--space-3);
  }
  .kv-banner {
    align-items: flex-start;
    flex-direction: column;
    gap: var(--space-3);
  }
  .kv-banner-actions {
    width: 100%;
  }
  .kv-banner-actions button {
    flex: 1;
    justify-content: center;
  }
  .kv-section-header {
    align-items: flex-start;
    flex-direction: column;
    gap: var(--space-2);
  }
  .kv-section-tools {
    width: 100%;
  }
  .kv-search,
  .kv-search input {
    width: 100%;
  }
  .kv-document-context {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
