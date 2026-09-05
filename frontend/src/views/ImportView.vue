<template>
  <div class="import-view">
    <header class="iv-header">
      <h1>
        <Upload :size="22" />
        <span>{{ t('import.title') }}</span>
      </h1>
      <p class="iv-sub">
        {{ t('import.subtitle') }}
      </p>
    </header>

    <!-- 当前工作区 -->
    <section class="iv-card iv-workspace">
      <div class="iv-workspace-left">
        <FolderOpen :size="16" />
        <div>
          <div class="iv-label">
            {{ t('import.targetWorkspace') }}
          </div>
          <div class="iv-value">
            <span v-if="currentWorkspace">{{ currentWorkspace.name }} — {{ currentWorkspace.path }}</span>
            <span
              v-else
              class="iv-warn"
            >{{ t('import.noWorkspace') }}</span>
          </div>
        </div>
      </div>
      <button
        v-if="!currentWorkspace"
        class="iv-btn iv-btn-primary"
        @click="router.push('/')"
      >
        {{ t('import.goChoose') }}
      </button>
    </section>

    <!-- 源选择 -->
    <section class="iv-card">
      <div class="iv-card-title">
        <FileUp :size="14" />
        <span>{{ t('import.sourceTitle') }}</span>
      </div>
      <div class="iv-source-grid">
        <button
          class="iv-source-card"
          :class="{ active: sourceType === 'folder' }"
          :disabled="isImporting"
          @click="chooseFolder"
        >
          <FolderOpen :size="22" />
          <div class="iv-source-name">
            {{ t('import.sourceFolder') }}
          </div>
          <div class="iv-source-desc">
            {{ t('import.sourceFolderDesc') }}
          </div>
          <div
            v-if="folderPath"
            class="iv-source-path"
          >
            {{ folderPath }}
          </div>
        </button>
        <button
          class="iv-source-card"
          :class="{ active: sourceType === 'zip' }"
          :disabled="isImporting"
          @click="chooseZip"
        >
          <FileArchive :size="22" />
          <div class="iv-source-name">
            {{ t('import.sourceZip') }}
          </div>
          <div class="iv-source-desc">
            {{ t('import.sourceZipDesc') }}
          </div>
          <div
            v-if="zipPath"
            class="iv-source-path"
          >
            {{ zipPath }}
          </div>
        </button>
      </div>
    </section>

    <!-- 选项 -->
    <section class="iv-card">
      <div class="iv-card-title">
        <Settings :size="14" />
        <span>{{ t('import.optionsTitle') }}</span>
      </div>
      <div class="iv-options">
        <div class="iv-field">
          <label>{{ t('import.conflictStrategy') }}</label>
          <select
            v-model="conflictStrategy"
            :disabled="isImporting"
          >
            <option value="skip">
              {{ t('import.strategySkip') }}
            </option>
            <option value="rename">
              {{ t('import.strategyRename') }}
            </option>
            <option value="overwrite">
              {{ t('import.strategyOverwrite') }}
            </option>
          </select>
        </div>
        <label class="iv-checkbox">
          <input
            v-model="includeSubdirs"
            type="checkbox"
            :disabled="isImporting"
          >
          <span>{{ t('import.includeSubdirs') }}</span>
        </label>
      </div>
    </section>

    <!-- 执行 -->
    <section class="iv-card iv-action">
      <button
        class="iv-btn iv-btn-primary"
        :disabled="!canImport || isImporting"
        @click="doImport"
      >
        <Loader2
          v-if="isImporting"
          :size="14"
          class="spin"
        />
        <Upload
          v-else
          :size="14"
        />
        <span>{{ isImporting ? t('import.importing') : t('import.startImport') }}</span>
      </button>
    </section>

    <!-- 结果 -->
    <section
      v-if="result"
      class="iv-card iv-result"
    >
      <div class="iv-card-title">
        <CheckCircle2 :size="14" />
        <span>{{ t('import.resultTitle') }}</span>
      </div>
      <div class="iv-result-stats">
        <div class="iv-stat">
          <div class="iv-stat-value">
            {{ result.imported }}
          </div>
          <div class="iv-stat-label">
            {{ t('import.imported') }}
          </div>
        </div>
        <div class="iv-stat">
          <div class="iv-stat-value">
            {{ result.skipped }}
          </div>
          <div class="iv-stat-label">
            {{ t('import.skipped') }}
          </div>
        </div>
        <div class="iv-stat">
          <div class="iv-stat-value">
            {{ result.renamed }}
          </div>
          <div class="iv-stat-label">
            {{ t('import.renamed') }}
          </div>
        </div>
        <div class="iv-stat">
          <div class="iv-stat-value">
            {{ result.errors?.length || 0 }}
          </div>
          <div class="iv-stat-label">
            {{ t('import.errors') }}
          </div>
        </div>
      </div>
      <div
        v-if="result.errors && result.errors.length"
        class="iv-error-list"
      >
        <div class="iv-error-title">
          {{ t('import.errorList') }}
        </div>
        <ul>
          <li
            v-for="(err, idx) in result.errors"
            :key="idx"
          >
            {{ err }}
          </li>
        </ul>
      </div>
      <button
        class="iv-btn iv-btn-ghost"
        @click="result = null"
      >
        {{ t('import.clearResult') }}
      </button>
    </section>

    <!-- Git 版本管理（P2-4） -->
    <section
      v-if="currentWorkspace"
      class="iv-card"
    >
      <div class="iv-card-title">
        <GitBranch :size="14" />
        <span>{{ t('git.title') }}</span>
      </div>
      <p class="iv-git-desc">
        {{ t('git.desc') }}
      </p>

      <!-- git 未安装 -->
      <div
        v-if="gitStatus && !gitStatus.installed"
        class="iv-git-warn"
      >
        {{ t('git.notInstalled') }}
      </div>

      <!-- 非仓库：初始化入口 -->
      <template v-else-if="gitStatus && !gitStatus.isRepo">
        <div class="iv-git-status-line">
          {{ t('git.notRepo') }}
        </div>
        <button
          class="iv-btn iv-btn-primary"
          :disabled="gitBusy"
          @click="doGitInit"
        >
          <Loader2
            v-if="gitBusy"
            :size="14"
            class="spin"
          />
          <GitBranch
            v-else
            :size="14"
          />
          <span>{{ t('git.initBtn') }}</span>
        </button>
      </template>

      <!-- 已是仓库：状态 + 一键提交 -->
      <template v-else-if="gitStatus">
        <div class="iv-git-stats">
          <div class="iv-git-stat">
            <span class="iv-git-stat-label">{{ t('git.branch') }}</span>
            <span class="iv-git-stat-value">{{ gitStatus.branch || '—' }}</span>
          </div>
          <div class="iv-git-stat">
            <span class="iv-git-stat-label">{{ t('git.changed') }}</span>
            <span class="iv-git-stat-value">{{ gitStatus.changed }}</span>
          </div>
          <div class="iv-git-stat">
            <span class="iv-git-stat-label">{{ t('git.untracked') }}</span>
            <span class="iv-git-stat-value">{{ gitStatus.untracked }}</span>
          </div>
        </div>
        <div class="iv-git-commit-row">
          <input
            v-model="gitCommitMsg"
            type="text"
            class="iv-git-input"
            :placeholder="t('git.commitPlaceholder')"
            :disabled="gitBusy"
            @keyup.enter="doGitCommit"
          >
          <button
            class="iv-btn iv-btn-primary"
            :disabled="gitBusy"
            @click="doGitCommit"
          >
            <Loader2
              v-if="gitBusy"
              :size="14"
              class="spin"
            />
            <CheckCircle2
              v-else
              :size="14"
            />
            <span>{{ gitBusy ? t('git.committing') : t('git.commitBtn') }}</span>
          </button>
        </div>
      </template>

      <div
        v-if="gitInfoMsg"
        class="iv-git-info"
      >
        {{ gitInfoMsg }}
      </div>
      <div
        v-if="gitErrorMsg"
        class="iv-error-banner"
      >
        <AlertCircle :size="14" />
        <span>{{ gitErrorMsg }}</span>
      </div>
    </section>

    <!-- 错误提示 -->
    <div
      v-if="errorMsg"
      class="iv-error-banner"
    >
      <AlertCircle :size="14" />
      <span>{{ errorMsg }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Upload,
  FileUp,
  FolderOpen,
  FileArchive,
  Settings,
  Loader2,
  CheckCircle2,
  AlertCircle,
  GitBranch,
} from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { AppService, GitService, ImportService } from '@bindings/github.com/notevault/notevault/index.js'
import type { ImportResult, GitStatus } from '@bindings/github.com/notevault/notevault/models.js'

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const { t } = useI18n()

const sourceType = ref<'folder' | 'zip' | ''>('')
const folderPath = ref('')
const zipPath = ref('')
const conflictStrategy = ref<'skip' | 'rename' | 'overwrite'>('rename')
const includeSubdirs = ref(true)
const isImporting = ref(false)
const result = ref<ImportResult | null>(null)
const errorMsg = ref('')

const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

const canImport = computed(() => {
  if (!currentWorkspace.value) return false
  if (sourceType.value === 'folder' && folderPath.value) return true
  if (sourceType.value === 'zip' && zipPath.value) return true
  return false
})

async function chooseFolder() {
  errorMsg.value = ''
  try {
    const p = await AppService.OpenFolderDialog()
    if (p) {
      folderPath.value = p
      zipPath.value = ''
      sourceType.value = 'folder'
    }
  } catch (e) {
    errorMsg.value = t('import.dialogFailed', { msg: (e as Error).message })
  }
}

async function chooseZip() {
  errorMsg.value = ''
  try {
    const p = await AppService.OpenFileDialog('*.zip')
    if (p) {
      zipPath.value = p
      folderPath.value = ''
      sourceType.value = 'zip'
    }
  } catch (e) {
    errorMsg.value = t('import.dialogFailed', { msg: (e as Error).message })
  }
}

async function doImport() {
  if (!currentWorkspace.value) {
    errorMsg.value = t('import.noWorkspace')
    return
  }
  isImporting.value = true
  errorMsg.value = ''
  result.value = null
  try {
    const opts = {
      conflictStrategy: conflictStrategy.value,
      includeSubdirs: includeSubdirs.value,
    }
    if (sourceType.value === 'folder' && folderPath.value) {
      result.value = await ImportService.ImportMarkdownFolder(
        folderPath.value,
        currentWorkspace.value.path,
        opts,
      )
    } else if (sourceType.value === 'zip' && zipPath.value) {
      result.value = await ImportService.ImportZip(zipPath.value, currentWorkspace.value.path, opts)
    }
  } catch (e) {
    errorMsg.value = t('import.importFailed', { msg: (e as Error).message })
  } finally {
    isImporting.value = false
  }
}

// ---------------------------------------------------------------------------
// Git 版本管理（P2-4）
// ---------------------------------------------------------------------------

const gitStatus = ref<GitStatus | null>(null)
const gitCommitMsg = ref('')
const gitBusy = ref(false)
const gitInfoMsg = ref('')
const gitErrorMsg = ref('')

async function loadGitStatus(): Promise<void> {
  if (!currentWorkspace.value) {
    gitStatus.value = null
    return
  }
  try {
    gitStatus.value = await GitService.Status(currentWorkspace.value.path)
  } catch {
    // Status 本身已把失败降级为「能力缺失」，这里兜底网络层异常
    gitStatus.value = null
  }
}

// 切换工作区时重新探测；进入页面时立即探测一次
watch(
  currentWorkspace,
  () => {
    gitInfoMsg.value = ''
    gitErrorMsg.value = ''
    void loadGitStatus()
  },
  { immediate: true },
)

async function doGitInit(): Promise<void> {
  if (!currentWorkspace.value || gitBusy.value) return
  gitBusy.value = true
  gitInfoMsg.value = ''
  gitErrorMsg.value = ''
  try {
    await GitService.InitRepo(currentWorkspace.value.path)
    gitInfoMsg.value = t('git.initDone')
    await loadGitStatus()
  } catch (e) {
    gitErrorMsg.value = t('git.failed', { msg: (e as Error).message })
  } finally {
    gitBusy.value = false
  }
}

// 后端返回面向用户的中文文案，映射回 i18n key，避免英文界面混中文
function mapCommitMessage(raw: string): string {
  if (raw.includes('没有可提交')) return t('git.clean')
  if (raw.includes('提交成功')) return t('git.commitDone')
  return raw
}

async function doGitCommit(): Promise<void> {
  if (!currentWorkspace.value || gitBusy.value) return
  gitBusy.value = true
  gitInfoMsg.value = ''
  gitErrorMsg.value = ''
  try {
    const raw = await GitService.CommitAll(currentWorkspace.value.path, gitCommitMsg.value.trim())
    gitInfoMsg.value = mapCommitMessage(raw)
    gitCommitMsg.value = ''
    await loadGitStatus()
  } catch (e) {
    gitErrorMsg.value = t('git.failed', { msg: (e as Error).message })
  } finally {
    gitBusy.value = false
  }
}
</script>

<style scoped>
.import-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-8);
  overflow-y: auto;
  background: var(--bg-content);
}

.iv-header {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
.iv-header h1 {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--text-2xl);
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
}
.iv-sub {
  font-size: var(--text-sm);
  color: var(--text-muted);
  margin: 0;
}

.iv-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.iv-workspace {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
}
.iv-workspace-left {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}
.iv-workspace-left > svg {
  color: var(--accent);
  flex-shrink: 0;
}
.iv-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-bottom: 2px;
}
.iv-value {
  font-size: var(--text-sm);
  color: var(--text-primary);
  word-break: break-all;
}
.iv-warn {
  color: #f59e0b;
}

.iv-card-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
}

.iv-source-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-3);
}
.iv-source-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-5) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-window);
  color: var(--text-secondary);
  cursor: pointer;
  transition: all var(--transition-fast);
  text-align: center;
}
.iv-source-card:hover:not(:disabled) {
  background: var(--bg-hover);
  border-color: var(--border-accent, var(--accent));
  color: var(--accent);
}
.iv-source-card.active {
  border-color: var(--accent);
  background: rgba(0, 122, 255, 0.06);
  color: var(--accent);
}
.iv-source-card:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.iv-source-name {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
}
.iv-source-desc {
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.iv-source-path {
  font-size: var(--text-xs);
  color: var(--accent);
  word-break: break-all;
  max-width: 100%;
}

.iv-options {
  display: flex;
  gap: var(--space-4);
  align-items: center;
  flex-wrap: wrap;
}
.iv-field {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.iv-field label {
  font-size: var(--text-sm);
  color: var(--text-secondary);
}
.iv-field select {
  height: 28px;
  padding: 0 var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-sm);
  outline: none;
}
.iv-field select:focus {
  border-color: var(--accent);
}
.iv-checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
  color: var(--text-secondary);
  cursor: pointer;
}

.iv-action {
  align-items: flex-start;
}
.iv-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  font-weight: 500;
  cursor: pointer;
  transition: all var(--transition-fast);
}
.iv-btn:hover:not(:disabled) {
  background: var(--bg-hover);
}
.iv-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.iv-btn-primary {
  background: var(--accent);
  color: var(--text-inverse);
  border-color: var(--accent);
}
.iv-btn-primary:hover:not(:disabled) {
  background: var(--accent-hover, var(--accent));
  color: var(--text-inverse);
}
.iv-btn-ghost {
  background: transparent;
}

.spin {
  animation: iv-spin 0.8s linear infinite;
}
@keyframes iv-spin {
  to {
    transform: rotate(360deg);
  }
}

.iv-result-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-3);
}
.iv-stat {
  text-align: center;
  padding: var(--space-3);
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.iv-stat-value {
  font-size: var(--text-2xl);
  font-weight: 700;
  color: var(--accent);
  line-height: 1.2;
}
.iv-stat-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin-top: 2px;
}

.iv-error-list {
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-3);
}
.iv-error-title {
  font-size: var(--text-xs);
  font-weight: 600;
  color: #ef4444;
  margin-bottom: var(--space-2);
}
.iv-error-list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 200px;
  overflow-y: auto;
}
.iv-error-list li {
  font-size: var(--text-xs);
  color: var(--text-secondary);
  padding: 2px 0;
  border-bottom: 1px dashed var(--border);
}
.iv-error-list li:last-child {
  border-bottom: none;
}

.iv-error-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: var(--space-3) var(--space-4);
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid #ef4444;
  border-radius: var(--radius-sm);
  color: #ef4444;
  font-size: var(--text-sm);
}

.iv-git-desc {
  font-size: var(--text-xs);
  color: var(--text-muted);
  margin: 0;
  line-height: 1.5;
}
.iv-git-warn {
  font-size: var(--text-sm);
  color: #f59e0b;
  padding: var(--space-2) var(--space-3);
  background: rgba(245, 158, 11, 0.08);
  border: 1px solid rgba(245, 158, 11, 0.4);
  border-radius: var(--radius-sm);
}
.iv-git-status-line {
  font-size: var(--text-sm);
  color: var(--text-secondary);
}
.iv-git-stats {
  display: flex;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.iv-git-stat {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.iv-git-stat-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.iv-git-stat-value {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
}
.iv-git-commit-row {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}
.iv-git-input {
  flex: 1;
  min-width: 0;
  height: 32px;
  padding: 0 var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-sm);
  outline: none;
}
.iv-git-input:focus {
  border-color: var(--accent);
}
.iv-git-info {
  font-size: var(--text-sm);
  color: #10b981;
  padding: var(--space-2) var(--space-3);
  background: rgba(16, 185, 129, 0.08);
  border: 1px solid rgba(16, 185, 129, 0.35);
  border-radius: var(--radius-sm);
}

@media (max-width: 720px) {
  .iv-source-grid,
  .iv-result-stats {
    grid-template-columns: 1fr 1fr;
  }
  .iv-workspace {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
