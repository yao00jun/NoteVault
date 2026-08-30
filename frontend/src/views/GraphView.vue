<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { GitGraph, RefreshCw, Circle, ZoomIn, ZoomOut, Maximize, AlertTriangle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { GraphService } from '@bindings/github.com/notevault/notevault/index.js'

interface GNode {
  id: string
  title: string
  path: string
  degree: number
  resolved: boolean
  x: number
  y: number
  vx: number
  vy: number
}
interface GEdge {
  source: string
  target: string
}
interface RenderedEdge extends GEdge {
  x1: number
  y1: number
  x2: number
  y2: number
}

const { t } = useI18n()
const router = useRouter()
const workspaceStore = useWorkspaceStore()

const containerRef = ref<HTMLElement | null>(null)
const nodes = ref<GNode[]>([])
const edges = ref<GEdge[]>([])
const loading = ref(false)
const errorMsg = ref('')

// 节点上限：超过此数量时仅显示连接数最多的节点
const maxGraphNodes = 300
const showLimitWarning = ref(false)
const totalNodeCount = ref(0)
const totalEdgeCount = ref(0)

const width = ref(800)
const height = ref(600)
const view = ref({ x: 0, y: 0, k: 1 })
const hoveredId = ref<string | null>(null)
const dragId = ref<string | null>(null)
const panning = ref(false)
const dragStart = ref({ x: 0, y: 0, vx: 0, vy: 0 })

const hasWorkspace = computed(() => !!workspaceStore.currentWorkspace)
const nodeCount = computed(() => nodes.value.length)
const edgeCount = computed(() => edges.value.length)
const orphanCount = computed(() => nodes.value.filter((n) => n.resolved && n.degree === 0).length)
const unresolvedCount = computed(() => nodes.value.filter((n) => !n.resolved).length)

// nodeMap: O(1) 节点查找（替代 .find() 线性查找）
const nodeMap = computed(() => {
  const map = new Map<string, GNode>()
  for (const n of nodes.value) map.set(n.id, n)
  return map
})

// renderedEdges: 预计算边的两端坐标，避免模板中 O(n) find 调用
const renderedEdges = computed<RenderedEdge[]>(() => {
  const map = nodeMap.value
  return edges.value.map((e) => {
    const s = map.get(e.source)
    const t = map.get(e.target)
    return {
      source: e.source,
      target: e.target,
      x1: s?.x || 0,
      y1: s?.y || 0,
      x2: t?.x || 0,
      y2: t?.y || 0,
    }
  })
})

const connectedIds = computed(() => {
  if (!hoveredId.value) return null
  const set = new Set<string>([hoveredId.value])
  for (const e of edges.value) {
    if (e.source === hoveredId.value) set.add(e.target)
    if (e.target === hoveredId.value) set.add(e.source)
  }
  return set
})

function isNodeActive(id: string): boolean {
  if (!connectedIds.value) return true
  return connectedIds.value.has(id)
}
function isEdgeActive(e: { source: string; target: string }): boolean {
  if (!hoveredId.value) return true
  return e.source === hoveredId.value || e.target === hoveredId.value
}

async function loadGraph() {
  if (!workspaceStore.currentWorkspace?.path) {
    nodes.value = []
    edges.value = []
    showLimitWarning.value = false
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    const data = (await GraphService.GetGraph(workspaceStore.currentWorkspace.path)) as {
      nodes: GNode[]
      edges: GEdge[]
    }
    const allNodes = (data.nodes || []).map((n) => ({ ...n, x: 0, y: 0, vx: 0, vy: 0 }))
    const allEdges = data.edges || []

    totalNodeCount.value = allNodes.length
    totalEdgeCount.value = allEdges.length

    // 节点上限：按 degree 降序取 top-N，过滤掉悬空边
    if (allNodes.length > maxGraphNodes) {
      allNodes.sort((a, b) => b.degree - a.degree)
      const topNodes = allNodes.slice(0, maxGraphNodes)
      const visibleIds = new Set(topNodes.map((n) => n.id))
      const filteredEdges = allEdges.filter(
        (e) => visibleIds.has(e.source) && visibleIds.has(e.target),
      )
      nodes.value = topNodes
      edges.value = filteredEdges
      showLimitWarning.value = true
    } else {
      nodes.value = allNodes
      edges.value = allEdges
      showLimitWarning.value = false
    }

    if (nodes.value.length > 0) {
      simulate()
    }
  } catch (e) {
    errorMsg.value = t('graph.loadFailed', { msg: (e as string) })
    console.error(e)
  } finally {
    loading.value = false
  }
}

// 力导向布局：斥力 + 弹簧 + 向心力，迭代收敛
function simulate() {
  const W = width.value
  const H = height.value
  const cx = W / 2
  const cy = H / 2
  const n = nodes.value.length
  const R = Math.max(80, Math.min(W, H) / 3)

  // 初始化：环形分布
  nodes.value.forEach((node, i) => {
    const angle = (i / Math.max(1, n)) * Math.PI * 2
    node.x = cx + R * Math.cos(angle) + (Math.random() - 0.5) * 20
    node.y = cy + R * Math.sin(angle) + (Math.random() - 0.5) * 20
    node.vx = 0
    node.vy = 0
  })

  // 自适应迭代次数：节点越多迭代越少（保持总计算量稳定）
  const iterations = n > 200 ? 120 : n > 100 ? 200 : 320
  // 斥力截断距离：超过此距离的节点对跳过斥力计算
  const repelCutoff = 400

  const kRepel = 9000
  const kSpring = 0.02
  const restLen = 90
  const kGravity = 0.015
  const damping = 0.85

  // 本地 nodeMap 供弹簧力 O(1) 查找
  const map = new Map<string, GNode>()
  for (const node of nodes.value) map.set(node.id, node)

  for (let it = 0; it < iterations; it++) {
    // 斥力（所有节点两两，带距离截断）
    for (let i = 0; i < n; i++) {
      const a = nodes.value[i]
      for (let j = i + 1; j < n; j++) {
        const b = nodes.value[j]
        let dx = a.x - b.x
        let dy = a.y - b.y
        let dist2 = dx * dx + dy * dy
        if (dist2 > repelCutoff * repelCutoff) continue // 截断
        if (dist2 < 0.01) {
          dx = (Math.random() - 0.5) * 2
          dy = (Math.random() - 0.5) * 2
          dist2 = 1
        }
        const dist = Math.sqrt(dist2)
        const force = kRepel / dist2
        const fx = (dx / dist) * force
        const fy = (dy / dist) * force
        a.vx += fx
        a.vy += fy
        b.vx -= fx
        b.vy -= fy
      }
    }
    // 弹簧（边）— O(1) map 查找
    for (const e of edges.value) {
      const a = map.get(e.source)
      const b = map.get(e.target)
      if (!a || !b) continue
      const dx = b.x - a.x
      const dy = b.y - a.y
      const dist = Math.sqrt(dx * dx + dy * dy) || 1
      const force = kSpring * (dist - restLen)
      const fx = (dx / dist) * force
      const fy = (dy / dist) * force
      a.vx += fx
      a.vy += fy
      b.vx -= fx
      b.vy -= fy
    }
    // 向心力 + 阻尼
    for (const node of nodes.value) {
      node.vx += (cx - node.x) * kGravity
      node.vy += (cy - node.y) * kGravity
      node.vx *= damping
      node.vy *= damping
      node.x += node.vx
      node.y += node.vy
    }
  }
  // 触发重渲染
  nodes.value = [...nodes.value]
}

function nodeRadius(node: GNode): number {
  return 6 + Math.min(18, node.degree * 2.2)
}
function nodeColor(node: GNode): string {
  if (!node.resolved) return 'var(--text-muted)'
  if (node.degree >= 4) return 'var(--accent)'
  if (node.degree === 0) return 'var(--danger, #e5484d)'
  return 'var(--accent-2, #4f9cf0)'
}

function screenToWorld(sx: number, sy: number) {
  return {
    x: (sx - view.value.x) / view.value.k,
    y: (sy - view.value.y) / view.value.k,
  }
}

function onWheel(e: WheelEvent) {
  e.preventDefault()
  const rect = containerRef.value!.getBoundingClientRect()
  const mx = e.clientX - rect.left
  const my = e.clientY - rect.top
  const oldK = view.value.k
  const factor = e.deltaY < 0 ? 1.1 : 0.9
  const newK = Math.min(3, Math.max(0.2, oldK * factor))
  const wx = (mx - view.value.x) / oldK
  const wy = (my - view.value.y) / oldK
  view.value = {
    k: newK,
    x: mx - wx * newK,
    y: my - wy * newK,
  }
}

function onNodeMouseDown(e: MouseEvent, id: string) {
  e.stopPropagation()
  dragId.value = id
  dragStart.value = { x: e.clientX, y: e.clientY, vx: view.value.x, vy: view.value.y }
}
function onBackgroundMouseDown(e: MouseEvent) {
  panning.value = true
  dragStart.value = { x: e.clientX, y: e.clientY, vx: view.value.x, vy: view.value.y }
}
function onMouseMove(e: MouseEvent) {
  if (dragId.value) {
    // O(1) map 查找替代 .find()
    const node = nodeMap.value.get(dragId.value)
    if (node) {
      const w = screenToWorld(e.clientX, e.clientY)
      node.x = w.x
      node.y = w.y
      node.vx = 0
      node.vy = 0
    }
  } else if (panning.value) {
    view.value = {
      ...view.value,
      x: dragStart.value.vx + (e.clientX - dragStart.value.x),
      y: dragStart.value.vy + (e.clientY - dragStart.value.y),
    }
  }
}
function onMouseUp() {
  dragId.value = null
  panning.value = false
}

function openNode(node: GNode) {
  if (!node.resolved) return
  workspaceStore.openFile(node.id)
  workspaceStore.incrementFileTreeVersion()
  router.push('/editor')
}

function zoomBy(factor: number) {
  const cx = width.value / 2
  const cy = height.value / 2
  const oldK = view.value.k
  const newK = Math.min(3, Math.max(0.2, oldK * factor))
  const wx = (cx - view.value.x) / oldK
  const wy = (cy - view.value.y) / oldK
  view.value = { k: newK, x: cx - wx * newK, y: cy - wy * newK }
}
function resetView() {
  view.value = { x: 0, y: 0, k: 1 }
  if (nodes.value.length) simulate()
}

function measure() {
  if (!containerRef.value) return
  width.value = containerRef.value.clientWidth
  height.value = containerRef.value.clientHeight
}

let resizeObserver: ResizeObserver | null = null
onMounted(async () => {
  await nextTick()
  measure()
  resizeObserver = new ResizeObserver(() => measure())
  if (containerRef.value) resizeObserver.observe(containerRef.value)
  await loadGraph()
})
onUnmounted(() => {
  resizeObserver?.disconnect()
  window.removeEventListener('mousemove', onMouseMove)
  window.removeEventListener('mouseup', onMouseUp)
})

watch(
  () => workspaceStore.currentWorkspace?.path,
  () => loadGraph(),
)

if (typeof window !== 'undefined') {
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}
</script>

<template>
  <div class="graph-view">
    <div class="graph-header">
      <div class="graph-title">
        <GitGraph :size="20" />
        <h2>{{ t('graph.title') }}</h2>
      </div>
      <div class="graph-stats">
        <span class="stat"><Circle
          :size="12"
          class="dot-resolved"
        /> {{ t('graph.linked', { count: nodeCount - unresolvedCount }) }}</span>
        <span class="stat"><Circle
          :size="12"
          class="dot-orphan"
        /> {{ t('graph.orphan', { count: orphanCount }) }}</span>
        <span class="stat"><Circle
          :size="12"
          class="dot-unresolved"
        /> {{ t('graph.unresolved', { count: unresolvedCount }) }}</span>
        <span class="stat">{{ t('graph.edges', { count: edgeCount }) }}</span>
      </div>
      <div class="graph-actions">
        <button
          class="icon-btn"
          :title="t('graph.relayout')"
          @click="resetView"
        >
          <RefreshCw :size="16" />
        </button>
        <button
          class="icon-btn"
          :title="t('graph.zoomIn')"
          @click="zoomBy(1.2)"
        >
          <ZoomIn :size="16" />
        </button>
        <button
          class="icon-btn"
          :title="t('graph.zoomOut')"
          @click="zoomBy(0.8)"
        >
          <ZoomOut :size="16" />
        </button>
        <button
          class="icon-btn"
          :title="t('graph.fitView')"
          @click="resetView"
        >
          <Maximize :size="16" />
        </button>
      </div>
    </div>

    <div
      v-if="showLimitWarning"
      class="graph-warning"
    >
      <AlertTriangle :size="14" />
      <span>{{ t('graph.limitWarning', { total: totalNodeCount, max: maxGraphNodes, totalEdges: totalEdgeCount, edges: edgeCount }) }}</span>
    </div>

    <div
      ref="containerRef"
      class="graph-canvas"
      @wheel="onWheel"
      @mousedown="onBackgroundMouseDown"
    >
      <div
        v-if="!hasWorkspace"
        class="graph-empty"
      >
        <div class="empty-icon">
          🕸️
        </div>
        <h3>{{ t('graph.noWorkspaceTitle') }}</h3>
        <p>{{ t('graph.noWorkspaceDesc') }}</p>
      </div>
      <div
        v-else-if="loading"
        class="graph-empty"
      >
        <div class="spinner" />
        <p>{{ t('graph.building') }}</p>
      </div>
      <div
        v-else-if="errorMsg"
        class="graph-empty"
      >
        <div class="empty-icon">
          ⚠️
        </div>
        <p>{{ errorMsg }}</p>
      </div>
      <div
        v-else-if="nodeCount === 0"
        class="graph-empty"
      >
        <div class="empty-icon">
          📭
        </div>
        <h3>{{ t('graph.emptyTitle') }}</h3>
        <p>{{ t('graph.emptyDesc') }}</p>
      </div>

      <svg
        v-else
        class="graph-svg"
        :width="width"
        :height="height"
      >
        <g :transform="`translate(${view.x},${view.y}) scale(${view.k})`">
          <!-- 边：预计算坐标，O(1) 查找 -->
          <line
            v-for="(e, i) in renderedEdges"
            :key="'e' + i"
            :x1="e.x1"
            :y1="e.y1"
            :x2="e.x2"
            :y2="e.y2"
            class="graph-edge"
            :class="{ dim: !isEdgeActive(e) }"
          />
          <!-- 节点 -->
          <g
            v-for="node in nodes"
            :key="node.id"
            :transform="`translate(${node.x},${node.y})`"
            class="graph-node"
            :class="{ dim: !isNodeActive(node.id), unresolved: !node.resolved }"
            style="cursor: pointer"
            @mousedown="onNodeMouseDown($event, node.id)"
            @mouseenter="hoveredId = node.id"
            @mouseleave="hoveredId = null"
            @click="openNode(node)"
          >
            <circle
              :r="nodeRadius(node)"
              :fill="nodeColor(node)"
              :stroke="hoveredId === node.id ? 'var(--text-primary)' : 'transparent'"
              stroke-width="2"
            />
            <text
              :x="nodeRadius(node) + 4"
              :y="4"
              class="graph-label"
            >{{ node.title }}</text>
          </g>
        </g>
      </svg>
    </div>
  </div>
</template>

<style scoped>
.graph-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.graph-header {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-6);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
  flex-wrap: wrap;
}
.graph-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--accent);
}
.graph-title h2 {
  font-size: var(--text-lg);
  font-weight: 700;
  margin: 0;
  color: var(--text-primary);
}
.graph-stats {
  display: flex;
  gap: var(--space-4);
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.stat {
  display: flex;
  align-items: center;
  gap: 4px;
}
.dot-resolved { color: var(--accent-2, #4f9cf0); }
.dot-orphan { color: var(--danger, #e5484d); }
.dot-unresolved { color: var(--text-muted); }
.graph-actions {
  margin-left: auto;
  display: flex;
  gap: var(--space-2);
}
.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
}
.icon-btn:hover {
  background: var(--bg-hover);
  color: var(--accent);
}
.graph-warning {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-6);
  background: var(--bg-sidebar);
  color: var(--text-muted);
  font-size: var(--text-xs);
  border-bottom: 1px solid var(--border);
}
.graph-canvas {
  flex: 1;
  position: relative;
  overflow: hidden;
  background:
    radial-gradient(circle at 1px 1px, var(--border) 1px, transparent 0) 0 0 / 24px 24px;
  background-color: var(--bg-window);
}
.graph-svg {
  display: block;
}
.graph-edge {
  stroke: var(--border-strong, #444);
  stroke-width: 1.2;
  transition: opacity var(--transition-fast);
}
.graph-node .graph-label {
  font-size: 12px;
  fill: var(--text-secondary);
  pointer-events: none;
  user-select: none;
}
.graph-node.unresolved .graph-label {
  fill: var(--text-muted);
  font-style: italic;
}
.graph-node.dim { opacity: 0.18; }
.graph-edge.dim { opacity: 0.05; }
.graph-empty {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  color: var(--text-muted);
  text-align: center;
  padding: var(--space-8);
}
.graph-empty h3 { margin: 0; color: var(--text-secondary); }
.graph-empty code {
  background: var(--bg-sidebar);
  padding: 2px 6px;
  border-radius: 4px;
  color: var(--accent);
}
.empty-icon { font-size: 48px; opacity: 0.6; }
.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
</style>
