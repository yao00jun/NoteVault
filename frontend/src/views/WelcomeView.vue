<script setup lang="ts">
import { ref, onMounted, computed, nextTick } from 'vue'
import {
  FolderPlus,
  FolderOpen,
  FileText,
  Clock,
  Sparkles,
  Keyboard,
  Palette,
  ChevronRight,
  X,
} from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { toWorkspace, toWorkspaceList } from '@/utils/workspace'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { WorkspaceService, FileService } from '@bindings/github.com/notevault/notevault/index.js'
import { useToast } from '@/composables/useToast'

const toast = useToast()

interface Workspace {
  id: string
  name: string
  path: string
  createdAt: string
  lastOpenedAt: string
}

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const router = useRouter()

const showDialog = ref(false)
const dialogMode = ref<'new-doc' | 'open-folder' | 'recent'>('new-doc')
const workspaceName = ref('')
const workspacePath = ref('')
const docName = ref('')
const isProcessing = ref(false)
const recentWorkspaces = ref<Workspace[]>([])
const isLoadingRecent = ref(false)
const recentError = ref('')
const recentSectionRef = ref<HTMLElement | null>(null)

const dialogTitle = computed(() => {
  return dialogMode.value === 'new-doc'
    ? t('welcome.dialog.newDocTitle')
    : dialogMode.value === 'recent'
      ? t('welcome.recentTitle')
      : t('welcome.dialog.openFolderTitle')
})

// 加载最近打开的工作区
async function loadRecentWorkspaces() {
  isLoadingRecent.value = true
  try {
    const list = await WorkspaceService.ListWorkspaces()
    recentWorkspaces.value = (list as Workspace[]).slice(0, 5)
  } catch (e) {
    console.error('Failed to load recent workspaces:', e)
    recentWorkspaces.value = []
  } finally {
    isLoadingRecent.value = false
  }
}

onMounted(() => {
  loadRecentWorkspaces()
})

// 处理快速操作点击
function handleAction(action: string) {
  if (action === 'new-doc') {
    dialogMode.value = 'new-doc'
    docName.value = t('sidebar.untitledDoc')
    // 总是弹出对话框，让用户确认工作区和文件名
    if (workspaceStore.currentWorkspace?.path) {
      // 有当前工作区，预填工作区信息
      workspaceName.value = workspaceStore.currentWorkspace.name
      workspacePath.value = workspaceStore.currentWorkspace.path
    } else {
      // 没有当前工作区，给默认值
      workspaceName.value = t('welcome.defaults.workspaceName')
      workspacePath.value = 'C:/Users/Public/Documents/NoteVault'
    }
    showDialog.value = true
  } else if (action === 'open-folder') {
    dialogMode.value = 'open-folder'
    workspaceName.value = ''
    workspacePath.value = ''
    showDialog.value = true
  } else if (action === 'recent') {
    // 「最近打开」卡片：弹窗列出最近工作区供选择（旧实现是滚动定位，
    // 窗口高度足够时毫无视觉反馈，被用户当成"点了没反应"）。
    // 没有任何历史时引导用户先打开一个文件夹。
    if (recentWorkspaces.value.length === 0) {
      recentError.value = t('welcome.recentEmpty')
      dialogMode.value = 'open-folder'
      workspaceName.value = ''
      workspacePath.value = ''
      showDialog.value = true
      return
    }
    dialogMode.value = 'recent'
    showDialog.value = true
  }
}

// 创建新文档
async function createNewDoc() {
  // 验证工作区信息
  if (!workspaceName.value.trim()) {
    toast.warning(t('welcome.errors.enterWorkspaceName'))
    return
  }
  if (!workspacePath.value.trim()) {
    toast.warning(t('welcome.errors.enterWorkspacePath'))
    return
  }
  if (!docName.value.trim()) {
    toast.warning(t('welcome.errors.enterDocName'))
    return
  }

  isProcessing.value = true
  try {
    // 创建或打开工作区（后端按路径去重）
    const ws = await WorkspaceService.CreateWorkspace(workspaceName.value, workspacePath.value)
    if (ws) {
      workspaceStore.setCurrentWorkspace(toWorkspace(ws))
    }
  } catch (e) {
    console.error('Failed to create workspace:', e)
    toast.error(t('welcome.errors.createWorkspaceFailed', { msg: (e as Error).message }))
    isProcessing.value = false
    return
  }

  // 创建新文档
  const fileName = docName.value.endsWith('.md') ? docName.value : docName.value + '.md'
  try {
    const node = await FileService.CreateFile(
      workspaceStore.currentWorkspace!.path,
      fileName,
      `# ${fileName.replace('.md', '')}\n\n`
    )
    showDialog.value = false
    workspaceStore.incrementFileTreeVersion()
    const filePath = typeof (node as { path?: string })?.path === 'string'
      ? (node as { path: string }).path
      : fileName
    router.push({
      path: '/editor',
      query: {
        file: encodeURIComponent(filePath),
      },
    })
  } catch (e) {
    if ((e as Error).message?.includes('exist')) {
      toast.warning(t('welcome.errors.fileExists'))
    } else {
      console.error('Failed to create file:', e)
      toast.error(t('welcome.errors.createFileFailed', { msg: (e as Error).message }))
    }
  } finally {
    isProcessing.value = false
  }
}

// 打开文件夹作为工作区
async function openFolder() {
  if (!workspaceName.value.trim()) {
    toast.warning(t('welcome.errors.enterWorkspaceName'))
    return
  }
  if (!workspacePath.value.trim()) {
    toast.warning(t('welcome.errors.enterFolderPath'))
    return
  }
  isProcessing.value = true
  try {
    // 检查工作区是否已存在
    const existing = recentWorkspaces.value.find(w => w.path === workspacePath.value)
    if (existing) {
      await WorkspaceService.SetCurrentWorkspace(existing.id)
      workspaceStore.setCurrentWorkspace(toWorkspace(existing))
    } else {
      const ws = await WorkspaceService.CreateWorkspace(workspaceName.value, workspacePath.value)
      if (ws) {
        workspaceStore.setCurrentWorkspace(toWorkspace(ws))
      }
    }
    showDialog.value = false
    await loadRecentWorkspaces()
    router.push('/editor')
  } catch (e) {
    console.error('Failed to open folder:', e)
    toast.error(t('welcome.errors.openFolderFailed', { msg: (e as Error).message }))
  } finally {
    isProcessing.value = false
  }
}

// 打开最近的工作区：按 path 单独锁定，让用户看到正在打开哪个。
// alert 在 Wails v3 里会被路由到 wails.localhost 的独立 dialog 上，
// 经常被用户看不见——改成 console.error 并额外写一行可视提示（红框 + 文字），
// 即使是 SetCurrentWorkspace 抛错，也能马上定位。
const openingPath = ref<string | null>(null)
async function openRecentWorkspace(ws: Workspace) {
  if (openingPath.value) return
  openingPath.value = ws.path
  recentError.value = ''
  console.log('[recent] opening workspace', ws.id, ws.path)
  try {
    await WorkspaceService.SetCurrentWorkspace(ws.id)
    workspaceStore.setCurrentWorkspace(toWorkspace(ws))
    await router.push('/editor')
  } catch (e) {
    // 错误必须可见：Wails v3 的 alert 会路由到独立 dialog，用户经常看不见。
    // 这里用页面内联红字提示，配合 console.error 便于排查。
    recentError.value = t('welcome.errors.openWorkspaceFailed', { msg: (e as Error).message || String(e) })
    console.error('[recent] failed to open workspace', ws.id, e)
  } finally {
    // 无论成功（页面跳转）还是失败，都必须释放锁，否则会永久禁用所有最近项点击
    openingPath.value = null
  }
}

// 打开演示工作区
async function openDemo() {
  isProcessing.value = true
  try {
    const demoPath = 'C:/Users/Public/Documents/NoteVault-Demo'
    const existing = recentWorkspaces.value.find(w => w.path === demoPath)
    if (existing) {
      await WorkspaceService.SetCurrentWorkspace(existing.id)
      workspaceStore.setCurrentWorkspace(toWorkspace(existing))
    } else {
      const ws = await WorkspaceService.CreateWorkspace('演示工作区', demoPath)
      if (ws) {
        workspaceStore.setCurrentWorkspace(toWorkspace(ws))
        // 创建一些演示文档
        await FileService.CreateFile(demoPath, '欢迎使用 NoteVault.md', '# 欢迎使用 NoteVault\n\n这是一个本地优先的个人知识库管理工具。\n\n## 功能特性\n\n- #Markdown 编辑\n- #标签 管理\n- 待办事项\n- 全文搜索\n\n## 待办示例\n\n- [ ] 探索 NoteVault 的功能\n- [x] 创建第一个文档\n- [ ] 设置自己的工作区\n')
        await FileService.CreateFile(demoPath, '快速入门.md', '# 快速入门\n\n## 基本操作\n\n1. 点击左侧文件树的 + 新建文档\n2. 在编辑器中输入 Markdown 内容\n3. 右侧实时预览\n4. 自动保存到本地\n\n## 快捷链接\n\n试试点击 [[欢迎使用 NoteVault]] 跳转到对应文档\n')
      }
    }
    await loadRecentWorkspaces()
    router.push('/editor')
  } catch (e) {
    console.error('Failed to open demo:', e)
    toast.error(t('welcome.errors.openDemoFailed', { msg: (e as Error).message }))
  } finally {
    isProcessing.value = false
  }
}

function closeDialog() {
  showDialog.value = false
}

// 浏览选择文件夹
async function browseFolder() {
  try {
    const runtime = await import('@wailsio/runtime')
    const selected = await runtime.Dialogs.OpenFile({
      Title: t('sidebar.chooseFolderTitle'),
      CanChooseDirectories: true,
      CanChooseFiles: false,
      CanCreateDirectories: true,
      ButtonText: t('welcome.dialog.chooseButton'),
    })
    if (selected && typeof selected === 'string' && selected.length > 0) {
      // 将反斜杠转换为正斜杠
      workspacePath.value = selected.replace(/\\/g, '/')
      // 如果没有填写工作区名称，用文件夹名作为名称
      if (!workspaceName.value.trim()) {
        const parts = workspacePath.value.split('/')
        workspaceName.value = parts[parts.length - 1] || t('welcome.defaults.workspaceNameFromPath')
      }
    }
    // 用户取消选择时不报错
  } catch (e) {
    // 用户取消选择时不报错
    const msg = (e as Error).message || ''
    if (msg.includes('cancelled') || msg.includes('cancel')) {
      return
    }
    console.error('Failed to open folder dialog:', e)
    toast.error(t('welcome.errors.dialogFailed', { msg }))
  }
}

const quickActions = computed(() => [
  { icon: FileText, label: t('welcome.actions.newDoc.label'), desc: t('welcome.actions.newDoc.desc'), action: 'new-doc' },
  { icon: FolderOpen, label: t('welcome.actions.openFolder.label'), desc: t('welcome.actions.openFolder.desc'), action: 'open-folder' },
  { icon: Clock, label: t('welcome.actions.recent.label'), desc: t('welcome.actions.recent.desc'), action: 'recent' },
])

const features = computed(() => [
  { icon: Sparkles, title: t('welcome.features.localFirst.title'), desc: t('welcome.features.localFirst.desc') },
  { icon: Keyboard, title: t('welcome.features.efficient.title'), desc: t('welcome.features.efficient.desc') },
  { icon: Palette, title: t('welcome.features.themes.title'), desc: t('welcome.features.themes.desc') },
])
</script>

<template>
  <div class="welcome-view">
    <div class="welcome-content">
      <!-- Logo 和标题 -->
      <div class="welcome-header">
        <div class="logo">
          📓
        </div>
        <h1 class="title">
          NoteVault
        </h1>
        <p class="subtitle">
          {{ t('welcome.tagline') }}
        </p>
      </div>

      <!-- 快速操作 -->
      <div class="quick-actions">
        <button
          v-for="action in quickActions"
          :key="action.action"
          class="action-card"
          :data-testid="`welcome-${action.action}`"
          :disabled="isProcessing"
          @click="handleAction(action.action)"
        >
          <div class="action-icon">
            <component
              :is="action.icon"
              :size="22"
            />
          </div>
          <div class="action-info">
            <span class="action-label">{{ action.label }}</span>
            <span class="action-desc">{{ action.desc }}</span>
          </div>
          <ChevronRight
            :size="16"
            class="action-arrow"
          />
        </button>
      </div>

      <!-- 最近打开的工作区 -->
      <div
        v-if="recentWorkspaces.length > 0"
        ref="recentSectionRef"
        class="recent-section"
      >
        <div class="recent-header">
          <Clock :size="14" />
          <span>{{ t('welcome.recentTitle') }}</span>
        </div>
        <div class="recent-list">
          <button
            v-for="ws in recentWorkspaces"
            :key="ws.id"
            class="recent-item"
            :disabled="openingPath !== null && openingPath !== ws.path"
            :data-loading="openingPath === ws.path"
            @click="openRecentWorkspace(ws)"
          >
            <FolderOpen :size="16" />
            <span class="recent-name">{{ ws.name }}</span>
            <span class="recent-path">{{ ws.path }}</span>
          </button>
        </div>
      </div>

      <!-- 打开失败的可见提示（不依赖 alert，Wails 下用户常看不见） -->
      <p
        v-if="recentError"
        class="recent-error"
      >
        {{ recentError }}
      </p>

      <!-- 演示入口 -->
      <div class="demo-section">
        <button
          class="demo-btn"
          :disabled="isProcessing"
          @click="openDemo"
        >
          <Sparkles :size="16" />
          <span>{{ t('welcome.demoEntry') }}</span>
        </button>
      </div>

      <!-- 特性介绍 -->
      <div class="features">
        <div
          v-for="feature in features"
          :key="feature.title"
          class="feature-item"
        >
          <component
            :is="feature.icon"
            :size="20"
            class="feature-icon"
          />
          <div class="feature-text">
            <span class="feature-title">{{ feature.title }}</span>
            <span class="feature-desc">{{ feature.desc }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 对话框 -->
    <div
      v-if="showDialog"
      class="dialog-overlay"
      @click.self="closeDialog"
    >
      <div class="dialog">
        <div class="dialog-header">
          <h3 class="dialog-title">
            {{ dialogTitle }}
          </h3>
          <button
            class="dialog-close"
            @click="closeDialog"
          >
            <X :size="18" />
          </button>
        </div>
        <div class="dialog-body">
          <!-- 新建文档模式：选择工作区 + 输入文件名 -->
          <template v-if="dialogMode === 'new-doc'">
            <div class="form-group">
              <label>{{ t('welcome.dialog.workspaceName') }}</label>
              <input
                v-model="workspaceName"
                type="text"
                data-testid="welcome-workspace-name"
                :placeholder="t('welcome.dialog.wsNamePlaceholder')"
                class="form-input"
              >
            </div>
            <div class="form-group">
              <label>{{ t('welcome.dialog.workspacePath') }}</label>
              <div class="path-input-row">
                <input
                  v-model="workspacePath"
                  type="text"
                  data-testid="welcome-workspace-path"
                  :placeholder="t('welcome.dialog.wsPathPlaceholder')"
                  class="form-input"
                >
                <button
                  type="button"
                  class="browse-btn"
                  @click="browseFolder"
                >
                  {{ t('welcome.dialog.browse') }}
                </button>
              </div>
              <p class="form-hint">
                {{ t('welcome.dialog.selectOrCreateHint') }}
              </p>
            </div>
            <div class="form-group">
              <label>{{ t('welcome.dialog.docName') }}</label>
              <input
                v-model="docName"
                type="text"
                data-testid="welcome-doc-name"
                :placeholder="t('welcome.dialog.docNamePlaceholder')"
                class="form-input"
              >
            </div>
          </template>
          <!-- 最近工作区选择模式 -->
          <template v-else-if="dialogMode === 'recent'">
            <div class="recent-list">
              <button
                v-for="ws in recentWorkspaces"
                :key="ws.id"
                class="recent-item"
                :disabled="openingPath !== null && openingPath !== ws.path"
                :data-loading="openingPath === ws.path"
                @click="closeDialog(); openRecentWorkspace(ws)"
              >
                <FolderOpen :size="16" />
                <span class="recent-name">{{ ws.name }}</span>
                <span class="recent-path">{{ ws.path }}</span>
              </button>
            </div>
          </template>
          <!-- 打开文件夹模式 -->
          <template v-else>
            <div class="form-group">
              <label>{{ t('welcome.dialog.workspaceName') }}</label>
              <input
                v-model="workspaceName"
                type="text"
                :placeholder="t('welcome.dialog.wsNameOpenPlaceholder')"
                class="form-input"
              >
            </div>
            <div class="form-group">
              <label>{{ t('welcome.dialog.folderPath') }}</label>
              <div class="path-input-row">
                <input
                  v-model="workspacePath"
                  type="text"
                  :placeholder="t('welcome.dialog.folderPathOpenPlaceholder')"
                  class="form-input"
                >
                <button
                  type="button"
                  class="browse-btn"
                  @click="browseFolder"
                >
                  {{ t('welcome.dialog.browse') }}
                </button>
              </div>
            </div>
            <p class="form-hint">
              {{ t('welcome.dialog.openFolderHint') }}
            </p>
          </template>
        </div>
        <div class="dialog-footer">
          <button
            class="btn-secondary"
            :disabled="isProcessing"
            @click="closeDialog"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="btn-primary"
            data-testid="welcome-confirm"
            :disabled="isProcessing"
            @click="dialogMode === 'new-doc' ? createNewDoc() : openFolder()"
          >
            {{ isProcessing ? t('common.processing') : t('common.confirm') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.welcome-view {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow-y: auto;
  padding: var(--space-6);
}

.welcome-content {
  width: 100%;
  max-width: 560px;
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
}

.welcome-header {
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
}

.logo { font-size: 48px; line-height: 1; margin-bottom: var(--space-2); }
.title { font-size: var(--text-2xl); font-weight: 700; color: var(--text-primary); margin: 0; }
.subtitle { font-size: var(--text-base); color: var(--text-secondary); margin: 0; }

.quick-actions { display: flex; flex-direction: column; gap: var(--space-2); }

.action-card {
  display: flex; align-items: center; gap: var(--space-3);
  width: 100%; padding: var(--space-4);
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius-md); text-align: left;
  transition: all var(--transition-fast); cursor: pointer;
}
.action-card:hover:not(:disabled) {
  border-color: var(--accent); background: var(--bg-hover);
  transform: translateY(-1px); box-shadow: var(--shadow-sm);
}
.action-card:disabled { opacity: 0.5; cursor: not-allowed; }

.action-icon {
  display: flex; align-items: center; justify-content: center;
  width: 44px; height: 44px; border-radius: var(--radius-md);
  background: var(--bg-active); color: var(--accent); flex-shrink: 0;
}
.action-info { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
.action-label { font-size: var(--text-base); font-weight: 600; color: var(--text-primary); }
.action-desc { font-size: var(--text-sm); color: var(--text-muted); }
.action-arrow { color: var(--text-muted); flex-shrink: 0; transition: transform var(--transition-fast); }
.action-card:hover .action-arrow { transform: translateX(3px); color: var(--accent); }

/* 最近工作区 */
.recent-section { display: flex; flex-direction: column; gap: var(--space-2); }
.recent-header {
  display: flex; align-items: center; gap: var(--space-1);
  font-size: var(--text-xs); font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: 0.5px; padding: 0 var(--space-1);
}
.recent-list { display: flex; flex-direction: column; gap: var(--space-1); }
.recent-item {
  display: flex; align-items: center; gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius-sm); cursor: pointer;
  transition: all var(--transition-fast); text-align: left;
}
.recent-item:hover:not(:disabled) { background: var(--bg-hover); border-color: var(--accent); }
.recent-item:disabled { opacity: 0.5; cursor: not-allowed; }
.recent-name { font-size: var(--text-sm); font-weight: 500; color: var(--text-primary); }
.recent-path { font-size: var(--text-xs); color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.recent-error { margin: 0; padding: var(--space-2) var(--space-3); border: 1px solid rgba(239, 68, 68, .4); border-radius: var(--radius-sm); background: rgba(239, 68, 68, .08); color: #ef4444; font-size: var(--text-sm); }

.demo-section { display: flex; justify-content: center; }
.demo-btn {
  display: flex; align-items: center; gap: var(--space-2);
  padding: var(--space-2) var(--space-4); border-radius: var(--radius-md);
  color: var(--accent); font-size: var(--text-sm); font-weight: 500;
  transition: background var(--transition-fast); cursor: pointer;
}
.demo-btn:hover:not(:disabled) { background: var(--bg-active); }
.demo-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.features {
  display: flex; flex-direction: column; gap: var(--space-3);
  padding-top: var(--space-4); border-top: 1px solid var(--border);
}
.feature-item { display: flex; align-items: flex-start; gap: var(--space-3); }
.feature-icon { color: var(--text-muted); flex-shrink: 0; margin-top: 2px; }
.feature-text { display: flex; flex-direction: column; gap: 2px; }
.feature-title { font-size: var(--text-sm); font-weight: 600; color: var(--text-primary); }
.feature-desc { font-size: var(--text-xs); color: var(--text-muted); line-height: 1.5; }

/* 对话框 */
.dialog-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.4);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000; backdrop-filter: blur(4px);
}
.dialog {
  width: 100%; max-width: 440px;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius-lg); box-shadow: var(--shadow-lg); overflow: hidden;
}
.dialog-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: var(--space-4) var(--space-4) var(--space-2);
}
.dialog-title { font-size: var(--text-lg); font-weight: 600; color: var(--text-primary); margin: 0; }
.dialog-close {
  display: flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; border-radius: var(--radius-sm);
  color: var(--text-muted); transition: all var(--transition-fast);
}
.dialog-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.dialog-body { padding: var(--space-2) var(--space-4) var(--space-4); display: flex; flex-direction: column; gap: var(--space-3); }
.form-group { display: flex; flex-direction: column; gap: var(--space-1); }
.form-group label { font-size: var(--text-sm); font-weight: 500; color: var(--text-secondary); }
.form-input {
  width: 100%; padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border); border-radius: var(--radius-sm);
  background: var(--bg-input); color: var(--text-primary); font-size: var(--text-sm);
}
.form-input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-alpha); }
.path-input-row { display: flex; gap: var(--space-2); align-items: center; }
.path-input-row .form-input { flex: 1; }
.browse-btn {
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  font-weight: 500;
  cursor: pointer;
  transition: all var(--transition-fast);
  white-space: nowrap;
}
.browse-btn:hover { background: var(--bg-hover); border-color: var(--accent); color: var(--accent); }
.form-hint { font-size: var(--text-xs); color: var(--text-muted); margin: 0; line-height: 1.5; }
.dialog-footer {
  display: flex; justify-content: flex-end; gap: var(--space-2);
  padding: var(--space-3) var(--space-4); border-top: 1px solid var(--border); background: var(--bg-window);
}
.btn-secondary {
  padding: var(--space-2) var(--space-4); border-radius: var(--radius-sm);
  color: var(--text-secondary); font-size: var(--text-sm); font-weight: 500;
  transition: background var(--transition-fast); cursor: pointer;
}
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-secondary:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary {
  padding: var(--space-2) var(--space-4); border-radius: var(--radius-sm);
  background: var(--accent); color: var(--text-inverse);
  font-size: var(--text-sm); font-weight: 500; transition: background var(--transition-fast); cursor: pointer;
}
.btn-primary:hover:not(:disabled) { background: var(--accent-hover); }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
