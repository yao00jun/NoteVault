import { describe, expect, it } from 'vitest'
import { buildContent, extractTags, hasFrontMatter, splitFrontMatter } from './frontmatter'

describe('splitFrontMatter', () => {
  it('无 front matter 时整体作为 body 返回', () => {
    const parsed = splitFrontMatter('# 标题\n\n正文内容')
    expect(parsed.raw).toBe('')
    expect(parsed.fields).toHaveLength(0)
    expect(parsed.body).toBe('# 标题\n\n正文内容')
    expect(hasFrontMatter('# 标题')).toBe(false)
  })

  it('解析 tags 列表与标量字段，保留顺序', () => {
    const doc = '---\ntitle: 笔记\nauthor: feng\ntags:\n  - SQL\n  - 数据库\ncreated: 2026-01-01\n---\n\n## 正文\n'
    const parsed = splitFrontMatter(doc)
    expect(parsed.fields.map((f) => f.key)).toEqual(['title', 'author', 'tags', 'created'])
    expect(parsed.fields[0].scalar).toBe('笔记')
    expect(parsed.fields[2].listItems).toEqual(['SQL', '数据库'])
    expect(parsed.body).toBe('\n## 正文\n')
    expect(extractTags(parsed)).toEqual(['SQL', '数据库'])
  })

  it('容忍 CRLF 行尾', () => {
    const doc = '---\r\ntags:\r\n  - a\r\n---\r\nbody'
    const parsed = splitFrontMatter(doc)
    expect(extractTags(parsed)).toEqual(['a'])
    expect(parsed.body).toBe('body')
  })

  it('未闭合的 --- 不被误判为 front matter', () => {
    const doc = '---\ntags:\n  - a\n正文没有结束标记'
    expect(hasFrontMatter(doc)).toBe(false)
    expect(splitFrontMatter(doc).body).toBe(doc)
  })
})

describe('buildContent', () => {
  it('tags 变更后重写 front matter，其他字段原样保留', () => {
    const doc = '---\ntitle: 笔记\ntags:\n  - SQL\n  - 数据库\ncreated: 2026-01-01\n---\n\n## 正文\n'
    const parsed = splitFrontMatter(doc)
    const out = buildContent(parsed, ['Go', '并发'])
    const reparsed = splitFrontMatter(out)
    expect(reparsed.fields.map((f) => f.key)).toEqual(['title', 'tags', 'created'])
    expect(reparsed.fields[0].scalar).toBe('笔记')
    expect(reparsed.fields[2].scalar).toBe('2026-01-01')
    expect(extractTags(reparsed)).toEqual(['Go', '并发'])
    // body 开头的空行被规范化掉（front matter 与正文之间固定一个换行）
    expect(reparsed.body).toBe('## 正文\n')
  })

  it('无 front matter 且无 tags 时返回原 body', () => {
    const parsed = splitFrontMatter('# 纯正文')
    expect(buildContent(parsed, [])).toBe('# 纯正文')
  })

  it('无 front matter 但添加了 tags 时生成新 front matter', () => {
    const parsed = splitFrontMatter('# 纯正文\n内容')
    const out = buildContent(parsed, ['新标签'])
    const reparsed = splitFrontMatter(out)
    expect(extractTags(reparsed)).toEqual(['新标签'])
    expect(reparsed.body).toBe('# 纯正文\n内容')
  })

  it('清空 tags 后保留空的 tags 字段', () => {
    const doc = '---\ntags:\n  - SQL\n---\nbody'
    const parsed = splitFrontMatter(doc)
    const out = buildContent(parsed, [])
    const reparsed = splitFrontMatter(out)
    expect(extractTags(reparsed)).toEqual([])
    expect(reparsed.body).toBe('body')
  })

  it('tags 中的特殊字符（冒号/引号/中文）原样保留', () => {
    const parsed = splitFrontMatter('# 正文')
    const out = buildContent(parsed, ['C++: 指南', '「引用」', '数据建模'])
    expect(extractTags(splitFrontMatter(out))).toEqual(['C++: 指南', '「引用」', '数据建模'])
  })
})
