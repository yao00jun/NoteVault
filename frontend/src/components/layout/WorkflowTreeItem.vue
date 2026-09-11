<script setup lang="ts">
import { computed } from 'vue'
import {
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  BookOpen,
  Rocket,
  Sparkles,
  Plus,
} from '@lucide/vue'
import { normalizeNotePath } from '@/utils/navigation'
import { normalizeFolderDisplayPath } from '@/utils/folderDisplayNames'
import type { FileNode } from '@/components/editor/FileTree.vue'

defineOptions({
  name: 'WorkflowTreeItem',
})

const props = withDefaults(
  defineProps<{
    node: FileNode
    depth?: number
    activePath?: string | null
    expandedDirs: Set<string>
    kind?: 'learning' | 'projects' | 'vault' | 'today' | 'filetree'
    folderDisplayNames?: Record<string, string>
    stripExtension?: boolean
    focusedDir?: string | null
  }>(),
  {
    depth: 0,
    activePath: null,
    kind: 'vault',
    folderDisplayNames: undefined,
    stripExtension: false,
    focusedDir: null,
  },
)

const emit = defineEmits<{
  'open-file': [node: FileNode]
  'toggle-dir': [node: FileNode]
  'quick-add': [node: FileNode]
  'context-menu': [event: MouseEvent, node: FileNode]
}>()

const isExpanded = computed(() => {
  const norm = normalizeNotePath(props.node.path)
  return props.expandedDirs.has(norm) || props.expandedDirs.has(props.node.path)
})

const isNodeActive = computed(() => {
  if (props.node.isDir || !props.activePath) return false
  return normalizeNotePath(props.activePath) === normalizeNotePath(props.node.path)
})

const isNodeFocused = computed(() => {
  if (!props.node.isDir || !props.focusedDir) return false
  return props.focusedDir === props.node.path || normalizeNotePath(props.focusedDir) === normalizeNotePath(props.node.path)
})

const childCount = computed(() => {
  if (!props.node.isDir || !props.node.children) return 0
  return props.node.children.length
})

const displayChildren = computed(() => {
  if (!props.node.children) return []
  if (props.kind === 'filetree') {
    return props.node.children
  }
  return [...props.node.children].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    return a.name.localeCompare(b.name, 'zh-CN')
  })
})

function folderIcon(node: FileNode) {
  if (props.kind === 'learning' && props.depth === 0) return BookOpen
  if (props.kind === 'projects' && props.depth === 0) return Rocket
  return isExpanded.value ? FolderOpen : Folder
}

function fileIcon(node: FileNode) {
  const lower = node.name.toLowerCase()
  if (lower === 'book.md') return BookOpen
  if (lower.endsWith('.canvas')) return Sparkles
  return FileText
}

const displayName = computed(() => {
  if (props.node.isDir && props.folderDisplayNames) {
    const alias = props.folderDisplayNames[normalizeFolderDisplayPath(props.node.path)]
    if (typeof alias === 'string' && alias.trim()) return alias.trim()
  }
  return props.stripExtension ? props.node.name.replace(/\.md$/i, '') : props.node.name
})
</script>

<template>
  <div class="workflow-tree-node">
    <!-- 目录行 -->
    <div
      v-if="node.isDir"
      class="tree-row folder-row tree-node is-dir"
      :class="{
        'is-focused': isNodeFocused,
        'is-active': isNodeActive,
      }"
      :data-path="node.path"
      :style="{ paddingLeft: `${depth * 14 + 10}px` }"
      :title="node.path"
      @click="emit('toggle-dir', node)"
      @contextmenu.prevent="emit('context-menu', $event, node)"
    >
      <button
        class="chevron-btn node-toggle"
        :aria-label="isExpanded ? '收起目录' : '展开目录'"
        @click.stop="emit('toggle-dir', node)"
      >
        <ChevronRight
          :size="12"
          class="chevron-icon"
          :class="{ expanded: isExpanded }"
        />
      </button>
      <component
        :is="folderIcon(node)"
        :size="14"
        class="item-icon folder-icon node-icon"
      />
      <span class="item-name node-name">{{ displayName }}</span>
      <span
        v-if="childCount > 0"
        class="count-badge"
      >{{ childCount }}</span>
      <button
        class="quick-add-btn"
        title="在当前目录新建文档"
        @click.stop="emit('quick-add', node)"
      >
        <Plus :size="12" />
      </button>
    </div>

    <!-- 文件行 -->
    <button
      v-else
      class="tree-row file-row tree-node"
      :class="{
        active: isNodeActive,
        'is-active': isNodeActive,
      }"
      :data-path="node.path"
      :style="{ paddingLeft: `${depth * 14 + 26}px` }"
      :title="node.path"
      @click="emit('open-file', node)"
      @contextmenu.prevent="emit('context-menu', $event, node)"
    >
      <component
        :is="fileIcon(node)"
        :size="13"
        class="item-icon file-icon node-icon"
      />
      <span class="item-name node-name">{{ displayName }}</span>
    </button>

    <!-- 子级递归 -->
    <div
      v-if="node.isDir && isExpanded && displayChildren.length > 0"
      class="tree-children"
      :style="{ '--guide-left': `${depth * 14 + 16}px` }"
    >
      <WorkflowTreeItem
        v-for="child in displayChildren"
        :key="child.path"
        :node="child"
        :depth="depth + 1"
        :active-path="activePath"
        :expanded-dirs="expandedDirs"
        :kind="kind"
        :folder-display-names="folderDisplayNames"
        :strip-extension="stripExtension"
        :focused-dir="focusedDir"
        @open-file="emit('open-file', $event)"
        @toggle-dir="emit('toggle-dir', $event)"
        @quick-add="emit('quick-add', $event)"
        @context-menu="(e, n) => emit('context-menu', e, n)"
      />
    </div>
  </div>
</template>

<style scoped>
.workflow-tree-node {
  display: flex;
  flex-direction: column;
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  width: 100%;
  box-sizing: border-box;
  padding-right: 8px;
  border-radius: var(--radius-sm, 6px);
  cursor: pointer;
  color: var(--text-secondary);
  font-size: var(--text-xs, 12px);
  user-select: none;
  background: transparent;
  border: none;
  text-align: left;
  transition: background 0.12s ease, color 0.12s ease;
}

.tree-row:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.05));
  color: var(--text-primary);
}

.chevron-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  border-radius: 3px;
}

.chevron-icon {
  transition: transform 0.15s ease;
}

.chevron-icon.expanded {
  transform: rotate(90deg);
}

.item-icon {
  flex-shrink: 0;
  opacity: 0.8;
}

.folder-icon {
  color: var(--accent, #eab308);
}

.file-icon {
  color: var(--text-muted);
}

.item-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.count-badge {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 9px;
  background: var(--bg-surface, rgba(255, 255, 255, 0.06));
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.quick-add-btn {
  display: none;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--text-muted);
  border-radius: 4px;
  cursor: pointer;
}

.quick-add-btn:hover {
  background: var(--bg-secondary, rgba(255, 255, 255, 0.12));
  color: var(--accent, #eab308);
}

.tree-row:hover .quick-add-btn {
  display: flex;
}

/* 阶段 3 视觉强化：当前文件高亮加左侧 2px accent 竖条 */
.file-row.active,
.file-row.is-active {
  background: color-mix(in srgb, var(--accent, #eab308) 15%, transparent);
  color: var(--accent, #eab308);
  font-weight: 500;
  border-left: 2px solid var(--accent, #eab308);
  border-top-left-radius: 0;
  border-bottom-left-radius: 0;
}

.file-row.active .file-icon,
.file-row.is-active .file-icon {
  color: var(--accent, #eab308);
  opacity: 1;
}

/* 空间卡直达的高亮（短暂）：用 accent 描边吸睛，2.4s 后自动消退 */
.tree-node.is-focused {
  background: var(--bg-active, rgba(255, 255, 255, 0.08));
  color: var(--accent, #eab308);
  outline: 1px solid var(--border-accent, var(--accent, #eab308));
  border-radius: var(--radius-sm, 6px);
}

/* 阶段 3 视觉强化：嵌套目录缩进引导线（子级区域左侧 1px 垂直细线，透明度 0.2） */
.tree-children {
  display: flex;
  flex-direction: column;
  position: relative;
}

.tree-children::before {
  content: '';
  position: absolute;
  top: 2px;
  bottom: 2px;
  left: var(--guide-left, 16px);
  width: 1px;
  background: var(--border-color, var(--text-muted, #888888));
  opacity: 0.2;
  pointer-events: none;
}
</style>
