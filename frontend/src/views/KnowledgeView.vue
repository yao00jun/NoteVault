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
import { FileService, WorkspaceService, StatsService, TagService, ExportService, TemplateService, TagInfo } from '@/api'
import { ref, computed, onMounted, watch } from 'vue'
import {
  Library,
  FileText,
  Star,
  FolderOpen,
  FilePlus,
  Download,
  Loader2,
  Flame,
  PenLine,
  Hash,
  History,
} from '@lucide/vue'
import WorkbenchWidgets from '@/components/knowledge/WorkbenchWidgets.vue'
import TemplateCreateDialog from '@/components/knowledge/TemplateCreateDialog.vue'
import { useDailyNote } from '@/composables/useDailyNote'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { toWorkspace, toWorkspaceList } from '@/utils/workspace'
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import { confirmDialog } from '@/composables/useConfirm'
import { isImeComposing } from '@/utils/ime'
import { promptDialog } from '@/composables/usePrompt'
import { useWorkbenchSpaces, normalizePath, isMarkdownFile } from '@/composables/useWorkbenchSpaces'

const toast = useToast()

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

// ---- 个人工作台 ----
// 问候语随时间变化；日期与连续记录展示在 Banner 副行
const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 5) return t('knowledge.workbench.greeting.night')
  if (h < 11) return t('knowledge.workbench.greeting.morning')
  if (h < 13) return t('knowledge.workbench.greeting.noon')
  if (h < 18) return t('knowledge.workbench.greeting.afternoon')
  return t('knowledge.workbench.greeting.evening')
})
const todayLabel = computed(() => {
  const d = new Date()
  const weekday = t(`knowledge.workbench.weekday.${d.getDay()}`)
  return `${d.getMonth() + 1}${t('knowledge.workbench.month')} ${d.getDate()}${t('knowledge.workbench.day')} ${weekday}`
})

// 快速捕获：一行输入回车即建笔记（认知负荷原则——记想法不需要离开工作台）
const captureText = ref('')
const capturing = ref(false)
function onCaptureEnter(e: KeyboardEvent) {
  // IME 守卫：拼音合成态的 Enter 是确认候选字，不建笔记
  if (isImeComposing(e)) return
  void quickCapture()
}

async function quickCapture() {
  const text = captureText.value.trim()
  if (!text || capturing.value) return
  if (!currentWorkspace.value) {
    toast.warning(t('knowledge.selectWorkspaceFirst'))
    return
  }
  capturing.value = true
  try {
    const d = new Date()
    const mm = String(d.getMonth() + 1).padStart(2, '0')
    const dd = String(d.getDate()).padStart(2, '0')
    const inboxPath = `Inbox/${d.getFullYear()}-${mm}-${dd}.md`
    let content: string
    try {
      content = await FileService.ReadFile(currentWorkspace.value.path, inboxPath)
    } catch {
      content = `# ${d.getFullYear()}-${mm}-${dd} 闪念

`
    }
    const timeTag = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
    content = `${content.replace(/\s*$/, '')}
- ${timeTag} ${text}
`
    await FileService.SaveFile(currentWorkspace.value.path, inboxPath, content)
    captureText.value = ''
    workspaceStore.incrementFileTreeVersion()
    toast.success(t('knowledge.workbench.captured', { name: inboxPath }))
  } catch (e) {
    toast.error(t('knowledge.workbench.captureFailed', { msg: (e as Error).message }))
  } finally {
    capturing.value = false
  }
}

// 连续记录（来自 StatsService，静默降级）
const streakDays = ref<number | null>(null)
const remindersToday = ref(0)
async function loadWorkbenchStats() {
  if (!currentWorkspace.value?.path) return
  try {
    const st = (await StatsService.GetTodayStats(currentWorkspace.value.path)) as {
      streakDays?: number
      dueReminders?: number
    } | null
    streakDays.value = st?.streakDays ?? null
    remindersToday.value = st?.dueReminders ?? 0
  } catch {
    streakDays.value = null
  }
}

async function loadHotTags() {
  if (!currentWorkspace.value?.path) return
  try {
    const list = (await TagService.GetAllTags(currentWorkspace.value.path)) as TagInfo[] | null
    tags.value = ((list ?? []) as TagInfo[]).filter((x) => !!x)
  } catch (e) {
    console.error('Failed to load tags:', e)
  }
}

// 知识空间（分类扫描/统计/过滤已抽到 useWorkbenchSpaces，蓝图 2.1 五空间）
const { openTodayNote } = useDailyNote()
function openSpace(space: { dir: string; key: string }) {
  if (space.key === 'daily') {
    void openTodayNote()
    return
  }
  // 直达知识库编辑页（目录在文件树中就地可见）
  router.push({ path: '/editor', query: { folder: space.dir } })
}

// 星标文档（下区右列，top 5）
const starredFiles = computed(() =>
  documentFiles.value.filter((f) => isStarred(f.path)).slice(0, 5),
)

// 标签速览（下区右列，top 8，点击进发现·标签）
const tags = ref<{ name: string; count: number }[]>([])
const hotTags = computed(() =>
  [...tags.value].sort((a, b) => b.count - a.count).slice(0, 8),
)
const workspaceStore = useWorkspaceStore()
const { t, locale } = useI18n()

// 状态
const allFiles = ref<FileNode[]>([])

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
const { knowledgeSpaces } = useWorkbenchSpaces(flatFiles)

const documentFiles = computed(() => flatFiles.value.filter(isMarkdownFile))

// 最近活跃编辑（UI-WORKBENCH-REDESIGN 下区）：全库按修改时间降序 top 8。
// 承接原 WorkbenchWidgets「今日编辑」卡的职责并放宽到最近窗口，信息量更大。
const recentNotes = computed(() =>
  [...documentFiles.value]
    .sort((a, b) => (b.modTime || '').localeCompare(a.modTime || ''))
    .slice(0, 8),
)

async function ensureWorkspace(): Promise<boolean> {
  if (!currentWorkspace.value) {
    try {
      const ws = await WorkspaceService.GetCurrentWorkspace()
      if (ws) {
        workspaceStore.setCurrentWorkspace(toWorkspace(ws))
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
  try {
    const tree = await FileService.GetFileTree(currentWorkspace.value!.path)
    allFiles.value = (tree as FileNode[]) || []
  } catch (e) {
    console.error('Failed to load knowledge view:', e)
    errorMsg.value = t('knowledge.loadFailed', { msg: (e as Error).message })
  }
}

function openFile(file: { path: string; name: string }) {
  workspaceStore.openFile(file.path)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

async function createNewDoc() {
  if (!currentWorkspace.value) {
    router.push('/')
    return
  }
  const name = await promptDialog({ message: t('knowledge.promptFileName'), defaultValue: t('knowledge.untitledDoc') })
  if (!name) return
  try {
    const cleanName = name.trim()
    const node = await FileService.CreateFile(
      currentWorkspace.value.path,
      cleanName,
      `# ${cleanName.replace(/\.(md|markdown)$/i, '')}\n\n`,
    )
    if (node) {
      workspaceStore.incrementFileTreeVersion()
      workspaceStore.openFile((node as any).path)
      router.push('/editor')
    }
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      toast.warning(t('knowledge.fileExists'))
    } else {
      toast.error(t('knowledge.createFailed', { msg: (e as Error).message }))
    }
  }
}

function handleCreateNewDoc() {
  void createNewDoc()
}

// 导出整个工作区为 zip（Markdown 打包）
const isExporting = ref(false)

// P2-2：模板创建对话框
const showTemplateDialog = ref(false)
async function exportWorkspace() {
  if (!currentWorkspace.value) {
    toast.warning(t('knowledge.selectWorkspaceFirst'))
    return
  }
  const runtime = await import('@wailsio/runtime')
  const defaultName = `${currentWorkspace.value.name || 'notevault'}-export.zip`
  let dest: string | null
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
    toast.success(t('knowledge.exported', { path: dest }))
  } catch (e) {
    toast.error(t('knowledge.exportFailed', { msg: (e as Error).message }))
  } finally {
    isExporting.value = false
  }
}

/** 创建今日日记：实现已抽到 composables/useDailyNote.ts（侧栏「今日日记」直达共用） */
function createDailyNote() {
  void openTodayNote()
}

/** P2-2：模板创建成功后打开新笔记 */
function onTemplateCreated(path: string) {
  showTemplateDialog.value = false
  workspaceStore.incrementFileTreeVersion()
  workspaceStore.openFile(path)
  router.push('/editor')
}

/** 切换待办完成状态 */

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
  loadWorkbenchStats()
  loadHotTags()
})

watch(() => currentWorkspace.value?.id, () => {
  loadAll()
  loadWorkbenchStats()
  loadHotTags()
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
            {{ greeting }}<span
              v-if="streakDays"
              class="kv-banner-streak"
            ><Flame :size="15" /> {{ streakDays }}</span>
          </h1>
          <p class="kv-banner-sub">
            <span>{{ todayLabel }}</span>
            <span v-if="currentWorkspace">
              <FolderOpen :size="12" />
              {{ currentWorkspace.name }} · {{ currentWorkspace.path }}
            </span>
            <span v-else>{{ t('knowledge.noWorkspace') }}</span>
          </p>
        </div>
      </div>

      <!-- 快速捕获：回车即建笔记 -->
      <div class="kv-capture">
        <PenLine :size="15" />
        <input
          v-model="captureText"
          class="kv-capture-input"
          type="text"
          :placeholder="t('knowledge.workbench.capturePlaceholder')"
          :disabled="!currentWorkspace"
          data-testid="quick-capture"
          @keyup.enter="onCaptureEnter"
        >
      </div>
      <div class="kv-banner-actions">
        <button
          class="kv-btn-primary"
          @click="handleCreateNewDoc"
        >
          <FilePlus :size="14" />
          <span>{{ t('knowledge.newDoc') }}</span>
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

    <!-- 今日焦点：可勾选待办 / 到期提醒（倒计时） / 今日编辑 -->
    <WorkbenchWidgets />

    <!-- 知识空间分类卡片（蓝图 2.1）：学习 / 项目 / 资料收藏 / 灵感收集箱 / 日记 -->
    <section class="kv-spaces">
      <button
        v-for="space in knowledgeSpaces"
        :key="space.key"
        class="kv-space-card"
        :data-testid="`space-${space.key}`"
        @click="openSpace(space)"
      >
        <div class="kv-space-icon">
          <component
            :is="space.icon"
            :size="18"
          />
        </div>
        <div class="kv-space-body">
          <div class="kv-space-name">{{ space.label }}</div>
          <div class="kv-space-desc">{{ space.desc }}</div>
        </div>
        <span class="kv-space-count">{{ space.count }}</span>
      </button>
    </section>

    <!-- 下区：最近活跃编辑 + 星标收藏 / 标签速览（UI-WORKBENCH-REDESIGN 4.2-4） -->
    <div class="kv-lower">
      <!-- 最近活跃编辑：全库按修改时间 top 8 -->
      <div
        class="kv-card"
        data-testid="recent-notes"
      >
        <div class="kv-card-header">
          <h3>
            <History :size="14" />
            <span>{{ t('knowledge.workbench.recentNotes') }}</span>
            <span class="kv-space-count">{{ recentNotes.length }}</span>
          </h3>
        </div>
        <div
          v-if="recentNotes.length === 0"
          class="wb-side-empty"
        >
          {{ t('knowledge.workbench.recentNotesEmpty') }}
        </div>
        <ul
          v-else
          class="wb-side-list"
        >
          <li
            v-for="f in recentNotes"
            :key="f.path"
            class="wb-side-item"
            :title="f.path"
            @click="openFile(f)"
          >
            <FileText :size="13" />
            <span>{{ f.name.replace(/\.(md|markdown)$/, '') }}</span>
            <span class="kv-recent-time">{{ formatRelativeTime(f.modTime) }}</span>
            <button
              class="kv-star-btn"
              :title="t('knowledge.workbench.starred')"
              @click.stop="toggleStar(f.path)"
            >
              <Star
                :size="13"
                :class="{ starred: isStarred(f.path) }"
              />
            </button>
          </li>
        </ul>
      </div>

      <!-- 右列：星标速览 + 标签速览 -->
      <div class="kv-lower-side">
        <div class="kv-card">
          <div class="kv-card-header">
            <h3>
              <Star :size="14" />
              <span>{{ t('knowledge.workbench.starred') }}</span>
            </h3>
          </div>
          <div
            v-if="starredFiles.length === 0"
            class="wb-side-empty"
          >
            {{ t('knowledge.workbench.starredEmpty') }}
          </div>
          <ul
            v-else
            class="wb-side-list"
          >
            <li
              v-for="f in starredFiles"
              :key="f.path"
              class="wb-side-item"
              :title="f.path"
              @click="openFile(f)"
            >
              <FileText :size="13" />
              <span>{{ f.name.replace(/\.(md|markdown)$/, '') }}</span>
            </li>
          </ul>
        </div>

        <div class="kv-card">
          <div class="kv-card-header">
            <h3>
              <Hash :size="14" />
              <span>{{ t('knowledge.workbench.tags') }}</span>
            </h3>
          </div>
          <div
            v-if="hotTags.length === 0"
            class="wb-side-empty"
          >
            {{ t('knowledge.workbench.tagsEmpty') }}
          </div>
          <div
            v-else
            class="kv-tag-chips"
          >
            <button
              v-for="tag in hotTags"
              :key="tag.name"
              class="kv-tag-chip"
              @click="router.push('/discover?tab=views&view=tags')"
            >
              # {{ tag.name }} <span class="kv-tag-count">{{ tag.count }}</span>
            </button>
          </div>
        </div>
      </div>
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

<!--
  样式说明：本视图的样式类全部 kv- 前缀命名，且文件浏览器已拆为
  components/knowledge/KnowledgeFileBrowser.vue（纯 props/emit 子组件）。
  Vue scoped 样式不穿透子组件，因此本块用非 scoped + 前缀命名约定代替
  scoped 隔离（等价于 BEM 约定），子组件内的 .kv-* class 才能命中。
-->
<style>
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

/* 主内容：文档主体 + 右列工作台出口 */
/* 下区：最近活跃编辑 + 星标/标签（UI-WORKBENCH-REDESIGN 4.2-4） */
.kv-lower {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(260px, 1fr);
  gap: var(--space-4);
  padding: var(--space-4) var(--space-8);
}

/* 知识空间卡片 */
.kv-spaces {
  display: grid;
  /* 五个空间（learning/projects/resources/inbox/daily）：自适应换行，窄屏不挤爆 */
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: var(--space-3);
  padding: var(--space-3) var(--space-8) 0;
}
.kv-space-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  transition: background var(--transition-fast), border-color var(--transition-fast), transform var(--transition-fast);
}
.kv-space-card:hover {
  background: var(--bg-hover);
  border-color: var(--border-accent, var(--accent));
  transform: translateY(-1px);
}
.kv-space-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: var(--radius-sm);
  background: var(--bg-active);
  color: var(--accent);
  flex-shrink: 0;
}
.kv-space-body {
  flex: 1;
  min-width: 0;
}
.kv-space-name {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
}
.kv-space-desc {
  font-size: var(--text-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.kv-space-count {
  font-size: var(--text-lg);
  font-weight: 700;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

/* 快速捕获条 */
.kv-capture {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 1;
  max-width: 420px;
  margin: 0 var(--space-4);
  padding: 0 var(--space-3);
  background: var(--bg-input, var(--bg-card));
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--text-muted);
  transition: border-color var(--transition-fast);
}
.kv-capture:focus-within {
  border-color: var(--border-accent, var(--accent));
}
.kv-capture-input {
  flex: 1;
  height: 34px;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-sm);
}
.kv-capture-input::placeholder {
  color: var(--text-muted);
}

/* Banner 连续记录徽标 */
.kv-banner-streak {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  margin-left: var(--space-3);
  padding: 2px 10px;
  border-radius: 999px;
  background: rgba(249, 115, 22, 0.14);
  color: #f97316;
  font-size: var(--text-sm);
  font-weight: 600;
  vertical-align: middle;
}

.wb-side-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.wb-side-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: var(--text-sm);
  color: var(--text-primary);
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  transition: background var(--transition-fast);
}
.wb-side-item:hover {
  background: var(--bg-hover);
}
.wb-side-item span {
  overflow: hidden;
  text-overflow: ellipsis;
}
.wb-side-empty {
  color: var(--text-muted);
  font-size: var(--text-xs);
  padding: var(--space-2) 0;
}
.kv-tag-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.kv-tag-chip {
  padding: 2px 10px;
  font-size: var(--text-xs);
  color: var(--text-secondary);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 999px;
  cursor: pointer;
  transition: color var(--transition-fast), border-color var(--transition-fast);
}
.kv-tag-chip:hover {
  color: var(--accent);
  border-color: var(--border-accent, var(--accent));
}
.kv-tag-count {
  color: var(--text-muted);
}

/* 下区布局细节：最近编辑时间戳与星标切换 */
.kv-recent-time {
  margin-left: auto;
  flex-shrink: 0;
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.kv-lower-side {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
}
.kv-star-btn {
  display: flex;
  align-items: center;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 2px;
  flex-shrink: 0;
  transition: color var(--transition-fast);
}
.kv-star-btn:hover {
  color: var(--warning, #e5a50a);
}
.kv-star-btn .starred {
  color: var(--warning, #e5a50a);
  fill: var(--warning, #e5a50a);
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
