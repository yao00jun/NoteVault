import { describe, it, expect } from 'vitest'
import {
  createEmptyCanvas,
  parseCanvas,
  serializeCanvas,
  collectCanvasFiles,
  genId,
  createNode,
  nodeAnchor,
  edgeEndpoints,
  type RawFileNode,
  nodesInGroup,
} from './canvas'
import type { CanvasData } from '@/types'

describe('canvas data layer', () => {
  it('createEmptyCanvas returns empty structure', () => {
    const c = createEmptyCanvas()
    expect(c.nodes).toEqual([])
    expect(c.edges).toEqual([])
  })

  it('parseCanvas on blank string returns empty canvas', () => {
    expect(parseCanvas('').nodes).toEqual([])
    expect(parseCanvas('   ').edges).toEqual([])
  })

  it('parseCanvas throws on invalid JSON', () => {
    expect(() => parseCanvas('{ not json')).toThrow(/合法 JSON/)
  })

  it('parseCanvas normalizes missing fields with sensible defaults', () => {
    const c = parseCanvas(JSON.stringify({ nodes: [{ type: 'text' }], edges: [] }))
    expect(c.nodes).toHaveLength(1)
    const n = c.nodes[0]
    expect(n.id).toBeTruthy()
    expect(n.x).toBe(0)
    expect(n.y).toBe(0)
    expect(n.width).toBe(250)
    expect(n.height).toBe(160)
    if (n.type !== 'text') throw new Error('expected text node')
    expect(n.text).toBe('')
  })

  it('parseCanvas coerces unknown node type to text', () => {
    const c = parseCanvas(JSON.stringify({ nodes: [{ type: 'weird', x: 10, y: 20 }] }))
    expect(c.nodes[0].type).toBe('text')
    expect(c.nodes[0].x).toBe(10)
    expect(c.nodes[0].y).toBe(20)
  })

  it('parseCanvas drops edges missing endpoints and keeps valid ones', () => {
    const c = parseCanvas(
      JSON.stringify({
        nodes: [
          { id: 'a', type: 'text', x: 0, y: 0 },
          { id: 'b', type: 'text', x: 100, y: 0 },
        ],
        edges: [
          { fromNode: 'a', toNode: 'b' },
          { fromNode: 'a' }, // missing toNode -> dropped
          {}, // missing both -> dropped
        ],
      }),
    )
    expect(c.edges).toHaveLength(1)
    expect(c.edges[0].id).toBeTruthy()
  })

  it('serializeCanvas round-trips through parse', () => {
    const data: CanvasData = {
      nodes: [
        { id: 'a', type: 'text', x: 10, y: 20, width: 250, height: 160, text: 'hello' },
        { id: 'b', type: 'file', x: 300, y: 20, width: 250, height: 80, file: 'note.md' },
        { id: 'g', type: 'group', x: 0, y: 0, width: 600, height: 400, label: 'G' },
      ],
      edges: [{ id: 'e1', fromNode: 'a', toNode: 'b', fromSide: 'right', toSide: 'left' }],
    }
    const round = parseCanvas(serializeCanvas(data))
    expect(round.nodes).toEqual(data.nodes)
    expect(round.edges).toEqual(data.edges)
  })

  it('serializeCanvas omits undefined optional fields', () => {
    const data: CanvasData = {
      nodes: [{ id: 'a', type: 'text', x: 0, y: 0, width: 250, height: 160, text: 'x' }],
      edges: [],
    }
    const json = serializeCanvas(data)
    expect(json).not.toContain('color')
    expect(json).not.toContain('subpath')
  })

  it('collectCanvasFiles walks tree recursively and sorts by name', () => {
    const tree: RawFileNode[] = [
      { name: 'Notes', path: 'Notes', isDir: true, children: [
        { name: 'mind.canvas', path: 'Notes/mind.canvas', isDir: false },
        { name: 'readme.md', path: 'Notes/readme.md', isDir: false },
      ] },
      { name: 'plan.canvas', path: 'plan.canvas', isDir: false },
      { name: 'ignore.txt', path: 'ignore.txt', isDir: false },
    ]
    const files = collectCanvasFiles(tree)
    expect(files.map((f) => f.path)).toEqual(['Notes/mind.canvas', 'plan.canvas'])
  })

  it('genId produces unique ids', () => {
    const ids = new Set<string>()
    for (let i = 0; i < 1000; i++) ids.add(genId())
    expect(ids.size).toBe(1000)
  })

  it('createNode builds correct defaults per type', () => {
    const t = createNode('text', 5, 7)
    expect(t.type).toBe('text')
    expect(t.x).toBe(5)
    expect(t.y).toBe(7)
    expect(t.width).toBe(250)
    const f = createNode('file', 0, 0, { file: 'a.md' } as any)
    if (f.type !== 'file') throw new Error('expected file')
    expect(f.file).toBe('a.md')
  })

  it('nodeAnchor returns correct side midpoints', () => {
    const node = createNode('text', 100, 200)
    node.width = 100
    node.height = 50
    expect(nodeAnchor(node, 'top')).toEqual({ x: 150, y: 200 })
    expect(nodeAnchor(node, 'bottom')).toEqual({ x: 150, y: 250 })
    expect(nodeAnchor(node, 'left')).toEqual({ x: 100, y: 225 })
    expect(nodeAnchor(node, 'right')).toEqual({ x: 200, y: 225 })
  })

  it('edgeEndpoints computes points or null when node missing', () => {
    const a = createNode('text', 0, 0)
    const b = createNode('text', 200, 0)
    const map = new Map([
      [a.id, a],
      [b.id, b],
    ])
    const ep = edgeEndpoints({ id: 'e', fromNode: a.id, toNode: b.id }, map)
    expect(ep).not.toBeNull()
    expect(ep!.from).toEqual({ x: a.x + a.width, y: a.y + a.height / 2 })
    expect(ep!.to).toEqual({ x: b.x, y: b.y + b.height / 2 })
    expect(edgeEndpoints({ id: 'e', fromNode: 'missing', toNode: b.id }, map)).toBeNull()
  })
})

describe('nodesInGroup', () => {
  it('卡片中心落在分组矩形内才算成员', () => {
    const g = { ...createNode('group', 0, 0), width: 200, height: 200 } as ReturnType<typeof createNode>
    const inside = createNode('text', 50, 50)
    // 中心恰好压在右下边界 (200,200) 上：createNode 会 round 坐标，取半宽/半高精确对齐
    const probe = createNode('text', 0, 0)
    const edgeCaseCenter = createNode('text', 200 - probe.width / 2, 200 - probe.height / 2)
    const outside = createNode('text', 250, 50)

    const members = nodesInGroup([g, inside, edgeCaseCenter, outside], g)
    expect(members.map((n) => n.id)).toContain(inside.id)
    expect(members.map((n) => n.id)).toContain(edgeCaseCenter.id)
    expect(members.map((n) => n.id)).not.toContain(outside.id)
    expect(members.map((n) => n.id)).not.toContain(g.id)
  })

  it('分组不嵌套：其他分组永远不是成员', () => {
    const g = { ...createNode('group', 0, 0), width: 200, height: 200 } as ReturnType<typeof createNode>
    const nested = { ...createNode('group', 50, 50), width: 50, height: 50 } as ReturnType<typeof createNode>
    expect(nodesInGroup([g, nested], g)).toHaveLength(0)
  })

  it('非分组节点查询返回空数组', () => {
    const t = createNode('text', 0, 0)
    expect(nodesInGroup([t], t)).toHaveLength(0)
  })
})
