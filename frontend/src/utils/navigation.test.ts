import { describe, it, expect } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { openDocument, normalizeNotePath, pageContext } from './navigation'

const routes = [
  { path: '/editor', component: { template: '<div>editor</div>' } },
  { path: '/canvas', component: { template: '<div>canvas</div>' } },
]

function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes })
}

function workspaceStub() {
  const opened: string[] = []
  return { opened, openFile: (p: string) => opened.push(p) }
}

describe('openDocument', () => {
  it('默认只带 file，不带 anchor', async () => {
    const router = makeRouter()
    const ws = workspaceStub()
    await openDocument(router, ws, 'docs/a.md')

    expect(ws.opened).toEqual(['docs/a.md'])
    expect(router.currentRoute.value.path).toBe('/editor')
    expect(router.currentRoute.value.query).toEqual({ file: 'docs/a.md' })
  })

  it('P3-9：带 anchor 时把标题文本传下去，供编辑器滚到章节', async () => {
    const router = makeRouter()
    const ws = workspaceStub()
    await openDocument(router, ws, 'docs/a.md', '架构分层')

    expect(router.currentRoute.value.query).toEqual({ file: 'docs/a.md', anchor: '架构分层' })
  })

  it('空白 anchor 视同没有（不产生空 query 项）', async () => {
    const router = makeRouter()
    await openDocument(router, workspaceStub(), 'docs/a.md', '   ')

    expect(router.currentRoute.value.query.anchor).toBeUndefined()
  })

  it('白板文件走 /canvas，且路径反斜杠归一化', async () => {
    const router = makeRouter()
    const ws = workspaceStub()
    await openDocument(router, ws, 'boards\\设计.CANVAS')

    expect(ws.opened).toEqual(['boards/设计.CANVAS'])
    expect(router.currentRoute.value.path).toBe('/canvas')
  })
})

describe('normalizeNotePath / pageContext 对新增 anchor 参数免疫', () => {
  it('anchor 不参与 pageContext 推导', () => {
    const ctx = pageContext({ path: '/editor', query: { file: 'Learning/Java/a.md', anchor: '某个标题' } })
    expect(ctx.file).toBe('Learning/Java/a.md')
    expect(ctx.section).toBe('learning')
  })

  it('normalizeNotePath 处理 ./ 前缀与反斜杠', () => {
    expect(normalizeNotePath('./a/b.md')).toBe('a/b.md')
    expect(normalizeNotePath('a\\b\\c.md')).toBe('a/b/c.md')
  })
})
