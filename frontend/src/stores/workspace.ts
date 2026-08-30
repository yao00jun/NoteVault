import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Workspace, FileNode } from '@/types'

export const useWorkspaceStore = defineStore('workspace', () => {
  const workspaces = ref<Workspace[]>([])
  const currentWorkspace = ref<Workspace | null>(null)
  const fileTree = ref<FileNode | null>(null)
  const openFiles = ref<string[]>([])
  const activeFile = ref<string | null>(null)
  const recentFiles = ref<{ path: string; title: string; openedAt: string }[]>([])
  const fileTreeVersion = ref(0) // 文件树版本号，变化时通知刷新

  const hasWorkspace = computed(() => currentWorkspace.value !== null)

  function setCurrentWorkspace(ws: Workspace | null) {
    currentWorkspace.value = ws
    if (ws) {
      ws.lastOpenedAt = new Date().toISOString()
    }
  }

  function addWorkspace(ws: Workspace) {
    workspaces.value.push(ws)
  }

  function openFile(path: string) {
    if (!openFiles.value.includes(path)) {
      openFiles.value.push(path)
    }
    activeFile.value = path
    // 添加到最近文件
    const existing = recentFiles.value.findIndex((f) => f.path === path)
    if (existing >= 0) {
      recentFiles.value.splice(existing, 1)
    }
    recentFiles.value.unshift({
      path,
      title: path.split('/').pop() || path,
      openedAt: new Date().toISOString(),
    })
    if (recentFiles.value.length > 20) {
      recentFiles.value.pop()
    }
  }

  function closeFile(path: string) {
    const idx = openFiles.value.indexOf(path)
    if (idx >= 0) {
      openFiles.value.splice(idx, 1)
      if (activeFile.value === path) {
        activeFile.value = openFiles.value[openFiles.value.length - 1] || null
      }
    }
  }

  function setActiveFile(path: string | null) {
    activeFile.value = path
  }

  function incrementFileTreeVersion() {
    fileTreeVersion.value++
  }

  return {
    workspaces,
    currentWorkspace,
    fileTree,
    openFiles,
    activeFile,
    recentFiles,
    fileTreeVersion,
    hasWorkspace,
    setCurrentWorkspace,
    addWorkspace,
    openFile,
    closeFile,
    setActiveFile,
    incrementFileTreeVersion,
  }
})
