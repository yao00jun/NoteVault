import type { FileNode } from '@/components/editor/FileTree.vue'
import { normalizeNotePath } from './navigation'

/** Qualified links identify one workspace file; legacy basename links still work. */
export function findFileByName(nodes: FileNode[], name: string): FileNode | null {
  const target = normalizeNotePath(name)
  if (!target || target.split('/').some(part => !part || part === '.' || part === '..')) return null
  const files: FileNode[] = []
  function collect(branch: FileNode[]) {
    for (const node of branch) {
      if (!node.isDir) files.push(node)
      if (node.children) collect(node.children)
    }
  }
  collect(nodes)
  const candidate = (node: FileNode) => target.includes('/') ? normalizeNotePath(node.path) : node.name
  const withoutExtension = (value: string) => value.replace(/\.(md|markdown)$/i, '')
  // Exact casing wins on case-sensitive volumes. Only fall back to aliases
  // after inspecting all files, so Windows links keep their real disk paths.
  return files.find(node => candidate(node) === target)
    ?? files.find(node => withoutExtension(candidate(node)) === target)
    ?? files.find(node => candidate(node).toLowerCase() === target.toLowerCase())
    ?? files.find(node => withoutExtension(candidate(node)).toLowerCase() === target.toLowerCase())
    ?? null
}
