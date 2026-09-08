import { describe, expect, it } from 'vitest'
import { parseSourceIntent } from './sourceIntent'

describe('parseSourceIntent', () => {
  it.each([
    ['请把 "E:\\Code\\Note Vault" 初始化成项目', 'project', 'folder', 'E:\\Code\\Note Vault'],
    ['帮我导入 https://example.org/book.epub 为一本书', 'book', 'url', 'https://example.org/book.epub'],
    ['Create a project from https://github.com/example/vault', 'project', 'url', 'https://github.com/example/vault'],
    ['Please import "/home/me/reading notes" as a book', 'book', 'folder', '/home/me/reading notes'],
    ['Could you initialize a topic from https://example.org/guide?', 'topic', 'url', 'https://example.org/guide'],
    ['用 E:\\Sources\\Backend 创建项目', 'project', 'folder', 'E:\\Sources\\Backend'],
    ['从 "E:\\Sources\\Backend" 初始化项目', 'project', 'folder', 'E:\\Sources\\Backend'],
    ['Can you import https://example.org/guide as a project?', 'project', 'url', 'https://example.org/guide'],
    ['Can you import https://example.org/guide?what=if&mode=copy as a project?', 'project', 'url', 'https://example.org/guide?what=if&mode=copy'],
    ['Please create a project from "/home/me/what if"', 'project', 'folder', '/home/me/what if'],
    ['可以帮我把 "E:\\Code\\Note Vault" 初始化成项目吗？', 'project', 'folder', 'E:\\Code\\Note Vault'],
    ['Would you please import https://example.org/guide as a book?', 'book', 'url', 'https://example.org/guide'],
    ['请帮我导入 https://example.org/guide 为一本书，好吗？', 'book', 'url', 'https://example.org/guide'],
    ['Can you import https://example.org/guide?q=收费吗 as a project?', 'project', 'url', 'https://example.org/guide?q=收费吗'],
  ])('recognizes an explicit source action: %s', (text, kind, sourceType, source) => {
    expect(parseSourceIntent(text)).toMatchObject({ kind, sourceType, source, autoStart: true })
  })

  it('opens a draft without auto-start when the requested collection kind is missing', () => {
    expect(parseSourceIntent('导入 https://example.org/article')).toMatchObject({
      sourceType: 'url', source: 'https://example.org/article', autoStart: false,
    })
    expect(parseSourceIntent('导入 https://example.org/book-project')).not.toHaveProperty('kind')
  })

  it('requires an explicit file selection for local file paths', () => {
    expect(parseSourceIntent('把 "E:\\Books\\Design.pdf" 导入成一本书')).toMatchObject({
      kind: 'book', sourceType: 'files', source: 'E:\\Books\\Design.pdf', autoStart: false,
    })
  })

  it.each([
    '创建项目 https://example.org/repo 会修改原文件吗？',
    '导入 https://example.org/repo 为项目会做哪些操作？',
    '创建项目 https://example.org/repo 会修改原文件吗',
    '导入 https://example.org/repo 为项目是否会覆盖已有笔记',
    '创建项目 https://example.org/repo 安全吗？',
    '导入 https://example.org/repo 为项目要登录吗？',
    '把 "E:\\Code\\Note Vault" 初始化成项目会有什么影响？',
    '创建项目 https://example.org/repo 的话需要配置 AI 吗？',
    '导入 https://example.org/repo 为项目，如果不会覆盖原文件才继续',
    '创建项目 https://example.org/repo 等我确认后再开始',
    '创建项目 https://example.org/repo，假设来源不存在会怎样？',
    'Create a project from https://example.org/repo; will it modify the original files?',
    'Import https://example.org/repo as a project, what will happen?',
    'Import https://example.org/repo as a project, do I need an API key?',
    'Create a project from https://example.org/repo only if the original files stay unchanged',
    'Can you import https://example.org/repo as a project if it supports Markdown?',
    'Please create a project from https://example.org/repo after I approve it',
    'Please create a project from https://example.org/repo after explaining how import works',
  ])('keeps an informational or conditional action-shaped message in chat: %s', text => {
    expect(parseSourceIntent(text)).toBeNull()
  })

  it.each([
    '创建项目 https://example.org/repo 有风险吗？',
    '创建项目 https://example.org/repo 要多长时间',
    '导入 https://example.org/repo 为项目有啥限制',
    '创建项目 https://example.org/repo 任意未识别的后续文字',
    'Please import https://example.org/repo as a project with some unspecified caveat',
    '创建项目 https://example.org/repo 有风险吗',
    '导入 https://example.org/repo 为项目收费吗？',
    '导入 https://example.org/repo 为项目收费吗',
    '创建项目 https://example.org/repo 划算不划算',
    '创建项目 https://example.org/repo 稳定不稳定',
    '创建项目 https://example.org/repo?',
    '创建项目 "https://example.org/repo"？',
    '请创建项目 https://example.org/repo 收费吗？',
    '可以帮我导入 https://example.org/repo 为项目有风险吗？',
    'Can you import https://example.org/repo as a project, extra consequences?',
    'Please create a project from https://example.org/repo; expensive?',
  ])('rejects general question forms regardless of the predicate or polite prefix: %s', text => {
    expect(parseSourceIntent(text)).toBeNull()
  })

  it.each(['耗电', '触发插件', '涉及权限', '执行未知行为'])('does not require enumerating the question predicate %s', predicate => {
    for (const marker of ['吗', '吗？', '呢', '？']) {
      expect(parseSourceIntent(`创建项目 https://example.org/repo ${predicate}${marker}`)).toBeNull()
    }
  })

  it.each([
    '这个项目 https://example.org/repo 是做什么的？',
    '如何把 https://example.org/repo 导入成项目？',
    'Explain how to import https://example.org/repo as a project',
    'What does create a book from https://example.org mean?',
    'Can you explain this project: https://example.org/repo?',
    '不要把 https://example.org/repo 导入成项目',
    '不要创建项目 https://example.org/repo',
    '创建一个空白项目',
    '创建项目 ../relative/path',
    '创建项目 E:relative-path',
    '创建项目 ftp://example.org/repo',
    '创建项目 https://example.org/a 和 https://example.org/b',
    '把 "E:\\Source" 和 "E:\\Other" 初始化为项目',
    '导入 E:\\My Notes 作为项目',
    '把 https://example.org/a 创建为项目或一本书',
    '请阅读下面的说明：把 https://example.org/a 初始化成项目',
    '```\n创建项目 https://example.org/a\n```',
    '创建项目 https://user:password@example.org/repo',
    '创建项目 https://',
  ])('keeps an ordinary, ambiguous, or invalid message in chat: %s', (text) => {
    expect(parseSourceIntent(text)).toBeNull()
  })
})
