<script setup lang="ts">
// ============================================================================
// EditorToolbar —— MarkdownEditor 的格式工具栏（含颜色面板 / “更多”下拉 / 插件按钮）。
//
// 职责边界：
//   - 本组件只负责「呈现 + 收集用户意图」，所有实际编辑操作经 emit 交给
//     MarkdownEditor.vue（它持有 EditorView），命令实现见 editorCommands.ts。
//   - 弹层（更多下拉 / 颜色面板）的开合状态与“点击外部关闭”在本组件内自治，
//     父组件不感知。
//   - 隐藏的原生 color input 仍在父组件（label[for] 是文档级 id，跨组件生效），
//     本组件只发 apply-color。
// ============================================================================
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'
import { TOOLBAR_ITEMS, TEXT_TOOLS, CALLOUT_TYPES, TABLE_TOOLS, type ToolbarItem } from './toolbarButtons'
import type { CustomCommand } from '@/types'
import type { RegisteredPluginToolbarButton } from '@/plugins/types'

const props = defineProps<{
  /** 格式刷是否处于激活态（状态在父组件：自动刷格式依赖编辑器 updateListener） */
  brushActive: boolean
  /** floating 模式的定位样式（父组件根据光标坐标计算） */
  floatingStyle: { left: string; top: string } | null
}>()

const emit = defineEmits<{
  /** 内建格式命令（bold/h1/table…），实现分发在父组件 */
  command: [id: string]
  /** 应用字体颜色 / 背景色 */
  'apply-color': [kind: 'color' | 'bg', hex: string]
  /** 切换格式刷 */
  'toggle-brush': []
  /** 文本处理工具（更多下拉） */
  'text-tool': [id: string]
  /** 自定义命令（更多下拉） */
  custom: [cmd: CustomCommand]
  /** Callout 快捷插入（更多下拉），携带 callout 类型 */
  callout: [type: string]
  /** 插件注入的工具栏按钮 */
  'plugin-button': [btn: RegisteredPluginToolbarButton, event?: MouseEvent]
}>()

const { t } = useI18n()
const settingsStore = useSettingsStore()
const pluginStore = usePluginRuntimeStore()

const rootEl = ref<HTMLElement | null>(null)
const moreRef = ref<HTMLElement | null>(null)

// 颜色/背景色选择面板：浏览器 native color picker 在 WebView2 里
// 位置不可控（经常飞离按钮到屏幕中央），所以改用自定义 popover 紧贴工具栏下方弹出。
const colorPickerOpen = ref(false)
const colorPickerKind = ref<'color' | 'bg'>('color')
const customColorRef = ref<HTMLInputElement | null>(null)
// 预设颜色：常用 12 色，让用户能直接选；自定义面板可调出 native picker
const COLOR_PRESETS = [
  '#000000', '#666666', '#999999', '#cccccc', '#ffffff',
  '#ef4444', '#f59e0b', '#eab308', '#22c55e', '#06b6d4',
  '#3b82f6', '#8b5cf6', '#ec4899', '#f43f5e',
]

function openColorPicker(kind: 'color' | 'bg') {
  colorPickerKind.value = kind
  colorPickerOpen.value = true
}

/** 面板内选色：先关弹层再上抛（原来是父组件 applyColor 顺带关闭，状态内聚后在此收口） */
function pickColor(hex: string) {
  colorPickerOpen.value = false
  emit('apply-color', colorPickerKind.value, hex)
}

// ===== 工具栏按钮（按 order 排序 + 按 visibleButtons 过滤，撤销/重做始终显示）=====
const visibleItems = computed<ToolbarItem[]>(() => {
  // 插件提供了按钮时，内建按钮整体让位，否则会出现两套工具栏。
  //
  // 内建实现刻意保留而非删除：它是「预装插件被禁用或删掉」时的回退，
  // 保证任何情况下用户都不会面对一个空工具栏。
  // 工具栏的主体实现是 plugins/bundled/editing-toolbar.js（出厂预装）。
  if (pluginStore.toolbarButtons.length > 0) return []

  const order = settingsStore.settings.toolbar.order
  const vis = settingsStore.settings.toolbar.visibleButtons
  const map = new Map(TOOLBAR_ITEMS.map((i) => [i.id as string, i]))
  const seen = new Set<string>()
  const out: ToolbarItem[] = []
  for (const id of order) {
    const it = map.get(id)
    if (!it) continue
    seen.add(id)
    if (it.fixed || vis.includes(id as string)) out.push(it)
  }
  // 补齐 order 中遗漏的按钮（防止 order 损坏导致按钮丢失）
  for (const it of TOOLBAR_ITEMS) {
    const id = it.id as string
    if (!seen.has(id) && (it.fixed || vis.includes(id))) out.push(it)
  }
  return out
})

// ===== “更多”下拉 =====
const showMore = ref(false)

function onButtonClick(item: ToolbarItem, e?: MouseEvent) {
  if (item.type === 'color') {
    openColorPicker('color')
    return
  }
  if (item.type === 'bg') {
    openColorPicker('bg')
    return
  }
  if (item.type === 'brush') {
    emit('toggle-brush')
    return
  }
  if (item.type === 'more') {
    showMore.value = !showMore.value
    // 阻止冒泡到 document，否则 onDocClick 会立即把刚打开的下拉关掉
    e?.stopPropagation()
    return
  }
  emit('command', item.id ?? '')
}

function onDocClick(e: MouseEvent) {
  if (showMore.value && moreRef.value && !moreRef.value.contains(e.target as Node)) {
    showMore.value = false
  }
  // 点击工具栏外区域关闭颜色 popover
  if (colorPickerOpen.value) {
    const target = e.target as HTMLElement | null
    // 工具栏上的 color/bg 按钮本身不关（让它走 onButtonClick toggle）
    if (target?.closest('.tb-color-popup, [data-color-picker]')) return
    colorPickerOpen.value = false
  }
}

function onDocKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && showMore.value) {
    showMore.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onDocKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onDocKeydown)
})

defineExpose({
  /** 父组件 floating 模式定位需要工具栏的实际尺寸 */
  rootEl,
  /** 插件 UI 注册协议经宿主触发取色面板（需要保留用户手势调用链） */
  openColorPicker,
})
</script>

<template>
  <div
    ref="rootEl"
    class="editor-toolbar"
    :class="[settingsStore.settings.toolbar.mode, { floating: settingsStore.settings.toolbar.mode === 'floating' }]"
    :style="settingsStore.settings.toolbar.mode === 'floating' ? props.floatingStyle ?? undefined : undefined"
  >
    <template v-for="(btn, i) in visibleItems" :key="btn.id || 'b' + i">
      <!-- 颜色/背景色：label[for] 触发隐藏 input，避免 WebView2 拒绝 input.click()/showPicker() -->
      <label
        v-if="btn.type === 'color'"
        class="tb-btn tb-color"
        for="nv-color-picker"
        :title="t(btn.i18nKey || btn.id || '')"
      >
        {{ btn.label }}
      </label>
      <label
        v-else-if="btn.type === 'bg'"
        class="tb-btn tb-bg"
        for="nv-bg-picker"
        :title="t(btn.i18nKey || btn.id || '')"
      >
        {{ btn.label }}
      </label>
      <button
        v-else
        type="button"
        class="tb-btn"
        :class="[btn.cls, { 'tb-active': (btn.type === 'brush' && brushActive) || (btn.type === 'more' && showMore) }]"
        :title="t(btn.i18nKey || btn.id || '')"
        @click="onButtonClick(btn, $event)"
      >
        {{ btn.label }}
      </button>
    </template>

    <!-- 颜色 / 背景色选择面板（紧贴工具栏下方，不再用 native picker） -->
    <div
      v-if="colorPickerOpen"
      class="tb-color-popup"
      @click.stop
    >
      <div class="tb-color-grid">
        <button
          v-for="color in COLOR_PRESETS"
          :key="color"
          type="button"
          class="tb-color-swatch"
          :style="{ background: color }"
          :title="color"
          @click="pickColor(color)"
        />
      </div>
      <label class="tb-color-custom">
        自定义…
        <input
          ref="customColorRef"
          type="color"
          @change="pickColor(customColorRef?.value || '#000000')"
        >
      </label>
    </div>

    <!-- 插件通过 UI 注册协议注入的工具栏按钮 -->
    <template v-if="pluginStore.toolbarButtons.length">
      <span class="tb-sep" />
      <button
        v-for="pbtn in pluginStore.toolbarButtons"
        :key="'plugin:' + pbtn.pluginId + ':' + (pbtn.id || '')"
        type="button"
        class="tb-btn tb-plugin"
        :title="pbtn.tooltip || pbtn.title"
        @click.stop="emit('plugin-button', pbtn, $event)"
      >
        {{ pbtn.icon || pbtn.title }}
      </button>
    </template>

    <!-- “更多”下拉（文本处理工具 + Callout + 自定义命令） -->
    <div
      v-if="showMore"
      ref="moreRef"
      class="tb-more-panel"
      @click.stop
    >
      <div class="tb-more-section">
        <div class="tb-more-title">
          {{ t('toolbar.tools.title') }}
        </div>
        <button
          v-for="tool in TEXT_TOOLS"
          :key="tool.id"
          type="button"
          class="tb-more-item"
          @click="emit('text-tool', tool.id)"
        >
          {{ t(tool.i18nKey) }}
        </button>
      </div>
      <div class="tb-more-divider" />
      <div class="tb-more-section">
        <div class="tb-more-title">
          {{ t('toolbar.callout.title') }}
        </div>
        <button
          v-for="c in CALLOUT_TYPES"
          :key="c.type"
          type="button"
          class="tb-more-item tb-more-callout"
          @click="emit('callout', c.type)"
        >
          <span class="tb-more-callout-icon">{{ c.icon }}</span>
          {{ t(c.i18nKey) }}
        </button>
      </div>
      <div class="tb-more-divider" />
      <div class="tb-more-section">
        <div class="tb-more-title">
          {{ t('toolbar.table.title') }}
        </div>
        <button
          v-for="tt in TABLE_TOOLS"
          :key="tt.id"
          type="button"
          class="tb-more-item"
          @click="emit('command', tt.id)"
        >
          {{ t(tt.i18nKey) }}
        </button>
      </div>
      <template v-if="settingsStore.settings.toolbar.customCommands.length">
        <div class="tb-more-divider" />
        <div class="tb-more-section">
          <div class="tb-more-title">
            {{ t('toolbar.customCommands') }}
          </div>
          <button
            v-for="cmd in settingsStore.settings.toolbar.customCommands"
            :key="cmd.id"
            type="button"
            class="tb-more-item"
            :title="cmd.type === 'wrap' ? (cmd.prefix || '') + '…' + (cmd.suffix || '') : (cmd.pattern || '')"
            @click="emit('custom', cmd)"
          >
            {{ cmd.name }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.editor-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-sidebar);
  flex: 0 0 auto;
  position: relative;
  order: 0;
}

/* top：顶部固定（默认顺序，在编辑区之前）；fixed：底部固定（order 推后） */
.editor-toolbar.fixed {
  order: 2;
  border-bottom: none;
  border-top: 1px solid var(--border);
}

.editor-toolbar.floating {
  position: fixed;
  z-index: 1000;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.28);
  border: 1px solid var(--border);
  border-radius: 8px;
  max-width: 92vw;
  order: 0;
}

.tb-btn {
  min-width: 28px;
  height: 28px;
  padding: 0 6px;
  border: none;
  background: transparent;
  color: var(--text);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: background 0.12s ease;
}

.tb-btn:hover {
  background: var(--bg-hover);
}

.tb-btn:active {
  background: var(--bg-active);
}

.tb-btn.tb-active {
  background: var(--bg-active);
  color: var(--accent);
}

.tb-sep {
  width: 1px;
  align-self: stretch;
  margin: 4px 4px;
  background: var(--border);
}

.tb-plugin {
  border: 1px dashed var(--border);
  font-size: 12px;
}

.tb-bold {
  font-weight: 700;
}

.tb-italic {
  font-style: italic;
}

.tb-underline {
  text-decoration: underline;
}

.tb-strike {
  text-decoration: line-through;
}

.tb-mono {
  font-family: var(--font-mono, monospace);
}

.tb-color {
  color: #e5484d;
  font-weight: 700;
}

.tb-bg {
  background: #f5d90a;
  color: #222;
  border-radius: 4px;
  padding: 0 2px;
}

/* 自定义颜色 popover（紧贴工具栏下方） */
.tb-color-popup {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  z-index: 50;
  background: var(--bg-window, #1e1f22);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  border-radius: var(--radius-md, 6px);
  padding: 8px;
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.45);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.tb-color-grid {
  display: grid;
  grid-template-columns: repeat(7, 18px);
  gap: 4px;
}
.tb-color-swatch {
  width: 18px;
  height: 18px;
  border: 1px solid rgba(255, 255, 255, 0.2);
  border-radius: 3px;
  padding: 0;
  cursor: pointer;
}
.tb-color-swatch:hover {
  border-color: #fff;
  transform: scale(1.1);
}
.tb-color-custom {
  font-size: var(--text-xs, 11px);
  color: var(--text-secondary, #aaa);
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.tb-color-custom input {
  width: 22px;
  height: 22px;
  padding: 0;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  border-radius: 3px;
  background: transparent;
}

/* "更多"下拉面板 */
.tb-more-panel {
  position: absolute;
  top: calc(100% + 4px);
  left: 8px;
  z-index: 1100;
  min-width: 180px;
  max-height: 60vh;
  overflow-y: auto;
  padding: 6px;
  border-radius: 8px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.editor-toolbar.floating .tb-more-panel {
  position: fixed;
  top: auto;
  left: 8px;
  bottom: calc(100% + 4px);
}

.tb-more-title {
  font-size: 11px;
  color: var(--text-muted);
  padding: 2px 6px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.tb-more-item {
  display: block;
  width: 100%;
  text-align: left;
  padding: 5px 8px;
  border: none;
  background: transparent;
  color: var(--text);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.tb-more-item:hover {
  background: var(--bg-hover);
}

.tb-more-callout {
  display: flex;
  align-items: center;
  gap: 8px;
}
.tb-more-callout-icon {
  font-size: 1.05em;
  line-height: 1;
}

.tb-more-divider {
  height: 1px;
  background: var(--border);
  margin: 4px 2px;
}
</style>
