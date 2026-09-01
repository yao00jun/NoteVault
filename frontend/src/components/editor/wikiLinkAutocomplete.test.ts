// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  GraphService: {
    GetLinkCandidates: vi.fn(),
  },
}))

import { createWikiLinkCompletionSource } from './wikiLinkAutocomplete'
import { GraphService } from '@bindings/github.com/notevault/notevault/index.js'

// 最小 CompletionContext：仅实现 matchBefore，按光标前文本匹配并锚定到光标。
function makeContext(doc: string, pos: number, explicit = false): any {
  return {
    pos,
    explicit,
    matchBefore(re: RegExp) {
      const textBefore = doc.slice(0, pos)
      const m = textBefore.match(re)
      if (m && m.index !== undefined && m.index + m[0].length === pos) {
        return { from: m.index, to: pos, text: m[0] }
      }
      return null
    },
  }
}

const mockedGet = vi.mocked(GraphService.GetLinkCandidates)

describe('createWikiLinkCompletionSource', () => {
  beforeEach(() => {
    mockedGet.mockReset()
  })

  it('光标前无 [[ 时返回 null', async () => {
    const src = createWikiLinkCompletionSource(() => '/vault')
    const res = await src(makeContext('没有链接', 4))
    expect(res).toBeNull()
  })

  it('未打开工作区（path 为空）返回 null 且不调后端', async () => {
    const src = createWikiLinkCompletionSource(() => undefined)
    mockedGet.mockResolvedValue([])
    const res = await src(makeContext('see [[', 6))
    expect(res).toBeNull()
    expect(mockedGet).not.toHaveBeenCalled()
  })

  it('[[ 后无输入 → 返回全部文件候选，apply 为 fileBase]]', async () => {
    mockedGet.mockResolvedValue([
      { kind: 'file', file: 'Alpha.md', fileBase: 'Alpha', heading: '', display: 'Alpha.md' },
      { kind: 'file', file: 'Beta.md', fileBase: 'Beta', heading: '', display: 'Beta.md' },
    ])
    const src = createWikiLinkCompletionSource(() => '/vault')
    const res = (await src(makeContext('see [[', 6))) as any
    expect(res.options).toHaveLength(2)
    expect(res.from).toBe(6) // 紧接 [[ 之后，空 query 不替换仅插入
    expect(res.options[0].apply).toBe('Alpha]]')
    expect(res.options[1].apply).toBe('Beta]]')
    expect(res.filter).toBe(false) // 服务端已过滤，禁用客户端二次过滤
  })

  it('输入文件名片段 → 后端过滤，apply 为 fileBase]]', async () => {
    mockedGet.mockResolvedValue([
      { kind: 'file', file: 'Alpha.md', fileBase: 'Alpha', heading: '', display: 'Alpha.md' },
    ])
    const src = createWikiLinkCompletionSource(() => '/vault')
    const res = (await src(makeContext('link [[Alp', 10))) as any
    expect(mockedGet).toHaveBeenCalledWith('/vault', 'Alp')
    expect(res.from).toBe(7) // 替换从 [[ 之后到光标（覆盖 "Alp"）
    expect(res.options[0].apply).toBe('Alpha]]')
  })

  it('输入 # 后 → 返回标题候选，apply 为 fileBase#heading]]', async () => {
    mockedGet.mockResolvedValue([
      { kind: 'heading', file: 'Alpha.md', fileBase: 'Alpha', heading: '小节', display: 'Alpha › 小节' },
    ])
    const src = createWikiLinkCompletionSource(() => '/vault')
    const res = (await src(makeContext('[[Alpha#', 8))) as any
    expect(mockedGet).toHaveBeenCalledWith('/vault', 'Alpha#')
    expect(res.options[0].apply).toBe('Alpha#小节]]')
    expect(res.options[0].type).toBe('property')
  })

  it('后端返回空 → 返回 null', async () => {
    mockedGet.mockResolvedValue([])
    const src = createWikiLinkCompletionSource(() => '/vault')
    const res = await src(makeContext('[[zzz', 5))
    expect(res).toBeNull()
  })

  it('后端抛错 → 返回 null（不崩）', async () => {
    mockedGet.mockRejectedValue(new Error('boom'))
    const src = createWikiLinkCompletionSource(() => '/vault')
    const res = await src(makeContext('[[', 2))
    expect(res).toBeNull()
  })
})
