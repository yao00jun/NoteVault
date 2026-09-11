import { ref } from 'vue'
import type { FileNode } from '@/components/editor/FileTree.vue'

export interface FileTreeContextMenuState {
  x: number
  y: number
  node: FileNode | null
  parentPath: string
}

export interface FileTreeContextMenuActions {
  onNewFile?: (parentPath: string) => void
  onNewFolder?: (parentPath: string) => void
  onRename?: (node: FileNode) => void
  onDelete?: (node: FileNode) => void
  onArchive?: (node: FileNode) => void
  onTrash?: (node: FileNode) => void
}

export function useFileTreeContextMenu(actions?: FileTreeContextMenuActions) {
  const contextMenu = ref<FileTreeContextMenuState | null>(null)

  function openContextMenu(e: MouseEvent, node: FileNode | null, parentPath: string) {
    e.preventDefault()
    contextMenu.value = { x: e.clientX, y: e.clientY, node, parentPath }
  }

  function closeContextMenu() {
    contextMenu.value = null
  }

  function handleNewFile(parentPath: string) {
    actions?.onNewFile?.(parentPath)
    closeContextMenu()
  }

  function handleNewFolder(parentPath: string) {
    actions?.onNewFolder?.(parentPath)
    closeContextMenu()
  }

  function handleRename(node: FileNode) {
    actions?.onRename?.(node)
    closeContextMenu()
  }

  function handleDelete(node: FileNode) {
    actions?.onDelete?.(node)
    closeContextMenu()
  }

  function handleArchive(node: FileNode) {
    actions?.onArchive?.(node)
    closeContextMenu()
  }

  function handleTrash(node: FileNode) {
    actions?.onTrash?.(node)
    closeContextMenu()
  }

  return {
    contextMenu,
    openContextMenu,
    handleContextMenu: openContextMenu,
    closeContextMenu,
    handleNewFile,
    handleNewFolder,
    handleRename,
    handleDelete,
    handleArchive,
    handleTrash,
  }
}
