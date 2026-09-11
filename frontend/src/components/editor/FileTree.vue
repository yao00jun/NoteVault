<script setup lang="ts">
import { computed, inject, provide, ref, watch, type Ref } from 'vue'
import {
  ChevronRight,
  ChevronDown,
  FileText,
  Folder,
  FolderOpen,
  MoreVertical,
  Archive,
  Trash2,
  Search,
  X,
  FolderCog,
  ChevronsDownUp,
} from '@lucide/vue'
import { normalizeNotePath } from '@/utils/navigation'
import { normalizeFolderDisplayPath } from '@/utils/folderDisplayNames'

export interface FileNode {
  name: string
  path: string
  fullPath: string
  isDir: boolean
  children?: FileNode[]
  size?: number
  modTime?: string
}

const props = withDefaults(
  defineProps<{
    nodes: FileNode[]
    activeFilePath?: string | null
    /** 工作台空间卡直达：展开并高亮指定目录（相对路径，如 "Learning/Java"） */
    focusFolder?: string | null
    folderDisplayNames?: Record<string, string>
    isRoot?: boolean
  }>(),
  {
    isRoot: true,
  },
)

const emit = defineEmits<{
  'open-file': [node: FileNode]
  'new-file': [parentPath: string]
  'new-folder': [parentPath: string]
  'rename': [node: FileNode]
  'delete': [node: FileNode]
  'archive': [node: FileNode]
  'trash': [node: FileNode]
}>()

const expandedDirs = ref<Set<string>>(new Set())
// Keep manual choices with the root tree when collapsed parents unmount their children.
const manualCollapseKey = 'notevault:file-tree-manual-collapse'
const manuallyCollapsedDirs = inject<Set<string>>(manualCollapseKey, () => new Set<string>(), true)
provide(manualCollapseKey, manuallyCollapsedDirs)
const contextMenu = ref<{ x: number; y: number; node: FileNode | null; parentPath: string } | null>(null)
/** 被聚焦高亮的目录路径（空间卡直达时短暂高亮，点击别处后清除） */
const focusedDir = ref<string | null>(null)
const activePath = computed(() => normalizeNotePath(props.activeFilePath || ''))

// Resynchronize available ancestors whenever the active path or directory nodes change.
watch(
  () => [activePath.value, props.nodes.filter(node => node.isDir).map(node => node.path)] as const,
  ([path], previous) => {
    if (!path) return
    const segments = path.split('/').filter(Boolean)
    const ancestors = new Set(segments.slice(0, -1).map((_, index) => segments.slice(0, index + 1).join('/')))
    // Remounted child trees keep manual choices until the selected file changes.
    if (previous && path !== previous[0]) {
      for (const ancestor of ancestors) manuallyCollapsedDirs.delete(ancestor)
    }
    for (const node of props.nodes) {
      const dirPath = normalizeNotePath(node.path)
      if (node.isDir && ancestors.has(dirPath) && !manuallyCollapsedDirs.has(dirPath)) {
        expandedDirs.value.add(dirPath)
      }
    }
  },
  { immediate: true },
)

function toggleDir(node: FileNode) {
  const path = normalizeNotePath(node.path)
  if (expandedDirs.value.has(path)) {
    expandedDirs.value.delete(path)
    manuallyCollapsedDirs.add(path)
  } else {
    expandedDirs.value.add(path)
    manuallyCollapsedDirs.delete(path)
  }
}

const searchKey = 'notevault:file-tree-search'
const injectedSearch = inject<Ref<string>>(searchKey, () => ref(''), true)
const searchQuery = props.isRoot ? ref('') : injectedSearch
if (props.isRoot) {
  provide(searchKey, searchQuery)
}

function isExpanded(node: FileNode) {
  if (searchQuery.value.trim()) return true
  return expandedDirs.value.has(normalizeNotePath(node.path))
}

function displayName(node: FileNode) {
  const alias = node.isDir ? props.folderDisplayNames?.[normalizeFolderDisplayPath(node.path)] : undefined
  return typeof alias === 'string' && alias.trim() ? alias.trim() : node.name
}

/**
 * 聚焦指定目录：递归树的每一层实例只负责展开自己这一段，
 * 剩余路径通过 focus-folder 转发给子层实例（子层有自己的 expandedDirs）。
 * 树未加载时（异步 GetFileTree）暂时匹配不到，由 nodes 变化触发重试。
 */
const childFocusFolder = ref<string | null>(null)

watch(
  () => [props.focusFolder, props.nodes] as const,
  ([folder]) => {
    if (typeof folder !== 'string' || !folder.trim()) return
    const segments = folder.split(/[\\/]+/).filter(Boolean)
    if (segments.length === 0) return
    const head = segments[0]
    const dir = props.nodes.find(
      (n) => n.isDir && (n.path === segments.join('/') || n.name === head),
    )
    if (!dir) return
    expandedDirs.value.add(normalizeNotePath(dir.path))
    if (segments.length > 1) {
      childFocusFolder.value = segments.slice(1).join('/')
    } else {
      focusedDir.value = dir.path
      // 高亮 2.4s 后自然消退，避免常亮干扰
      window.setTimeout(() => {
        if (focusedDir.value === dir.path) focusedDir.value = null
      }, 2400)
    }
  },
  { immediate: true },
)

const SYSTEM_DIR_NAMES = new Set(['assets', 'templates', '.templates', '.trash', '.notevault'])

function isSystemNode(node: FileNode): boolean {
  return node.isDir && SYSTEM_DIR_NAMES.has(node.name.toLowerCase())
}

const primaryNodes = computed(() => {
  if (!props.isRoot) return props.nodes
  return props.nodes.filter(n => !isSystemNode(n))
})

const systemNodes = computed(() => {
  if (!props.isRoot) return []
  return props.nodes.filter(n => isSystemNode(n))
})

const systemExpanded = ref(false)

function filterNodeList(list: FileNode[], query: string): FileNode[] {
  const q = query.toLowerCase()
  const result: FileNode[] = []
  for (const node of list) {
    if (node.isDir) {
      const filteredChildren = node.children ? filterNodeList(node.children, query) : []
      const nameMatches = node.name.toLowerCase().includes(q) || displayName(node).toLowerCase().includes(q)
      if (nameMatches || filteredChildren.length > 0) {
        result.push({
          ...node,
          children: filteredChildren.length > 0 ? filteredChildren : node.children,
        })
      }
    } else {
      if (node.name.toLowerCase().includes(q)) {
        result.push(node)
      }
    }
  }
  return result
}

const displayPrimaryNodes = computed(() => {
  if (!props.isRoot || !searchQuery.value.trim()) return primaryNodes.value
  return filterNodeList(primaryNodes.value, searchQuery.value.trim())
})

const displaySystemNodes = computed(() => {
  if (!props.isRoot || !searchQuery.value.trim()) return systemNodes.value
  return filterNodeList(systemNodes.value, searchQuery.value.trim())
})

function collapseAll() {
  expandedDirs.value.clear()
}

watch(
  activePath,
  (path) => {
    if (!path || !props.isRoot) return
    const segments = path.toLowerCase().split('/')
    if (segments.length > 0 && SYSTEM_DIR_NAMES.has(segments[0]!)) {
      systemExpanded.value = true
    }
  },
  { immediate: true },
)

watch(searchQuery, (q) => {
  if (!q.trim()) return
  function expandAllDirs(list: FileNode[]) {
    for (const n of list) {
      if (n.isDir) {
        expandedDirs.value.add(normalizeNotePath(n.path))
        if (n.children) expandAllDirs(n.children)
      }
    }
  }
  expandAllDirs(displayPrimaryNodes.value)
  if (displaySystemNodes.value.length > 0) {
    systemExpanded.value = true
    expandAllDirs(displaySystemNodes.value)
  }
})

function handleFileClick(node: FileNode) {
  if (node.isDir) {
    toggleDir(node)
  } else {
    emit('open-file', node)
  }
}

function handleContextMenu(e: MouseEvent, node: FileNode | null, parentPath: string) {
  e.preventDefault()
  contextMenu.value = { x: e.clientX, y: e.clientY, node, parentPath }
}

function closeContextMenu() {
  contextMenu.value = null
}

function handleNewFile(parentPath: string) {
  emit('new-file', parentPath)
  closeContextMenu()
}

function handleNewFolder(parentPath: string) {
  emit('new-folder', parentPath)
  closeContextMenu()
}

function handleRename(node: FileNode) {
  emit('rename', node)
  closeContextMenu()
}

function handleDelete(node: FileNode) {
  emit('delete', node)
  closeContextMenu()
}

function handleArchive(node: FileNode) {
  emit('archive', node)
  closeContextMenu()
}

function handleTrash(node: FileNode) {
  emit('trash', node)
  closeContextMenu()
}
</script>

<template>
  <div
    class="file-tree"
    :class="{ 'is-root': isRoot }"
    @click="closeContextMenu"
  >
    <!-- 顶部过滤与操作条（仅根树展示） -->
    <div
      v-if="isRoot"
      class="tree-toolbar"
    >
      <div class="tree-search-wrapper">
        <Search
          :size="13"
          class="search-icon"
        />
        <input
          v-model="searchQuery"
          type="text"
          class="tree-search-input"
          placeholder="过滤文档..."
          aria-label="过滤文档"
        >
        <button
          v-if="searchQuery"
          class="search-clear-btn"
          title="清空"
          @click="searchQuery = ''"
        >
          <X :size="12" />
        </button>
      </div>
      <div class="tree-actions">
        <button
          class="tree-action-btn"
          title="全部折叠"
          @click="collapseAll"
        >
          <ChevronsDownUp :size="14" />
        </button>
      </div>
    </div>

    <!-- 文件树 -->
    <div class="tree-nodes">
      <template
        v-for="node in (isRoot ? displayPrimaryNodes : nodes)"
        :key="node.path"
      >
        <div
          class="tree-node"
          :data-path="node.path"
          :class="{
            'is-dir': node.isDir,
            'is-active': !node.isDir && activePath === normalizeNotePath(node.path),
            'is-focused': node.isDir && focusedDir === node.path,
          }"
          @click="handleFileClick(node)"
          @contextmenu="handleContextMenu($event, node, node.isDir ? node.path : node.path.substring(0, node.path.lastIndexOf('/')))"
        >
          <span
            v-if="!node.isDir"
            class="node-indent"
          />
          <span
            v-if="node.isDir"
            class="node-toggle"
          >
            <ChevronRight
              v-if="!isExpanded(node)"
              :size="14"
            />
            <ChevronDown
              v-else
              :size="14"
            />
          </span>
          <span class="node-icon">
            <Folder
              v-if="node.isDir && !isExpanded(node)"
              :size="16"
            />
            <FolderOpen
              v-else-if="node.isDir"
              :size="16"
            />
            <FileText
              v-else
              :size="16"
            />
          </span>
          <span
            class="node-name"
            :title="node.path"
          >{{ displayName(node) }}</span>
        </div>

        <!-- 子节点 -->
        <div
          v-if="node.isDir && isExpanded(node) && node.children"
          class="tree-children"
        >
          <FileTree
            :nodes="node.children"
            :active-file-path="activeFilePath"
            :focus-folder="childFocusFolder"
            :folder-display-names="folderDisplayNames"
            :is-root="false"
            @open-file="(n) => emit('open-file', n)"
            @new-file="(p) => emit('new-file', p)"
            @new-folder="(p) => emit('new-folder', p)"
            @rename="(n) => emit('rename', n)"
            @delete="(n) => emit('delete', n)"
            @archive="(n) => emit('archive', n)"
            @trash="(n) => emit('trash', n)"
          />
        </div>
      </template>

      <!-- 底部系统与模板折叠区（仅根树展示） -->
      <div
        v-if="isRoot && systemNodes.length > 0"
        class="system-section"
      >
        <div
          class="tree-node system-header-node"
          :class="{ 'is-open': systemExpanded }"
          @click="systemExpanded = !systemExpanded"
        >
          <span class="node-toggle">
            <ChevronRight
              v-if="!systemExpanded"
              :size="14"
            />
            <ChevronDown
              v-else
              :size="14"
            />
          </span>
          <span class="node-icon">
            <FolderCog :size="15" />
          </span>
          <span class="node-name system-name">系统与模板</span>
          <span class="system-badge">{{ systemNodes.length }}</span>
        </div>

        <div
          v-if="systemExpanded"
          class="system-children"
        >
          <template
            v-for="node in (searchQuery.trim() ? displaySystemNodes : systemNodes)"
            :key="node.path"
          >
            <div
              class="tree-node"
              :data-path="node.path"
              :class="{
                'is-dir': node.isDir,
                'is-active': !node.isDir && activePath === normalizeNotePath(node.path),
                'is-focused': node.isDir && focusedDir === node.path,
              }"
              @click="handleFileClick(node)"
              @contextmenu="handleContextMenu($event, node, node.isDir ? node.path : node.path.substring(0, node.path.lastIndexOf('/')))"
            >
              <span
                v-if="!node.isDir"
                class="node-indent"
              />
              <span
                v-if="node.isDir"
                class="node-toggle"
              >
                <ChevronRight
                  v-if="!isExpanded(node)"
                  :size="14"
                />
                <ChevronDown
                  v-else
                  :size="14"
                />
              </span>
              <span class="node-icon">
                <Folder
                  v-if="node.isDir && !isExpanded(node)"
                  :size="16"
                />
                <FolderOpen
                  v-else-if="node.isDir"
                  :size="16"
                />
                <FileText
                  v-else
                  :size="16"
                />
              </span>
              <span
                class="node-name"
                :title="node.path"
              >{{ displayName(node) }}</span>
            </div>

            <!-- 子节点 -->
            <div
              v-if="node.isDir && isExpanded(node) && node.children"
              class="tree-children"
            >
              <FileTree
                :nodes="node.children"
                :active-file-path="activeFilePath"
                :focus-folder="childFocusFolder"
                :folder-display-names="folderDisplayNames"
                :is-root="false"
                @open-file="(n) => emit('open-file', n)"
                @new-file="(p) => emit('new-file', p)"
                @new-folder="(p) => emit('new-folder', p)"
                @rename="(n) => emit('rename', n)"
                @delete="(n) => emit('delete', n)"
                @archive="(n) => emit('archive', n)"
                @trash="(n) => emit('trash', n)"
              />
            </div>
          </template>
        </div>
      </div>
    </div>

    <!-- 右键菜单 -->
    <div
      v-if="contextMenu"
      class="context-menu"
      :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }"
      @click.stop
    >
      <button
        class="context-menu-item"
        @click="handleNewFile(contextMenu.node?.isDir ? contextMenu.node.path : contextMenu.parentPath)"
      >
        <FileText :size="14" />
        <span>新建文档</span>
      </button>
      <button
        class="context-menu-item"
        @click="handleNewFolder(contextMenu.node?.isDir ? contextMenu.node.path : contextMenu.parentPath)"
      >
        <Folder :size="14" />
        <span>新建文件夹</span>
      </button>
      <div
        v-if="contextMenu.node"
        class="context-menu-divider"
      />
      <button
        v-if="contextMenu.node && !contextMenu.node.isDir"
        class="context-menu-item"
        @click="handleArchive(contextMenu.node)"
      >
        <Archive :size="14" />
        <span>归档</span>
      </button>
      <button
        v-if="contextMenu.node && !contextMenu.node.isDir"
        class="context-menu-item"
        @click="handleTrash(contextMenu.node)"
      >
        <Trash2 :size="14" />
        <span>移动到回收站</span>
      </button>
      <button
        v-if="contextMenu.node"
        class="context-menu-item"
        @click="handleRename(contextMenu.node)"
      >
        <MoreVertical :size="14" />
        <span>重命名</span>
      </button>
      <button
        v-if="contextMenu.node"
        class="context-menu-item danger"
        @click="handleDelete(contextMenu.node)"
      >
        <span>永久删除</span>
      </button>
    </div>
  </div>
</template>

<script lang="ts">
// 递归组件需要 name
export default { name: 'FileTree' }
</script>

<style scoped>
.file-tree {
  width: 100%;
  font-size: var(--text-sm);
  position: relative;
}

.file-tree.is-root {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tree-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-sidebar);
  flex-shrink: 0;
}

.tree-search-wrapper {
  position: relative;
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
}

.search-icon {
  position: absolute;
  left: 7px;
  color: var(--text-muted);
  pointer-events: none;
}

.tree-search-input {
  width: 100%;
  height: 24px;
  padding: 0 20px 0 24px;
  font-size: var(--text-xs);
  color: var(--text-primary);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  outline: none;
  transition: border-color var(--transition-fast);
}

.tree-search-input:focus {
  border-color: var(--accent);
}

.tree-search-input::placeholder {
  color: var(--text-muted);
}

.search-clear-btn {
  position: absolute;
  right: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}

.search-clear-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tree-actions {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.tree-action-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.tree-action-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tree-nodes {
  padding: var(--space-1) var(--space-1);
}

.is-root > .tree-nodes {
  flex: 1;
  overflow-y: auto;
}

/* 系统与模板折叠区 */
.system-section {
  margin-top: var(--space-3);
  padding-top: var(--space-2);
  border-top: 1px dashed var(--border);
}

.system-header-node {
  opacity: 0.75;
  font-size: var(--text-xs);
}

.system-header-node:hover {
  opacity: 1;
}

.system-name {
  color: var(--text-muted);
  font-style: normal;
}

.system-badge {
  font-size: 10px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--bg-hover);
  color: var(--text-muted);
  margin-left: auto;
}

.system-children {
  margin-top: 2px;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: var(--space-1) var(--space-2);
  border-left: 2px solid transparent;
  border-radius: var(--radius-sm);
  cursor: pointer;
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tree-node:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tree-node.is-active {
  background: var(--accent-alpha);
  color: var(--text-primary);
  border-left-color: var(--accent);
}

/* 空间卡直达的高亮（短暂）：用 accent 描边吸睛，2.4s 后自动消退 */
.tree-node.is-focused {
  background: var(--bg-active);
  color: var(--accent);
  outline: 1px solid var(--border-accent, var(--accent));
  border-radius: var(--radius-sm);
}

.node-indent {
  width: 14px;
  flex-shrink: 0;
}

.node-toggle {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  flex-shrink: 0;
  color: var(--text-muted);
}

.node-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--text-muted);
}

.tree-node.is-active .node-icon {
  color: var(--accent);
}

.node-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tree-children {
  padding-left: var(--space-4);
}

.context-menu {
  position: fixed;
  z-index: 1000;
  min-width: 160px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-lg);
  padding: var(--space-1);
}

.context-menu-item {
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

.context-menu-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.context-menu-item.danger {
  color: #ef4444;
}

.context-menu-item.danger:hover {
  background: rgba(239, 68, 68, 0.1);
}

.context-menu-divider {
  height: 1px;
  background: var(--border);
  margin: var(--space-1) 0;
}
</style>
