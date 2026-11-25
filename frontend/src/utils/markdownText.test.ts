import { describe, it, expect, afterEach, vi } from 'vitest'
import {
  slugifyHeading,
  cssEscape,
  WIKI_LINK_RE,
  parseWikiLinkTarget,
  parseWikiLinkFile,
} from './markdownText'

/**
 * 这些函数原先在 EditorView.vue 与 MarkdownPreview.vue 各有一份逐字相同的实现。
 * 合并后这里同时充当「口径锁」：用例里的期望值就是两份旧实现的共同行为，
 * 任何一侧想改口径，必须先改这里——否则锚点跳转会与出链/建边口径不一致。
 */

describe('slugifyHeading', () => {
  it('小写并把空格转成横线', () => {
    expect(slugifyHeading('Hello World')).toBe('hello-world')
  })

  it('保留 CJK 与数字，剥掉标点', () => {
    expect(slugifyHeading('第 1 章：缓存失效')).toBe('第-1-章缓存失效')
    expect(slugifyHeading('What is `BM25`?')).toBe('what-is-bm25')
  })

  it('压缩连续空白与连续横线', () => {
    expect(slugifyHeading('  a   b  ')).toBe('a-b')
    expect(slugifyHeading('a -- b')).toBe('a-b')
  })

  it('全标点时返回空串（调用方据此跳过滚动）', () => {
    expect(slugifyHeading('!!!???')).toBe('')
  })

  it('保留下划线以外的连字符语义：下划线属于标点，会被剥掉', () => {
    expect(slugifyHeading('snake_case')).toBe('snakecase')
  })
})

describe('cssEscape', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('优先使用 CSS.escape', () => {
    // 测试环境（node）没有原生 CSS，用桩验证「优先走 CSS.escape」这条分支
    const escape = vi.fn((s: string) => `escaped:${s}`)
    vi.stubGlobal('CSS', { escape })
    expect(cssEscape('a.b')).toBe('escaped:a.b')
    expect(escape).toHaveBeenCalledWith('a.b')
  })

  it('CSS 不可用时退化为只转义引号与反斜杠', () => {
    // 旧 WebView 里 CSS 可能未定义：直接调用会抛 TypeError，必须走兜底
    vi.stubGlobal('CSS', undefined)
    expect(cssEscape('a"b\\c')).toBe('a\\"b\\\\c')
    expect(cssEscape('普通.字符串')).toBe('普通.字符串')
  })
})

describe('parseWikiLinkTarget', () => {
  it('四元组齐全：文件 + 锚点 + 块 ID + 别名', () => {
    expect(parseWikiLinkTarget('note#标题^blk|别名')).toEqual({
      file: 'note',
      anchor: '标题',
      block: 'blk',
      alias: '别名',
      raw: 'note#标题^blk|别名',
    })
  })

  it('先剥别名，别名内的 # 与 ^ 不参与解析', () => {
    expect(parseWikiLinkTarget('note|别#名^字')).toMatchObject({
      file: 'note',
      anchor: '',
      block: '',
      alias: '别#名^字',
    })
  })

  it('块 ID 取最后一个 ^（标题里可以有 ^）', () => {
    expect(parseWikiLinkTarget('note#a^b^blk')).toMatchObject({
      file: 'note',
      anchor: 'a^b',
      block: 'blk',
    })
  })

  it('锚点取第一个 #，同文件锚点没有 file', () => {
    expect(parseWikiLinkTarget('#标题')).toEqual({
      file: '',
      anchor: '标题',
      block: '',
      alias: '',
      raw: '#标题',
    })
  })

  it('同文件块引用没有 file', () => {
    expect(parseWikiLinkTarget('^blk')).toMatchObject({ file: '', anchor: '', block: 'blk' })
  })

  it('普通文件名原样返回', () => {
    expect(parseWikiLinkTarget('sub/dir/note.md')).toMatchObject({ file: 'sub/dir/note.md' })
  })

  it('各部分 trim', () => {
    expect(parseWikiLinkTarget(' note # 标题 ^ blk | 别名 ')).toMatchObject({
      file: 'note',
      anchor: '标题',
      block: 'blk',
      alias: '别名',
    })
  })
})

describe('parseWikiLinkFile', () => {
  it('只返回文件部分，与 parseWikiLinkTarget().file 一致', () => {
    const cases = ['note#标题^blk|别名', '#标题', '^blk', 'a/b.md', '被 空格 分隔']
    for (const c of cases) {
      expect(parseWikiLinkFile(c)).toBe(parseWikiLinkTarget(c).file)
    }
  })
})

describe('WIKI_LINK_RE', () => {
  it('matchAll 能取出全部链接且不共享 lastIndex', () => {
    const text = '前 [[a]] 中 [[b#c|d]] 后'
    const first = [...text.matchAll(WIKI_LINK_RE)].map((m) => m[1])
    const second = [...text.matchAll(WIKI_LINK_RE)].map((m) => m[1])
    expect(first).toEqual(['a', 'b#c|d'])
    expect(second).toEqual(first)
  })

  it('不跨行匹配', () => {
    expect([...'[[a\nb]]'.matchAll(WIKI_LINK_RE)]).toHaveLength(0)
  })
})
