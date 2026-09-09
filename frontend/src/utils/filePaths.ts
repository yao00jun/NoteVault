import { normalizeNotePath } from './navigation'

/** Match complete path segments, so renaming Go never renames Gopher. */
export function windowsWorkspace(path: string): boolean { return /^(?:[A-Za-z]:|[\\/]{2})/.test(path) }

export function isEntryPath(path: string, entry: string, directory: boolean, caseInsensitive = false): boolean {
  path = normalizeNotePath(path)
  entry = normalizeNotePath(entry).replace(/\/$/, '')
  if (caseInsensitive) { path = path.toLowerCase(); entry = entry.toLowerCase() }
  return path === entry || (directory && path.startsWith(entry + '/'))
}

export function renamedEntryPath(path: string, from: string, to: string, directory: boolean, caseInsensitive = false): string {
  path = normalizeNotePath(path)
  from = normalizeNotePath(from).replace(/\/$/, '')
  to = normalizeNotePath(to).replace(/\/$/, '')
  return isEntryPath(path, from, directory, caseInsensitive) ? to + path.slice(from.length) : path
}
