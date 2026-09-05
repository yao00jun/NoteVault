// JSON Canvas 数据层（P2-3）
//
// 纯函数、无 DOM 依赖，便于单元测试。画布即工作区内的 .canvas 纯 JSON 文件，
// 与 Obsidian 双向兼容（https://jsoncanvas.org）。读取时对任意缺失/非法字段做容错
// 归一化，保证「打开别人（或旧版）的 .canvas 不崩」；写出时严格 JSON Canvas 结构。

import type {
  CanvasData,
  CanvasEdge,
  CanvasNode,
  CanvasNodeColor,
  CanvasNodeSide,
} from '@/types'

/** 后端 GetFileTree 返回节点的精简形状（与 bindings FileNode 对齐） */
export interface RawFileNode {
  name: string
  path: string
  isDir: boolean
  children?: RawFileNode[]
}

export interface Point {
  x: number
  y: number
}

const DEFAULT_SIZE: Record<CanvasNode['type'], { width: number; height: number }> = {
  text: { width: 250, height: 160 },
  file: { width: 250, height: 80 },
  link: { width: 250, height: 80 },
  group: { width: 320, height: 240 },
}

const NODE_TYPES: CanvasNode['type'][] = ['text', 'file', 'link', 'group']
const VALID_SIDES: CanvasNodeSide[] = ['top', 'right', 'bottom', 'left']

let idCounter = 0

/** 生成画布内唯一 id（带随机熵，跨会话/跨画布不冲突） */
export function genId(prefix = 'n'): string {
  idCounter += 1
  const rand = Math.random().toString(36).slice(2, 8)
  return `${prefix}_${Date.now().toString(36)}_${idCounter}_${rand}`
}

export function createEmptyCanvas(): CanvasData {
  return { nodes: [], edges: [] }
}

export function defaultSizeForType(type: CanvasNode['type']): { width: number; height: number } {
  return DEFAULT_SIZE[type] ?? DEFAULT_SIZE.text
}

/** 工厂：按类型在 (x,y) 处创建一个带默认尺寸的节点 */
export function createNode(
  type: CanvasNode['type'],
  x: number,
  y: number,
  extra: Partial<CanvasNode> = {},
): CanvasNode {
  const size = defaultSizeForType(type)
  const base = {
    id: genId(type),
    x: Math.round(x),
    y: Math.round(y),
    width: size.width,
    height: size.height,
  }
  switch (type) {
    case 'text':
      return { ...base, type: 'text', text: (extra as Partial<CanvasTextNode>).text ?? '' }
    case 'file':
      return { ...base, type: 'file', file: (extra as Partial<CanvasFileNode>).file ?? '' }
    case 'link':
      return { ...base, type: 'link', url: (extra as Partial<CanvasLinkNode>).url ?? '' }
    default:
      return { ...base, type: 'group', label: (extra as Partial<CanvasGroupNode>).label ?? '' }
  }
}

/** 解析 JSON Canvas；非法 JSON 抛错，其余缺失/非法字段做容错归一化 */
export function parseCanvas(raw: string): CanvasData {
  if (!raw || !raw.trim()) return createEmptyCanvas()
  let obj: any
  try {
    obj = JSON.parse(raw)
  } catch (e) {
    throw new Error(`画布文件不是合法 JSON：${(e as Error).message}`, { cause: e })
  }
  const nodesRaw = Array.isArray(obj?.nodes) ? obj.nodes : []
  const edgesRaw = Array.isArray(obj?.edges) ? obj.edges : []
  const nodes = nodesRaw.map((n: any) => normalizeNode(n))
  const edges = edgesRaw
    .map((e: any) => normalizeEdge(e))
    .filter((e: CanvasEdge | null): e is CanvasEdge => e !== null)
  return { nodes, edges }
}

function normalizeNode(n: any): CanvasNode {
  const type = NODE_TYPES.includes(n?.type) ? n.type : 'text'
  const size = defaultSizeForType(type)
  const base: any = {
    id: typeof n?.id === 'string' && n.id ? n.id : genId(type),
    x: Number.isFinite(n?.x) ? Number(n.x) : 0,
    y: Number.isFinite(n?.y) ? Number(n.y) : 0,
    width: Number.isFinite(n?.width) ? Number(n.width) : size.width,
    height: Number.isFinite(n?.height) ? Number(n.height) : size.height,
  }
  if (typeof n?.color === 'string') base.color = n.color as CanvasNodeColor
  switch (type) {
    case 'text':
      return { ...base, type: 'text', text: typeof n?.text === 'string' ? n.text : '' }
    case 'file':
      return {
        ...base,
        type: 'file',
        file: typeof n?.file === 'string' ? n.file : '',
        ...(typeof n?.subpath === 'string' ? { subpath: n.subpath } : {}),
      }
    case 'link':
      return { ...base, type: 'link', url: typeof n?.url === 'string' ? n.url : '' }
    default:
      return {
        ...base,
        type: 'group',
        ...(typeof n?.label === 'string' ? { label: n.label } : {}),
      }
  }
}

function normalizeEdge(e: any): CanvasEdge | null {
  if (!e || typeof e.fromNode !== 'string' || typeof e.toNode !== 'string') return null
  const out: CanvasEdge = {
    id: typeof e?.id === 'string' && e.id ? e.id : genId('e'),
    fromNode: e.fromNode,
    toNode: e.toNode,
  }
  if (VALID_SIDES.includes(e?.fromSide)) out.fromSide = e.fromSide
  if (VALID_SIDES.includes(e?.toSide)) out.toSide = e.toSide
  if (e?.fromEnd === 'arrow' || e?.fromEnd === 'none') out.fromEnd = e.fromEnd
  if (e?.toEnd === 'arrow' || e?.toEnd === 'none') out.toEnd = e.toEnd
  if (typeof e?.color === 'string') out.color = e.color
  if (typeof e?.label === 'string' && e.label) out.label = e.label
  return out
}

/** 序列化回 JSON Canvas（2 空格缩进，人类可读，可被 Obsidian 直接打开） */
export function serializeCanvas(data: CanvasData): string {
  const out: CanvasData = {
    nodes: data.nodes ?? [],
    edges: data.edges ?? [],
  }
  return JSON.stringify(out, null, 2)
}

/** 节点某条边的中点（用于连线端点几何） */
export function nodeAnchor(node: CanvasNode, side: CanvasNodeSide = 'right'): Point {
  switch (side) {
    case 'top':
      return { x: node.x + node.width / 2, y: node.y }
    case 'bottom':
      return { x: node.x + node.width / 2, y: node.y + node.height }
    case 'left':
      return { x: node.x, y: node.y + node.height / 2 }
    case 'right':
    default:
      return { x: node.x + node.width, y: node.y + node.height / 2 }
  }
}

/** 计算一条边的两个端点（节点缺失时返回 null） */
export function edgeEndpoints(
  edge: CanvasEdge,
  nodeMap: Map<string, CanvasNode>,
): { from: Point; to: Point } | null {
  const from = nodeMap.get(edge.fromNode)
  const to = nodeMap.get(edge.toNode)
  if (!from || !to) return null
  return {
    from: nodeAnchor(from, edge.fromSide ?? 'right'),
    to: nodeAnchor(to, edge.toSide ?? 'left'),
  }
}

/** 递归收集工作区里所有 .canvas 文件（按名称排序） */
export function collectCanvasFiles(nodes: RawFileNode[]): { path: string; name: string }[] {
  const out: { path: string; name: string }[] = []
  const walk = (list: RawFileNode[]) => {
    for (const n of list) {
      if (n.isDir) {
        if (n.children) walk(n.children)
      } else if (n.name.toLowerCase().endsWith('.canvas')) {
        out.push({ path: n.path, name: n.name })
      }
    }
  }
  walk(nodes)
  out.sort((a, b) => a.name.localeCompare(b.name))
  return out
}

/**
 * 分组容器语义：卡片中心落在分组矩形内即视为成员（不含其他分组）。
 * 拖动分组 / 删除分组时用它圈定要联动（或一并删除）的卡片。
 */
export function nodesInGroup(nodes: CanvasNode[], group: CanvasNode): CanvasNode[] {
  if (group.type !== 'group') return []
  const right = group.x + group.width
  const bottom = group.y + group.height
  return nodes.filter((n) => {
    if (n.id === group.id || n.type === 'group') return false
    const cx = n.x + n.width / 2
    const cy = n.y + n.height / 2
    return cx >= group.x && cx <= right && cy >= group.y && cy <= bottom
  })
}

// 类型再导出，方便视图层单点 import
import type { CanvasTextNode, CanvasFileNode, CanvasLinkNode, CanvasGroupNode } from '@/types'
export type {
  CanvasTextNode,
  CanvasFileNode,
  CanvasLinkNode,
  CanvasGroupNode,
}
