/**
 * 搜索文本处理工具测试
 */
import { describe, it, expect } from 'vitest'
import { stripMarkdownNoise, escapeHtml, escapeRegex, highlightText, cleanSnippet } from './text'

describe('stripMarkdownNoise', () => {
  it('去掉标题符号', () => {
    expect(stripMarkdownNoise('# 标题\n正文')).toBe('标题 正文')
  })

  it('保留链接文字', () => {
    expect(stripMarkdownNoise('点击 [这里](https://example.com) 继续')).toContain('点击 这里')
    expect(stripMarkdownNoise('点击 [这里](https://example.com) 继续')).not.toContain('https')
  })

  it('去掉代码标记但保留代码文字', () => {
    expect(stripMarkdownNoise('使用 `console.log()` 输出')).toBe('使用 console.log() 输出')
  })

  it('把 wiki-link 简化', () => {
    expect(stripMarkdownNoise('参考 [[WikiLink]] 和 [[W|B]]')).toContain('WikiLink')
    expect(stripMarkdownNoise('参考 [[WikiLink]] 和 [[W|B]]')).toContain('B')
  })

  it('去掉任务列表前缀但保留勾选', () => {
    expect(stripMarkdownNoise('- [ ] 待办\n- [x] 完成')).toContain('☐ 待办')
    expect(stripMarkdownNoise('- [ ] 待办\n- [x] 完成')).toContain('☐ 完成')
  })

  it('去掉引用和水平线', () => {
    expect(stripMarkdownNoise('> 引用\n\n---\n内容')).toContain('引用')
    expect(stripMarkdownNoise('> 引用\n\n---\n内容')).not.toMatch(/^-+$/m)
  })

  it('合并多余空白', () => {
    expect(stripMarkdownNoise('多   个\n\n空  白')).toBe('多 个 空 白')
  })

  it('处理空输入', () => {
    expect(stripMarkdownNoise('')).toBe('')
  })
})

describe('escapeHtml', () => {
  it('转义所有特殊字符', () => {
    expect(escapeHtml('<script>alert("xss")</script>')).toBe(
      '&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt;',
    )
    expect(escapeHtml("a & b 'c'")).toBe('a &amp; b &#039;c&#039;')
  })
})

describe('escapeRegex', () => {
  it('转义正则元字符', () => {
    expect(escapeRegex('a.b*c+d?')).toBe('a\\.b\\*c\\+d\\?')
    expect(escapeRegex('[hello]')).toBe('\\[hello\\]')
  })
})

describe('highlightText', () => {
  it('高亮关键词（不区分大小写）', () => {
    const html = highlightText('Hello World, hello there', 'hello')
    expect(html).toContain('<mark class="search-highlight">Hello</mark>')
    expect(html).toContain('<mark class="search-highlight">hello</mark>')
  })

  it('转义 HTML 特殊字符', () => {
    const html = highlightText('包含 <script> 不安全', '不安全')
    expect(html).not.toContain('<script>')
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('<mark class="search-highlight">不安全</mark>')
  })

  it('空查询返回原文', () => {
    const html = highlightText('hello world', '')
    expect(html).toBe('hello world')
  })

  it('查询不存在则不修改', () => {
    const html = highlightText('hello world', 'xyz')
    expect(html).toBe('hello world')
    expect(html).not.toContain('<mark')
  })

  it('支持正则元字符的关键字', () => {
    const html = highlightText('a.b 是文件', 'a.b')
    expect(html).toContain('<mark class="search-highlight">a.b</mark>')
  })
})

describe('cleanSnippet', () => {
  it('清洗 Markdown 噪音后高亮', () => {
    const snippet = '# 标题\n这是一段 **包含** [链接](http://x) 的 [snippet](https://y)，含有关键词'
    const result = cleanSnippet(snippet, '关键词')
    expect(result).toContain('<mark class="search-highlight">关键词</mark>')
    expect(result).not.toContain('**')
    expect(result).not.toContain('http')
  })
})
