import { inject, provide, ref, type Ref } from 'vue'
import { normalizeFolderDisplayPath } from '@/utils/folderDisplayNames'
import type { FileNode } from '@/components/editor/FileTree.vue'

export const FILE_TREE_SEARCH_KEY = 'notevault:file-tree-search'

export function filterNodeList(
  list: FileNode[],
  query: string,
  folderDisplayNames?: Record<string, string>,
): FileNode[] {
  const q = query.toLowerCase()
  const result: FileNode[] = []
  for (const node of list) {
    if (node.isDir) {
      const filteredChildren = node.children ? filterNodeList(node.children, query, folderDisplayNames) : []
      const alias = folderDisplayNames?.[normalizeFolderDisplayPath(node.path)]
      const name = typeof alias === 'string' && alias.trim() ? alias.trim() : node.name
      const nameMatches = node.name.toLowerCase().includes(q) || name.toLowerCase().includes(q)
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

export function useFileTreeSearch(options?: {
  isRoot?: boolean
  initialQuery?: string
}) {
  const isRoot = options?.isRoot ?? true
  const injectedSearch = inject<Ref<string>>(FILE_TREE_SEARCH_KEY, () => ref(''), true)
  const searchQuery = isRoot ? ref(options?.initialQuery ?? '') : injectedSearch

  if (isRoot) {
    provide(FILE_TREE_SEARCH_KEY, searchQuery)
  }

  function clearSearch() {
    searchQuery.value = ''
  }

  return {
    searchQuery,
    clearSearch,
    filterNodeList,
  }
}
