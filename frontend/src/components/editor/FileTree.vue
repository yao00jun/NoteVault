<script setup lang="ts">
import { computed, inject, provide, ref, watch } from 'vue'
import { ChevronRight, ChevronDown, FileText, Folder, FolderOpen, MoreVertical, Archive, Trash2 } from '@lucide/vue'
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

const props = defineProps<{
  nodes: FileNode[]
  activeFilePath?: string | null
  /** 工作台空间卡直达：展开并高亮指定目录（相对路径，如 "Learning/Java"） */
  focusFolder?: string | null
  folderDisplayNames?: Record<string, string>
}>()

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
  ([path]) => {
    if (!path) return
    const segments = path.split('/').filter(Boolean)
    const ancestors = new Set(segments.slice(0, -1).map((_, index) => segments.slice(0, index + 1).join('/')))
    for (const node of props.nodes) {
      const dirPath = normalizeNotePath(node.path)
      if (node.isDir && ancestors.has(dirPath) && !manuallyCollapsedDirs.has(dirPath)) {
        expandedDirs.value.add(node.path)
      }
    }
  },
  { immediate: true },
)

function toggleDir(node: FileNode) {
  const path = normalizeNotePath(node.path)
  if (expandedDirs.value.has(node.path)) {
    expandedDirs.value.delete(node.path)
    manuallyCollapsedDirs.add(path)
  } else {
    expandedDirs.value.add(node.path)
    manuallyCollapsedDirs.delete(path)
  }
}

function isExpanded(node: FileNode) {
  return expandedDirs.value.has(node.path)
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
    expandedDirs.value.add(dir.path)
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
    @click="closeContextMenu"
  >
    <!-- 文件树 -->
    <div class="tree-nodes">
      <template
        v-for="node in nodes"
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
  height: 100%;
  overflow-y: auto;
  font-size: var(--text-sm);
  position: relative;
}

.tree-nodes {
  padding: 0 var(--space-1);
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
