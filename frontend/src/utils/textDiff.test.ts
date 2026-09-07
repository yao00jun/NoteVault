import { describe, it, expect } from 'vitest'
import { diffLines, toSplitPairs } from './textDiff'

describe('toSplitPairs（Beyond Compare 式左右分栏）', () => {
  it('全同文本：左右镜像，行号一一对齐', () => {
    const rows = diffLines('a\nb\nc', 'a\nb\nc')
    const pairs = toSplitPairs(rows)
    expect(pairs).toHaveLength(3)
    for (const p of pairs) {
      expect(p.left?.type).toBe('same')
      expect(p.right?.type).toBe('same')
      expect(p.left?.text).toBe(p.right?.text)
    }
  })

  it('删改对照：removed 与 added 就近配对成一行', () => {
    const rows = diffLines('old line\nshared', 'new line\nshared')
    const pairs = toSplitPairs(rows)
    expect(pairs).toHaveLength(2)
    expect(pairs[0]!.left?.type).toBe('removed')
    expect(pairs[0]!.left?.text).toBe('old line')
    expect(pairs[0]!.right?.type).toBe('added')
    expect(pairs[0]!.right?.text).toBe('new line')
    expect(pairs[1]!.left?.text).toBe('shared')
    expect(pairs[1]!.right?.text).toBe('shared')
  })

  it('纯删除：右侧补 null 占位，保持对齐', () => {
    const pairs = toSplitPairs([{ type: 'removed', text: 'gone' }])
    expect(pairs).toEqual([{ left: { type: 'removed', text: 'gone' }, right: null }])
  })

  it('纯新增：左侧补 null 占位，保持对齐', () => {
    const pairs = toSplitPairs([{ type: 'added', text: 'fresh' }])
    expect(pairs).toEqual([{ left: null, right: { type: 'added', text: 'fresh' } }])
  })

  it('混合块：多删少增时多出的删除行单独占左栏', () => {
    const rows: Parameters<typeof toSplitPairs>[0] = [
      { type: 'removed', text: 'd1' },
      { type: 'removed', text: 'd2' },
      { type: 'removed', text: 'd3' },
      { type: 'added', text: 'n1' },
      { type: 'same', text: 'tail' },
    ]
    const pairs = toSplitPairs(rows)
    expect(pairs).toHaveLength(4)
    expect(pairs[0]).toEqual({ left: { type: 'removed', text: 'd1' }, right: { type: 'added', text: 'n1' } })
    expect(pairs[1]!.left?.text).toBe('d2')
    expect(pairs[1]!.right).toBeNull()
    expect(pairs[2]!.left?.text).toBe('d3')
    expect(pairs[2]!.right).toBeNull()
    expect(pairs[3]!.left?.text).toBe('tail')
  })
})
