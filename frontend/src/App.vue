<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import TitleBar from '@/components/layout/TitleBar.vue'
import SideBar from '@/components/layout/SideBar.vue'
import StatusBar from '@/components/layout/StatusBar.vue'
import CommandPalette from '@/components/layout/CommandPalette.vue'
import ToastHost from '@/components/layout/ToastHost.vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'
import { useReminderNotifications } from '@/composables/useReminderNotifications'
import { WorkspaceService } from '@bindings/github.com/notevault/notevault/index.js'
import { applyInlineFormat, getActiveEditor } from '@/plugins/editorBridge'

const workspaceStore = useWorkspaceStore()
const pluginRuntimeStore = usePluginRuntimeStore()
const route = useRoute()
const router = useRouter()

// 命令面板状态
const showCommandPalette = ref(false)

// 欢迎页和设置页有自己的独立布局，需要隐藏主应用侧边栏
const hideSidebarRoutes = ['/', '/settings']
const showSidebar = computed(() => workspaceStore.hasWorkspace && !hideSidebarRoutes.includes(route.path))

// 启用提醒通知（系统级通知）
useReminderNotifications(() => workspaceStore.currentWorkspace?.path)

// 全局快捷键
function handleGlobalKeydown(e: KeyboardEvent) {
  // Ctrl+P：打开命令面板（搜索 + 命令的入口）
  if (e.ctrlKey && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'p') {
    e.preventDefault()
    showCommandPalette.value = !showCommandPalette.value
    return
  }
  // Ctrl+,：打开设置
  if (e.ctrlKey && e.key === ',') {
    e.preventDefault()
    router.push('/settings')
    return
  }
  // Ctrl+N：新建文档（仅在编辑器页）
  if (e.ctrlKey && e.key.toLowerCase() === 'n' && route.path === '/editor') {
    e.preventDefault()
    // 触发 SideBar 的新建文档
    const event = new CustomEvent('notevault:new-file')
    window.dispatchEvent(event)
    return
  }
  // 行内格式：Ctrl+B 加粗 / Ctrl+I 斜体 / Ctrl+K 链接 / Ctrl+\ 行内代码。
  // 编辑器聚焦时交给 CM6 keymap 处理（上面 return），避免双重应用；
  // 这里作为全局兜底——焦点不在编辑器但仍停留在编辑器页时也能作用于活动编辑器。
  if (e.ctrlKey && !e.shiftKey && !e.altKey && route.path === '/editor') {
    const inlineMap: Record<string, 'bold' | 'italic' | 'link' | 'code'> = {
      b: 'bold',
      i: 'italic',
      k: 'link',
      '\\': 'code',
    }
    const fmt = inlineMap[e.key.toLowerCase()]
    if (fmt) {
      const view = getActiveEditor()
      if (view && view.dom.contains(document.activeElement)) return
      if (view) {
        e.preventDefault()
        applyInlineFormat(fmt)
      }
      return
    }
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleGlobalKeydown)
  void pluginRuntimeStore.initialize()
  void restoreCurrentWorkspace()
})

// 桌面应用重启后后端仍记住当前工作区；前端 store 需要先恢复，
// 否则搜索这类依赖 store 的页面会在首次进入时静默跳过请求。
async function restoreCurrentWorkspace() {
  if (workspaceStore.currentWorkspace) return
  try {
    const workspace = await WorkspaceService.GetCurrentWorkspace()
    if (workspace) workspaceStore.setCurrentWorkspace(workspace as any)
  } catch (e) {
    console.warn('Failed to restore current workspace:', e)
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  window.removeEventListener('app:open-command-palette', openCommandPaletteFromEvent)
})

// 顶栏搜索按钮通过 CustomEvent 打开命令面板（避免 router 跳到 /search 时
// 把当前工作区切走；让 palette 直接浮在当前视图上）
function openCommandPaletteFromEvent() {
  showCommandPalette.value = true
}
window.addEventListener('app:open-command-palette', openCommandPaletteFromEvent)

function closeCommandPalette() {
  showCommandPalette.value = false
}

function handleNewFileFromPalette() {
  const event = new CustomEvent('notevault:new-file')
  window.dispatchEvent(event)
}
</script>

<template>
  <div class="app-container">
    <TitleBar />
    <div class="app-body">
      <SideBar v-if="showSidebar" />
      <main class="app-main">
        <RouterView v-slot="{ Component }">
          <transition
            name="fade"
            mode="out-in"
          >
            <component :is="Component" />
          </transition>
        </RouterView>
      </main>
    </div>
    <StatusBar />
    <CommandPalette
      :visible="showCommandPalette"
      @close="closeCommandPalette"
      @new-file="handleNewFileFromPalette"
    />
    <ToastHost />
    <div class="plugin-notification-stack">
      <div
        v-for="notification in pluginRuntimeStore.notifications"
        :key="notification.id"
        class="plugin-notification"
        :class="{ error: notification.kind === 'error' }"
        data-testid="plugin-notification"
      >
        <span>{{ notification.message }}</span>
        <button
          type="button"
          @click="pluginRuntimeStore.clearNotification(notification.id)"
        >
          ×
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.app-container {
  display: flex;
  flex-direction: column;
  height: 100vh;
  width: 100vw;
  overflow: hidden;
  background: var(--bg-window);
}

.app-body {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.app-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-content);
}

.plugin-notification-stack {
  position: fixed;
  right: var(--space-4);
  bottom: 40px;
  z-index: 10000;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.plugin-notification {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 260px;
  max-width: 380px;
  padding: 10px 12px;
  border: 1px solid rgba(22, 163, 74, 0.4);
  border-radius: var(--radius-md);
  background: var(--bg-window);
  color: #16a34a;
  box-shadow: 0 10px 30px rgba(0, 0, 0, .25);
  font-size: var(--text-sm);
}
.plugin-notification.error {
  border-color: rgba(239, 68, 68, .4);
  color: #ef4444;
}
.plugin-notification span {
  flex: 1;
  word-break: break-word;
}
</style>
