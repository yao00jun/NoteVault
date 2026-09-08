// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { useNavigationStore } from './navigation'
import { useWorkspaceStore } from './workspace'
import { pageContext, sectionFallback } from '@/utils/navigation'

const routes = ['/', '/today', '/vault', '/projects', '/learning', '/editor', '/insights', '/settings', '/discover', '/review']
const ws = (name: string) => ({ id: name, name, path: 'C:/' + name, createdAt: '', lastOpenedAt: '' })

function settled(router: Router, action: () => void) {
  return new Promise<void>(resolve => {
    const stop = router.afterEach(() => { stop(); resolve() })
    action()
  })
}

async function setup(path = '/vault') {
  const router = createRouter({ history: createMemoryHistory(), routes: routes.map(path => ({ path, component: { template: '<div />' } })) })
  const workspace = useWorkspaceStore()
  workspace.setCurrentWorkspace(ws('one'))
  await router.push(path)
  const navigation = useNavigationStore()
  const stop = navigation.connect(router, workspace)
  return { router, workspace, navigation, stop }
}

describe('application navigation', () => {
  beforeEach(() => { setActivePinia(createPinia()); localStorage.clear() })

  it('returns through actual document and settings visits, then forwards', async () => {
    const { router, navigation, stop } = await setup('/projects?project=Projects/shop')
    await router.push('/editor?file=Projects/shop/architecture.md')
    await router.push('/settings')
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.query.file).toBe('Projects/shop/architecture.md')
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.fullPath).toBe('/projects?project=Projects/shop')
    await settled(router, () => navigation.forward(router))
    expect(router.currentRoute.value.path).toBe('/editor')
    stop()
  })

  it('keeps filters and tool tabs in the same visit and retains the origin section', async () => {
    const { router, navigation, stop } = await setup()
    await router.replace('/vault?space=Learning&q=Java')
    await router.push('/insights?tab=graph')
    await router.replace('/insights?tab=bases')
    await router.replace('/insights?tab=graph')
    await router.push('/editor?file=Learning/Java/100%25.md')
    expect(navigation.context.section).toBe('vault')
    expect(navigation.context.file).toBe('Learning/Java/100%.md')
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.fullPath).toBe('/insights?tab=graph')
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.query).toEqual({ space: 'Learning', q: 'Java' })
    stop()
  })

  it('distinguishes consecutive documents on the same editor route', async () => {
    const { router, navigation, stop } = await setup('/learning?book=Learning/Java')
    await router.push({ path: '/editor', query: { file: 'Learning/Java/一.md' } })
    await router.push({ path: '/editor', query: { file: 'Learning/Java/二.md' } })
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.query.file).toBe('Learning/Java/一.md')
    expect(navigation.context.section).toBe('learning')
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.query.book).toBe('Learning/Java')
    stop()
  })

  it('has a useful parent fallback for deep links and Today is workspace home', async () => {
    const { router, navigation, stop } = await setup('/editor?file=Learning/Java/one.md')
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.query.book).toBe('Learning/Java')
    expect(router.currentRoute.value.path).toBe('/learning')
    await navigation.home(router)
    expect(router.currentRoute.value.path).toBe('/today')
    stop()
  })

  it('does not let browser back open an old workspace document', async () => {
    const { router, workspace, navigation, stop } = await setup()
    await router.push('/editor?file=Projects/private.md')
    workspace.setCurrentWorkspace(ws('two'))
    await nextTick()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(router.currentRoute.value.path).toBe('/today')
    expect(navigation.canGoBack).toBe(false)
    await settled(router, () => router.back())
    expect(router.currentRoute.value.path).toBe('/today')
    expect(navigation.context.file).toBeNull()
    stop()
  })

  it('does not seed old history when a workspace picker also navigates to Today', async () => {
    const { router, workspace, navigation, stop } = await setup('/editor?file=Projects/private.md')
    await router.push('/')
    workspace.setCurrentWorkspace(ws('two'))
    // The shell schedules replace while the picker immediately pushes its
    // destination. The cancelled replacement still resolves its promise.
    await router.push('/today')
    await nextTick()
    expect(navigation.visits.map(visit => visit.fullPath)).toEqual(['/today'])
    expect(navigation.canGoBack).toBe(false)
    await settled(router, () => router.back())
    expect(router.currentRoute.value.path).toBe('/today')
    stop()
  })

  it('seeds the new workspace when both workspace redirects stay on Today', async () => {
    const { router, workspace, navigation, stop } = await setup('/today')
    workspace.setCurrentWorkspace(ws('two'))
    await router.push('/today')
    await nextTick()
    expect(navigation.visits.map(visit => visit.fullPath)).toEqual(['/today'])
    expect(navigation.canGoBack).toBe(false)
    stop()
  })

  it('truncates forward history when opening a different destination after back', async () => {
    const { router, navigation, stop } = await setup()
    await router.push('/projects')
    await router.push('/learning')
    await settled(router, () => navigation.back(router))
    await router.push('/settings')
    expect(navigation.canGoForward).toBe(false)
    await settled(router, () => navigation.back(router))
    expect(router.currentRoute.value.path).toBe('/projects')
    stop()
  })
})

describe('page context', () => {
  it('uses route identity instead of the last open document', () => {
    expect(pageContext({ path: '/projects', query: { project: 'Projects/shop' } }).folder).toBe('Projects/shop')
    expect(pageContext({ path: '/today', query: {} }).file).toBeNull()
    expect(pageContext({ path: '/editor', query: { file: 'Learning/SQL/100%.md' } }).file).toBe('Learning/SQL/100%.md')
    expect(pageContext({ path: '/editor', query: { file: 'Learning/SQL/100%.md' } }).section).toBe('learning')
  })
  it('maps top-level and auxiliary fallbacks without the legacy Today redirect', () => {
    expect(sectionFallback({ path: '/insights', query: { tab: 'graph' } }, true)).toBe('/vault')
    expect(sectionFallback({ path: '/settings', query: {} }, true)).toBe('/today')
    expect(sectionFallback({ path: '/today', query: {} }, false)).toBe('/')
  })
})
