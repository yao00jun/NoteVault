// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { sanitizeHtml } from './sanitize'

describe('sanitizeHtml（XSS 防线）', () => {
  it('剥离 script 标签', () => {
    const out = sanitizeHtml('<p>a</p><script>alert(1)</script>')
    expect(out).not.toContain('<script')
    expect(out).toContain('<p>a</p>')
  })

  it('剥离事件处理器属性', () => {
    const out = sanitizeHtml('<img src=x onerror="alert(1)">')
    expect(out).not.toContain('onerror')
  })

  it('剥离 javascript: 链接', () => {
    const out = sanitizeHtml('<a href="javascript:alert(1)">x</a>')
    expect(out.toLowerCase()).not.toContain('javascript:')
  })

  it('剥离 iframe / object / embed / style', () => {
    const out = sanitizeHtml(
      '<iframe src="https://evil"></iframe><object></object><embed><style>body{}</style>',
    )
    expect(out).not.toContain('<iframe')
    expect(out).not.toContain('<object')
    expect(out).not.toContain('<embed')
    expect(out).not.toContain('<style')
  })

  it('保留正常 Markdown 产物（标题/加粗/链接/表格/任务列表）', () => {
    const md =
      '<h1>t</h1><p><strong>b</strong> <a href="https://e.com">l</a></p>' +
      '<table><tr><td>c</td></tr></table>' +
      '<input type="checkbox" checked disabled>'
    const out = sanitizeHtml(md)
    expect(out).toContain('<h1>t</h1>')
    expect(out).toContain('<strong>b</strong>')
    expect(out).toContain('href="https://e.com"')
    expect(out).toContain('<td>c</td>')
    expect(out).toContain('type="checkbox"')
  })

  it('保留扩展语法依赖的 data-* 属性（wiki 链接跳转用）', () => {
    const out = sanitizeHtml('<span data-nv-wikilink="docs/a.md">link</span>')
    expect(out).toContain('data-nv-wikilink="docs/a.md"')
  })

  it('保留本地图片相对路径', () => {
    const out = sanitizeHtml('<img src="assets/pic.png" alt="p">')
    expect(out).toContain('src="assets/pic.png"')
  })

  it('拒绝非图片的 data: URI', () => {
    const out = sanitizeHtml('<a href="data:text/html,<script>alert(1)</script>">x</a>')
    expect(out.toLowerCase()).not.toContain('data:text/html')
  })
})
