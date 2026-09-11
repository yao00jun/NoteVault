import { computed, inject, provide, ref, watch, type Ref, type ComputedRef } from 'vue'
import { normalizeNotePath } from '@/utils/navigation'
import type { FileNode } from '@/components/editor/FileTree.vue'

export const FILE_TREE_MANUAL_COLLAPSE_KEY = 'notevault:file-tree-manual-collapse'

export interface UseFileTreeExpansionOptions {
  nodes: Ref<FileNode[]> | ComputedRef<FileNode[]>
  activeFilePath?: Ref<string | null | undefined> | ComputedRef<string | null | undefined>
  focusFolder?: Ref<string | null | undefined> | ComputedRef<string | null | undefined>
  searchQuery?: Ref<string> | ComputedRef<string>
  isRoot?: boolean
}

export function useFileTreeExpansion(options: UseFileTreeExpansionOptions) {
  const isRoot = options.isRoot !== false
  const expandedDirs = ref<Set<string>>(new Set())

  // 每个根树拥有独立的手动折叠记忆集合，并向下注入给子树
  const manuallyCollapsedDirs = isRoot
    ? new Set<string>()
    : inject<Set<string>>(FILE_TREE_MANUAL_COLLAPSE_KEY, () => new Set<string>(), true)

  if (isRoot) {
    provide(FILE_TREE_MANUAL_COLLAPSE_KEY, manuallyCollapsedDirs)
  }

  const focusedDir = ref<string | null>(null)
  const childFocusFolder = ref<string | null>(null)
  const activePath = computed(() => normalizeNotePath(options.activeFilePath?.value || ''))

  // 递归树查找匹配路径的所有祖先路径
  function collectAncestors(nodes: FileNode[], targetNorm: string, set: Set<string>) {
    for (const node of nodes) {
      const norm = normalizeNotePath(node.path)
      if (node.isDir) {
        if (targetNorm === norm || targetNorm.startsWith(norm + '/')) {
          set.add(norm)
          set.add(node.path)
          if (node.children) {
            collectAncestors(node.children, targetNorm, set)
          }
        }
      }
    }
  }

  // 当活跃文档或节点列表变化时，自动展开当前文件所有的祖先目录
  watch(
    () => [activePath.value, options.nodes.value] as const,
    ([path], previous) => {
      if (!path) return
      const segments = path.split('/').filter(Boolean)
      const ancestorSegments = new Set<string>()
      for (let i = 1; i < segments.length; i++) {
        ancestorSegments.add(segments.slice(0, i).join('/'))
      }

      // 如果切换了不同的激活文件，清除对应祖先的手动收起记录
      if (previous && path !== previous[0]) {
        for (const ancestor of ancestorSegments) {
          manuallyCollapsedDirs.delete(ancestor)
        }
      }

      const allAncestors = new Set<string>(ancestorSegments)
      collectAncestors(options.nodes.value, path, allAncestors)

      for (const dir of allAncestors) {
        const norm = normalizeNotePath(dir)
        if (!manuallyCollapsedDirs.has(norm) && !manuallyCollapsedDirs.has(dir)) {
          expandedDirs.value.add(norm)
          expandedDirs.value.add(dir)
        }
      }
    },
    { immediate: true, deep: true },
  )

  function toggleDir(node: FileNode) {
    const norm = normalizeNotePath(node.path)
    const raw = node.path
    if (expandedDirs.value.has(norm) || expandedDirs.value.has(raw)) {
      expandedDirs.value.delete(norm)
      expandedDirs.value.delete(raw)
      manuallyCollapsedDirs.add(norm)
      manuallyCollapsedDirs.add(raw)
    } else {
      expandedDirs.value.add(norm)
      expandedDirs.value.add(raw)
      manuallyCollapsedDirs.delete(norm)
      manuallyCollapsedDirs.delete(raw)
    }
  }

  function collapseAll() {
    expandedDirs.value.clear()
  }

  function isExpanded(node: FileNode): boolean {
    if (options.searchQuery?.value.trim()) return true
    const norm = normalizeNotePath(node.path)
    return expandedDirs.value.has(norm) || expandedDirs.value.has(node.path)
  }

  // 空间卡直达指定目录聚焦高亮：自动展开祖先链并触发短暂高亮
  watch(
    () => [options.focusFolder?.value, options.nodes.value] as const,
    ([folder]) => {
      if (typeof folder !== 'string' || !folder.trim()) return
      const normalized = normalizeNotePath(folder)
      const segments = normalized.split('/').filter(Boolean)
      for (let i = 1; i <= segments.length; i++) {
        const prefix = segments.slice(0, i).join('/')
        expandedDirs.value.add(prefix)
      }

      function expandMatching(nodes: FileNode[]) {
        for (const node of nodes) {
          const norm = normalizeNotePath(node.path)
          if (node.isDir) {
            if (normalized === norm || normalized.startsWith(norm + '/')) {
              expandedDirs.value.add(norm)
              expandedDirs.value.add(node.path)
              if (normalized === norm || node.name === segments[segments.length - 1]) {
                focusedDir.value = node.path
                window.setTimeout(() => {
                  if (focusedDir.value === node.path) focusedDir.value = null
                }, 2400)
              }
              if (node.children) expandMatching(node.children)
            }
          }
        }
      }
      expandMatching(options.nodes.value)
    },
    { immediate: true, deep: true },
  )

  return {
    expandedDirs,
    manuallyCollapsedDirs,
    focusedDir,
    childFocusFolder,
    activePath,
    toggleDir,
    collapseAll,
    isExpanded,
  }
}
