<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import type { CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { FileService } from '@bindings/github.com/notevault/notevault/index.js'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { confirmDialog } from '@/composables/useConfirm'
import {
  collectCanvasFiles,
  parseCanvas,
  serializeCanvas,
  createNode,
  genId,
  nodeAnchor,
  edgeEndpoints,
  type RawFileNode,
} from '@/lib/canvas'
import type { CanvasData, CanvasNode, CanvasEdge, CanvasNodeColor, CanvasGroupNode } from '@/types'
import MarkdownPreview from '@/components/editor/MarkdownPreview.vue'

const { t } = useI18n()
const router = useRouter()
const workspaceStore = useWorkspaceStore()
const toast = useToast()

const workspacePath = computed(() => workspaceStore.currentWorkspace?.path ?? '')

// ---- 列表模式状态 ----
const canvasFiles = ref<{ path: string; name: string }[]>([])
const listLoading = ref(false)

// ---- 编辑器模式状态 ----
const currentPath = ref<string | null>(null)
const data = ref<CanvasData>({ nodes: [], edges: [] })
const selectedId = ref<string | null>(null)
const selectedEdgeId = ref<string | null>(null)
const editingId = ref<string | null>(null)
const errorMsg = ref<string | null>(null)
const saveStatus = ref<'saved' | 'saving' | 'unsaved'>('saved')
const connectFrom = ref<string | null>(null)
const tempEnd = ref<{ x: number; y: number } | null>(null)

const viewport = reactive({ x: 40, y: 40, scale: 1 })
const surfaceRef = ref<HTMLElement | null>(null)

const nodeMap = computed(() => new Map(data.value.nodes.map((n) => [n.id, n])))
function isGroup(n: CanvasNode): n is CanvasGroupNode {
  return n.type === 'group'
}
const groupNodes = computed(() => data.value.nodes.filter(isGroup))
const otherNodes = computed(() => data.value.nodes.filter((n) => n.type !== 'group'))

// ============ 颜色 ============
const COLOR_MAP: Record<CanvasNodeColor, string> = {
  red: '#e5484d',
  orange: '#f76808',
  yellow: '#caa11a',
  green: '#46a758',
  cyan: '#22b8cf',
  blue: '#3b82f6',
  purple: '#8e51ff',
  pink: '#e93d82',
  '1': '#6b7280',
  '2': '#f59e0b',
  '3': '#10b981',
  '4': '#3b82f6',
  '5': '#8b5cf6',
  '6': '#ec4899',
}
function colorOf(c?: CanvasNodeColor): string | null {
  return c ? COLOR_MAP[c] ?? null : null
}
function nodeAccentStyle(n: CanvasNode): Record<string, string> {
  const c = colorOf(n.color)
  if (!c) return {}
  if (n.type === 'group') return { background: hexToRgba(c, 0.12), borderColor: hexToRgba(c, 0.4) }
  return { borderLeft: `3px solid ${c}` }
}
function hexToRgba(hex: string, a: number): string {
  const h = hex.replace('#', '')
  const r = parseInt(h.slice(0, 2), 16)
  const g = parseInt(h.slice(2, 4), 16)
  const b = parseInt(h.slice(4, 6), 16)
  return `rgba(${r},${g},${b},${a})`
}

// ============ 列表 ============
async function loadList() {
  if (!workspacePath.value) return
  listLoading.value = true
  try {
    const tree = (await FileService.GetFileTree(workspacePath.value)) as RawFileNode[]
    canvasFiles.value = collectCanvasFiles(tree ?? [])
  } catch (e) {
    toast.error(t('canvas.parseError', { msg: (e as Error).message }))
  } finally {
    listLoading.value = false
  }
}

async function createCanvas() {
  if (!workspacePath.value) return
  const name = prompt(t('canvas.newCanvasPrompt'), t('canvas.untitledCanvas'))
  if (!name) return
  const fileName = name.endsWith('.canvas') ? name : `${name}.canvas`
  try {
    await FileService.CreateFile(workspacePath.value, fileName, serializeCanvas({ nodes: [], edges: [] }))
    await loadList()
    await openCanvas(fileName)
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      toast.error(t('sidebar.fileExists'))
    } else {
      toast.error((e as Error).message)
    }
  }
}

async function deleteCanvas(path: string) {
  if (!(await confirmDialog({ message: t('canvas.deleteCanvasConfirm', { name: path.split('/').pop() }), danger: true }))) return
  try {
    await FileService.DeleteFile(workspacePath.value, path)
    await loadList()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function openCanvas(path: string) {
  if (!workspacePath.value) return
  try {
    const raw = await FileService.ReadFile(workspacePath.value, path)
    data.value = parseCanvas(raw)
    currentPath.value = path
    selectedId.value = null
    selectedEdgeId.value = null
    editingId.value = null
    errorMsg.value = null
    saveStatus.value = 'saved'
    viewport.x = 40
    viewport.y = 40
    viewport.scale = 1
  } catch (e) {
    errorMsg.value = t('canvas.parseError', { msg: (e as Error).message })
  }
}

function backToList() {
  flushSave()
  currentPath.value = null
  void loadList()
}

// ============ 保存 ============
let saveTimer: ReturnType<typeof setTimeout> | null = null
function scheduleSave() {
  saveStatus.value = 'unsaved'
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => void doSave(), 800)
}
async function doSave() {
  if (!currentPath.value || !workspacePath.value) return
  saveStatus.value = 'saving'
  try {
    await FileService.SaveFile(workspacePath.value, currentPath.value, serializeCanvas(data.value))
    saveStatus.value = 'saved'
  } catch (e) {
    saveStatus.value = 'unsaved'
    errorMsg.value = (e as Error).message
  }
}
function flushSave() {
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  if (saveStatus.value !== 'saved' && currentPath.value) void doSave()
}

// ============ 节点操作 ============
function addNode(type: CanvasNode['type']) {
  const p = getCanvasPointCentered()
  const node = createNode(type, p.x, p.y)
  data.value.nodes.push(node)
  selectedId.value = node.id
  selectedEdgeId.value = null
  scheduleSave()
}
function updateNode(id: string, patch: Partial<CanvasNode>) {
  const n = data.value.nodes.find((x) => x.id === id)
  if (!n) return
  Object.assign(n, patch)
  scheduleSave()
}
async function deleteNode(id: string) {
  if (!(await confirmDialog({ message: t('canvas.deleteNodeConfirm'), danger: true }))) return
  data.value.nodes = data.value.nodes.filter((n) => n.id !== id)
  data.value.edges = data.value.edges.filter((e) => e.fromNode !== id && e.toNode !== id)
  if (selectedId.value === id) selectedId.value = null
  scheduleSave()
}
function addEdge(from: string, to: string) {
  if (from === to) return
  const dup = data.value.edges.some((e) => e.fromNode === from && e.toNode === to)
  if (dup) return
  const edge: CanvasEdge = { id: genId('e'), fromNode: from, toNode: to }
  data.value.edges.push(edge)
  scheduleSave()
}
function deleteEdge(id: string) {
  data.value.edges = data.value.edges.filter((e) => e.id !== id)
  if (selectedEdgeId.value === id) selectedEdgeId.value = null
  scheduleSave()
}

function openNote(node: CanvasNode) {
  if (node.type !== 'file' || !node.file) {
    toast.warning(t('canvas.openNoteMissing'))
    return
  }
  workspaceStore.openFile(node.file)
  router.push('/editor')
}
function openLink(node: CanvasNode) {
  if (node.type !== 'link' || !node.url) return
  window.open(node.url, '_blank', 'noopener')
}

// ============ 坐标换算 ============
function getCanvasPoint(e: MouseEvent): { x: number; y: number } {
  const rect = surfaceRef.value!.getBoundingClientRect()
  return {
    x: (e.clientX - rect.left - viewport.x) / viewport.scale,
    y: (e.clientY - rect.top - viewport.y) / viewport.scale,
  }
}
function getCanvasPointCentered() {
  const cx = (surfaceRef.value!.clientWidth / 2 - viewport.x) / viewport.scale
  const cy = (surfaceRef.value!.clientHeight / 2 - viewport.y) / viewport.scale
  return { x: cx - 125, y: cy - 80 }
}
function clamp(v: number, min: number, max: number) {
  return Math.max(min, Math.min(max, v))
}

// ============ 鼠标交互 ============
let drag: {
  mode: 'pan' | 'node' | 'resize' | 'connect'
  nodeId?: string
  startX: number
  startY: number
  nodeX?: number
  nodeY?: number
  nodeW?: number
  nodeH?: number
  vx?: number
  vy?: number
} | null = null

function onSurfaceMouseDown(e: MouseEvent) {
  if (editingId.value) return
  const target = e.target as HTMLElement
  const handle = target.closest('[data-handle]') as HTMLElement | null
  if (handle) {
    const nodeId = handle.getAttribute('data-node-id')!
    const kind = handle.getAttribute('data-handle')
    const node = data.value.nodes.find((n) => n.id === nodeId)
    if (!node) return
    if (kind === 'port') {
      connectFrom.value = nodeId
      const p = getCanvasPoint(e)
      tempEnd.value = p
      drag = { mode: 'connect', nodeId, startX: e.clientX, startY: e.clientY }
      window.addEventListener('mousemove', onWindowMove)
      window.addEventListener('mouseup', onWindowUp)
      e.preventDefault()
      return
    }
    if (kind === 'resize') {
      drag = {
        mode: 'resize',
        nodeId,
        startX: e.clientX,
        startY: e.clientY,
        nodeW: node.width,
        nodeH: node.height,
      }
      window.addEventListener('mousemove', onWindowMove)
      window.addEventListener('mouseup', onWindowUp)
      e.preventDefault()
      return
    }
  }
  const nodeEl = target.closest('[data-node-id]') as HTMLElement | null
  if (nodeEl) {
    const nodeId = nodeEl.getAttribute('data-node-id')!
    const node = data.value.nodes.find((n) => n.id === nodeId)
    if (!node) return
    selectedId.value = nodeId
    selectedEdgeId.value = null
    drag = {
      mode: 'node',
      nodeId,
      startX: e.clientX,
      startY: e.clientY,
      nodeX: node.x,
      nodeY: node.y,
    }
    window.addEventListener('mousemove', onWindowMove)
    window.addEventListener('mouseup', onWindowUp)
    e.preventDefault()
    return
  }
  // 背景：平移
  selectedId.value = null
  selectedEdgeId.value = null
  drag = { mode: 'pan', startX: e.clientX, startY: e.clientY, vx: viewport.x, vy: viewport.y }
  window.addEventListener('mousemove', onWindowMove)
  window.addEventListener('mouseup', onWindowUp)
}

function onWindowMove(e: MouseEvent) {
  if (!drag) return
  if (drag.mode === 'pan') {
    viewport.x = drag.vx! + (e.clientX - drag.startX)
    viewport.y = drag.vy! + (e.clientY - drag.startY)
  } else if (drag.mode === 'node') {
    const node = data.value.nodes.find((n) => n.id === drag!.nodeId)
    if (!node) return
    node.x = Math.round(drag!.nodeX! + (e.clientX - drag!.startX) / viewport.scale)
    node.y = Math.round(drag!.nodeY! + (e.clientY - drag!.startY) / viewport.scale)
    scheduleSave()
  } else if (drag.mode === 'resize') {
    const node = data.value.nodes.find((n) => n.id === drag!.nodeId)
    if (!node) return
    node.width = Math.max(80, Math.round(drag!.nodeW! + (e.clientX - drag!.startX) / viewport.scale))
    node.height = Math.max(60, Math.round(drag!.nodeH! + (e.clientY - drag!.startY) / viewport.scale))
    scheduleSave()
  } else if (drag.mode === 'connect') {
    tempEnd.value = getCanvasPoint(e)
  }
}

function onWindowUp(e: MouseEvent) {
  window.removeEventListener('mousemove', onWindowMove)
  window.removeEventListener('mouseup', onWindowUp)
  if (drag?.mode === 'connect' && connectFrom.value) {
    const el = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null
    const targetNode = el?.closest('[data-node-id]') as HTMLElement | null
    if (targetNode) {
      const toId = targetNode.getAttribute('data-node-id')!
      if (toId !== connectFrom.value) addEdge(connectFrom.value, toId)
    }
    connectFrom.value = null
    tempEnd.value = null
  }
  drag = null
}

function onSurfaceDblClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (target.closest('[data-node-id]')) return
  const p = getCanvasPoint(e)
  const node = createNode('text', p.x - 125, p.y - 40)
  data.value.nodes.push(node)
  selectedId.value = node.id
  editingId.value = node.id
  scheduleSave()
}

function onWheel(e: WheelEvent) {
  e.preventDefault()
  const rect = surfaceRef.value!.getBoundingClientRect()
  const cx = e.clientX - rect.left
  const cy = e.clientY - rect.top
  const factor = e.deltaY < 0 ? 1.1 : 1 / 1.1
  const newScale = clamp(viewport.scale * factor, 0.2, 3)
  const canvasX = (cx - viewport.x) / viewport.scale
  const canvasY = (cy - viewport.y) / viewport.scale
  viewport.x = cx - canvasX * newScale
  viewport.y = cy - canvasY * newScale
  viewport.scale = newScale
}

function zoomBy(factor: number) {
  const cx = surfaceRef.value!.clientWidth / 2
  const cy = surfaceRef.value!.clientHeight / 2
  const newScale = clamp(viewport.scale * factor, 0.2, 3)
  const canvasX = (cx - viewport.x) / viewport.scale
  const canvasY = (cy - viewport.y) / viewport.scale
  viewport.x = cx - canvasX * newScale
  viewport.y = cy - canvasY * newScale
  viewport.scale = newScale
}
function resetView() {
  viewport.x = 40
  viewport.y = 40
  viewport.scale = 1
}

function onKeyDown(e: KeyboardEvent) {
  if (!currentPath.value) return
  const tag = (document.activeElement?.tagName ?? '').toLowerCase()
  if (tag === 'input' || tag === 'textarea') return
  if (e.key === 'Delete' || e.key === 'Backspace') {
    if (selectedEdgeId.value) {
      e.preventDefault()
      deleteEdge(selectedEdgeId.value)
    } else if (selectedId.value) {
      e.preventDefault()
      deleteNode(selectedId.value)
    }
  }
}

// ============ 边几何 ============
const edgePaths = computed(() =>
  data.value.edges
    .map((edge) => {
      const pts = edgeEndpoints(edge, nodeMap.value)
      if (!pts) return null
      return { id: edge.id, from: pts.from, to: pts.to, color: colorOf(edge.color) }
    })
    .filter((x): x is { id: string; from: { x: number; y: number }; to: { x: number; y: number }; color: string | null } => x !== null),
)
const tempPath = computed(() => {
  if (!connectFrom.value || !tempEnd.value) return null
  const from = nodeMap.value.get(connectFrom.value)
  if (!from) return null
  return { from: nodeAnchor(from, 'right'), to: tempEnd.value }
})

const worldStyle = computed<CSSProperties>(() => ({
  transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.scale})`,
  transformOrigin: '0 0',
  position: 'absolute',
  top: '0',
  left: '0',
}))

onMounted(() => {
  void loadList()
  window.addEventListener('keydown', onKeyDown)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  // 卸载时可能正处于拖拽中：window 级 mousemove/mouseup 监听器带闭包，
  // 不摘除会连着 drag 状态一起泄漏
  window.removeEventListener('mousemove', onWindowMove)
  window.removeEventListener('mouseup', onWindowUp)
  flushSave()
})

function typeLabel(type: CanvasNode['type']): string {
  return t(`canvas.type${type.charAt(0).toUpperCase() + type.slice(1)}`)
}
</script>

<template>
  <div class="canvas-page">
    <!-- ===== 列表模式 ===== -->
    <template v-if="!currentPath">
      <header class="page-header">
        <h1>{{ t('canvas.listTitle') }}</h1>
        <button class="primary-btn" @click="createCanvas">{{ t('canvas.newCanvas') }}</button>
      </header>
      <div v-if="listLoading" class="list-hint">{{ t('common.loading') }}</div>
      <div v-else-if="canvasFiles.length === 0" class="empty-state">
        <p class="empty-title">{{ t('canvas.listEmpty') }}</p>
        <p class="empty-hint">{{ t('canvas.listEmptyHint') }}</p>
        <button class="primary-btn" @click="createCanvas">{{ t('canvas.newCanvas') }}</button>
      </div>
      <div v-else class="canvas-grid">
        <div
          v-for="f in canvasFiles"
          :key="f.path"
          class="canvas-card"
          @click="openCanvas(f.path)"
        >
          <div class="canvas-card-name">{{ f.name }}</div>
          <div class="canvas-card-actions">
            <button class="ghost-btn" @click.stop="openCanvas(f.path)">{{ t('canvas.open') }}</button>
            <button class="danger-btn" @click.stop="deleteCanvas(f.path)">{{ t('canvas.delete') }}</button>
          </div>
        </div>
      </div>
    </template>

    <!-- ===== 编辑器模式 ===== -->
    <template v-else>
      <header class="canvas-toolbar">
        <button class="ghost-btn" @click="backToList">{{ t('canvas.backToList') }}</button>
        <span class="canvas-name">{{ currentPath.split('/').pop() }}</span>
        <div class="toolbar-sep" />
        <button class="tool-btn" @click="addNode('text')">{{ t('canvas.addText') }}</button>
        <button class="tool-btn" @click="addNode('file')">{{ t('canvas.addFile') }}</button>
        <button class="tool-btn" @click="addNode('link')">{{ t('canvas.addLink') }}</button>
        <button class="tool-btn" @click="addNode('group')">{{ t('canvas.addGroup') }}</button>
        <div class="toolbar-sep" />
        <button class="icon-btn" :title="t('canvas.zoomOut')" @click="zoomBy(1 / 1.2)">−</button>
        <span class="zoom-label">{{ Math.round(viewport.scale * 100) }}%</span>
        <button class="icon-btn" :title="t('canvas.zoomIn')" @click="zoomBy(1.2)">+</button>
        <button class="icon-btn" :title="t('canvas.resetView')" @click="resetView">⤢</button>
        <div class="toolbar-spacer" />
        <span class="save-status" :class="saveStatus">
          {{ saveStatus === 'saving' ? t('canvas.saving') : saveStatus === 'saved' ? t('canvas.saved') : t('canvas.unsaved') }}
        </span>
        <button class="primary-btn" @click="doSave">{{ t('canvas.save') }}</button>
      </header>

      <div
        v-if="errorMsg"
        class="canvas-error"
      >{{ errorMsg }}</div>

      <div
        ref="surfaceRef"
        class="canvas-surface"
        @mousedown="onSurfaceMouseDown"
        @dblclick="onSurfaceDblClick"
        @wheel="onWheel"
      >
        <div class="canvas-world" :style="worldStyle">
          <!-- 分组（背景层） -->
          <div
            v-for="n in groupNodes"
            :key="n.id"
            class="node node-group"
            :class="{ selected: selectedId === n.id, connecting: connectFrom === n.id }"
            :data-node-id="n.id"
            :style="{ left: n.x + 'px', top: n.y + 'px', width: n.width + 'px', height: n.height + 'px', ...nodeAccentStyle(n) }"
            @dblclick.stop="editingId = n.id"
          >
            <div class="group-label" v-if="n.type === 'group' && n.label">{{ n.label }}</div>
            <input
              v-if="editingId === n.id && n.type === 'group'"
              class="group-input"
              :value="n.label || ''"
              @mousedown.stop
              @input="updateNode(n.id, { label: ($event.target as HTMLInputElement).value } as Partial<CanvasNode>)"
              @blur="editingId = null"
              @keyup.enter="editingId = null"
            />
            <span class="node-port" :data-handle="'port'" :data-node-id="n.id" />
            <span class="node-resize" :data-handle="'resize'" :data-node-id="n.id" />
          </div>

          <!-- 其它节点 -->
          <div
            v-for="n in otherNodes"
            :key="n.id"
            class="node"
            :class="['node-' + n.type, { selected: selectedId === n.id, connecting: connectFrom === n.id }]"
            :data-node-id="n.id"
            :style="{ left: n.x + 'px', top: n.y + 'px', width: n.width + 'px', height: n.height + 'px', ...nodeAccentStyle(n) }"
          >
            <div class="node-badge">{{ typeLabel(n.type) }}</div>

            <!-- 文字节点：非编辑态用 Markdown 渲染，双击进入编辑（纯文本 textarea） -->
            <template v-if="n.type === 'text'">
              <textarea
                v-if="editingId === n.id"
                class="node-textarea"
                :value="n.text"
                @mousedown.stop
                @input="updateNode(n.id, { text: ($event.target as HTMLTextAreaElement).value } as Partial<CanvasNode>)"
                @blur="editingId = null"
                @keyup.ctrl.enter="editingId = null"
              />
              <div
                v-else
                class="node-text"
                :title="t('canvas.editText')"
                @dblclick.stop="editingId = n.id"
              >
                <MarkdownPreview
                  :content="n.text || ''"
                  class="node-markdown"
                />
              </div>
            </template>

            <!-- 文件节点 -->
            <template v-else-if="n.type === 'file'">
              <div class="node-content node-file" @dblclick.stop="editingId = n.id">
                <span class="node-icon">📄</span>
                <span class="node-file-path">{{ n.file || '—' }}</span>
              </div>
              <input
                v-if="editingId === n.id"
                class="node-edit-input"
                :value="n.file"
                :placeholder="t('canvas.nodePath')"
                @mousedown.stop
                @input="updateNode(n.id, { file: ($event.target as HTMLInputElement).value } as Partial<CanvasNode>)"
                @blur="editingId = null"
                @keyup.enter="editingId = null"
              />
              <button class="node-action" @click.stop="openNote(n)">{{ t('canvas.openNote') }}</button>
            </template>

            <!-- 链接节点 -->
            <template v-else-if="n.type === 'link'">
              <div class="node-content node-link" @dblclick.stop="editingId = n.id">
                <span class="node-icon">🔗</span>
                <span class="node-link-url">{{ n.url || '—' }}</span>
              </div>
              <input
                v-if="editingId === n.id"
                class="node-edit-input"
                :value="n.url"
                :placeholder="t('canvas.nodeUrl')"
                @mousedown.stop
                @input="updateNode(n.id, { url: ($event.target as HTMLInputElement).value } as Partial<CanvasNode>)"
                @blur="editingId = null"
                @keyup.enter="editingId = null"
              />
              <button class="node-action" @click.stop="openLink(n)">{{ t('canvas.open') }}</button>
            </template>

            <span class="node-port" :data-handle="'port'" :data-node-id="n.id" />
            <span class="node-resize" :data-handle="'resize'" :data-node-id="n.id" />
          </div>

          <!-- 边 -->
          <svg class="edge-layer">
            <defs>
              <marker
                id="canvas-arrow"
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="7"
                markerHeight="7"
                orient="auto-start-reverse"
              >
                <path d="M 0 0 L 10 5 L 0 10 z" fill="#8a8f98" />
              </marker>
            </defs>
            <g v-for="ep in edgePaths" :key="ep.id">
              <path
                class="edge-hit"
                :d="`M ${ep.from.x} ${ep.from.y} L ${ep.to.x} ${ep.to.y}`"
                @mousedown.stop="selectedEdgeId = ep.id; selectedId = null"
              />
              <path
                class="edge-line"
                :class="{ selected: selectedEdgeId === ep.id }"
                :d="`M ${ep.from.x} ${ep.from.y} L ${ep.to.x} ${ep.to.y}`"
                :style="ep.color ? { stroke: ep.color } : {}"
                marker-end="url(#canvas-arrow)"
              />
            </g>
            <path
              v-if="tempPath"
              class="edge-temp"
              :d="`M ${tempPath.from.x} ${tempPath.from.y} L ${tempPath.to.x} ${tempPath.to.y}`"
            />
          </svg>
        </div>

        <div v-if="data.nodes.length === 0" class="canvas-empty-hint">
          {{ t('canvas.emptyHint') }}
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.canvas-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}
.page-header h1 {
  font-size: var(--text-lg);
  margin: 0;
}
.list-hint {
  padding: var(--space-4);
  color: var(--text-muted);
}
.empty-state {
  padding: 64px var(--space-4);
  text-align: center;
  color: var(--text-muted);
}
.empty-title {
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: var(--space-2);
}
.empty-hint {
  margin-bottom: var(--space-4);
}
.canvas-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: var(--space-3);
  padding: var(--space-4);
  overflow-y: auto;
}
.canvas-card {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  padding: var(--space-3);
  cursor: pointer;
  transition: border-color var(--transition-fast), transform var(--transition-fast);
}
.canvas-card:hover {
  border-color: var(--border-accent);
  transform: translateY(-2px);
}
.canvas-card-name {
  font-weight: 600;
  margin-bottom: var(--space-3);
  word-break: break-all;
}
.canvas-card-actions {
  display: flex;
  gap: var(--space-2);
}

/* 编辑器 */
.canvas-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
  flex-wrap: wrap;
}
.canvas-name {
  font-weight: 600;
  color: var(--text-primary);
}
.toolbar-sep {
  width: 1px;
  height: 20px;
  background: var(--border);
  margin: 0 var(--space-1);
}
.toolbar-spacer {
  flex: 1;
}
.zoom-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
  min-width: 40px;
  text-align: center;
}
.save-status {
  font-size: var(--text-xs);
  padding: 2px 8px;
  border-radius: var(--radius-sm);
}
.save-status.saved {
  color: var(--accent);
}
.save-status.saving {
  color: var(--text-muted);
}
.save-status.unsaved {
  color: #f76808;
}
.canvas-error {
  padding: var(--space-2) var(--space-3);
  background: rgba(229, 72, 77, 0.12);
  color: #e5484d;
  font-size: var(--text-sm);
}

.canvas-surface {
  position: relative;
  flex: 1;
  overflow: hidden;
  background:
    radial-gradient(circle, var(--border) 1px, transparent 1px);
  background-size: 24px 24px;
  background-color: var(--bg-secondary, #f6f7f9);
  cursor: grab;
}
.canvas-surface:active {
  cursor: grabbing;
}
.canvas-world {
  width: 0;
  height: 0;
}
.canvas-empty-hint {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  color: var(--text-muted);
  pointer-events: none;
  font-size: var(--text-sm);
}

/* 节点 */
.node {
  position: absolute;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
  padding: var(--space-2);
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  user-select: none;
}
.node.selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px var(--accent-alpha, rgba(0, 122, 255, 0.25));
}
.node.connecting {
  border-color: #8e51ff;
}
.node-group {
  background: rgba(142, 81, 255, 0.08);
  border-style: dashed;
  z-index: 0;
}
.node:not(.node-group) {
  z-index: 1;
}
.group-label {
  position: absolute;
  top: 4px;
  left: 8px;
  font-size: var(--text-xs);
  font-weight: 600;
  color: var(--text-secondary);
}
.group-input,
.node-edit-input,
.node-textarea {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 4px 6px;
  font-size: var(--text-sm);
  font-family: inherit;
  resize: none;
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  box-sizing: border-box;
}
.node-textarea {
  flex: 1;
}
.node-badge {
  position: absolute;
  top: 4px;
  right: 6px;
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.node-text {
  flex: 1;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: var(--text-sm);
  color: var(--text-primary);
  padding-top: 14px;
}
/* Markdown 渲染的文本节点：让预览内容贴合卡片尺寸 */
.node-markdown {
  font-size: var(--text-sm);
  line-height: 1.5;
  color: var(--text-primary);
}
.node-markdown :deep(h1),
.node-markdown :deep(h2),
.node-markdown :deep(h3),
.node-markdown :deep(h4) {
  margin: 0.3em 0 0.2em;
  line-height: 1.25;
}
.node-markdown :deep(h1) { font-size: 1.15em; }
.node-markdown :deep(h2) { font-size: 1.08em; }
.node-markdown :deep(h3),
.node-markdown :deep(h4) { font-size: 1em; }
.node-markdown :deep(p),
.node-markdown :deep(ul),
.node-markdown :deep(ol),
.node-markdown :deep(blockquote),
.node-markdown :deep(pre) {
  margin: 0.3em 0;
}
.node-markdown :deep(pre) {
  background: var(--bg-sidebar, #f3f4f6);
  padding: 6px 8px;
  border-radius: 6px;
  overflow-x: auto;
}
.node-markdown :deep(code) {
  font-family: var(--font-mono, monospace);
  font-size: 0.9em;
}
.node-markdown :deep(img) {
  max-width: 100%;
}
.node-content {
  flex: 1;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding-top: 14px;
  overflow: hidden;
}
.node-icon {
  flex-shrink: 0;
}
.node-file-path,
.node-link-url {
  font-size: var(--text-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-action {
  margin-top: var(--space-1);
  align-self: flex-start;
  font-size: var(--text-xs);
  color: var(--accent);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 0;
}
.node-action:hover {
  text-decoration: underline;
}

/* 连接点 / 缩放手柄 */
.node-port {
  position: absolute;
  right: -6px;
  bottom: -6px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--accent);
  border: 2px solid var(--bg-card);
  cursor: crosshair;
  z-index: 3;
}
.node-resize {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 14px;
  height: 14px;
  cursor: nwse-resize;
  z-index: 2;
}

/* 边 */
.edge-layer {
  position: absolute;
  top: 0;
  left: 0;
  width: 1px;
  height: 1px;
  overflow: visible;
  pointer-events: none;
  z-index: 2;
}
.edge-line {
  stroke: #8a8f98;
  stroke-width: 2;
  fill: none;
}
.edge-line.selected {
  stroke: var(--accent);
  stroke-width: 3;
}
.edge-hit {
  stroke: transparent;
  stroke-width: 14;
  fill: none;
  pointer-events: stroke;
  cursor: pointer;
}
.edge-temp {
  stroke: #8e51ff;
  stroke-width: 2;
  stroke-dasharray: 5 4;
  fill: none;
}

/* 按钮 */
.primary-btn,
.tool-btn,
.ghost-btn,
.danger-btn,
.icon-btn {
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  cursor: pointer;
  padding: 6px 12px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-primary);
  transition: background var(--transition-fast), border-color var(--transition-fast);
}
.primary-btn {
  background: var(--accent);
  color: var(--text-inverse);
  border-color: var(--accent);
}
.primary-btn:hover {
  background: var(--accent-hover);
}
.tool-btn:hover,
.ghost-btn:hover {
  background: var(--bg-hover);
}
.danger-btn {
  color: #e5484d;
  border-color: rgba(229, 72, 77, 0.4);
}
.danger-btn:hover {
  background: rgba(229, 72, 77, 0.1);
}
.icon-btn {
  width: 30px;
  padding: 4px 0;
  text-align: center;
  font-size: var(--text-base);
}
</style>
