<!--
  QuickAccessCard.vue：今日页的「快捷入口」卡。
  侧边栏原来常驻的 固定 / 最近文档 / 迷你月历 三块内容搬到这里，
  用 tab 切换，避免在侧栏纵向堆叠。固定项的交互（点击、拖拽排序、
  Alt+↑↓ 排序、右键取消、上限 8 条）与旧侧栏行为保持一致。
-->
<script setup lang="ts">
import { computed, nextTick, ref, watch, type Component } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BookOpen, FileText, Pin, PinOff, Rocket, GripVertical, Zap } from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { useToast } from '@/composables/useToast'
import { formatRelativeTime } from '@/utils/relativeTime'
import SidebarCalendar from '@/components/layout/SidebarCalendar.vue'

interface PinnedItem { path: string; title: string }

const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const workspaceStore = useWorkspaceStore()

const active = ref<'pins' | 'recent' | 'calendar'>('pins')
const pins = computed(() => workspaceStore.pinnedItems.slice(0, 8))
const recentFiles = computed(() => workspaceStore.recentFiles.slice(0, 6))

const tabs = computed(() => [
  { id: 'pins' as const, label: t('today.quick.pins'), count: pins.value.length },
  { id: 'recent' as const, label: t('today.quick.recent'), count: recentFiles.value.length },
  { id: 'calendar' as const, label: t('today.quick.calendar'), count: 0 },
])

const draggedPin = ref<string | null>(null)
const dragOverIndex = ref<number | null>(null)
const announcement = ref('')
const pinMenu = ref<{ item: PinnedItem; x: number; y: number } | null>(null)
const pinMenuRef = ref<HTMLElement | null>(null)
let pinMenuTrigger: HTMLElement | null = null

function pinIcon(path: string): Component {
  if (/\/project\.md$/i.test(path)) return Rocket
  if (/\/book\.md$/i.test(path)) return BookOpen
  return FileText
}

function openFile(path: string) {
  workspaceStore.openFile(path)
  void router.push({ path: '/editor', query: { file: path } })
}

function openPin(item: PinnedItem) {
  const path = item.path.replace(/\\/g, '/')
  const folder = path.slice(0, path.lastIndexOf('/'))
  if (/\/project\.md$/i.test(path)) void router.push({ path: '/projects', query: { project: folder } })
  else if (/\/book\.md$/i.test(path)) void router.push({ path: '/learning', query: { book: folder } })
  else openFile(path)
}

function pinCurrentFile() {
  if (!workspaceStore.activeFile) return
  if (!workspaceStore.togglePin(workspaceStore.activeFile)) toast.warning(t('sidebar.pinLimit'))
}

function movePin(item: PinnedItem, index: number) {
  const target = Math.max(0, Math.min(index, pins.value.length - 1))
  workspaceStore.movePin(item.path, target)
  announcement.value = t('sidebar.pinMoved', { title: item.title, position: target + 1 })
}

function startPinDrag(event: DragEvent, item: PinnedItem) {
  draggedPin.value = item.path
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', item.path)
  }
  pinMenu.value = null
}

function finishPinDrag() {
  draggedPin.value = null
  dragOverIndex.value = null
}

function dropPin(index: number) {
  const item = pins.value.find(pin => pin.path === draggedPin.value)
  if (item) movePin(item, index)
  finishPinDrag()
}

function showPinMenu(event: MouseEvent, item: PinnedItem) {
  pinMenuTrigger = event.currentTarget as HTMLElement
  pinMenu.value = { item, x: Math.min(event.clientX, window.innerWidth - 180), y: Math.min(event.clientY, window.innerHeight - 60) }
  void nextTick(() => pinMenuRef.value?.querySelector<HTMLButtonElement>('button')?.focus())
}

function closePinMenu() {
  if (!pinMenu.value) return
  pinMenu.value = null
  pinMenuTrigger?.focus()
}

function unpinItem() {
  const item = pinMenu.value?.item
  if (item && workspaceStore.isPinned(item.path)) {
    workspaceStore.togglePin(item.path)
    announcement.value = t('sidebar.pinRemoved', { title: item.title })
  }
  closePinMenu()
}

function handlePinKey(event: KeyboardEvent, item: PinnedItem, index: number) {
  if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
    event.preventDefault()
    movePin(item, index + (event.key === 'ArrowUp' ? -1 : 1))
  } else if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
    event.preventDefault()
    pinMenuTrigger = event.currentTarget as HTMLElement
    const rect = pinMenuTrigger.getBoundingClientRect()
    pinMenu.value = { item, x: rect.left + 12, y: Math.min(rect.bottom, window.innerHeight - 60) }
    void nextTick(() => pinMenuRef.value?.querySelector<HTMLButtonElement>('button')?.focus())
  }
}

watch(() => workspaceStore.currentWorkspace?.path, () => {
  pinMenu.value = null
  finishPinDrag()
})
</script>

<template>
  <section
    class="quick-card"
    data-testid="today-quick"
  >
    <header class="quick-head">
      <h3><Zap :size="15" />{{ t('today.quick.title') }}</h3>
      <button
        v-if="workspaceStore.activeFile"
        type="button"
        class="quick-pin-btn"
        data-testid="quick-pin-current"
        :title="workspaceStore.isPinned(workspaceStore.activeFile) ? t('sidebar.unpinCurrent') : t('sidebar.pinCurrent')"
        :aria-label="workspaceStore.isPinned(workspaceStore.activeFile) ? t('sidebar.unpinCurrent') : t('sidebar.pinCurrent')"
        :aria-pressed="workspaceStore.isPinned(workspaceStore.activeFile)"
        @click="pinCurrentFile"
      >
        <Pin :size="13" />
      </button>
    </header>

    <div
      class="quick-tabs"
      role="tablist"
    >
      <button
        v-for="tab in tabs"
        :key="tab.id"
        type="button"
        role="tab"
        class="quick-tab"
        :class="{ active: active === tab.id }"
        :aria-selected="active === tab.id"
        :data-testid="`quick-tab-${tab.id}`"
        @click="active = tab.id"
      >
        {{ tab.label }}
        <span
          v-if="tab.count"
          class="quick-tab-count"
        >{{ tab.count }}</span>
      </button>
    </div>

    <div
      v-if="active === 'pins'"
      class="quick-list"
    >
      <p
        v-if="!pins.length"
        class="quick-empty"
        data-testid="quick-pins-empty"
      >
        {{ t('today.quick.emptyPins') }}
      </p>
      <button
        v-for="(item, index) in pins"
        :key="item.path"
        type="button"
        class="quick-row"
        data-testid="quick-pin"
        :class="{ 'drag-target': dragOverIndex === index && draggedPin !== item.path, 'is-dragging': draggedPin === item.path }"
        :title="t('sidebar.pinItemTip', { title: item.title })"
        :aria-label="item.title"
        draggable="true"
        @click="openPin(item)"
        @keydown="handlePinKey($event, item, index)"
        @contextmenu.prevent="showPinMenu($event, item)"
        @dragstart="startPinDrag($event, item)"
        @dragover.prevent="dragOverIndex = index"
        @dragleave="dragOverIndex = null"
        @drop.prevent="dropPin(index)"
        @dragend="finishPinDrag"
      >
        <component
          :is="pinIcon(item.path)"
          :size="15"
        />
        <span class="quick-row-label">{{ item.title }}</span>
        <GripVertical
          :size="12"
          class="quick-grip"
          aria-hidden="true"
        />
      </button>
    </div>

    <div
      v-else-if="active === 'recent'"
      class="quick-list"
    >
      <p
        v-if="!recentFiles.length"
        class="quick-empty"
        data-testid="quick-recent-empty"
      >
        {{ t('sidebar.recentEmpty') }}
      </p>
      <button
        v-for="file in recentFiles"
        :key="file.path"
        type="button"
        class="quick-row"
        data-testid="quick-recent-file"
        :title="file.path"
        :aria-label="file.title"
        @click="openFile(file.path)"
      >
        <FileText :size="14" />
        <span class="quick-row-label">{{ file.title }}</span>
        <span class="quick-row-time">{{ formatRelativeTime(file.openedAt) }}</span>
      </button>
    </div>

    <div
      v-else
      class="quick-calendar"
    >
      <SidebarCalendar />
    </div>

    <span
      class="quick-sr"
      aria-live="polite"
    >{{ announcement }}</span>

    <Teleport to="body">
      <div
        v-if="pinMenu"
        ref="pinMenuRef"
        class="quick-pin-menu"
        role="menu"
        :aria-label="t('sidebar.pinMenuAria')"
        :style="{ left: `${pinMenu.x}px`, top: `${pinMenu.y}px` }"
      >
        <button
          type="button"
          role="menuitem"
          data-testid="quick-unpin"
          @click="unpinItem"
        >
          <PinOff :size="14" />
          {{ t('sidebar.unpinAction') }}
        </button>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.quick-card {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface-panel, var(--bg-card));
}
.quick-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 10px;
}
.quick-head h3 {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.quick-pin-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.quick-pin-btn:hover {
  color: var(--accent);
  border-color: var(--accent);
}
.quick-tabs {
  display: flex;
  gap: 2px;
  padding: 2px;
  margin-bottom: 8px;
  border-radius: var(--radius-sm);
  background: var(--bg-hover);
}
.quick-tab {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  min-height: 24px;
  padding: 3px 8px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
}
.quick-tab.active {
  background: var(--surface-panel, var(--bg-card));
  color: var(--text-primary);
  font-weight: 500;
}
.quick-tab-count {
  color: var(--text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.quick-list {
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.quick-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 28px;
  padding: 4px 6px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-primary);
  font-size: 12px;
  text-align: left;
  cursor: pointer;
}
.quick-row:hover {
  background: var(--bg-hover);
}
.quick-row.is-dragging {
  opacity: 0.45;
}
.quick-row.drag-target {
  box-shadow: inset 0 1px 0 var(--accent);
}
.quick-row-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.quick-row-time {
  color: var(--text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.quick-grip {
  color: var(--text-muted);
  opacity: 0;
}
.quick-row:hover .quick-grip {
  opacity: 1;
}
.quick-empty {
  margin: 0;
  padding: 8px 6px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}
.quick-calendar {
  padding-top: 2px;
}
.quick-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
.quick-pin-menu {
  position: fixed;
  z-index: 60;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-panel, var(--bg-card));
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.16);
}
.quick-pin-menu button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 10px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-primary);
  font-size: 12px;
  cursor: pointer;
}
.quick-pin-menu button:hover {
  background: var(--bg-hover);
}
</style>
