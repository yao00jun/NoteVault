<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import {
  Library,
  MessageCircle,
  Square,
  Upload,
  Puzzle,
  FolderOpen,
  FileText,
  Tags,
  BarChart3,
  GitGraph,
  Table2,
  CheckSquare,
  Clock,
  History,
  Archive,
  Trash2,
  Sparkles,
  ChevronRight,
  ChevronDown,
  Plus,
  Search,
  Settings as SettingsIcon,
  ChevronsUpDown,
  Check,
} from 'lucide-vue-next'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { FileService, WorkspaceService } from '@bindings/github.com/notevault/notevault/index.js'

const { t } = useI18n()
const settingsStore = useSettingsStore()
const workspaceStore = useWorkspaceStore()
const router = useRouter()
const route = useRoute()

// 工作区下拉菜单状态
const workspaceMenuOpen = ref(false)
const workspaceMenuRef = ref<HTMLElement | null>(null)
const allWorkspaces = ref<{ id: string; name: string; path: string }[]>([])

async function loadAllWorkspaces() {
  try {
    const list = await WorkspaceService.ListWorkspaces()
    allWorkspaces.value = (list as any[]) || []
  } catch (e) {
    console.error('Failed to list workspaces:', e)
  }
}

function toggleWorkspaceMenu() {
  workspaceMenuOpen.value = !workspaceMenuOpen.value
  if (workspaceMenuOpen.value) loadAllWorkspaces()
}

async function switchWorkspace(wsId: string) {
  try {
    // 先设置当前工作区
    await WorkspaceService.SetCurrentWorkspace(wsId)
    // 然后获取详细信息
    const ws = await WorkspaceService.GetWorkspaceByID(wsId)
    if (ws) {
      workspaceStore.setCurrentWorkspace(ws as any)
      workspaceStore.incrementFileTreeVersion()
      // 切换后回到知识库主页
      router.push('/knowledge')
    }
  } catch (e) {
    console.error('Failed to switch workspace:', e)
    alert(t('sidebar.switchFailed', { msg: (e as Error).message }))
  }
  workspaceMenuOpen.value = false
}

async function createWorkspace() {
  const runtime = await import('@wailsio/runtime')
  try {
    const result = await runtime.Dialogs.OpenFile({
      Title: t('sidebar.chooseFolderTitle'),
      CanChooseDirectories: true,
      CanChooseFiles: false,
      AllowsMultipleSelection: false,
    })
    if (!result || (Array.isArray(result) && result.length === 0)) return
    const selectedPath = Array.isArray(result) ? result[0] : result
    const folderName = selectedPath.split(/[\\/]/).pop() || t('sidebar.defaultWorkspaceName')
    const name = prompt(t('sidebar.promptWorkspaceName'), folderName)
    if (!name) return
    const ws = await WorkspaceService.CreateWorkspace(name, selectedPath)
    if (ws) {
      workspaceStore.setCurrentWorkspace(ws as any)
      workspaceStore.incrementFileTreeVersion()
      router.push('/knowledge')
    }
  } catch (e) {
    // 用户取消文件夹选择是正常交互，不弹错误（兜底：后端 OpenFolderDialog 也会过滤此错误）
    if (isUserCancelledError(e)) return
    console.error('Failed to create workspace:', e)
    alert(t('sidebar.createWorkspaceFailed', { msg: (e as Error).message }))
  }
  workspaceMenuOpen.value = false
}

// 跨平台识别用户主动取消的错误
function isUserCancelledError(e: unknown): boolean {
  const msg = e instanceof Error ? e.message : String(e)
  return /cancel+(ed|led)\b/i.test(msg) || /cancelled\s*by\s*user/i.test(msg)
}

function openSettings() {
  router.push('/settings')
}

// 点击外部关闭工作区菜单
function handleClickOutside(e: MouseEvent) {
  if (!workspaceMenuRef.value) return
  if (!workspaceMenuRef.value.contains(e.target as Node)) {
    workspaceMenuOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})

async function createNewDoc() {
  if (!workspaceStore.currentWorkspace?.path) {
    alert(t('sidebar.selectOrCreateFirst'))
    router.push('/knowledge')
    return
  }
  const name = prompt(t('sidebar.promptFileName'), t('sidebar.untitledDoc'))
  if (!name) return
  try {
    const node = await FileService.CreateFile(
      workspaceStore.currentWorkspace.path,
      name,
      `# ${name.replace('.md', '')}\n\n`,
    )
    if (node) {
      router.push('/editor')
      workspaceStore.incrementFileTreeVersion()
      setTimeout(() => {
        workspaceStore.openFile((node as any).path)
        workspaceStore.setActiveFile((node as any).path)
      }, 100)
    }
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      alert(t('sidebar.fileExists'))
    } else {
      console.error('Failed to create file:', e)
      alert(t('sidebar.createFileFailed', { msg: (e as Error).message }))
    }
  }
}

const expandedGroups = ref<Record<string, boolean>>({
  library: true,
  tasks: true,
  workspace: true,
  manage: true,
})

interface NavItem {
  id: string
  label: string
  icon: any
  route?: string
  group: string
  /** 高亮使用：用于在当前页面时高亮 */
  activeOn?: string[]
}

// 侧边栏导航按"使用频率 + 功能类别"重分组：
// - library 知识库浏览（高频）：知识库、知识图谱
// - tasks 任务管理（高频 GTD）：待办、提醒
// - workspace 工作区浏览（高频）：文档、搜索、标签
// - manage 工具与数据管理（低频）：AI 问答、插件、数据导入、版本历史、归档、回收站
const navItems = computed<NavItem[]>(() => [
  { id: 'knowledge', label: t('sidebar.nav.knowledge'), icon: Library, route: '/knowledge', group: 'library' },
  { id: 'graph', label: t('sidebar.nav.graph'), icon: GitGraph, route: '/graph', group: 'library' },
  { id: 'reports', label: t('sidebar.nav.reports'), icon: BarChart3, route: '/reports', group: 'library' },
  { id: 'bases', label: t('sidebar.nav.bases'), icon: Table2, route: '/bases', group: 'library' },
  { id: 'canvas', label: t('sidebar.nav.canvas'), icon: Square, route: '/canvas', group: 'library' },
  { id: 'todos', label: t('sidebar.nav.todos'), icon: CheckSquare, route: '/todos', group: 'tasks' },
  { id: 'reminders', label: t('sidebar.nav.reminders'), icon: Clock, route: '/reminders', group: 'tasks' },
  { id: 'files', label: t('sidebar.nav.files'), icon: FolderOpen, route: '/editor', group: 'workspace' },
  { id: 'search', label: t('sidebar.nav.search'), icon: Search, route: '/search', group: 'workspace' },
  { id: 'tags', label: t('sidebar.nav.tags'), icon: Tags, route: '/tags', group: 'workspace' },
  { id: 'qna', label: t('sidebar.nav.qna'), icon: MessageCircle, route: '/qna', group: 'manage' },
  { id: 'plugins', label: t('sidebar.nav.plugins'), icon: Puzzle, route: '/plugins', group: 'manage' },
  { id: 'import', label: t('sidebar.nav.import'), icon: Upload, route: '/import', group: 'manage' },
  { id: 'history', label: t('sidebar.nav.history'), icon: History, route: '/history', group: 'manage' },
  { id: 'archive', label: t('sidebar.nav.archive'), icon: Archive, route: '/archive', group: 'manage' },
  { id: 'trash', label: t('sidebar.nav.trash'), icon: Trash2, route: '/trash', group: 'manage' },
  { id: 'compile', label: t('sidebar.nav.compile'), icon: Sparkles, route: '/compile', group: 'manage' },
])

const groupedItems = computed(() => {
  const groups: Record<string, NavItem[]> = {}
  for (const item of navItems.value) {
    if (!groups[item.group]) groups[item.group] = []
    groups[item.group].push(item)
  }
  return groups
})

const groupLabels = computed<Record<string, string>>(() => ({
  library: t('sidebar.groups.library'),
  tasks: t('sidebar.groups.tasks'),
  workspace: t('sidebar.groups.workspace'),
  manage: t('sidebar.groups.manage'),
}))

function toggleGroup(group: string) {
  expandedGroups.value[group] = !expandedGroups.value[group]
}

function isActive(item: NavItem) {
  return item.route === route.path
}

function navigate(item: NavItem) {
  if (item.route) {
    router.push(item.route)
  }
}

const sidebarWidth = computed(() =>
  settingsStore.settings.sidebarCollapsed ? '56px' : 'var(--sidebar-width)',
)
</script>

<template>
  <aside
    class="sidebar"
    :style="{ width: sidebarWidth }"
  >
    <!-- 工作区选择器（下拉菜单） -->
    <div
      ref="workspaceMenuRef"
      class="workspace-selector"
    >
      <button
        v-if="!settingsStore.settings.sidebarCollapsed"
        class="ws-btn"
        @click="toggleWorkspaceMenu"
      >
        <FolderOpen
          :size="16"
          class="ws-icon"
        />
        <span class="ws-name">
          {{ workspaceStore.currentWorkspace?.name || t('sidebar.selectWorkspace') }}
        </span>
        <ChevronsUpDown
          :size="14"
          class="ws-chevron"
        />
      </button>
      <button
        v-else
        class="ws-btn collapsed"
        :title="t('sidebar.workspaceTitle')"
        @click="toggleWorkspaceMenu"
      >
        <FolderOpen :size="18" />
      </button>

      <!-- 下拉菜单 -->
      <div
        v-if="workspaceMenuOpen && !settingsStore.settings.sidebarCollapsed"
        class="ws-dropdown"
      >
        <div class="ws-dropdown-header">
          <span>{{ t('sidebar.switchWorkspace') }}</span>
        </div>
        <div class="ws-dropdown-list">
          <button
            v-for="ws in allWorkspaces"
            :key="ws.id"
            class="ws-dropdown-item"
            :class="{ active: ws.id === workspaceStore.currentWorkspace?.id }"
            @click="switchWorkspace(ws.id)"
          >
            <span class="ws-item-name">{{ ws.name }}</span>
            <Check
              v-if="ws.id === workspaceStore.currentWorkspace?.id"
              :size="14"
            />
          </button>
          <div
            v-if="allWorkspaces.length === 0"
            class="ws-dropdown-empty"
          >
            {{ t('sidebar.noWorkspace') }}
          </div>
        </div>
        <div class="ws-dropdown-divider" />
        <button
          class="ws-dropdown-item primary"
          @click="createWorkspace"
        >
          <Plus :size="14" />
          <span>{{ t('sidebar.newWorkspace') }}</span>
        </button>
        <button
          class="ws-dropdown-item"
          @click="openSettings"
        >
          <SettingsIcon :size="14" />
          <span>{{ t('sidebar.manage') }}</span>
        </button>
      </div>
    </div>

    <!-- 新建按钮 -->
    <div
      v-if="!settingsStore.settings.sidebarCollapsed"
      class="new-section"
    >
      <button
        class="new-btn"
        @click="createNewDoc"
      >
        <Plus :size="14" />
        <span>{{ t('sidebar.newDoc') }}</span>
      </button>
    </div>

    <!-- 导航列表 -->
    <nav class="nav-list">
      <template
        v-for="(items, group) in groupedItems"
        :key="group"
      >
        <!-- 分组标题 -->
        <div
          v-if="!settingsStore.settings.sidebarCollapsed"
          class="nav-group-header"
          @click="toggleGroup(group)"
        >
          <component
            :is="expandedGroups[group] ? ChevronDown : ChevronRight"
            :size="12"
            class="group-chevron"
          />
          <span class="group-label">{{ groupLabels[group] }}</span>
        </div>

        <!-- 分组项 -->
        <div
          v-show="expandedGroups[group]"
          class="nav-group-items"
        >
          <button
            v-for="item in items"
            :key="item.id"
            class="nav-item"
            :data-testid="`nav-${item.id}`"
            :class="{
              collapsed: settingsStore.settings.sidebarCollapsed,
              active: isActive(item),
            }"
            :title="settingsStore.settings.sidebarCollapsed ? item.label : ''"
            @click="navigate(item)"
          >
            <component
              :is="item.icon"
              :size="16"
              class="nav-icon"
            />
            <span
              v-if="!settingsStore.settings.sidebarCollapsed"
              class="nav-label"
            >
              {{ item.label }}
            </span>
          </button>
        </div>
      </template>
    </nav>

    <!-- 底部：折叠按钮 -->
    <div class="sidebar-footer">
      <button
        class="collapse-btn"
        :title="settingsStore.settings.sidebarCollapsed ? t('sidebar.expandSidebar') : t('sidebar.collapseSidebar')"
        @click="settingsStore.toggleSidebar()"
      >
        <ChevronRight
          :size="16"
          :style="{ transform: settingsStore.settings.sidebarCollapsed ? 'rotate(0deg)' : 'rotate(180deg)' }"
        />
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border);
  flex-shrink: 0;
  transition: width var(--transition-base);
  overflow: hidden;
}

.workspace-selector {
  padding: var(--space-2);
  flex-shrink: 0;
  position: relative;
}

.ws-btn {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  border: 1px solid var(--border);
  color: var(--text-primary);
  font-size: var(--text-sm);
  font-weight: 500;
  transition: background var(--transition-fast), border-color var(--transition-fast);
}

.ws-btn:hover {
  background: var(--bg-hover);
  border-color: var(--border-accent);
}

.ws-btn.collapsed {
  justify-content: center;
  padding: var(--space-2);
}

.ws-icon {
  color: var(--accent);
  flex-shrink: 0;
}

.ws-name {
  flex: 1;
  text-align: left;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ws-chevron {
  color: var(--text-muted);
  flex-shrink: 0;
}

/* 工作区下拉菜单 */
.ws-dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: var(--space-2);
  right: var(--space-2);
  z-index: var(--z-dropdown);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.15);
  padding: var(--space-1);
  max-height: 320px;
  overflow-y: auto;
}

.ws-dropdown-header {
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.ws-dropdown-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.ws-dropdown-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.ws-dropdown-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.ws-dropdown-item.active {
  background: var(--bg-active, var(--accent-alpha, rgba(0, 122, 255, 0.1)));
  color: var(--accent);
}

.ws-dropdown-item.primary {
  color: var(--accent);
  font-weight: 500;
}

.ws-dropdown-item.primary:hover {
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
}

.ws-item-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ws-dropdown-empty {
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-align: center;
}

.ws-dropdown-divider {
  height: 1px;
  background: var(--border);
  margin: var(--space-1) 0;
}

/* 新建按钮 */
.new-section {
  padding: 0 var(--space-2) var(--space-2);
  flex-shrink: 0;
}

.new-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--accent);
  color: var(--text-inverse);
  font-size: var(--text-sm);
  font-weight: 500;
  transition: background var(--transition-fast), opacity var(--transition-fast);
}

.new-btn:hover {
  background: var(--accent-hover);
}

/* 导航列表 */
.nav-list {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-1);
}

.nav-group-header {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-2) var(--space-2) var(--space-1);
  cursor: pointer;
}

.group-chevron {
  color: var(--text-muted);
  flex-shrink: 0;
}

.group-label {
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.nav-group-items {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.nav-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.nav-item.active {
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
  color: var(--accent);
  font-weight: 600;
}

.nav-item.collapsed {
  justify-content: center;
  padding: var(--space-2);
}

.nav-icon {
  flex-shrink: 0;
}

/* 底部 */
.sidebar-footer {
  padding: var(--space-2);
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.collapse-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: var(--space-1);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: background var(--transition-fast), color var(--transition-fast);
}

.collapse-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
</style>
