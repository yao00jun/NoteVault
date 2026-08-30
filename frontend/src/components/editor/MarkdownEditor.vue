<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { EditorState, EditorSelection, Compartment } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab, undo, redo, indentMore, indentLess } from '@codemirror/commands'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching } from '@codemirror/language'
import { oneDark } from '@codemirror/theme-one-dark'
import { useSettingsStore } from '@/stores/settings'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'
import { TOOLBAR_ITEMS, TEXT_TOOLS, type ToolbarItem } from './toolbarButtons'
import {
  buildPluginDecorationExtension,
  buildPluginKeymapExtension,
  setActiveEditor,
  applyInlineFormat,
} from '@/plugins/editorBridge'
import type { CustomCommand } from '@/types'
import type { RegisteredPluginToolbarButton } from '@/plugins/types'

const props = defineProps<{
  modelValue: string
  readonly?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'save': []
  'paste-image': [payload: { file: File; insertText: (text: string) => void }]
}>()

const { t } = useI18n()
const editorRef = ref<HTMLElement | null>(null)
const colorInputRef = ref<HTMLInputElement | null>(null)
const bgInputRef = ref<HTMLInputElement | null>(null)
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
const floatToolbarRef = ref<HTMLElement | null>(null)
const moreRef = ref<HTMLElement | null>(null)
let editorView: EditorView | null = null

// 插件声明的编辑器扩展放进独立的 Compartment：
// 插件随时可能注册/卸载，装饰集合变化时热替换这一格即可，
// 不必重建整个编辑器状态（那样会丢光标、丢撤销历史）。
const pluginExtensionCompartment = new Compartment()

function reconfigurePluginExtensions(): void {
  if (!editorView) return
  editorView.dispatch({
    effects: pluginExtensionCompartment.reconfigure([
      buildPluginDecorationExtension(
        pluginStore.editorDecorations,
        pluginStore.editorWidgets,
      ),
      buildPluginKeymapExtension(pluginStore.editorKeymaps, (pluginId, commandId) => {
        void pluginStore.runCommand(pluginId, commandId)
      }),
    ]),
  })
}

// 插件扩展集合变化（注册、卸载、热重载）时同步到编辑器
watch(
  () => [
    pluginStore.editorDecorations,
    pluginStore.editorWidgets,
    pluginStore.editorKeymaps,
  ],
  () => reconfigurePluginExtensions(),
)

const settingsStore = useSettingsStore()
const pluginStore = usePluginRuntimeStore()

const saveKeymap = keymap.of([
  {
    key: 'Mod-s',
    run: () => {
      emit('save')
      return true
    },
  },
])

// 行内格式快捷键（组件级）：编辑器聚焦时由 CM6 直接处理。
// 键位：Ctrl+B 加粗 / Ctrl+I 斜体 / Ctrl+K 链接 / Ctrl+\ 行内代码。
// 全局兜底见 App.vue——编辑器未聚焦时也能作用于当前活动编辑器。
const formatKeymap = keymap.of([
  {
    key: 'Mod-b',
    run: () => {
      applyInlineFormat('bold')
      return true
    },
  },
  {
    key: 'Mod-i',
    run: () => {
      applyInlineFormat('italic')
      return true
    },
  },
  {
    key: 'Mod-k',
    run: () => {
      applyInlineFormat('link')
      return true
    },
  },
  {
    key: 'Mod-\\',
    run: () => {
      applyInlineFormat('code')
      return true
    },
  },
])

const editorTheme = computed(() => {
  if (settingsStore.settings.theme === 'islands-dark' || settingsStore.settings.theme === 'winui') {
    return oneDark
  }
  return []
})

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

// ===== 字体颜色 / 背景色 / 格式刷 =====
const lastFormat = ref<{ kind: 'color' | 'bg'; value: string } | null>(null)
const brushTip = ref('')
const actionTip = ref('')
const brushActive = ref(false)
let applyingBrush = false

/** 唤起系统取色器：优先用 showPicker()（需要用户手势），回退到 .click() */
function openColorPicker(kind: 'color' | 'bg') {
  // 不再调 native picker：浏览器/WV2 行为不可控，
  // 改为打开紧贴工具栏的自定义 popover
  colorPickerKind.value = kind
  colorPickerOpen.value = true
}

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
    toggleBrush()
    return
  }
  if (item.type === 'more') {
    showMore.value = !showMore.value
    // 阻止冒泡到 document，否则 onDocClick 会立即把刚打开的下拉关掉
    e?.stopPropagation()
    return
  }
  applyFormat(item.id ?? '')
}

/** 插件通过 UI 注册协议注入的工具栏按钮：点击时在真实编辑器上执行其声明变换 */
function onPluginButtonClick(btn: RegisteredPluginToolbarButton, event?: MouseEvent) {
  if (props.readonly) return
  // 颜色/背景色按钮会让 openColorPicker 打开 popover，click 必须 stopPropagation，
  // 否则 document.onDocClick 会立即关掉它。
  if (btn.command === 'editor:pickColor' || btn.command === 'editor:pickBackground') {
    event?.stopPropagation()
  }
  pluginStore.runToolbarButton(btn.pluginId, btn.id ?? '')
  // 视觉反馈：插件按钮不像内置命令能立刻看到明显的排版变化，给个轻提示
  const label = btn.title || btn.id || '插件'
  showActionTip(t('toolbar.applied', { name: label }))
}

function toggleBrush() {
  if (brushActive.value) {
    brushActive.value = false
    brushTip.value = t('toolbar.brushOff')
    autoHideTip()
    return
  }
  if (!lastFormat.value) {
    brushTip.value = t('toolbar.brushHint')
    autoHideTip()
    return
  }
  brushActive.value = true
  brushTip.value = t('toolbar.brushOn')
  autoHideTip()
}

let tipTimer: number | undefined
function autoHideTip() {
  if (tipTimer) window.clearTimeout(tipTimer)
  tipTimer = window.setTimeout(() => {
    brushTip.value = ''
  }, 2600)
}

function showActionTip(message: string) {
  actionTip.value = message
  if (tipTimer) window.clearTimeout(tipTimer)
  tipTimer = window.setTimeout(() => {
    actionTip.value = ''
  }, 1800)
}

function onColorInput(kind: 'color' | 'bg', e: Event) {
  const hex = (e.target as HTMLInputElement).value
  applyColor(kind, hex)
}

/** 用 <span style> 包裹选区为字体颜色 / 背景色；并记录为格式刷模板 */
function applyColor(kind: 'color' | 'bg', hex: string) {
  const view = editorView
  if (!view || props.readonly) return
  const before = kind === 'color' ? `<span style="color:${hex}">` : `<span style="background:${hex}">`
  const after = '</span>'
  wrap(before, after)
  lastFormat.value = { kind, value: hex }
  view.focus()
  // 自定义 popover 选色后顺手关掉
  colorPickerOpen.value = false
}

/** 格式刷自动模式：把上一次格式刷到当前选区（由 selectionSet 监听触发） */
function applyBrushAuto() {
  if (!lastFormat.value) return
  applyingBrush = true
  applyColor(lastFormat.value.kind, lastFormat.value.value)
  applyingBrush = false
}

// ===== “更多”下拉（文本处理工具 + 自定义命令）=====
const showMore = ref(false)

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

function onEditorKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (brushActive.value) {
      brushActive.value = false
      brushTip.value = t('toolbar.brushOff')
      autoHideTip()
    }
    if (showMore.value) showMore.value = false
  }
}

function onContextMenu(e: MouseEvent) {
  if (brushActive.value) {
    e.preventDefault()
    brushActive.value = false
    brushTip.value = t('toolbar.brushOff')
    autoHideTip()
  }
}

// ===== 跟随光标的浮层工具栏 =====
const showFloating = ref(false)
const floatingStyle = ref<{ left: string; top: string }>({ left: '0px', top: '0px' })
const floatingMid = ref(0)
const floatingTop = ref(0)

function updateFloating(view: EditorView) {
  const sel = view.state.selection.main
  if (sel.empty) {
    showFloating.value = false
    return
  }
  const coords = view.coordsAtPos(sel.head)
  if (!coords) {
    showFloating.value = false
    return
  }
  floatingMid.value = (coords.left + coords.right) / 2
  floatingTop.value = coords.top
  showFloating.value = true
  nextTick(positionFloating)
}

function positionFloating() {
  const el = floatToolbarRef.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const half = rect.width / 2
  let left = floatingMid.value
  left = Math.max(half + 8, Math.min(left, window.innerWidth - half - 8))
  let top = floatingTop.value - rect.height - 8
  if (top < 8) top = floatingTop.value + 24
  floatingStyle.value = { left: left + 'px', top: top + 'px' }
}

// 工具栏是否显示：floating 模式依赖 showFloating；其它模式常显
const toolbarVisible = computed(() =>
  !props.readonly && (settingsStore.settings.toolbar.mode === 'floating' ? showFloating.value : true),
)

function createEditor() {
  if (!editorRef.value) return

  const state = EditorState.create({
    doc: props.modelValue || '',
    extensions: [
      lineNumbers(),
      highlightActiveLine(),
      highlightActiveLineGutter(),
      history(),
      bracketMatching(),
      markdown({ base: markdownLanguage }),
      syntaxHighlighting(defaultHighlightStyle),
      keymap.of([
        ...defaultKeymap,
        ...historyKeymap,
        indentWithTab,
      ]),
      saveKeymap,
      formatKeymap,
      // 插件声明的编辑器扩展，初始为空，创建后再 reconfigure 填充
      pluginExtensionCompartment.of([]),
      editorTheme.value,
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          emit('update:modelValue', update.state.doc.toString())
        }
        if (!props.readonly && (update.selectionSet || update.docChanged)) {
          if (
            brushActive.value &&
            !applyingBrush &&
            !update.state.selection.main.empty &&
            lastFormat.value
          ) {
            applyBrushAuto()
          }
          if (settingsStore.settings.toolbar.mode === 'floating') {
            if (update.selectionSet || update.geometryChanged) updateFloating(update.view)
          } else if (showFloating.value) {
            showFloating.value = false
          }
        }
      }),
      EditorView.theme({
        '&': {
          height: '100%',
          fontSize: settingsStore.settings.fontSize + 'px',
        },
        '.cm-scroller': {
          fontFamily: 'var(--font-mono, "JetBrains Mono", "Fira Code", Consolas, monospace)',
          lineHeight: String(settingsStore.settings.editor.lineHeight),
        },
        '.cm-gutters': {
          backgroundColor: 'var(--bg-sidebar)',
          borderRight: '1px solid var(--border)',
          color: 'var(--text-muted)',
        },
        '.cm-activeLine': {
          backgroundColor: 'var(--bg-hover)',
        },
        '.cm-activeLineGutter': {
          backgroundColor: 'var(--bg-hover)',
        },
      }),
    ],
  })

  editorView = new EditorView({
    state,
    parent: editorRef.value,
  })
  setActiveEditor(editorView)
  // 编辑器建好后再灌入当前已注册的插件扩展
  reconfigurePluginExtensions()
}

function destroyEditor() {
  if (editorView) {
    setActiveEditor(null)
    editorView.destroy()
    editorView = null
  }
}

function handlePaste(event: ClipboardEvent) {
  const items = event.clipboardData?.items
  if (!items) return

  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (item.type.startsWith('image/')) {
      event.preventDefault()
      const file = item.getAsFile()
      if (file) {
        emit('paste-image', {
          file,
          insertText: (text: string) => {
            if (editorView) {
              const cursor = editorView.state.selection.main.head
              editorView.dispatch({
                changes: { from: cursor, insert: text },
                selection: { anchor: cursor + text.length },
              })
            }
          },
        })
      }
      break
    }
  }
}

watch(() => props.modelValue, (newVal) => {
  if (editorView && editorView.state.doc.toString() !== newVal) {
    editorView.dispatch({
      changes: {
        from: 0,
        to: editorView.state.doc.length,
        insert: newVal || '',
      },
    })
  }
})

watch(() => settingsStore.settings.theme, () => {
  destroyEditor()
  createEditor()
})

watch(() => settingsStore.settings.fontSize, () => {
  destroyEditor()
  createEditor()
})

watch(() => settingsStore.settings.editor.lineHeight, () => {
  destroyEditor()
  createEditor()
})

onMounted(() => {
  createEditor()
  editorRef.value?.addEventListener('paste', handlePaste as EventListener)
  editorRef.value?.addEventListener('contextmenu', onContextMenu as EventListener)
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onEditorKeydown)
  // 取色器与格式刷依赖组件里的隐藏 input 和响应式状态，
  // 挪不走，所以在这里注册给宿主：插件通过 command 名字即可触发。
  // 调用链是点击事件的同步延续，showPicker() 需要的用户手势得以保留。
  pluginStore.registerEditorUiHandlers({
    pickBackground: () => openColorPicker('bg'),
    pickColor: () => openColorPicker('color'),
    toggleBrush: () => toggleBrush(),
  })
})

onBeforeUnmount(() => {
  editorRef.value?.removeEventListener('paste', handlePaste as EventListener)
  editorRef.value?.removeEventListener('contextmenu', onContextMenu as EventListener)
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onEditorKeydown)
  if (tipTimer) window.clearTimeout(tipTimer)
  destroyEditor()
})

// ===== 编辑区格式工具栏逻辑 =====
/** 用 before/after 包裹选区；无选区时插入占位并把光标置于中间 */
function wrap(before: string, after: string = before) {
  const view = editorView
  if (!view) return
  const { state } = view
  const spec = state.changeByRange((range) => {
    const selected = state.sliceDoc(range.from, range.to)
    if (selected) {
      return {
        changes: { from: range.from, to: range.to, insert: before + selected + after },
        range: EditorSelection.range(range.from + before.length, range.from + before.length + selected.length),
      }
    }
    const insert = before + after
    return {
      changes: { from: range.from, insert },
      range: EditorSelection.cursor(range.from + before.length),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
}

/** 给选区覆盖的每一行设置/切换标题级别 */
function setHeading(level: number) {
  const view = editorView
  if (!view) return
  const { state } = view
  const changes: { from: number; to: number; insert: string }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let l = startLine; l <= endLine; l++) {
      if (seen.has(l)) continue
      seen.add(l)
      const line = state.doc.line(l)
      const text = line.text
      const m = text.match(/^(#{1,6}\s+)/)
      let insert: string
      if (m && m[0].length - 1 === level) {
        insert = text.slice(m[0].length)
      } else {
        insert = '#'.repeat(level) + ' ' + text.replace(/^#{1,6}\s+/, '')
      }
      changes.push({ from: line.from, to: line.to, insert })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
}

/** 给选区覆盖的每一行加/去行前缀（引用、列表） */
function prefixLines(prefix: string) {
  const view = editorView
  if (!view) return
  const { state } = view
  const changes: { from: number; to: number; insert: string }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let l = startLine; l <= endLine; l++) {
      if (seen.has(l)) continue
      seen.add(l)
      const line = state.doc.line(l)
      const text = line.text
      const insert = text.startsWith(prefix) ? text.slice(prefix.length) : prefix + text
      changes.push({ from: line.from, to: line.to, insert })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
}

/** 给选区覆盖的每一行施加/取消对齐（HTML 包裹，复刻 Obsidian 08-11） */
function alignLines(align: 'left' | 'center' | 'right' | 'justify') {
  const view = editorView
  if (!view) return
  const { state } = view
  const wrapText = (s: string) =>
    align === 'center' ? `<center>${s}</center>` : `<p align="${align}">${s}</p>`
  const changes: { from: number; to: number; insert: string }[] = []
  const seen = new Set<number>()
  for (const range of state.selection.ranges) {
    const startLine = state.doc.lineAt(range.from).number
    const endLine = state.doc.lineAt(range.to).number
    for (let l = startLine; l <= endLine; l++) {
      if (seen.has(l)) continue
      seen.add(l)
      const line = state.doc.line(l)
      const text = line.text
      const inner = unwrapAlign(text)
      let insert: string
      if (inner !== null && isAlignTag(text, align)) {
        insert = inner // 已是该对齐 → 取消
      } else if (inner !== null) {
        insert = wrapText(inner) // 是其它对齐 → 换成当前
      } else {
        insert = wrapText(text)
      }
      changes.push({ from: line.from, to: line.to, insert })
    }
  }
  view.dispatch(state.update({ changes, selection: state.selection, userEvent: 'input' }))
}

function unwrapAlign(text: string): string | null {
  let m = text.match(/^<p align="(?:left|center|right|justify)">(.*)<\/p>$/s)
  if (m) return m[1]
  m = text.match(/^<center>(.*)<\/center>$/s)
  if (m) return m[1]
  return null
}

function isAlignTag(text: string, align: string): boolean {
  if (align === 'center') return /^<center>[\s\S]*<\/center>$/.test(text)
  return new RegExp(`^<p align="${align}">[\\s\\S]*</p>$`).test(text)
}

/** 用 before/after 包裹选区为块（代码块等），无选区用占位 */
function insertBlock(before: string, after: string) {
  const view = editorView
  if (!view) return
  const { state } = view
  const spec = state.changeByRange((range) => {
    const sel = state.sliceDoc(range.from, range.to)
    const inner = sel || '代码'
    const text = before + inner + after
    return {
      changes: { from: range.from, to: range.to, insert: text },
      range: EditorSelection.range(range.from + before.length, range.from + before.length + inner.length),
    }
  })
  view.dispatch({ ...spec, userEvent: 'input' })
}

/** 在光标处插入纯文本（分割线、表格等） */
function insertText(text: string) {
  const view = editorView
  if (!view) return
  const { state } = view
  const spec = state.changeByRange((range) => ({
    changes: { from: range.from, insert: text },
    range: EditorSelection.cursor(range.from + text.length),
  }))
  view.dispatch({ ...spec, userEvent: 'input' })
}

function applyFormat(id: string) {
  const view = editorView
  if (!view || props.readonly) return
  switch (id) {
    case 'undo': undo(view); break
    case 'redo': redo(view); break
    case 'bold': wrap('**'); break
    case 'italic': wrap('*'); break
    case 'underline': wrap('<u>', '</u>'); break
    case 'strike': wrap('~~'); break
    case 'code': wrap('`'); break
    case 'h1': setHeading(1); break
    case 'h2': setHeading(2); break
    case 'h3': setHeading(3); break
    case 'h4': setHeading(4); break
    case 'h5': setHeading(5); break
    case 'h6': setHeading(6); break
    case 'quote': prefixLines('> '); break
    case 'ul': prefixLines('- '); break
    case 'ol': prefixLines('1. '); break
    case 'indent': if (editorView) indentMore(editorView); break
    case 'undent': if (editorView) indentLess(editorView); break
    case 'align-left': alignLines('left'); break
    case 'align-center': alignLines('center'); break
    case 'align-right': alignLines('right'); break
    case 'align-justify': alignLines('justify'); break
    case 'link': wrap('[', '](url)'); break
    case 'image': wrap('![', '](url)'); break
    case 'codeblock': insertBlock('```\n', '\n```\n'); break
    case 'table':
      insertText('| 列1 | 列2 | 列3 |\n| --- | --- | --- |\n| 单元格 | 单元格 | 单元格 |\n')
      break
    case 'hr': insertText('\n---\n'); break
  }
  if (id !== 'undo' && id !== 'redo' && id !== 'indent' && id !== 'undent') {
    view.focus()
  }
}

// ===== 文本处理工具（more 下拉）=====
function getTargetRange(): { from: number; to: number } {
  const view = editorView
  if (!view) return { from: 0, to: 0 }
  const sel = view.state.selection.main
  if (sel.empty) return { from: 0, to: view.state.doc.length }
  return { from: sel.from, to: sel.to }
}

function runTextTool(id: string) {
  const view = editorView
  if (!view || props.readonly) return
  const { from, to } = getTargetRange()
  const text = view.state.sliceDoc(from, to)
  const newText = transformText(id, text)
  view.dispatch({
    changes: { from, to, insert: newText },
    selection: EditorSelection.range(from, from + newText.length),
    userEvent: 'input',
  })
  view.focus()
  showMore.value = false
}

function transformText(id: string, text: string): string {
  const lines = text.split('\n')
  switch (id) {
    case 'removeBlankLines':
      return lines.filter((l) => l.trim() !== '').join('\n')
    case 'insertBlankLines':
      return lines.join('\n\n')
    case 'splitLines':
      return text
        .split(/[。！？!?；;\n]+/)
        .map((s) => s.trim())
        .filter(Boolean)
        .join('\n')
    case 'mergeLines':
      return lines
        .map((l) => l.trim())
        .filter(Boolean)
        .join(' ')
    case 'dedupeLines': {
      const seen = new Set<string>()
      return lines
        .filter((l) => {
          const k = l.trim()
          if (seen.has(k)) return false
          seen.add(k)
          return true
        })
        .join('\n')
    }
    case 'sortLines':
      return [...lines].sort((a, b) => a.localeCompare(b, 'zh')).join('\n')
    case 'fullHalfConvert':
      return text
        .replace(/[！-～]/g, (ch) => String.fromCharCode(ch.charCodeAt(0) - 0xfee0))
        .replace(/　/g, ' ')
    case 'numberLines':
      return lines.map((l, i) => `${i + 1}. ${l}`).join('\n')
    case 'trimLineEnds':
      return lines.map((l) => l.replace(/\s+$/, '')).join('\n')
    case 'shrinkSpaces':
      return text.replace(/[ \t]+/g, ' ')
    case 'removeAllWhitespace':
      return text.replace(/\s+/g, '')
    case 'listToTable': {
      const items = lines
        .map((l) => l.replace(/^([-*+]\s+|\d+\.\s+)/, ''))
        .filter(Boolean)
      if (!items.length) return text
      return `| 项 |\n| --- |\n${items.map((it) => `| ${it} |`).join('\n')}`
    }
    case 'tableToList': {
      const rows = lines
        .map((l) => l.trim())
        .filter((l) => l.startsWith('|') && !/^\|[\s:|-]+\|$/.test(l))
        .map((l) =>
          l
            .split('|')
            .slice(1, -1)
            .map((c) => c.trim())
            .join(' / '),
        )
        .filter(Boolean)
      return rows.map((it) => `- ${it}`).join('\n')
    }
    default:
      return text
  }
}

// ===== 自定义命令（wrap / regex）=====
function runCustom(cmd: CustomCommand) {
  const view = editorView
  if (!view || props.readonly) return
  if (cmd.type === 'wrap') {
    wrap(cmd.prefix ?? '', cmd.suffix ?? '')
    showMore.value = false
    return
  }
  const { from, to } = getTargetRange()
  const text = view.state.sliceDoc(from, to)
  let newText = text
  try {
    const re = new RegExp(cmd.pattern || '', cmd.flags || 'g')
    newText = text.replace(re, cmd.replacement ?? '')
  } catch (e) {
    brushTip.value = t('toolbar.customError') + ' ' + (e as Error).message
    autoHideTip()
    return
  }
  view.dispatch({
    changes: { from, to, insert: newText },
    selection: EditorSelection.range(from, from + newText.length),
    userEvent: 'input',
  })
  view.focus()
  showMore.value = false
}

defineExpose({
  focus: () => editorView?.focus(),
  getContent: () => editorView?.state.doc.toString() || '',
})
</script>

<template>
  <div class="markdown-editor">
    <!-- 工具栏：top / floating / fixed 三种模式由 CSS + 条件控制（单一渲染） -->
    <div
      v-if="toolbarVisible"
      ref="floatToolbarRef"
      class="editor-toolbar"
      :class="[settingsStore.settings.toolbar.mode, { floating: settingsStore.settings.toolbar.mode === 'floating' }]"
      :style="settingsStore.settings.toolbar.mode === 'floating' ? floatingStyle : undefined"
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
            @click="applyColor(colorPickerKind, color)"
          />
        </div>
        <label class="tb-color-custom">
          自定义…
          <input
            ref="customColorRef"
            type="color"
            @change="applyColor(colorPickerKind, customColorRef?.value || '#000000')"
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
          @click.stop="onPluginButtonClick(pbtn, $event)"
        >
          {{ pbtn.icon || pbtn.title }}
        </button>
      </template>

      <!-- “更多”下拉（文本处理工具 + 自定义命令） -->
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
            @click="runTextTool(tool.id)"
          >
            {{ t(tool.i18nKey) }}
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
              @click="runCustom(cmd)"
            >
              {{ cmd.name }}
            </button>
          </div>
        </template>
      </div>
    </div>

    <!-- 隐藏的颜色选择器：被上面 label[for] 原生触发，避免 WebView2 拒绝 input.click() -->
    <input
      id="nv-color-picker"
      ref="colorInputRef"
      type="color"
      class="tb-color-input"
      @change="(e) => onColorInput('color', e)"
    >
    <input
      id="nv-bg-picker"
      ref="bgInputRef"
      type="color"
      class="tb-color-input"
      @change="(e) => onColorInput('bg', e)"
    >

    <!-- 格式刷提示 -->
    <div
      v-if="brushTip"
      class="tb-tip"
    >
      {{ brushTip }}
    </div>

    <!-- 操作轻提示（插件按钮等） -->
    <div
      v-if="actionTip"
      class="tb-tip tb-tip-action"
    >
      {{ actionTip }}
    </div>

    <div
      ref="editorRef"
      class="editor-cm"
    />
  </div>
</template>

<style scoped>
.markdown-editor {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

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

.editor-cm {
  order: 1;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
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

.tb-color-input {
  /* 自定义 popover 用不到这个：保留以免 label[for] 引用悬空 */
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  border: 0;
  padding: 0;
  margin: 0;
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

.tb-more-divider {
  height: 1px;
  background: var(--border);
  margin: 4px 2px;
}

.tb-tip {
  position: fixed;
  left: 50%;
  bottom: 24px;
  transform: translateX(-50%);
  z-index: 1100;
  padding: 6px 12px;
  border-radius: 6px;
  background: var(--bg-card);
  color: var(--text);
  border: 1px solid var(--border);
  font-size: 12px;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.25);
}

.tb-tip-action {
  bottom: 56px;
  color: var(--accent);
  border-color: var(--accent);
}

.markdown-editor :deep(.cm-editor) {
  height: 100%;
}

.markdown-editor :deep(.cm-editor.cm-focused) {
  outline: none;
}
</style>
