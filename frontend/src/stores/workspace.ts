import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Workspace, FileNode } from '@/types'

interface PinnedItem { path: string; title: string }
interface RecentFile { path: string; title: string; openedAt: string }
const normalizePath = (path: string) => path.replace(/\\/g, '/').replace(/^\.\//, '')

export const useWorkspaceStore = defineStore('workspace', () => {
  const workspaces = ref<Workspace[]>([])
  const currentWorkspace = ref<Workspace | null>(null)
  const fileTree = ref<FileNode | null>(null)
  const openFiles = ref<string[]>([])
  const activeFile = ref<string | null>(null)
  const recentFiles = ref<RecentFile[]>([])
  const pinnedItems = ref<PinnedItem[]>([])
  const fileTreeVersion = ref(0) // 文件树版本号，变化时通知刷新

  const hasWorkspace = computed(() => currentWorkspace.value !== null)

  function setCurrentWorkspace(ws: Workspace | null) {
    const changed = currentWorkspace.value?.path !== ws?.path
    if (changed) saveNavigation()
    currentWorkspace.value = ws
    if (ws) {
      ws.lastOpenedAt = new Date().toISOString()
    }
    if (changed) {
      openFiles.value = []
      activeFile.value = null
      fileTree.value = null
      recentFiles.value = []
      pinnedItems.value = []
      loadNavigation()
    }
  }

  // Navigation preferences are workspace-scoped; all content stays in Markdown.
  function navigationKey() {
    return currentWorkspace.value ? `notevault_navigation:${normalizePath(currentWorkspace.value.path)}` : ''
  }

  function loadNavigation() {
    if (!navigationKey() || typeof localStorage === 'undefined') return
    try {
      const raw = localStorage.getItem(navigationKey())
      if (raw) {
        const data = JSON.parse(raw)
        const seen = new Set<string>()
        pinnedItems.value = (Array.isArray(data.pins) ? data.pins : []).filter((item: PinnedItem) => {
          if (typeof item?.path !== 'string' || typeof item?.title !== 'string' || seen.has(normalizePath(item.path))) return false
          seen.add(normalizePath(item.path))
          return true
        }).slice(0, 8).map((item: PinnedItem) => ({ ...item, path: normalizePath(item.path) }))
        recentFiles.value = (Array.isArray(data.recent) ? data.recent : []).filter((item: RecentFile) => typeof item?.path === 'string' && typeof item?.title === 'string' && typeof item?.openedAt === 'string').slice(0, 20)
      } else if (!localStorage.getItem('notevault_starred_migrated')) {
        const legacy = JSON.parse(localStorage.getItem('notevault_starred') || '[]')
        if (Array.isArray(legacy)) {
          pinnedItems.value = [...new Set(legacy.filter((path): path is string => typeof path === 'string').map(normalizePath))]
            .slice(0, 8).map(path => ({ path, title: path.split('/').pop()?.replace(/\.md$/i, '') || path }))
          if (legacy.length) {
            localStorage.setItem('notevault_starred_migrated', navigationKey())
            saveNavigation()
          }
        }
      }
    } catch { /* A corrupt preference must not prevent opening a workspace. */ }
  }

  function saveNavigation() {
    if (!navigationKey() || typeof localStorage === 'undefined') return
    try { localStorage.setItem(navigationKey(), JSON.stringify({ pins: pinnedItems.value, recent: recentFiles.value })) } catch { /* Storage can be unavailable in a private WebView. */ }
  }

  function isPinned(path: string) {
    return pinnedItems.value.some(item => item.path === normalizePath(path))
  }

  function togglePin(path: string, title?: string): boolean {
    path = normalizePath(path)
    if (!path || !currentWorkspace.value) return false
    const index = pinnedItems.value.findIndex(item => item.path === path)
    if (index >= 0) pinnedItems.value.splice(index, 1)
    else {
      if (pinnedItems.value.length >= 8) return false
      pinnedItems.value.push({ path, title: title || path.split('/').pop()?.replace(/\.md$/i, '') || path })
    }
    saveNavigation()
    return true
  }

  function movePin(path: string, targetIndex: number) {
    const index = pinnedItems.value.findIndex(item => item.path === normalizePath(path))
    if (index < 0) return
    const [item] = pinnedItems.value.splice(index, 1)
    pinnedItems.value.splice(Math.max(0, Math.min(targetIndex, pinnedItems.value.length)), 0, item)
    saveNavigation()
  }

  function addWorkspace(ws: Workspace) {
    workspaces.value.push(ws)
  }

  function openFile(path: string) {
    path = normalizePath(path)
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
    saveNavigation()
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
    pinnedItems,
    fileTreeVersion,
    hasWorkspace,
    setCurrentWorkspace,
    addWorkspace,
    openFile,
    closeFile,
    setActiveFile,
    incrementFileTreeVersion,
    isPinned,
    togglePin,
    movePin,
  }
})
