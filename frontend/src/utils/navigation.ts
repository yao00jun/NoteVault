import type { RouteLocationRaw, Router } from 'vue-router'

export type WorkflowSection = 'today' | 'projects' | 'learning' | 'vault'
export interface RouteContextInput { path: string; query: Record<string, unknown> }
export interface PageContext {
  section: WorkflowSection
  title: string
  file: string | null
  folder: string | null
  entityFolder: string | null
  entityKind: 'project' | 'book' | null
}
export const sectionLabels: Record<WorkflowSection, string> = { today: '今日', projects: '项目', learning: '学习', vault: '知识库' }
export function queryText(value: unknown): string { return typeof value === 'string' ? value : '' }
export function normalizeNotePath(path: string): string { return path.replace(/\\/g, '/').replace(/^\.\//, '') }

/** Vue Router has already decoded query values. Decoding again corrupts literal % filenames. */
export function pageContext(route: RouteContextInput, origin?: WorkflowSection): PageContext {
  const query = route.query
  const resourcePage = ['/editor', '/canvas'].includes(route.path)
  const file = resourcePage ? normalizeNotePath(queryText(query.file)) || null : null
  const explicitFolder = normalizeNotePath(queryText(query.folder))
  const entity = normalizeNotePath(queryText(route.path === '/projects' ? query.project : route.path === '/learning' ? query.book : ''))
  const folder = entity.replace(/\/(?:project|book)\.md$/i, '') || explicitFolder || (file?.includes('/') ? file.slice(0, file.lastIndexOf('/')) : '') || null
  let section: WorkflowSection = 'vault'
  if (['/today', '/knowledge', '/review', '/'].includes(route.path)) section = 'today'
  else if (route.path === '/projects') section = 'projects'
  else if (route.path === '/learning') section = 'learning'
  else if (resourcePage || route.path === '/settings') {
    if (origin) section = origin
    else if (/^Projects\//i.test(file || folder || '')) section = 'projects'
    else if (/^Learning\//i.test(file || folder || '')) section = 'learning'
    else if (/^Daily\//i.test(file || folder || '')) section = 'today'
    else if (route.path === '/settings') section = 'today'
  }
  const parts = (file || folder || '').split('/')
  const entityKind = section === 'projects' && parts[0] === 'Projects' && parts[1] ? 'project'
    : section === 'learning' && parts[0] === 'Learning' && parts[1] ? 'book' : null
  const entityFolder = entityKind ? parts.slice(0, 2).join('/') : null
  const titles: Record<string, string> = {
    '/': '工作区', '/today': '今日工作台', '/knowledge': '今日工作台', '/projects': '项目组合', '/learning': '技术书架',
    '/vault': '文档管理', '/library': '文档管理', '/settings': '设置', '/editor': '文档', '/canvas': '白板',
    '/insights': query.tab === 'compile' ? 'AI 整理' : query.tab === 'bases' ? '属性视图' : '知识图谱',
    '/discover': query.tab === 'qna' ? '知识问答' : query.tab === 'views' ? '主题与标签' : '搜索',
    '/review': query.tab === 'versions' ? '版本历史' : '工作回顾', '/archive': '归档', '/trash': '回收站', '/import': '导入与迁移', '/plugins': '插件',
  }
  const title = file ? file.split('/').pop()! : entity ? entity.split('/').pop()! : titles[route.path] || 'NoteVault'
  return { section, title, file, folder, entityFolder, entityKind }
}

/** A history-less link still has an understandable parent. */
export function sectionFallback(route: RouteContextInput, hasWorkspace: boolean, origin?: WorkflowSection): string {
  if (!hasWorkspace) return '/'
  const context = pageContext(route, origin)
  if (['/editor', '/canvas'].includes(route.path) && context.entityFolder) {
    const key = context.entityKind === 'project' ? 'project' : 'book'
    return `/${context.section}?${key}=${encodeURIComponent(context.entityFolder)}`
  }
  if (route.path === '/projects' && queryText(route.query.project)) return '/projects'
  if (route.path === '/learning' && queryText(route.query.book)) return '/learning'
  if (route.path === '/settings') return '/today'
  if (['/today', '/projects', '/learning', '/vault', '/'].includes(route.path)) return '/today'
  return '/' + context.section
}

export function navigationIdentity(route: RouteContextInput): string {
  return [route.path, queryText(route.query.file), queryText(route.query.folder), queryText(route.query.project), queryText(route.query.book)].join('|')
}

export function openDocument(router: Router, workspace: { openFile(path: string): void }, path: string): Promise<unknown> {
  const file = normalizeNotePath(path)
  workspace.openFile(file)
  return router.push({ path: /\.canvas$/i.test(file) ? '/canvas' : '/editor', query: { file } })
}

export interface Breadcrumb { label: string; to?: RouteLocationRaw }
export function contextBreadcrumbs(context: PageContext, route: RouteContextInput): Breadcrumb[] {
  const isResourcePage = ['/editor', '/canvas'].includes(route.path)
  if (isResourcePage && context.file) {
    const parts = context.file.split('/')
    const fileName = parts.pop() || context.title
    const crumbs: Breadcrumb[] = []

    if (parts[0] === 'Learning' && parts.length >= 2) {
      crumbs.push({ label: sectionLabels.learning, to: '/learning' })
      crumbs.push({ label: parts[1]!, to: { path: '/learning', query: { book: parts.slice(0, 2).join('/') } } })
      for (let i = 2; i < parts.length; i++) {
        crumbs.push({ label: parts[i]! })
      }
    } else if (parts[0] === 'Projects' && parts.length >= 2) {
      crumbs.push({ label: sectionLabels.projects, to: '/projects' })
      crumbs.push({ label: parts[1]!, to: { path: '/projects', query: { project: parts.slice(0, 2).join('/') } } })
      for (let i = 2; i < parts.length; i++) {
        crumbs.push({ label: parts[i]! })
      }
    } else if (parts[0] === 'Daily') {
      crumbs.push({ label: sectionLabels.today, to: '/today' })
    } else {
      crumbs.push({ label: sectionLabels.vault, to: '/vault' })
      let cumulative = ''
      for (let i = 0; i < parts.length; i++) {
        cumulative = cumulative ? `${cumulative}/${parts[i]}` : parts[i]!
        crumbs.push({ label: parts[i]!, to: { path: '/vault', query: { folder: cumulative } } })
      }
    }

    crumbs.push({ label: fileName })
    return crumbs
  }

  const crumbs: Breadcrumb[] = [{ label: sectionLabels[context.section], to: '/' + context.section }]
  if (context.entityFolder) {
    crumbs.push({ label: context.entityFolder.split('/').pop()!, to: { path: '/' + context.section, query: { [context.entityKind === 'project' ? 'project' : 'book']: context.entityFolder } } })
  } else if (context.file && context.folder) {
    crumbs.push({ label: context.folder, to: { path: '/vault', query: { folder: context.folder } } })
  }
  if (context.file || !['/today', '/projects', '/learning', '/vault'].includes(route.path)) crumbs.push({ label: context.title })
  if (crumbs.length === 1 || (!context.file && context.entityFolder)) delete crumbs[crumbs.length - 1]!.to
  return crumbs
}

