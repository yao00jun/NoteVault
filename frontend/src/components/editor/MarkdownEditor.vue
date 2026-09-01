<script setup lang="ts">
// ============================================================================
// MarkdownEditor —— CodeMirror 6 编辑器核心。
//
// 拆分结构（E 批次前端分解收尾）：
//   - EditorToolbar.vue      工具栏呈现（按钮/颜色面板/更多下拉/插件按钮）
//   - editorCommands.ts      纯 CM6 编辑命令（wrap/heading/align/applyFormat…）
//   - editorTextTools.ts     “更多”下拉的纯文本变换
//   - editorBridge.ts        插件装饰/keymap 注入桥（不变）
// 本组件保留：EditorView 生命周期、快捷键、粘贴图片、格式刷状态、浮层定位。
// ============================================================================
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { EditorState, EditorSelection, Compartment } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching } from '@codemirror/language'
import { autocompletion } from '@codemirror/autocomplete'
import { oneDark } from '@codemirror/theme-one-dark'
import { useSettingsStore } from '@/stores/settings'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'
import { useWorkspaceStore } from '@/stores/workspace'
import { createWikiLinkCompletionSource } from './wikiLinkAutocomplete'
import EditorToolbar from './EditorToolbar.vue'
import { wrapSelection, applyFormat, insertCallout } from './editorCommands'
import { transformText } from './editorTextTools'
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
const workspaceStore = useWorkspaceStore()
const wikiLinkCompletionSource = createWikiLinkCompletionSource(
  () => workspaceStore.currentWorkspace?.path,
)
const editorRef = ref<HTMLElement | null>(null)
const toolbarRef = ref<InstanceType<typeof EditorToolbar> | null>(null)
const colorInputRef = ref<HTMLInputElement | null>(null)
const bgInputRef = ref<HTMLInputElement | null>(null)
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

// ===== 字体颜色 / 背景色 / 格式刷 =====
const lastFormat = ref<{ kind: 'color' | 'bg'; value: string } | null>(null)
const brushTip = ref('')
const actionTip = ref('')
const brushActive = ref(false)
let applyingBrush = false

/** 唤起取色面板：面板状态在 EditorToolbar 内，这里只转发（插件宿主手势链需要） */
function openColorPicker(kind: 'color' | 'bg') {
  toolbarRef.value?.openColorPicker(kind)
}

function onCommand(id: string) {
  if (!editorView || props.readonly) return
  applyFormat(editorView, id)
}

/** “更多”下拉的 Callout 快捷插入 */
function onCallout(type: string) {
  if (!editorView || props.readonly) return
  insertCallout(editorView, type)
}

/** 插件通过 UI 注册协议注入的工具栏按钮：点击时在真实编辑器上执行其声明变换 */
function onPluginButtonClick(btn: RegisteredPluginToolbarButton, event?: MouseEvent) {
  if (props.readonly) return
  // 颜色/背景色按钮会让 openColorPicker 打开 popover，click 必须 stopPropagation，
  // 否则 document 上的点击外关闭会立即关掉它。
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
  wrapSelection(view, before, after)
  lastFormat.value = { kind, value: hex }
  view.focus()
}

/** 格式刷自动模式：把上一次格式刷到当前选区（由 selectionSet 监听触发） */
function applyBrushAuto() {
  if (!lastFormat.value) return
  applyingBrush = true
  applyColor(lastFormat.value.kind, lastFormat.value.value)
  applyingBrush = false
}

function onEditorKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && brushActive.value) {
    brushActive.value = false
    brushTip.value = t('toolbar.brushOff')
    autoHideTip()
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
  const el = toolbarRef.value?.rootEl
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
      // [[ 自动补全：override 仅用我们自己的 wiki-link 源（笔记编辑器不需要默认单词补全）
      autocompletion({ override: [wikiLinkCompletionSource] }),
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
  // Escape 关格式刷（弹层类的 Escape 在 EditorToolbar 内自治）
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
  document.removeEventListener('keydown', onEditorKeydown)
  if (tipTimer) window.clearTimeout(tipTimer)
  destroyEditor()
})

// ===== “更多”下拉：文本处理工具 + 自定义命令 =====
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
}

function runCustom(cmd: CustomCommand) {
  const view = editorView
  if (!view || props.readonly) return
  if (cmd.type === 'wrap') {
    wrapSelection(view, cmd.prefix ?? '', cmd.suffix ?? '')
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
}

defineExpose({
  focus: () => editorView?.focus(),
  getContent: () => editorView?.state.doc.toString() || '',
})
</script>

<template>
  <div class="markdown-editor">
    <!-- 工具栏：top / floating / fixed 三种模式由 CSS + 条件控制（单一渲染） -->
    <EditorToolbar
      v-if="toolbarVisible"
      ref="toolbarRef"
      :brush-active="brushActive"
      :floating-style="settingsStore.settings.toolbar.mode === 'floating' ? floatingStyle : null"
      @command="onCommand"
      @apply-color="applyColor"
      @toggle-brush="toggleBrush"
      @text-tool="runTextTool"
      @callout="onCallout"
      @custom="runCustom"
      @plugin-button="onPluginButtonClick"
    />

    <!-- 隐藏的颜色选择器：被工具栏 label[for] 原生触发，避免 WebView2 拒绝 input.click() -->
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

.editor-cm {
  order: 1;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
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
