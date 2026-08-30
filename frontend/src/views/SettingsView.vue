<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { Settings, Palette, Keyboard, Info, ArrowLeft, Sparkles, Eye, EyeOff, AlertTriangle } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { TOOLBAR_ITEMS, VISIBLE_DEFAULT, TOOLBAR_ORDER_DEFAULT, type ToolbarItem } from '@/components/editor/toolbarButtons'
import type { ThemeType } from '@/types'
import type { Locale } from '@/i18n'

const { t } = useI18n()
const settingsStore = useSettingsStore()
const router = useRouter()
const apiKeyVisible = ref(false)

function goBack() {
  if (window.history.length > 1) {
    router.back()
  } else {
    router.push('/')
  }
}

const themes: { value: ThemeType; label: string }[] = [
  { value: 'macos', label: 'macOS' },
  { value: 'winui', label: 'WinUI' },
  { value: 'islands-dark', label: 'Islands Dark' },
]

const activeSection = ref<string>('appearance')

const sections = computed(() => [
  { id: 'appearance', label: t('settings.nav.appearance'), icon: Palette },
  { id: 'editor', label: t('settings.nav.editor'), icon: Settings },
  { id: 'ai', label: t('settings.nav.ai'), icon: Sparkles },
  { id: 'shortcuts', label: t('settings.nav.shortcuts'), icon: Keyboard },
  { id: 'errorReport', label: t('settings.nav.errorReport'), icon: AlertTriangle },
  { id: 'about', label: t('settings.nav.about'), icon: Info },
])

// 真实生效的快捷键（全局 App.vue + 编辑器组件级 CM6 keymap）。
// B/I/K/\ 已由两处共同注册，这里如实列出。
const shortcuts = computed(() => [
  { keys: 'Ctrl + P', desc: t('settings.shortcuts.commandPalette') },
  { keys: 'Ctrl + ,', desc: t('settings.shortcuts.openSettings') },
  { keys: 'Ctrl + N', desc: t('settings.shortcuts.newFile') },
  { keys: 'Ctrl + S', desc: t('settings.shortcuts.saveDoc') + ' (编辑器内)' },
  { keys: 'Ctrl + B', desc: t('settings.shortcuts.bold') + ' (编辑器内)' },
  { keys: 'Ctrl + I', desc: t('settings.shortcuts.italic') + ' (编辑器内)' },
  { keys: 'Ctrl + K', desc: t('settings.shortcuts.link') + ' (编辑器内)' },
  { keys: 'Ctrl + \\', desc: t('settings.shortcuts.code') + ' (编辑器内)' },
  { keys: 'Esc', desc: t('settings.shortcuts.esc') + ' (命令面板打开时)' },
])

function toggleToolbarButton(id: string) {
  const arr = settingsStore.settings.toolbar.visibleButtons
  if (arr.includes(id)) {
    settingsStore.settings.toolbar.visibleButtons = arr.filter((x) => x !== id)
  } else {
    settingsStore.settings.toolbar.visibleButtons = [...arr, id]
  }
}

function resetToolbarButtons() {
  settingsStore.settings.toolbar.visibleButtons = [...VISIBLE_DEFAULT]
  settingsStore.settings.toolbar.order = [...TOOLBAR_ORDER_DEFAULT]
}

// 工具栏按钮名称 / 固定标识查询
const itemMap = new Map<string, ToolbarItem>(TOOLBAR_ITEMS.map((i) => [i.id as string, i]))
function toolbarLabel(id: string): string {
  const it = itemMap.get(id)
  return it ? t(it.i18nKey || id) : id
}
function isFixedButton(id: string): boolean {
  return itemMap.get(id)?.fixed === true
}

// 拖拽排序（对应 Editing Toolbar 的 menu dragging and sorting）
let dragId = ''
function onDragStart(id: string, e: DragEvent) {
  dragId = id
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
function onDrop(targetId: string) {
  const order = [...settingsStore.settings.toolbar.order]
  const from = order.indexOf(dragId)
  const to = order.indexOf(targetId)
  if (from < 0 || to < 0 || from === to) return
  order.splice(from, 1)
  order.splice(to, 0, dragId)
  settingsStore.settings.toolbar.order = order
}

// 自定义命令管理
function addCustomCommand() {
  settingsStore.settings.toolbar.customCommands = [
    ...settingsStore.settings.toolbar.customCommands,
    {
      id: 'cmd-' + Date.now().toString(36),
      name: t('settings.editor.toolbar.newCommand'),
      type: 'wrap',
      prefix: '',
      suffix: '',
    },
  ]
}
function removeCustomCommand(idx: number) {
  settingsStore.settings.toolbar.customCommands = settingsStore.settings.toolbar.customCommands.filter(
    (_, i) => i !== idx,
  )
}

function onLanguageChange(e: Event) {
  const value = (e.target as HTMLSelectElement).value as Locale
  settingsStore.setLanguage(value)
}

const inputType = (v: string) => (v === 'password' ? 'password' : 'text')
function toggleApiKeyVisible() {
  apiKeyVisible.value = !apiKeyVisible.value
}

// 点击左侧菜单：滚动到对应 section 并更新 active 状态
async function scrollToSection(id: string) {
  activeSection.value = id
  await nextTick()
  const el = document.getElementById(`settings-section-${id}`)
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
}
</script>

<template>
  <div class="settings-view">
    <div class="settings-sidebar">
      <button
        class="back-button"
        @click="goBack"
      >
        <ArrowLeft :size="16" />
        <span>{{ t('settings.back') }}</span>
      </button>
      <h2 class="settings-title">
        {{ t('settings.title') }}
      </h2>
      <nav class="settings-nav">
        <button
          v-for="section in sections"
          :key="section.id"
          class="settings-nav-item"
          :class="{ active: activeSection === section.id }"
          type="button"
          @click="scrollToSection(section.id)"
        >
          <component
            :is="section.icon"
            :size="16"
          />
          <span>{{ section.label }}</span>
        </button>
      </nav>
    </div>

    <div class="settings-content">
      <!-- 1. 外观 -->
      <div
        id="settings-section-appearance"
        class="settings-section"
      >
        <h3 class="section-title">
          {{ t('settings.appearance.title') }}
        </h3>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.appearance.theme') }}</span>
            <span class="setting-desc">{{ t('settings.appearance.themeDesc') }}</span>
          </div>
          <div class="theme-options">
            <button
              v-for="th in themes"
              :key="th.value"
              class="theme-option"
              :class="{ active: settingsStore.settings.theme === th.value }"
              @click="settingsStore.setTheme(th.value)"
            >
              {{ th.label }}
            </button>
          </div>
        </div>

        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.appearance.language') }}</span>
            <span class="setting-desc">{{ t('settings.appearance.languageDesc') }}</span>
          </div>
          <select
            :value="settingsStore.settings.language"
            class="setting-select"
            @change="onLanguageChange"
          >
            <option value="zh-CN">
              {{ t('settings.appearance.langZh') }}
            </option>
            <option value="en-US">
              {{ t('settings.appearance.langEn') }}
            </option>
          </select>
        </div>

        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.appearance.fontSize') }}</span>
            <span class="setting-desc">{{ t('settings.appearance.fontSizeDesc') }}</span>
          </div>
          <input
            v-model.number="settingsStore.settings.fontSize"
            type="range"
            min="11"
            max="18"
            class="setting-range"
          >
          <span class="range-value">{{ settingsStore.settings.fontSize }}px</span>
        </div>
      </div>

      <!-- 2. 编辑器 -->
      <div
        id="settings-section-editor"
        class="settings-section"
      >
        <h3 class="section-title">
          {{ t('settings.editor.title') }}
        </h3>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.editor.lineHeight') }}</span>
            <span class="setting-desc">{{ t('settings.editor.lineHeightDesc') }}</span>
          </div>
          <input
            v-model.number="settingsStore.settings.editor.lineHeight"
            type="range"
            data-testid="editor-line-height"
            min="1.2"
            max="2.2"
            step="0.1"
            class="setting-range"
          >
          <span class="range-value">{{ settingsStore.settings.editor.lineHeight }}</span>
        </div>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.editor.previewFontSize') }}</span>
            <span class="setting-desc">{{ t('settings.editor.previewFontSizeDesc') }}</span>
          </div>
          <input
            v-model.number="settingsStore.settings.editor.previewFontSize"
            type="range"
            data-testid="preview-font-size"
            min="12"
            max="20"
            class="setting-range"
          >
          <span class="range-value">{{ settingsStore.settings.editor.previewFontSize }}px</span>
        </div>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.editor.autoSaveInterval') }}</span>
            <span class="setting-desc">{{ t('settings.editor.autoSaveIntervalDesc') }}</span>
          </div>
          <input
            v-model.number="settingsStore.settings.autoSaveInterval"
            type="range"
            min="300"
            max="3000"
            step="100"
            class="setting-range"
          >
          <span class="range-value">{{ settingsStore.settings.autoSaveInterval }}ms</span>
        </div>
        <div class="setting-item toolbar-config">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.editor.toolbar.mode') }}</span>
            <span class="setting-desc">{{ t('settings.editor.toolbar.modeDesc') }}</span>
          </div>
          <div class="theme-options">
            <button
              class="theme-option"
              :class="{ active: settingsStore.settings.toolbar.mode === 'top' }"
              @click="settingsStore.settings.toolbar.mode = 'top'"
            >
              {{ t('settings.editor.toolbar.modeTop') }}
            </button>
            <button
              class="theme-option"
              :class="{ active: settingsStore.settings.toolbar.mode === 'floating' }"
              @click="settingsStore.settings.toolbar.mode = 'floating'"
            >
              {{ t('settings.editor.toolbar.modeFloating') }}
            </button>
            <button
              class="theme-option"
              :class="{ active: settingsStore.settings.toolbar.mode === 'fixed' }"
              @click="settingsStore.settings.toolbar.mode = 'fixed'"
            >
              {{ t('settings.editor.toolbar.modeFixed') }}
            </button>
          </div>
        </div>
        <div class="setting-item toolbar-config column">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.editor.toolbar.buttons') }}</span>
            <span class="setting-desc">{{ t('settings.editor.toolbar.buttonsDesc') }}</span>
          </div>
          <div class="toolbar-btn-grid">
            <label
              v-for="id in settingsStore.settings.toolbar.order"
              :key="id"
              class="tb-toggle"
              draggable="true"
              @dragstart="onDragStart(id, $event)"
              @dragover.prevent
              @drop="onDrop(id)"
            >
              <input
                type="checkbox"
                :checked="settingsStore.settings.toolbar.visibleButtons.includes(id)"
                :disabled="isFixedButton(id)"
                @change="toggleToolbarButton(id)"
              >
              <span>{{ toolbarLabel(id) }}</span>
              <span class="tb-drag">⠿</span>
            </label>
          </div>
          <button
            class="reset-btn"
            @click="resetToolbarButtons"
          >
            {{ t('settings.editor.toolbar.reset') }}
          </button>
          <div class="setting-item toolbar-config column cmd-config">
            <div class="setting-info">
              <span class="setting-label">{{ t('settings.editor.toolbar.customCommands') }}</span>
              <span class="setting-desc">{{ t('settings.editor.toolbar.customCommandsDesc') }}</span>
            </div>
            <div class="cmd-list">
              <div
                v-for="(cmd, idx) in settingsStore.settings.toolbar.customCommands"
                :key="cmd.id"
                class="cmd-row"
              >
                <input
                  v-model="cmd.name"
                  class="setting-input cmd-name"
                  :placeholder="t('settings.editor.toolbar.cmdName')"
                >
                <select
                  v-model="cmd.type"
                  class="setting-select"
                >
                  <option value="wrap">{{ t('settings.editor.toolbar.cmdWrap') }}</option>
                  <option value="regex">{{ t('settings.editor.toolbar.cmdRegex') }}</option>
                </select>
                <template v-if="cmd.type === 'wrap'">
                  <input
                    v-model="cmd.prefix"
                    class="setting-input cmd-arg"
                    :placeholder="t('settings.editor.toolbar.cmdPrefix')"
                  >
                  <input
                    v-model="cmd.suffix"
                    class="setting-input cmd-arg"
                    :placeholder="t('settings.editor.toolbar.cmdSuffix')"
                  >
                </template>
                <template v-else>
                  <input
                    v-model="cmd.pattern"
                    class="setting-input cmd-arg"
                    :placeholder="t('settings.editor.toolbar.cmdPattern')"
                  >
                  <input
                    v-model="cmd.replacement"
                    class="setting-input cmd-arg"
                    :placeholder="t('settings.editor.toolbar.cmdReplacement')"
                  >
                </template>
                <button
                  class="cmd-del"
                  type="button"
                  :title="t('settings.editor.toolbar.cmdDelete')"
                  @click="removeCustomCommand(idx)"
                >
                  ✕
                </button>
              </div>
            </div>
            <button
              class="reset-btn"
              type="button"
              @click="addCustomCommand"
            >
              {{ t('settings.editor.toolbar.addCommand') }}
            </button>
          </div>
        </div>
      </div>

      <!-- 3. AI 总结 -->
      <div
        id="settings-section-ai"
        class="settings-section"
      >
        <h3 class="section-title">
          {{ t('settings.ai.title') }}
        </h3>
        <p class="section-hint">
          {{ t('settings.ai.desc') }}
        </p>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.ai.baseURL') }}</span>
            <span class="setting-desc">{{ t('settings.ai.baseURLDesc') }}</span>
          </div>
          <input
            v-model="settingsStore.settings.ai.baseURL"
            class="setting-input"
            placeholder="https://api.openai.com/v1"
          >
        </div>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.ai.model') }}</span>
            <span class="setting-desc">{{ t('settings.ai.modelDesc') }}</span>
          </div>
          <input
            v-model="settingsStore.settings.ai.model"
            class="setting-input"
            placeholder="gpt-4o-mini"
          >
        </div>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.ai.apiKey') }}</span>
            <span class="setting-desc">{{ t('settings.ai.apiKeyDesc') }}</span>
          </div>
          <div class="input-with-action">
            <input
              v-model="settingsStore.settings.ai.apiKey"
              class="setting-input"
              :type="apiKeyVisible ? 'text' : 'password'"
              placeholder="sk-..."
            >
            <button
              class="icon-toggle"
              :title="apiKeyVisible ? t('settings.ai.hide') : t('settings.ai.show')"
              @click="toggleApiKeyVisible"
            >
              <Eye
                v-if="!apiKeyVisible"
                :size="14"
              />
              <EyeOff
                v-else
                :size="14"
              />
            </button>
          </div>
        </div>
      </div>

      <!-- 4. 快捷键 -->
      <div
        id="settings-section-shortcuts"
        class="settings-section"
      >
        <h3 class="section-title">
          {{ t('settings.shortcuts.title') }}
        </h3>
        <div class="shortcuts-list">
          <div
            v-for="s in shortcuts"
            :key="s.keys"
            class="shortcut-item"
          >
            <kbd class="shortcut-keys">{{ s.keys }}</kbd>
            <span class="shortcut-desc">{{ s.desc }}</span>
          </div>
        </div>
      </div>

      <!-- 5. 错误监控 -->
      <div
        id="settings-section-errorReport"
        class="settings-section"
      >
        <h3 class="section-title">
          {{ t('settings.errorReport.title') }}
        </h3>
        <p class="section-desc">
          {{ t('settings.errorReport.desc') }}
        </p>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.errorReport.sentryDSN') }}</span>
            <span class="setting-desc">{{ t('settings.errorReport.sentryDSNDesc') }}</span>
          </div>
          <input
            v-model="settingsStore.settings.errorReport.sentryDSN"
            class="setting-input"
            type="password"
            placeholder="https://publickey@sentry.io/1"
          >
        </div>
        <div class="setting-item">
          <div class="setting-info">
            <span class="setting-label">{{ t('settings.errorReport.enableLocalLog') }}</span>
            <span class="setting-desc">{{ t('settings.errorReport.enableLocalLogDesc') }}</span>
          </div>
          <label class="toggle-switch">
            <input
              v-model="settingsStore.settings.errorReport.enableLocalLog"
              type="checkbox"
            >
            <span class="toggle-slider" />
          </label>
        </div>
      </div>

      <!-- 6. 关于 -->
      <div
        id="settings-section-about"
        class="settings-section"
      >
        <h3 class="section-title">
          {{ t('settings.about.title') }}
        </h3>
        <div class="about-info">
          <div class="about-logo">
            📓
          </div>
          <div>
            <div class="about-name">
              NoteVault
            </div>
            <div class="about-version">
              {{ t('settings.about.version') }}
            </div>
          </div>
        </div>
        <p class="about-desc">
          {{ t('settings.about.desc') }}
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-view {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.settings-sidebar {
  width: 220px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border);
  padding: var(--space-4);
  flex-shrink: 0;
  overflow-y: auto;
}

.back-button {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  margin-bottom: var(--space-3);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.back-button:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.settings-title {
  font-size: var(--text-lg);
  font-weight: 700;
  color: var(--text-primary);
  margin: 0 0 var(--space-4);
}

.settings-nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.settings-nav-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  text-align: left;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.settings-nav-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.settings-nav-item.active {
  background: var(--bg-active);
  color: var(--accent);
  font-weight: 500;
}

.settings-content {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-6);
}

.settings-section {
  max-width: 640px;
  margin-bottom: var(--space-6);
}

.section-title {
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 var(--space-4);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--border);
}

.setting-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--border-light);
}

.setting-info {
  flex: 1;
  min-width: 0;
}

.setting-label {
  display: block;
  font-size: var(--text-sm);
  font-weight: 500;
  color: var(--text-primary);
  margin-bottom: 2px;
}

.setting-desc {
  display: block;
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.theme-options {
  display: flex;
  gap: var(--space-2);
}

.theme-option {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  transition: all var(--transition-fast);
}

.theme-option:hover {
  border-color: var(--border-accent);
}

.theme-option.active {
  border-color: var(--accent);
  background: var(--bg-active);
  color: var(--accent);
  font-weight: 500;
}

.setting-select {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-sm);
  outline: none;
  cursor: pointer;
}

.setting-select:focus {
  border-color: var(--accent);
}

.setting-range {
  width: 120px;
  accent-color: var(--accent);
}

.range-value {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  min-width: 40px;
}

.about-info {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}

.about-logo {
  font-size: 40px;
}

.about-name {
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text-primary);
}

.about-version {
  font-size: var(--text-sm);
  color: var(--text-muted);
}

.about-desc {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  line-height: 1.6;
  margin: 0;
}

.section-hint {
  font-size: var(--text-sm);
  color: var(--text-muted);
  line-height: 1.6;
  margin: 0 0 var(--space-4);
}

.setting-input {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-sm);
  outline: none;
  width: 260px;
  font-family: inherit;
}

.setting-input:focus {
  border-color: var(--accent);
}

.input-with-action {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.input-with-action .setting-input {
  width: 220px;
}

.icon-toggle {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  transition: all var(--transition-fast);
}

.icon-toggle:hover {
  background: var(--bg-hover);
  color: var(--accent);
}

.shortcuts-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.shortcut-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border-light);
}

.shortcut-keys {
  display: inline-block;
  min-width: 120px;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--bg-sidebar);
  border: 1px solid var(--border);
  color: var(--text-primary);
  font-size: var(--text-xs);
  font-family: var(--font-mono, monospace);
  text-align: center;
}

.shortcut-desc {
  font-size: var(--text-sm);
  color: var(--text-secondary);
}

/* 切换开关（错误监控等场景） */
.toggle-switch {
  position: relative;
  display: inline-block;
  width: 38px;
  height: 20px;
  flex-shrink: 0;
}
.toggle-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.toggle-slider {
  position: absolute;
  inset: 0;
  background: var(--bg-sidebar, #ccc);
  border-radius: 20px;
  cursor: pointer;
  transition: background var(--transition-fast);
}
.toggle-slider::before {
  content: "";
  position: absolute;
  width: 16px;
  height: 16px;
  left: 2px;
  top: 2px;
  background: white;
  border-radius: 50%;
  transition: transform var(--transition-fast);
}
.toggle-switch input:checked + .toggle-slider {
  background: var(--accent);
}
.toggle-switch input:checked + .toggle-slider::before {
  transform: translateX(18px);
}

/* 编辑器工具栏配置 */
.toolbar-config.column {
  flex-direction: column;
  align-items: stretch;
}

.toolbar-btn-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 6px 12px;
  margin-top: 4px;
}

.tb-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
  color: var(--text-secondary);
  cursor: pointer;
}

.tb-toggle input {
  accent-color: var(--accent);
}

.tb-drag {
  margin-left: auto;
  color: var(--text-muted);
  cursor: grab;
  font-size: 12px;
  user-select: none;
}

.tb-toggle:active .tb-drag {
  cursor: grabbing;
}

.cmd-config {
  margin-top: 8px;
}

.cmd-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
}

.cmd-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.cmd-name {
  width: 140px;
}

.cmd-arg {
  width: 120px;
}

.cmd-del {
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  cursor: pointer;
}

.cmd-del:hover {
  border-color: var(--border-accent);
  color: var(--accent);
}

.reset-btn {
  align-self: flex-start;
  margin-top: 10px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: all var(--transition-fast);
}

.reset-btn:hover {
  border-color: var(--border-accent);
  color: var(--accent);
}
</style>
