<!--
  FileTree.vue: 编辑器专用文件树容器组件
  - 职责边界：负责文件树顶部工具栏（搜索过滤、全部折叠）、系统与模板文件夹分组收纳及右键操作弹层管理。
  - 节点渲染：全部委托给通用轻量组件 WorkflowTreeItem 递归渲染，不再手写私有节点与子树。
  - 状态抽取：展开/折叠逻辑位于 useFileTreeExpansion，搜索过滤位于 useFileTreeSearch，右键操作位于 useFileTreeContextMenu。
-->
<script setup lang="ts">
import { computed, ref, watch, toRef } from 'vue'
import {
  ChevronRight,
  ChevronDown,
  FileText,
  Folder,
  MoreVertical,
  Archive,
  Trash2,
  Search,
  X,
  FolderCog,
  ChevronsDownUp,
} from '@lucide/vue'
import { normalizeNotePath } from '@/utils/navigation'
import WorkflowTreeItem from '@/components/layout/WorkflowTreeItem.vue'
import { useFileTreeExpansion } from '@/composables/useFileTreeExpansion'
import { useFileTreeSearch } from '@/composables/useFileTreeSearch'
import { useFileTreeContextMenu } from '@/composables/useFileTreeContextMenu'

export interface FileNode {
  name: string
  path: string
  fullPath: string
  isDir: boolean
  children?: FileNode[]
  size?: number
  modTime?: string
}

defineOptions({ name: 'FileTree' })

const props = withDefaults(
  defineProps<{
    nodes: FileNode[]
    activeFilePath?: string | null
    focusFolder?: string | null
    folderDisplayNames?: Record<string, string>
    isRoot?: boolean
  }>(),
  { isRoot: true },
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

// 1. 搜索过滤
const { searchQuery, clearSearch, filterNodeList } = useFileTreeSearch({ isRoot: props.isRoot })

// 2. 目录展开/收起与定位
const { expandedDirs, focusedDir, activePath, toggleDir, collapseAll } = useFileTreeExpansion({
  nodes: toRef(props, 'nodes'),
  activeFilePath: toRef(props, 'activeFilePath'),
  focusFolder: toRef(props, 'focusFolder'),
  searchQuery,
  isRoot: props.isRoot,
})

// 3. 右键菜单与操作分发
const {
  contextMenu,
  openContextMenu,
  closeContextMenu,
  handleNewFile,
  handleNewFolder,
  handleRename,
  handleDelete,
  handleArchive,
  handleTrash,
} = useFileTreeContextMenu({
  onNewFile: (p) => emit('new-file', p),
  onNewFolder: (p) => emit('new-folder', p),
  onRename: (n) => emit('rename', n),
  onDelete: (n) => emit('delete', n),
  onArchive: (n) => emit('archive', n),
  onTrash: (n) => emit('trash', n),
})

function onNodeContextMenu(e: MouseEvent, n: FileNode) {
  openContextMenu(e, n, n.isDir ? n.path : n.path.substring(0, n.path.lastIndexOf('/')))
}

// 4. 系统与模板目录收纳分组
const SYSTEM_DIR_NAMES = new Set(['assets', 'templates', '.templates', '.trash', '.notevault'])
function isSystemNode(node: FileNode): boolean {
  return node.isDir && SYSTEM_DIR_NAMES.has(node.name.toLowerCase())
}

const primaryNodes = computed(() => (props.isRoot ? props.nodes.filter((n) => !isSystemNode(n)) : props.nodes))
const systemNodes = computed(() => (props.isRoot ? props.nodes.filter((n) => isSystemNode(n)) : []))
const systemExpanded = ref(false)

const displayPrimaryNodes = computed(() => {
  if (!props.isRoot || !searchQuery.value.trim()) return primaryNodes.value
  return filterNodeList(primaryNodes.value, searchQuery.value.trim(), props.folderDisplayNames)
})

const displaySystemNodes = computed(() => {
  if (!props.isRoot || !searchQuery.value.trim()) return systemNodes.value
  return filterNodeList(systemNodes.value, searchQuery.value.trim(), props.folderDisplayNames)
})

watch(activePath, (path) => {
  if (!path || !props.isRoot) return
  const segments = path.toLowerCase().split('/')
  if (segments.length > 0 && SYSTEM_DIR_NAMES.has(segments[0]!)) systemExpanded.value = true
}, { immediate: true })

watch(searchQuery, (q) => {
  if (!q.trim()) return
  const expandAll = (list: FileNode[]) => {
    for (const n of list) {
      if (n.isDir) {
        expandedDirs.value.add(normalizeNotePath(n.path))
        if (n.children) expandAll(n.children)
      }
    }
  }
  expandAll(displayPrimaryNodes.value)
  if (displaySystemNodes.value.length > 0) {
    systemExpanded.value = true
    expandAll(displaySystemNodes.value)
  }
})

const menuItems = computed(() => {
  if (!contextMenu.value) return []
  const { node, parentPath } = contextMenu.value
  const target = node?.isDir ? node.path : parentPath
  const items: Array<{ key: string; label: string; icon: any; danger?: boolean; dividerBefore?: boolean; action: () => void }> = [
    { key: 'new-file', label: '新建文档', icon: FileText, action: () => handleNewFile(target) },
    { key: 'new-folder', label: '新建文件夹', icon: Folder, action: () => handleNewFolder(target) },
  ]
  if (node && !node.isDir) {
    items.push(
      { key: 'archive', label: '归档', icon: Archive, dividerBefore: true, action: () => handleArchive(node) },
      { key: 'trash', label: '移动到回收站', icon: Trash2, action: () => handleTrash(node) },
    )
  }
  if (node) {
    items.push(
      { key: 'rename', label: '重命名', icon: MoreVertical, dividerBefore: !items.some(i => i.dividerBefore), action: () => handleRename(node) },
      { key: 'delete', label: '永久删除', icon: null, danger: true, action: () => handleDelete(node) },
    )
  }
  return items
})
</script>

<template>
  <div class="file-tree" :class="{ 'is-root': isRoot }" @click="closeContextMenu">
    <!-- 顶部过滤与操作条（仅根树展示） -->
    <div v-if="isRoot" class="tree-toolbar">
      <div class="tree-search-wrapper">
        <Search :size="13" class="search-icon" />
        <input
          v-model="searchQuery"
          type="text"
          class="tree-search-input"
          placeholder="过滤文档..."
          aria-label="过滤文档"
        >
        <button v-if="searchQuery" class="search-clear-btn" title="清空" @click="clearSearch">
          <X :size="12" />
        </button>
      </div>
      <div class="tree-actions">
        <button class="tree-action-btn" title="全部折叠" @click="collapseAll">
          <ChevronsDownUp :size="14" />
        </button>
      </div>
    </div>

    <!-- 文件树主区域（委托 WorkflowTreeItem 统一渲染） -->
    <div class="tree-nodes">
      <WorkflowTreeItem
        v-for="node in (isRoot ? displayPrimaryNodes : nodes)"
        :key="node.path"
        :node="node"
        :depth="0"
        :active-path="activePath"
        :expanded-dirs="expandedDirs"
        :kind="'filetree'"
        :folder-display-names="folderDisplayNames"
        :focused-dir="focusedDir"
        @open-file="emit('open-file', $event)"
        @toggle-dir="toggleDir"
        @quick-add="handleNewFile($event.path)"
        @context-menu="onNodeContextMenu"
      />

      <!-- 底部系统与模板折叠区（仅根树展示） -->
      <div v-if="isRoot && systemNodes.length > 0" class="system-section">
        <div
          class="tree-node system-header-node"
          :class="{ 'is-open': systemExpanded }"
          @click="systemExpanded = !systemExpanded"
        >
          <span class="node-toggle">
            <ChevronRight v-if="!systemExpanded" :size="14" />
            <ChevronDown v-else :size="14" />
          </span>
          <span class="node-icon"><FolderCog :size="15" /></span>
          <span class="node-name system-name">系统与模板</span>
          <span class="system-badge">{{ systemNodes.length }}</span>
        </div>

        <div v-if="systemExpanded" class="system-children">
          <WorkflowTreeItem
            v-for="node in (searchQuery.trim() ? displaySystemNodes : systemNodes)"
            :key="node.path"
            :node="node"
            :depth="0"
            :active-path="activePath"
            :expanded-dirs="expandedDirs"
            :kind="'filetree'"
            :folder-display-names="folderDisplayNames"
            :focused-dir="focusedDir"
            @open-file="emit('open-file', $event)"
            @toggle-dir="toggleDir"
            @quick-add="handleNewFile($event.path)"
            @context-menu="onNodeContextMenu"
          />
        </div>
      </div>
    </div>

    <!-- 右键菜单弹层 -->
    <div
      v-if="contextMenu"
      class="context-menu"
      :style="{ left: `${contextMenu.x}px`, top: `${contextMenu.y}px` }"
      @click.stop
    >
      <template v-for="item in menuItems" :key="item.key">
        <div v-if="item.dividerBefore" class="context-menu-divider" />
        <button
          class="context-menu-item"
          :class="{ danger: item.danger }"
          @click="item.action()"
        >
          <component :is="item.icon" v-if="item.icon" :size="14" />
          <span>{{ item.label }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.file-tree { width: 100%; font-size: var(--text-sm); position: relative; }
.file-tree.is-root { height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.tree-toolbar { display: flex; align-items: center; gap: var(--space-1); padding: 6px 8px; border-bottom: 1px solid var(--border); background: var(--bg-sidebar); flex-shrink: 0; }
.tree-search-wrapper { position: relative; flex: 1; min-width: 0; display: flex; align-items: center; }
.search-icon { position: absolute; left: 7px; color: var(--text-muted); pointer-events: none; }
.tree-search-input { width: 100%; height: 24px; padding: 0 20px 0 24px; font-size: var(--text-xs); color: var(--text-primary); background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-sm); outline: none; transition: border-color var(--transition-fast); }
.tree-search-input:focus { border-color: var(--accent); }
.tree-search-input::placeholder { color: var(--text-muted); }
.search-clear-btn { position: absolute; right: 4px; display: flex; align-items: center; justify-content: center; width: 16px; height: 16px; border: none; border-radius: 50%; background: transparent; color: var(--text-muted); cursor: pointer; padding: 0; }
.search-clear-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.tree-actions { display: flex; align-items: center; flex-shrink: 0; }
.tree-action-btn { display: flex; align-items: center; justify-content: center; width: 24px; height: 24px; border: none; border-radius: var(--radius-sm); background: transparent; color: var(--text-muted); cursor: pointer; transition: background var(--transition-fast), color var(--transition-fast); }
.tree-action-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.tree-nodes { padding: var(--space-1) var(--space-1); }
.is-root > .tree-nodes { flex: 1; overflow-y: auto; }
.system-section { margin-top: var(--space-3); padding-top: var(--space-2); border-top: 1px dashed var(--border); }
.system-header-node { display: flex; align-items: center; gap: 4px; padding: var(--space-1) var(--space-2); border-radius: var(--radius-sm); cursor: pointer; color: var(--text-secondary); opacity: 0.75; font-size: var(--text-xs); user-select: none; transition: background var(--transition-fast), color var(--transition-fast); }
.system-header-node:hover { opacity: 1; background: var(--bg-hover); }
.system-name { color: var(--text-muted); font-style: normal; }
.system-badge { font-size: 10px; padding: 0 5px; border-radius: 999px; background: var(--bg-hover); color: var(--text-muted); margin-left: auto; }
.system-children { margin-top: 2px; }
.node-toggle, .node-icon { display: flex; align-items: center; justify-content: center; width: 14px; flex-shrink: 0; color: var(--text-muted); }
.node-name { flex: 1; overflow: hidden; text-overflow: ellipsis; }
.context-menu { position: fixed; z-index: 1000; min-width: 160px; background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-md); box-shadow: var(--shadow-lg); padding: var(--space-1); }
.context-menu-item { display: flex; align-items: center; gap: var(--space-2); width: 100%; padding: var(--space-2) var(--space-3); border-radius: var(--radius-sm); color: var(--text-secondary); font-size: var(--text-sm); text-align: left; border: none; background: transparent; cursor: pointer; transition: background var(--transition-fast), color var(--transition-fast); }
.context-menu-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.context-menu-item.danger { color: #ef4444; }
.context-menu-item.danger:hover { background: rgba(239, 68, 68, 0.1); }
.context-menu-divider { height: 1px; background: var(--border); margin: var(--space-1) 0; }
</style>
