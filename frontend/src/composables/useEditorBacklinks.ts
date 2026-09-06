/**
 * useEditorBacklinks - 反向链接提取与跳转
 *
 * 用全局搜索 `[[当前文档名]]` 找出引用当前文档的笔记（排除自身），
 * 供右侧 Context Drawer 的反链 Tab 展示与跳转。搜索失败静默清空
 * （渐进降级约定：反链是增强信息，不能因检索故障阻断编辑）。
 */
import { ref, watch, type ComputedRef, type Ref } from 'vue'
import { SearchService } from '@/api'
import type { FileNode } from '@/components/editor/FileTree.vue'

export interface BacklinkItem {
  path: string
  name: string
}

/** 在文件树中递归查找指定路径的节点 */
export function findFileByPath(nodes: FileNode[], path: string): FileNode | null {
  for (const node of nodes) {
    if (node.path === path) return node
    if (node.children) {
      const found = findFileByPath(node.children, path)
      if (found) return found
    }
  }
  return null
}

export function useEditorBacklinks(options: {
  /** 当前工作区绝对路径（未选择工作区时为 undefined） */
  workspacePath: ComputedRef<string | undefined>
  /** 当前活动标签页文件名（无活动标签时为 undefined） */
  activeTabName: ComputedRef<string | undefined>
  /** 当前活动标签页相对路径 */
  activeTabPath: ComputedRef<string | undefined>
  /** 活动标签页索引（切换时自动重新加载反链） */
  activeTabIndex: Ref<number>
  /** 完整文件树（跳转时在其中定位节点） */
  fileTree: Ref<FileNode[]>
  /** 点击反链后的打开动作（由宿主组件提供） */
  openFile: (node: FileNode) => void
}) {
  const backlinks = ref<BacklinkItem[]>([])

  async function loadBacklinks() {
    const wsPath = options.workspacePath.value
    const tabName = options.activeTabName.value
    if (!wsPath || !tabName) {
      backlinks.value = []
      return
    }
    const currentName = tabName.replace(/\.md$/, '').replace(/\.markdown$/, '')
    try {
      // 搜索包含 [[当前文档名]] 的文档
      const results = await SearchService.Search(wsPath, `[[${currentName}]]`)
      backlinks.value = (Array.isArray(results) ? results : [])
        .filter((r): r is NonNullable<typeof r> => !!r && r.path !== options.activeTabPath.value)
        .map((r) => ({
          path: r.path,
          name: r.title ?? r.path,
        }))
    } catch (e) {
      console.error('Failed to load backlinks:', e)
      backlinks.value = []
    }
  }

  // 监听当前标签页变化，加载反向链接
  watch(options.activeTabIndex, () => {
    void loadBacklinks()
  })

  // 打开反向链接文档
  function openBacklink(link: BacklinkItem) {
    const node = findFileByPath(options.fileTree.value, link.path)
    if (node) {
      options.openFile(node)
    }
  }

  return { backlinks, loadBacklinks, openBacklink }
}
