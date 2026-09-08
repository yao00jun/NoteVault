// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { router as applicationRouter } from './index'

// Route transitions remain real. Page rendering belongs to each view's tests.
function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: applicationRouter.options.routes.map(record => record.redirect
      ? record
      : {
          path: record.path,
          name: record.name,
          ...(record.alias !== undefined ? { alias: record.alias } : {}),
          meta: record.meta,
          beforeEnter: record.beforeEnter,
          component: { template: '<div />' },
        }),
  })
}

describe('Workbench routes', () => {
  it.each(['today', 'projects', 'learning', 'vault'])('opens the %s workflow directly', async name => {
    const router = createTestRouter()
    await router.push('/' + name)
    expect(router.currentRoute.value.name).toBe(name)
    expect(router.currentRoute.value.matched).toHaveLength(1)
  })

  it('keeps the legacy workbench link compatible with Today', async () => {
    const router = createTestRouter()
    await router.push('/knowledge?from=legacy#focus')
    expect(router.currentRoute.value.path).toBe('/today')
    expect(router.currentRoute.value.query).toEqual({ from: 'legacy' })
    expect(router.currentRoute.value.hash).toBe('#focus')
  })

  it('opens the document browser through the legacy library URL', async () => {
    const router = createTestRouter()
    await router.push('/library')
    expect(router.currentRoute.value.name).toBe('vault')
    expect(router.currentRoute.value.path).toBe('/vault')
    expect(router.currentRoute.value.query.view).toBe('documents')
  })

  it.each([
    ['/search', '/discover', { tab: 'search' }],
    ['/qna', '/discover', { tab: 'qna' }],
    ['/tags', '/discover', { tab: 'views', view: 'tags' }],
    ['/graph', '/insights', { tab: 'graph' }],
    ['/bases', '/insights', { tab: 'bases' }],
    ['/compile', '/insights', { tab: 'compile' }],
    ['/reports', '/review', { tab: 'reports' }],
    ['/todos', '/review', { tab: 'tasks', sub: 'todos' }],
    ['/reminders', '/review', { tab: 'tasks', sub: 'reminders' }],
    ['/history', '/review', { tab: 'versions' }],
  ])('preserves the tool and context of %s links', async (path, destination, query) => {
    const router = createTestRouter()
    await router.push({ path, query: { file: 'Projects/notes.md' }, hash: '#section' })
    expect(router.currentRoute.value.path).toBe(destination)
    expect(router.currentRoute.value.query).toEqual({ ...query, file: 'Projects/notes.md' })
    expect(router.currentRoute.value.hash).toBe('#section')
  })

  it.each([
    ['bases', '/insights', 'bases'],
    ['qna', '/discover', 'qna'],
    ['todos', '/review', 'tasks'],
    ['reminders', '/review', 'tasks'],
    ['history', '/review', 'versions'],
  ])('keeps named legacy %s navigation consistent with its URL', async (name, path, tab) => {
    const router = createTestRouter()
    await router.push({ name, query: { file: 'notes.md' } })
    expect(router.currentRoute.value.path).toBe(path)
    expect(router.currentRoute.value.query.tab).toBe(tab)
    expect(router.currentRoute.value.query.file).toBe('notes.md')
  })

  it.each(['editor', 'canvas', 'archive', 'import', 'plugins', 'trash', 'settings', 'discover', 'review', 'insights'])(
    'keeps the existing %s tool accessible', async name => {
      const router = createTestRouter()
      await router.push({ name })
      expect(router.currentRoute.value.path).toBe('/' + name)
      expect(router.currentRoute.value.matched).toHaveLength(1)
    },
  )
})
