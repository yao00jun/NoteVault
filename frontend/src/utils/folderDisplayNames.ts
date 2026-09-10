import { normalizeNotePath } from './navigation'

export const COMMON_FOLDER_PATHS = ['Daily', 'Inbox', 'Learning', 'Projects', 'Resources', 'Templates'] as const

/** Display preferences use workspace-relative paths; filesystem names stay untouched. */
export function normalizeFolderDisplayPath(value: string): string {
  const path = normalizeNotePath(value.trim()).replace(/\/+$/, '').replace(/\/{2,}/g, '/')
  if (!path || path.startsWith('/') || /^[a-z]:/i.test(path)) return ''
  if (path.split('/').some(segment => segment === '.' || segment === '..')) return ''
  return path
}

export function normalizeFolderDisplayNames(value: unknown): Record<string, string> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  const entries: [string, string][] = []
  for (const [rawPath, rawName] of Object.entries(value)) {
    const path = normalizeFolderDisplayPath(rawPath)
    if (path && typeof rawName === 'string' && rawName.trim()) entries.push([path, rawName.trim()])
  }
  return Object.fromEntries(entries)
}
