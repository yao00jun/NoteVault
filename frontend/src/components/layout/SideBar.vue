<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch, type Component } from 'vue'
import {
  Library,
  FolderOpen,
  FileText,
  BarChart3,
  Clock,
  Trash2,
  ChevronRight,
  Plus,
  Settings as SettingsIcon,
  ChevronsUpDown,
  Check,
  Zap,
  BookOpen,
  Calendar,
  Rocket,
  Pin,
  PinOff,
  GripVertical,
} from '@lucide/vue'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { useWorkbenchStore } from '@/stores/workbench'
import { toWorkspace, toWorkspaceList } from '@/utils/workspace'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { FileService, WorkspaceService } from '@/api'
import { useToast } from '@/composables/useToast'
import { promptDialog } from '@/composables/usePrompt'
import { useDailyNote } from '@/composables/useDailyNote'
import { usePageContext } from '@/composables/usePageContext'
import { flushOpenEditor } from '@/composables/useEditorSession'
import { requestSourceImport } from '@/composables/useSourceImport'

const toast = useToast()
const { openTodayNote } = useDailyNote()

const { t } = useI18n()
const settingsStore = useSettingsStore()
const workspaceStore = useWorkspaceStore()
const workbenchStore = useWorkbenchStore()
const router = useRouter()
const route = useRoute()
const { context } = usePageContext()
const collapsed = computed(() => settingsStore.settings.sidebarCollapsed)
const newMenuOpen = ref(false)

// 工作区下拉菜单状态
const workspaceMenuOpen = ref(false)
const workspaceMenuRef = ref<HTMLElement | null>(null)
const allWorkspaces = ref<{ id: string; name: string; path: string }[]>([])

async function loadAllWorkspaces() {
  try {
    const list = await WorkspaceService.ListWorkspaces()
    allWorkspaces.value = toWorkspaceList(list)
  } catch (e) {
    console.error('Failed to list workspaces:', e)
  }
}

function toggleWorkspaceMenu() {
  workspaceMenuOpen.value = !workspaceMenuOpen.value
  if (workspaceMenuOpen.value) loadAllWorkspaces()
}

async function switchWorkspace(wsId: string) {
  if (!await flushOpenEditor()) { toast.warning('请先处理未保存的草稿或外部冲突，再切换工作区。'); return }
  try {
    // 先设置当前工作区
    await WorkspaceService.SetCurrentWorkspace(wsId)
    // 然后获取详细信息
    const ws = await WorkspaceService.GetWorkspaceByID(wsId)
    if (ws) {
      workspaceStore.setCurrentWorkspace(toWorkspace(ws))
      workspaceStore.incrementFileTreeVersion()
      router.push('/today')
    }
  } catch (e) {
    console.error('Failed to switch workspace:', e)
    toast.error(t('sidebar.switchFailed', { msg: (e as Error).message }))
  }
  workspaceMenuOpen.value = false
}

async function createWorkspace() {
  if (!await flushOpenEditor()) { toast.warning('请先处理未保存的草稿或外部冲突，再创建工作区。'); return }
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
    const name = await promptDialog({ message: t('sidebar.promptWorkspaceName'), defaultValue: folderName })
    if (!name) return
    const ws = await WorkspaceService.CreateWorkspace(name, selectedPath)
    if (ws) {
      workspaceStore.setCurrentWorkspace(toWorkspace(ws))
      workspaceStore.incrementFileTreeVersion()
      router.push('/today')
    }
  } catch (e) {
    // 用户取消文件夹选择是正常交互，不弹错误（兜底：后端 OpenFolderDialog 也会过滤此错误）
    if (isUserCancelledError(e)) return
    console.error('Failed to create workspace:', e)
    toast.error(t('sidebar.createWorkspaceFailed', { msg: (e as Error).message }))
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
  if (!(e.target as HTMLElement).closest('.new-action-menu')) newMenuOpen.value = false
  if (!workspaceMenuRef.value?.contains(e.target as Node)) {
    workspaceMenuOpen.value = false
    newMenuOpen.value = false
  }
  if (!pinMenuRef.value?.contains(e.target as Node)) pinMenu.value = null
}

function handleEscape(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    workspaceMenuOpen.value = false
    closePinMenu()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})

async function createNewDoc(folder?: string) {
  newMenuOpen.value = false
  if (!workspaceStore.currentWorkspace?.path) {
    toast.warning(t('sidebar.selectOrCreateFirst'))
    router.push('/today')
    return
  }
  const workspacePath = workspaceStore.currentWorkspace.path
  const destination = folder || context.value.folder || (route.path === '/vault' && typeof route.query.space === 'string' ? route.query.space : '') || (context.value.section === 'projects' ? 'Projects' : context.value.section === 'learning' ? 'Learning' : 'Inbox')
  const name = await promptDialog({ message: `${t('sidebar.promptFileName')} · 保存到 ${destination}/`, defaultValue: t('sidebar.untitledDoc') })
  if (!name || workspaceStore.currentWorkspace?.path !== workspacePath) return
  try {
    const node = await FileService.CreateFile(
      workspacePath,
      `${destination}/${name}`,
      `# ${name.replace('.md', '')}\n\n`,
    )
    if (node?.path && workspaceStore.currentWorkspace?.path === workspacePath) {
      workspaceStore.openFile(node.path)
      workspaceStore.incrementFileTreeVersion()
      await router.push({ path: '/editor', query: { file: node.path } })
    }
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      toast.warning(t('sidebar.fileExists'))
    } else {
      console.error('Failed to create file:', e)
      toast.error(t('sidebar.createFileFailed', { msg: (e as Error).message }))
    }
  }
}

defineExpose({ createNewDoc })

function createCollection(kind: 'project' | 'book') {
  newMenuOpen.value = false
  requestSourceImport({ kind, sourceType: 'empty' })
}

function importSources() {
  newMenuOpen.value = false
  requestSourceImport({ sourceType: 'folder', kind: context.value.section === 'projects' ? 'project' : context.value.section === 'learning' ? 'book' : 'topic' })
}

interface NavItem {
  id: string
  label: string
  description: string
  icon: Component
  route: string
  activeOn: string[]
  badge?: string | number
  alert?: boolean
}

const completedToday = computed(() => workbenchStore.todayTasks.filter(task => task.completed).length)
const navItems = computed<NavItem[]>(() => [
  {
    id: 'today',
    label: '今日',
    description: '个人工作台',
    icon: Zap,
    route: '/today',
    activeOn: ['/today', '/knowledge'],
    badge: workspaceStore.hasWorkspace && workbenchStore.lastUpdated
      ? `${completedToday.value}/${workbenchStore.todayTasks.length}` : undefined,
  },
  {
    id: 'projects',
    label: '项目',
    description: '组合看板',
    icon: Rocket,
    route: '/projects',
    activeOn: ['/projects'],
    badge: workspaceStore.hasWorkspace && workbenchStore.lastUpdated ? workbenchStore.projects.length : undefined,
    alert: workbenchStore.blockers.length > 0,
  },
  {
    id: 'learning',
    label: '学习',
    description: '技术书架',
    icon: BookOpen,
    route: '/learning',
    activeOn: ['/learning'],
    badge: workspaceStore.hasWorkspace && workbenchStore.lastUpdated && workbenchStore.reviewQueue.length ? `待复习 ${workbenchStore.reviewQueue.length}` : undefined,
  },
  {
    id: 'vault',
    label: '知识库',
    description: '知识沉淀',
    icon: Library,
    route: '/vault',
    activeOn: ['/vault', '/library', '/editor', '/insights', '/discover', '/review', '/canvas', '/archive', '/import', '/plugins'],
    badge: 'PARA',
  },
])

function isActive(item: NavItem) {
  return context.value.section === item.id
}

function openReport() {
  window.dispatchEvent(new CustomEvent('notevault:daily-report'))
}

interface PinnedItem { path: string; title: string }
const pins = computed(() => workspaceStore.pinnedItems.slice(0, 8))
const recentFiles = computed(() => workspaceStore.recentFiles.slice(0, 4))
const draggedPin = ref<string | null>(null)
const dragOverIndex = ref<number | null>(null)
const pinAnnouncement = ref('')
const pinMenu = ref<{ item: PinnedItem; x: number; y: number } | null>(null)
const pinMenuRef = ref<HTMLElement | null>(null)
let pinMenuTrigger: HTMLElement | null = null

function pinIcon(path: string) {
  if (/\/project\.md$/i.test(path)) return Rocket
  if (/\/book\.md$/i.test(path)) return BookOpen
  return FileText
}

function openFile(path: string) {
  workspaceStore.openFile(path)
  router.push({ path: '/editor', query: { file: path } })
}

function openPin(item: PinnedItem) {
  const path = item.path.replace(/\\/g, '/')
  const folder = path.slice(0, path.lastIndexOf('/'))
  if (/\/project\.md$/i.test(path)) router.push({ path: '/projects', query: { project: folder } })
  else if (/\/book\.md$/i.test(path)) router.push({ path: '/learning', query: { book: folder } })
  else openFile(path)
}

function pinCurrentFile() {
  if (!workspaceStore.activeFile) return
  if (!workspaceStore.togglePin(workspaceStore.activeFile)) toast.warning('最多固定 8 项，请先取消一个固定项')
}

function movePin(item: PinnedItem, index: number) {
  const target = Math.max(0, Math.min(index, pins.value.length - 1))
  workspaceStore.movePin(item.path, target)
  pinAnnouncement.value = `已将 ${item.title} 移到第 ${target + 1} 位`
}

function startPinDrag(event: DragEvent, item: PinnedItem) {
  draggedPin.value = item.path
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', item.path)
  }
  pinMenu.value = null
}

function finishPinDrag() {
  draggedPin.value = null
  dragOverIndex.value = null
}

function dropPin(index: number) {
  const item = pins.value.find(pin => pin.path === draggedPin.value)
  if (item) movePin(item, index)
  finishPinDrag()
}

function showPinMenu(event: MouseEvent, item: PinnedItem) {
  pinMenuTrigger = event.currentTarget as HTMLElement
  pinMenu.value = { item, x: Math.min(event.clientX, window.innerWidth - 180), y: Math.min(event.clientY, window.innerHeight - 60) }
  void nextTick(() => pinMenuRef.value?.querySelector<HTMLButtonElement>('button')?.focus())
}

function closePinMenu() {
  if (!pinMenu.value) return
  pinMenu.value = null
  pinMenuTrigger?.focus()
}

function unpinItem() {
  const item = pinMenu.value?.item
  if (item && workspaceStore.isPinned(item.path)) {
    workspaceStore.togglePin(item.path)
    pinAnnouncement.value = `已取消固定 ${item.title}`
  }
  closePinMenu()
}

function handlePinKey(event: KeyboardEvent, item: PinnedItem, index: number) {
  if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
    event.preventDefault()
    movePin(item, index + (event.key === 'ArrowUp' ? -1 : 1))
  } else if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
    event.preventDefault()
    pinMenuTrigger = event.currentTarget as HTMLElement
    const rect = pinMenuTrigger.getBoundingClientRect()
    pinMenu.value = { item, x: rect.left + 12, y: Math.min(rect.bottom, window.innerHeight - 60) }
    void nextTick(() => pinMenuRef.value?.querySelector<HTMLButtonElement>('button')?.focus())
  }
}

watch(() => workspaceStore.currentWorkspace?.path, () => {
  pinMenu.value = null
  finishPinDrag()
})

const indexStatus = computed(() => {
  if (!workspaceStore.hasWorkspace) return { state: 'idle', label: '选择工作区', detail: '打开工作区后开始索引' }
  if (workbenchStore.loading || workbenchStore.busy) return { state: 'syncing', label: '更新索引', detail: '正在读取或保存本地工作区内容' }
  if (workbenchStore.error) return { state: 'error', label: '索引失败', detail: workbenchStore.error }
  if (workbenchStore.lastUpdated) return { state: 'ready', label: '索引就绪', detail: `上次索引：${workbenchStore.lastUpdated}` }
  return { state: 'idle', label: '等待索引', detail: '尚未读取工作区内容' }
})

const sidebarWidth = computed(() => collapsed.value ? '56px' : 'var(--sidebar-width)')
</script>

<template>
  <aside
    class="sidebar"
    :class="{ collapsed }"
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
        :aria-expanded="workspaceMenuOpen"
        aria-haspopup="true"
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
        :aria-label="t('sidebar.workspaceTitle')"
        :aria-expanded="workspaceMenuOpen"
        aria-haspopup="true"
        @click="toggleWorkspaceMenu"
      >
        <FolderOpen :size="18" />
      </button>

      <!-- 下拉菜单 -->
      <div
        v-if="workspaceMenuOpen"
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

    <section
      class="sidebar-zone action-zone"
      aria-label="快捷动作"
    >
      <div
        v-if="!collapsed"
        class="section-heading"
      >
        快捷动作
      </div>
      <div class="action-grid">
        <div class="new-action-menu">
          <button
            class="action-btn new-btn"
            data-testid="action-new"
            title="新建"
            aria-label="新建"
            :aria-expanded="newMenuOpen"
            aria-haspopup="menu"
            @click.stop="newMenuOpen = !newMenuOpen"
          >
            <Plus :size="15" /><span v-if="!collapsed">新建</span>
          </button>
          <div
            v-if="newMenuOpen"
            class="new-action-options"
            role="menu"
          >
            <button
              role="menuitem"
              @click="createNewDoc()"
            >
              <FileText :size="15" />文档
            </button>
            <button
              role="menuitem"
              @click="createCollection('project')"
            >
              <Rocket :size="15" />项目
            </button>
            <button
              role="menuitem"
              @click="createCollection('book')"
            >
              <BookOpen :size="15" />技术分册
            </button>
            <button
              role="menuitem"
              @click="importSources"
            >
              <FolderOpen :size="15" />从资料创建
            </button>
          </div>
        </div>
        <button
          class="action-btn"
          data-testid="action-daily"
          :title="t('sidebar.nav.daily')"
          :aria-label="t('sidebar.nav.daily')"
          @click="openTodayNote"
        >
          <Calendar :size="15" />
          <span v-if="!collapsed">{{ t('sidebar.nav.daily') }}</span>
        </button>
        <button
          class="action-btn report-btn"
          data-testid="action-report"
          title="生成日报"
          aria-label="生成日报"
          @click="openReport"
        >
          <BarChart3 :size="15" />
          <span v-if="!collapsed">生成日报</span>
          <span
            v-if="!collapsed && workbenchStore.lastUpdated"
            class="report-count"
            :title="`今日已完成 ${completedToday} 项任务`"
          >{{ completedToday }}</span>
        </button>
      </div>
    </section>

    <div class="sidebar-scroll">
      <section
        class="sidebar-zone pins-zone"
        data-testid="sidebar-pins"
        aria-label="固定内容"
      >
        <div
          v-if="!collapsed"
          class="section-heading"
        >
          <Pin :size="12" />
          <span>固定</span>
          <span class="zone-count">{{ pins.length }}/8</span>
          <button
            v-if="workspaceStore.activeFile && workspaceStore.hasWorkspace"
            class="pin-current-btn"
            data-testid="sidebar-pin-current"
            :title="workspaceStore.isPinned(workspaceStore.activeFile) ? '取消固定当前文档' : '固定当前文档'"
            :aria-label="workspaceStore.isPinned(workspaceStore.activeFile) ? '取消固定当前文档' : '固定当前文档'"
            :aria-pressed="workspaceStore.isPinned(workspaceStore.activeFile)"
            @click="pinCurrentFile"
          >
            <Pin :size="13" />
          </button>
        </div>
        <p
          v-if="!pins.length && !collapsed"
          class="zone-empty"
        >
          为文档、项目或书籍加星，随时直达。
        </p>
        <button
          v-for="(item, index) in pins"
          :key="item.path"
          class="file-shortcut pin-item"
          data-testid="sidebar-pin"
          :class="{ 'drag-target': dragOverIndex === index && draggedPin !== item.path, 'is-dragging': draggedPin === item.path }"
          :title="`${item.title} · Alt+↑/↓ 调整顺序 · 右键取消固定`"
          :aria-label="item.title"
          draggable="true"
          @click="openPin(item)"
          @keydown="handlePinKey($event, item, index)"
          @contextmenu.prevent="showPinMenu($event, item)"
          @dragstart="startPinDrag($event, item)"
          @dragover.prevent="dragOverIndex = index"
          @dragleave="dragOverIndex = null"
          @drop.prevent="dropPin(index)"
          @dragend="finishPinDrag"
        >
          <component
            :is="pinIcon(item.path)"
            :size="15"
          />
          <span
            v-if="!collapsed"
            class="file-shortcut-label"
          >{{ item.title }}</span>
          <GripVertical
            v-if="!collapsed"
            :size="12"
            class="pin-grip"
            aria-hidden="true"
          />
        </button>
      </section>

      <nav
        class="sidebar-zone nav-list"
        aria-label="工作流"
      >
        <div
          v-if="!collapsed"
          class="section-heading"
        >
          工作流
        </div>
        <button
          v-for="item in navItems"
          :key="item.id"
          class="nav-item"
          :data-testid="`nav-${item.id}`"
          :class="{ collapsed, active: isActive(item) }"
          :aria-current="isActive(item) ? 'page' : undefined"
          :aria-label="item.label"
          :title="item.alert ? `${item.label} · ${workbenchStore.blockers.length} 个卡点待处理` : `${item.label} · ${item.description}`"
          @click="router.push(item.route)"
        >
          <component
            :is="item.icon"
            :size="17"
            class="nav-icon"
          />
          <span
            v-if="!collapsed"
            class="nav-label"
          >
            <span>{{ item.label }}</span>
            <small>{{ item.description }}</small>
          </span>
          <span
            v-if="!collapsed && item.badge !== undefined"
            class="nav-badge"
            :class="{ 'has-blockers': item.alert }"
          >{{ item.badge }}</span>
          <span
            v-if="collapsed && item.alert"
            class="blocker-dot"
            aria-label="有卡点待处理"
          />
        </button>
      </nav>

      <section
        class="sidebar-zone recent-zone"
        data-testid="sidebar-recent"
        aria-label="最近文档"
      >
        <div
          v-if="!collapsed"
          class="section-heading"
        >
          <Clock :size="12" />
          <span>最近</span>
        </div>
        <p
          v-if="!recentFiles.length && !collapsed"
          class="zone-empty"
        >
          打开过的文档会出现在这里。
        </p>
        <button
          v-for="file in recentFiles"
          :key="file.path"
          class="file-shortcut"
          data-testid="sidebar-recent-file"
          :class="{ active: route.path === '/editor' && workspaceStore.activeFile === file.path }"
          :title="file.path"
          :aria-label="file.title"
          @click="openFile(file.path)"
        >
          <FileText :size="14" />
          <span
            v-if="!collapsed"
            class="file-shortcut-label"
          >{{ file.title }}</span>
        </button>
      </section>
    </div>

    <div class="sidebar-footer">
      <div
        class="index-status"
        data-testid="sidebar-index-status"
        :data-state="indexStatus.state"
        :title="indexStatus.detail"
        :aria-label="indexStatus.label"
        role="status"
      >
        <span class="status-dot" />
        <span v-if="!collapsed">{{ indexStatus.label }}</span>
      </div>
      <div
        v-if="!collapsed"
        class="shortcut-hints"
      >
        <span><kbd>Ctrl+K</kbd> 搜索 / 命令</span>
        <span><kbd>Ctrl+J</kbd> Copilot</span>
      </div>
      <div class="footer-actions">
        <button
          class="footer-settings-btn"
          data-testid="sidebar-trash"
          :title="t('sidebar.nav.trash')"
          :aria-label="t('sidebar.nav.trash')"
          @click="router.push('/trash')"
        >
          <Trash2 :size="15" />
          <span v-if="!collapsed">{{ t('sidebar.nav.trash') }}</span>
        </button>
        <button
          class="footer-settings-btn"
          data-testid="sidebar-settings"
          :title="t('titlebar.settings')"
          :aria-label="t('titlebar.settings')"
          @click="router.push('/settings')"
        >
          <SettingsIcon :size="15" />
          <span v-if="!collapsed">{{ t('titlebar.settings') }}</span>
        </button>
        <button
          class="collapse-btn"
          :title="collapsed ? t('sidebar.expandSidebar') : t('sidebar.collapseSidebar')"
          :aria-label="collapsed ? t('sidebar.expandSidebar') : t('sidebar.collapseSidebar')"
          @click="settingsStore.toggleSidebar()"
        >
          <ChevronRight
            :size="15"
            :style="{ transform: collapsed ? 'rotate(0deg)' : 'rotate(180deg)' }"
          />
        </button>
      </div>
    </div>
    <span
      class="sr-only"
      aria-live="polite"
    >{{ pinAnnouncement }}</span>
    <Teleport to="body">
      <div
        v-if="pinMenu"
        ref="pinMenuRef"
        class="pin-context-menu"
        role="menu"
        aria-label="固定内容操作"
        :style="{ left: `${pinMenu.x}px`, top: `${pinMenu.y}px` }"
      >
        <button
          role="menuitem"
          data-testid="sidebar-unpin"
          @click="unpinItem"
        >
          <PinOff :size="14" />
          取消固定
        </button>
      </div>
    </Teleport>
  </aside>
</template>

<style scoped>
.new-action-menu { position: relative; min-width: 0; }
.new-action-menu > button { width: 100%; }
.new-action-options { position: absolute; top: calc(100% + 5px); left: 0; min-width: 170px; z-index: 60; padding: 5px; border: 1px solid var(--border); border-radius: var(--panel-radius, 8px); background: var(--surface-panel, var(--bg-card)); box-shadow: var(--shadow-lg); }
.new-action-options button { display: flex; align-items: center; gap: 8px; width: 100%; background: none; border: 0; color: var(--text-primary); text-align: left; padding: 10px; border-radius: var(--control-radius, 5px); font-size: 12px; }
.new-action-options button:hover { background: var(--bg-hover); }
.sidebar {
  display: flex;
  flex-direction: column;
  background: var(--bg-sidebar);
  /* Codex 式玻璃侧栏：主题的 --bg-sidebar 都是半透明值，blur 让内容透出柔光 */
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-right: 1px solid var(--border);
  flex-shrink: 0;
  transition: width var(--transition-base);
  overflow: visible;
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

/* Four navigation zones share a compact rhythm and a single scrolling region. */
.sidebar-zone { padding: 8px; }
.action-zone { flex-shrink: 0; border-bottom: 1px solid var(--border); }
.section-heading {
  display: flex; align-items: center; gap: 6px; min-height: 24px;
  padding: 0 6px 5px; color: var(--text-muted); font-size: 11px; font-weight: 600;
}
.zone-count { margin-left: auto; font-size: 10px; font-variant-numeric: tabular-nums; font-weight: 400; }
.action-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
.action-btn {
  display: flex; align-items: center; justify-content: center; gap: 6px;
  min-height: 34px; padding: 7px 5px; border: 1px solid var(--border);
  border-radius: var(--radius-sm); background: var(--bg-card);
  color: var(--text-secondary); font-size: 12px; white-space: nowrap;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.action-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.new-btn { background: var(--accent); border-color: var(--accent); color: var(--text-inverse); }
.new-btn:hover { background: var(--accent-hover); color: var(--text-inverse); }
.report-btn { grid-column: 1 / -1; color: var(--accent); background: var(--accent-alpha, rgba(0, 122, 255, 0.06)); }
.report-count { display: inline-flex; align-items: center; justify-content: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 10px; background: var(--bg-card); font-size: 10px; font-variant-numeric: tabular-nums; }
.sidebar-scroll { flex: 1; min-height: 0; overflow-y: auto; overflow-x: hidden; }
.sidebar-scroll .sidebar-zone + .sidebar-zone { border-top: 1px solid var(--border); }
.zone-empty { margin: 2px 6px 4px; color: var(--text-muted); font-size: 11px; line-height: 1.7; }
.pin-current-btn {
  display: flex; align-items: center; justify-content: center; width: 22px; height: 22px;
  border-radius: 4px; color: var(--text-muted);
}
.pin-current-btn:hover, .pin-current-btn[aria-pressed='true'] { color: var(--accent); background: var(--bg-hover); }
.file-shortcut {
  display: flex; align-items: center; gap: 8px; width: 100%; min-height: 30px;
  padding: 6px 8px; border: 1px solid transparent; border-radius: var(--radius-sm);
  color: var(--text-secondary); text-align: left; font-size: 12px;
  transition: background var(--transition-fast), border-color var(--transition-fast);
}
.file-shortcut svg { flex-shrink: 0; color: var(--text-muted); }
.file-shortcut:hover { background: var(--bg-hover); color: var(--text-primary); }
.file-shortcut.active { background: var(--accent-alpha, rgba(0, 122, 255, 0.08)); color: var(--accent); }
.file-shortcut-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pin-grip { opacity: 0; cursor: grab; }
.pin-item:hover .pin-grip, .pin-item:focus-visible .pin-grip { opacity: 0.6; }
.pin-item.drag-target { border-color: var(--accent); background: var(--accent-alpha, rgba(0, 122, 255, 0.08)); }
.pin-item.is-dragging { opacity: 0.45; }
.nav-item {
  position: relative; display: flex; align-items: center; gap: 9px; width: 100%;
  min-height: 40px; margin: 2px 0; padding: 8px 10px; border-radius: var(--radius-sm);
  color: var(--text-secondary); font-size: var(--text-sm); text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.nav-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.nav-item.active { background: var(--accent-alpha, rgba(0, 122, 255, 0.1)); color: var(--accent); font-weight: 600; }
.nav-icon { flex-shrink: 0; }
.nav-label { display: flex; align-items: baseline; gap: 7px; flex: 1; min-width: 0; white-space: nowrap; }
.nav-label small { color: var(--text-muted); font-size: 10px; font-weight: 400; }
.nav-badge {
  flex-shrink: 0; font-size: 10px; font-weight: 500; font-variant-numeric: tabular-nums;
  color: var(--text-muted); padding: 2px 5px; border-radius: 5px; background: var(--bg-card);
}
.nav-badge.has-blockers { color: var(--error, #d54c4c); background: rgba(213, 76, 76, 0.1); }
.blocker-dot { position: absolute; top: 5px; right: 6px; width: 5px; height: 5px; border-radius: 50%; background: var(--error, #d54c4c); }
.sidebar-footer { padding: 8px; border-top: 1px solid var(--border); flex-shrink: 0; }
.index-status { display: flex; align-items: center; gap: 6px; min-height: 20px; padding: 0 5px; color: var(--text-muted); font-size: 11px; }
.status-dot { width: 6px; height: 6px; flex-shrink: 0; border-radius: 50%; background: var(--text-muted); }
.index-status[data-state='ready'] .status-dot { background: var(--success, #2e9e64); }
.index-status[data-state='syncing'] .status-dot { background: #3b82f6; animation: status-pulse 1.4s ease-in-out infinite; }
.index-status[data-state='error'] .status-dot { background: var(--error, #d54c4c); }
.shortcut-hints { display: flex; flex-wrap: wrap; gap: 4px 10px; padding: 5px; color: var(--text-muted); font-size: 10px; }
.shortcut-hints span { white-space: nowrap; }
.shortcut-hints kbd { font-family: inherit; font-size: 10px; }
.footer-actions { display: flex; align-items: center; gap: 2px; margin-top: 3px; }
.footer-settings-btn, .collapse-btn {
  display: flex; align-items: center; justify-content: center; gap: 6px; min-height: 30px;
  padding: 6px; border-radius: var(--radius-sm); color: var(--text-secondary); font-size: 12px;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.footer-settings-btn { flex: 1; white-space: nowrap; }
.collapse-btn { flex-shrink: 0; color: var(--text-muted); }
.footer-settings-btn:hover, .collapse-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.sidebar.collapsed .action-grid { grid-template-columns: minmax(0, 1fr); }
.sidebar.collapsed .report-btn { grid-column: auto; }
.sidebar.collapsed .sidebar-zone { padding: 6px; }
.sidebar.collapsed .file-shortcut, .nav-item.collapsed { justify-content: center; padding: 8px; }
.sidebar.collapsed .footer-actions { flex-direction: column; }
.sidebar.collapsed .footer-settings-btn, .sidebar.collapsed .collapse-btn { width: 100%; }
.sidebar.collapsed .index-status { justify-content: center; }
.sidebar.collapsed .ws-dropdown { right: auto; width: 220px; }
.sidebar button:focus-visible, .pin-context-menu button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.pin-context-menu {
  position: fixed; z-index: var(--z-dropdown, 1000); min-width: 166px; padding: 4px;
  border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--bg-card); box-shadow: 0 6px 24px rgba(0, 0, 0, 0.16);
}
.pin-context-menu button {
  display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 10px;
  color: var(--text-primary); font-size: 12px; text-align: left; border-radius: 4px;
}
.pin-context-menu button:hover { background: var(--bg-hover); }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
@keyframes status-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }
@media (prefers-reduced-motion: reduce) { .index-status[data-state='syncing'] .status-dot { animation: none; } }
</style>
