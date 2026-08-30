<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Search,
  Settings,
  Home,
  Minus,
  Square,
  Copy,
  X,
  Palette,
  ChevronDown,
} from 'lucide-vue-next'
import { useSettingsStore } from '@/stores/settings'
import type { ThemeType } from '@/types'
import { AppService } from '@bindings/github.com/notevault/notevault/index.js'

// Wails v3 窗口控制 API（动态导入，避免模块加载时初始化失败导致整个应用崩溃）
let currentWindow: any = null
let currentApplication: any = null
let currentDialogs: any = null
async function getWindow() {
  if (!currentWindow) {
    try {
      const runtime = await import('@wailsio/runtime')
      currentWindow = runtime.Window
    } catch (e) {
      console.error('Failed to load Wails runtime:', e)
    }
  }
  return currentWindow
}
async function getApplication() {
  if (!currentApplication) {
    try {
      const runtime = await import('@wailsio/runtime')
      currentApplication = runtime.Application
    } catch (e) {
      console.error('Failed to load Wails Application runtime:', e)
    }
  }
  return currentApplication
}
async function getDialogs() {
  if (!currentDialogs) {
    try {
      const runtime = await import('@wailsio/runtime')
      currentDialogs = runtime.Dialogs
    } catch (e) {
      console.error('Failed to load Wails Dialogs:', e)
    }
  }
  return currentDialogs
}

const { t } = useI18n()
const settingsStore = useSettingsStore()
const router = useRouter()

const showThemeMenu = ref(false)
const isMaximised = ref(false)

const themes = [
  { value: 'macos' as ThemeType, label: 'macOS', descKey: 'titlebar.themes.macos' },
  { value: 'winui' as ThemeType, label: 'WinUI', descKey: 'titlebar.themes.winui' },
  { value: 'islands-dark' as ThemeType, label: 'Islands Dark', descKey: 'titlebar.themes.islandsDark' },
]

function selectTheme(theme: ThemeType) {
  settingsStore.setTheme(theme)
  showThemeMenu.value = false
}

function toggleSidebar() {
  settingsStore.toggleSidebar()
}

function openSettings() {
  router.push('/settings')
}

function goHome() {
  router.push('/')
}

function openSearch() {
  // 走 CustomEvent 让 App.vue 打开命令面板，
  // 不要 router.push('/search')：那会把当前工作区切走。
  window.dispatchEvent(new CustomEvent('app:open-command-palette'))
}

// 窗口控制
async function minimizeWindow() {
  const win = await getWindow()
  win?.Minimise()
}

async function toggleMaximize() {
  const win = await getWindow()
  if (!win) return
  if (isMaximised.value) {
    win.UnMaximise()
    isMaximised.value = false
  } else {
    win.Maximise()
    isMaximised.value = true
  }
}

// 退出确认用自定义弹框（不用 window.confirm：WV2 把它转成 native dialog 弹在右上角）
const showExitConfirm = ref(false)

async function requestExit() {
  showExitConfirm.value = true
}

async function confirmExit() {
  showExitConfirm.value = false
  // Wails v3 beta 的 app.Quit() 是 InvokeSync(destroy)：Go 端同步等主线程销毁窗口，
  // 主线程又等 WebView2 退出，WebView2 等前端 pending 调用返回 —— 三方死锁（已知 beta 问题）。
  // 这里直接走后端 os.Exit(0)，配合编辑器 500ms 自动保存 + localStorage 同步持久化，风险可控。
  try {
    await AppService.ForceQuit()
  } catch {
    // 兜底：bindings 调用失败时退回 Wails Quit（尽力而为）
    const app = await getApplication()
    void app?.Quit()
  }
}

function cancelExit() {
  showExitConfirm.value = false
}

// 点击外部关闭主题菜单
function handleClickOutside(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (!target.closest('.theme-switcher')) {
    showThemeMenu.value = false
  }
}

// 挂载时监听全局点击
if (typeof window !== 'undefined') {
  document.addEventListener('click', handleClickOutside)
}
</script>

<template>
  <div class="titlebar">
    <!-- 左侧：拖拽区域 + 应用名 -->
    <div class="titlebar-left">
      <div class="app-icon">
        📓
      </div>
      <span class="app-name">NoteVault</span>
    </div>

    <!-- 中间：搜索框 -->
    <div class="titlebar-center">
      <button
        class="search-box"
        data-testid="titlebar-search"
        @click="openSearch"
      >
        <Search
          :size="14"
          class="search-icon"
        />
        <span class="search-placeholder">{{ t('titlebar.searchPlaceholder') }}</span>
      </button>
    </div>

    <!-- 右侧：主页 + 主题切换 + 设置 + 窗口控制 -->
    <div class="titlebar-right">
      <!-- 主页按钮 -->
      <button
        class="icon-btn"
        data-testid="titlebar-home"
        :title="t('titlebar.home')"
        @click="goHome"
      >
        <Home :size="16" />
      </button>

      <!-- 主题切换 -->
      <div class="theme-switcher">
        <button
          class="icon-btn"
          :title="t('titlebar.switchTheme')"
          @click.stop="showThemeMenu = !showThemeMenu"
        >
          <Palette :size="16" />
        </button>
        <div
          v-if="showThemeMenu"
          class="theme-menu"
          @click.stop
        >
          <div class="theme-menu-title">
            {{ t('titlebar.chooseTheme') }}
          </div>
          <button
            v-for="th in themes"
            :key="th.value"
            class="theme-item"
            :class="{ active: settingsStore.settings.theme === th.value }"
            @click="selectTheme(th.value)"
          >
            <span
              class="theme-dot"
              :class="`dot-${th.value}`"
            />
            <span class="theme-info">
              <span class="theme-label">{{ th.label }}</span>
              <span class="theme-desc">{{ t(th.descKey) }}</span>
            </span>
            <ChevronDown
              v-if="settingsStore.settings.theme === th.value"
              :size="14"
              class="check-icon"
              style="transform: rotate(-90deg)"
            />
          </button>
        </div>
      </div>

      <!-- 设置按钮 -->
      <button
        class="icon-btn"
        data-testid="titlebar-settings"
        :title="t('titlebar.settings')"
        @click="openSettings"
      >
        <Settings :size="16" />
      </button>

      <!-- 窗口控制按钮：窗口已设为 frameless（不再有 Windows 原生标题栏），
           这一组是唯一的 — □ ×，避免与 OS 原生按钮叠成两套。 -->
      <div class="window-controls">
        <button
          class="win-btn minimize"
          :title="t('titlebar.minimize')"
          @click="minimizeWindow"
        >
          <Minus :size="14" />
        </button>
        <button
          class="win-btn maximize"
          :title="t('titlebar.maximize')"
          @click="toggleMaximize"
        >
          <component
            :is="isMaximised ? Copy : Square"
            :size="12"
          />
        </button>
        <button
          class="win-btn close"
          data-testid="titlebar-close"
          :title="t('titlebar.close')"
          @click="requestExit"
        >
          <X :size="14" />
        </button>
      </div>
    </div>

    <!-- 退出确认弹框（fixed 在视口正中央，不用 WV2 native dialog 位置不可控） -->
    <div
      v-if="showExitConfirm"
      class="exit-confirm-mask"
      @click.self="cancelExit"
    >
      <div
        class="exit-confirm"
        role="dialog"
        aria-modal="true"
        @keydown.esc="cancelExit"
      >
        <p class="exit-confirm-title">{{ t('titlebar.exitConfirmTitle') }}</p>
        <p class="exit-confirm-text">{{ t('titlebar.exitConfirm') }}</p>
        <div class="exit-confirm-actions">
          <button
            class="exit-btn cancel"
            type="button"
            @click="cancelExit"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="exit-btn confirm"
            type="button"
            autofocus
            @click="confirmExit"
          >
            {{ t('titlebar.exit') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.titlebar {
  height: var(--titlebar-height);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-3);
  background: var(--bg-sidebar);
  border-bottom: 1px solid var(--border);
  -webkit-app-region: drag;
  flex-shrink: 0;
  z-index: 10;
}

.titlebar-left {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 140px;
}

.app-icon {
  font-size: 16px;
}

.app-name {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
}

.titlebar-center {
  flex: 1;
  display: flex;
  justify-content: center;
  max-width: 480px;
}

.search-box {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  max-width: 360px;
  height: 28px;
  padding: 0 var(--space-3);
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  cursor: pointer;
  -webkit-app-region: no-drag;
  transition: border-color var(--transition-fast), background var(--transition-fast);
}

.search-box:hover {
  border-color: var(--border-accent);
}

.search-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.search-placeholder {
  font-size: var(--text-sm);
  color: var(--text-muted);
}

.titlebar-right {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 180px;
  justify-content: flex-end;
}

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  -webkit-app-region: no-drag;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* 主题切换菜单 */
.theme-switcher {
  position: relative;
}

.theme-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  width: 220px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-lg);
  padding: var(--space-2);
  z-index: var(--z-dropdown);
  -webkit-app-region: no-drag;
}

.theme-menu-title {
  font-size: var(--text-xs);
  color: var(--text-muted);
  padding: var(--space-1) var(--space-2) var(--space-2);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.theme-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2);
  border-radius: var(--radius-sm);
  text-align: left;
  transition: background var(--transition-fast);
}

.theme-item:hover {
  background: var(--bg-hover);
}

.theme-item.active {
  background: var(--bg-active);
}

.theme-dot {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  flex-shrink: 0;
  border: 2px solid var(--border);
}

.dot-macos {
  background: linear-gradient(135deg, #007aff 0%, #5ac8fa 100%);
}

.dot-winui {
  background: linear-gradient(135deg, #0078d4 0%, #00bcf2 100%);
}

.dot-islands-dark {
  background: linear-gradient(135deg, #1e1f22 0%, #f0a732 100%);
}

.theme-info {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}

.theme-label {
  font-size: var(--text-sm);
  font-weight: 500;
  color: var(--text-primary);
}

.theme-desc {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.check-icon {
  color: var(--accent);
  flex-shrink: 0;
}

/* 窗口控制按钮 */
.window-controls {
  display: flex;
  align-items: center;
  margin-left: var(--space-2);
  -webkit-app-region: no-drag;
}

.win-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: var(--titlebar-height);
  color: var(--text-secondary);
  transition: background var(--transition-fast), color var(--transition-fast);
  /* 顶栏整体是 drag region，按钮必须排除，否则点击会被拖拽吃掉 */
  -webkit-app-region: no-drag;
}

.win-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.win-btn.close:hover {
  background: #e81123;
  color: #ffffff;
}

/* 退出确认弹框：固定在视口正中央（不走 WV2 native dialog） */
.exit-confirm-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
}
.exit-confirm {
  background: var(--bg-window, #1e1f22);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  border-radius: 8px;
  padding: 20px 24px 16px;
  min-width: 320px;
  max-width: 80vw;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
}
.exit-confirm-title {
  font-size: var(--text-base, 14px);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 8px;
}
.exit-confirm-text {
  font-size: var(--text-sm, 13px);
  color: var(--text-secondary, #aaa);
  margin: 0 0 16px;
  line-height: 1.5;
}
.exit-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.exit-btn {
  padding: 6px 16px;
  border-radius: 4px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  background: transparent;
  color: var(--text-primary);
  font-size: var(--text-sm, 13px);
  cursor: pointer;
  transition: background var(--transition-fast);
}
.exit-btn:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.08));
}
.exit-btn.confirm {
  background: #dc2626;
  border-color: #dc2626;
  color: #fff;
}
.exit-btn.confirm:hover {
  background: #b91c1c;
}
</style>
