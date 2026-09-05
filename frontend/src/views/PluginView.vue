<template>
  <div class="plugin-view">
    <header class="pv-header">
      <h1>
        <Puzzle :size="22" />
        <span>{{ t('plugins.title') }}</span>
      </h1>
      <p class="pv-sub">
        {{ t('plugins.subtitle') }}
      </p>
      <div class="pv-header-actions">
        <code class="pv-dir">{{ pluginsDir || '...' }}</code>
        <button
          class="pv-btn pv-btn-ghost"
          @click="openPluginsDir"
        >
          <FolderOpen :size="14" />
          {{ t('plugins.openDir') }}
        </button>
        <button
          class="pv-btn pv-btn-ghost"
          @click="loadPlugins"
        >
          <RefreshCw :size="14" />
          {{ t('plugins.refresh') }}
        </button>
      </div>
    </header>

    <div
      v-if="loading"
      class="pv-loading"
    >
      <Loader2
        :size="20"
        class="spin"
      />
      {{ t('common.loading') }}
    </div>

    <div
      v-else-if="error"
      class="pv-error-banner"
    >
      <AlertCircle :size="14" />
      <span>{{ error }}</span>
    </div>

    <div
      v-if="pluginRuntimeStore.failedPlugins.length > 0"
      class="pv-error-banner"
      data-testid="plugin-runtime-failed"
    >
      <AlertCircle :size="14" />
            <span>{{ t('plugins.runtime.failedTitle') }}：{{ failedPluginNames.join('、') }}</span>
    </div>

    <div
      v-else-if="plugins.length === 0"
      class="pv-empty"
    >
      <Puzzle :size="32" />
      <h3>{{ t('plugins.emptyTitle') }}</h3>
      <p>{{ t('plugins.emptyDesc') }}</p>
      <p class="pv-empty-hint">
        {{ t('plugins.emptyHint') }}
      </p>
    </div>

    <div
      v-else
      class="pv-list"
    >
      <div
        v-for="p in plugins"
        :key="p.manifest.id"
        class="pv-card"
        :data-testid="`plugin-card-${p.manifest.id}`"
        :class="{ errored: p.hasError }"
      >
        <div class="pv-card-head">
          <div class="pv-card-title">
            <Puzzle :size="16" />
            <span>{{ p.manifest.name }}</span>
            <span class="pv-version">v{{ p.manifest.version }}</span>
            <span
              v-if="p.manifest.author"
              class="pv-author"
            >@{{ p.manifest.author }}</span>
          </div>
          <label class="toggle-switch">
            <input
              type="checkbox"
              data-testid="plugin-toggle"
              :checked="p.enabled"
              @change="onToggle(p.manifest.id, $event)"
            >
            <span class="toggle-slider" />
          </label>
        </div>
        <div class="pv-runtime-status">
          <span
            v-if="pluginRuntimeStore.activeIds.includes(p.manifest.id)"
            class="pv-badge pv-badge-active"
            data-testid="plugin-runtime-active"
          >
            {{ t('plugins.runtime.active') }}
          </span>
          <span
            v-else-if="p.enabled && !p.hasError"
            class="pv-badge pv-badge-failed"
            data-testid="plugin-runtime-blocked"
          >
            {{ t('plugins.runtime.failedTitle') }}
          </span>
          <span
            v-else
            class="pv-badge"
          >
            {{ t('plugins.runtime.inactive') }}
          </span>
          <span class="pv-meta-item">
            {{ t('plugins.runtime.commandsCount', { count: pluginCommands(p.manifest.id).length }) }}
          </span>
        </div>
        <p
          v-if="p.manifest.description"
          class="pv-card-desc"
        >
          {{ p.manifest.description }}
        </p>
        <div class="pv-card-meta">
          <span class="pv-meta-item">
            <FileCode :size="12" />
            {{ p.filePath }}
          </span>
          <span class="pv-meta-item">
            <HashIcon :size="12" />
            {{ p.hash }}
          </span>
          <span class="pv-meta-item">{{ formatSize(p.size) }}</span>
        </div>
        <div
          v-if="p.hasError"
          class="pv-card-error"
        >
          <AlertTriangle :size="14" />
          <span>{{ p.loadError }}</span>
        </div>
        <div class="pv-permissions">
          <span class="pv-permissions-title">{{ t('plugins.permissions.title') }}</span>
          <div class="pv-permission-list">
            <code
              v-for="permission in p.manifest.permissions ?? []"
              :key="permission"
              class="pv-permission"
            >{{ permission }}</code>
            <span
              v-if="(p.manifest.permissions ?? []).length === 0"
              class="pv-permission-empty"
            >
              {{ t('plugins.permissions.none') }}
            </span>
          </div>
        </div>
        <div
          v-if="p.manifest.trust === 'full'"
          class="pv-trust"
        >
          <span class="pv-trust-title">{{ t('plugins.trust.title') }}</span>
          <div class="pv-trust-row">
            <span
              class="pv-trust-badge"
              :class="p.trustGranted ? 'is-granted' : 'is-pending'"
            >
              {{ p.trustGranted ? t('plugins.trust.granted') : t('plugins.trust.requested') }}
            </span>
            <button
              v-if="!p.trustGranted"
              class="pv-btn pv-btn-warn"
              @click="pendingGrantId = p.manifest.id"
            >
              {{ t('plugins.trust.grant') }}
            </button>
            <button
              v-else
              class="pv-btn"
              @click="revokeTrust(p.manifest.id)"
            >
              {{ t('plugins.trust.revoke') }}
            </button>
          </div>
          <p class="pv-trust-note">{{ t('plugins.trust.note') }}</p>

          <div
            v-if="pendingGrantId === p.manifest.id"
            class="pv-trust-confirm"
          >
            <AlertTriangle :size="14" />
            <div class="pv-trust-confirm-body">
              <strong>{{ t('plugins.trust.confirmTitle') }}</strong>
              <ul>
                <li>{{ t('plugins.trust.riskData') }}</li>
                <li>{{ t('plugins.trust.riskUi') }}</li>
                <li>{{ t('plugins.trust.riskCrash') }}</li>
                <li>{{ t('plugins.trust.riskRevoke') }}</li>
              </ul>
              <div class="pv-trust-actions">
                <button
                  class="pv-btn pv-btn-warn"
                  @click="confirmGrant(p.manifest.id)"
                >
                  {{ t('plugins.trust.confirmGrant') }}
                </button>
                <button
                  class="pv-btn"
                  @click="pendingGrantId = ''"
                >
                  {{ t('common.cancel') }}
                </button>
              </div>
            </div>
          </div>
        </div>
        <div
          v-if="pluginSettings[p.manifest.id]"
          class="pv-settings"
        >
          <span class="pv-settings-title">
            {{ pluginSettings[p.manifest.id].title || t('plugins.settings.title') }}
          </span>
          <div
            v-for="item in pluginSettings[p.manifest.id].items"
            :key="item.key"
            class="pv-setting-row"
          >
            <label :for="`${p.manifest.id}-${item.key}`">{{ item.label }}</label>
            <input
              v-if="item.type === 'toggle'"
              :id="`${p.manifest.id}-${item.key}`"
              type="checkbox"
              :checked="settingValue(p, item) === true"
              @change="onSettingChange(p.manifest.id, item.key, ($event.target as HTMLInputElement).checked)"
            >
            <input
              v-else-if="item.type === 'number'"
              :id="`${p.manifest.id}-${item.key}`"
              class="pv-setting-input"
              type="number"
              :value="settingValue(p, item)"
              @change="onSettingChange(p.manifest.id, item.key, Number(($event.target as HTMLInputElement).value))"
            >
            <input
              v-else
              :id="`${p.manifest.id}-${item.key}`"
              class="pv-setting-input"
              type="text"
              :value="settingValue(p, item)"
              @change="onSettingChange(p.manifest.id, item.key, ($event.target as HTMLInputElement).value)"
            >
            <small v-if="item.description">{{ item.description }}</small>
          </div>
        </div>
        <ul
          v-if="pluginCommands(p.manifest.id).length > 0"
          class="pv-command-list"
        >
          <li
            v-for="command in pluginCommands(p.manifest.id)"
            :key="`${p.manifest.id}:${command.id}`"
          >
            <span>{{ command.label }}</span>
            <small v-if="commandStateText(p.manifest.id, command.id)">
              {{ commandStateText(p.manifest.id, command.id) }}
            </small>
          </li>
        </ul>
        <details class="pv-source">
          <summary>{{ t('plugins.viewSource') }}</summary>
          <pre>{{ p.source }}</pre>
        </details>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Puzzle,
  FolderOpen,
  RefreshCw,
  Loader2,
  AlertCircle,
  AlertTriangle,
  FileCode,
  Hash as HashIcon,
} from '@lucide/vue'
import { PluginService } from '@bindings/github.com/notevault/notevault/index.js'
import type { PluginInfo } from '@bindings/github.com/notevault/notevault/models.js'
import type { PluginSettingItem } from '@/plugins/types'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'

const { t } = useI18n()
const pluginRuntimeStore = usePluginRuntimeStore()
// 插件声明的设置 schema（#29），按 pluginId 索引
const pluginSettings = computed(() => pluginRuntimeStore.pluginSettings)
const plugins = ref<PluginInfo[]>([])
const pluginsDir = ref('')
const loading = ref(true)
const error = ref('')
// 等待用户二次确认的插件 ID（空表示没有待确认的授权）
const pendingGrantId = ref('')

async function loadPlugins() {
  loading.value = true
  error.value = ''
  try {
    pluginsDir.value = await PluginService.GetPluginsDir()
    plugins.value = (await PluginService.ListPlugins()) ?? []
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function onToggle(id: string, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  try {
    if (checked) {
      await PluginService.EnablePlugin(id)
    } else {
      await PluginService.DisablePlugin(id)
    }
    await Promise.all([loadPlugins(), pluginRuntimeStore.refreshPlugins()])
  } catch (e) {
    error.value = (e as Error).message
  }
}

// 设置项（#29）：schema 由插件声明，宿主渲染；值回传后由插件自己 saveData 持久化。
// 宿主不认识具体含义，只做「取值 → 渲染 → 回传」这一件事。
function settingValue(plugin: PluginInfo, item: PluginSettingItem): unknown {
  const settings = pluginRuntimeStore.pluginSettings[plugin.manifest.id]
  const value = settings?.values?.[item.key]
  return value === undefined ? item.default : value
}

function onSettingChange(pluginId: string, key: string, value: unknown): void {
  pluginRuntimeStore.updateSetting(pluginId, key, value)
}

// 授权 / 撤销后都必须 reload + refresh：
// reload 拿到新的 trustGranted，refresh 让运行时按新通道重启插件
// （授权变化不重启的话，撤销后插件仍跑在主进程）。
async function confirmGrant(id: string) {
  pendingGrantId.value = ''
  try {
    await PluginService.GrantTrust(id)
    await Promise.all([loadPlugins(), pluginRuntimeStore.refreshPlugins()])
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function revokeTrust(id: string) {
  try {
    await PluginService.RevokeTrust(id)
    await Promise.all([loadPlugins(), pluginRuntimeStore.refreshPlugins()])
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function openPluginsDir() {
  // Wails 没有暴露打开文件夹方法，简单复制路径到剪贴板
  try {
    await navigator.clipboard.writeText(pluginsDir.value)
    alert(t('plugins.dirCopied'))
  } catch {
    alert(pluginsDir.value)
  }
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  return `${(bytes / 1024 / 1024).toFixed(2)}MB`
}

function pluginCommands(pluginId: string) {
  return pluginRuntimeStore.commands.filter(command => command.pluginId === pluginId)
}

function commandState(pluginId: string, commandId: string) {
  return pluginRuntimeStore.commandStates[`${pluginId}:${commandId}`]
}

function commandStateText(pluginId: string, commandId: string): string {
  const state = commandState(pluginId, commandId)
  if (!state) return ''
  return t(`plugins.runtime.state${{ loading: 'Loading', ok: 'Ok', failed: 'Failed' }[state]}`)
}

const failedPluginNames = computed(() =>
  pluginRuntimeStore.failedPlugins.map(plugin => plugin.name),
)

onMounted(async () => {
  await Promise.all([loadPlugins(), pluginRuntimeStore.initialize()])
})
</script>

<style scoped>
.plugin-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-8);
  overflow-y: auto;
  background: var(--bg-content);
}

.pv-header {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.pv-header h1 {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--text-2xl);
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
}
.pv-sub {
  font-size: var(--text-sm);
  color: var(--text-muted);
  margin: 0;
}
.pv-header-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-2);
  flex-wrap: wrap;
}
.pv-dir {
  flex: 1;
  font-size: var(--text-xs);
  color: var(--text-muted);
  background: var(--bg-window);
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  word-break: break-all;
  font-family: var(--font-mono, monospace);
}

.pv-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  font-weight: 500;
  cursor: pointer;
  transition: all var(--transition-fast);
}
.pv-btn:hover {
  background: var(--bg-hover);
  color: var(--accent);
  border-color: var(--accent);
}
.pv-btn-ghost {
  background: transparent;
}

.pv-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-12);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.spin {
  animation: pv-spin 0.8s linear infinite;
}
@keyframes pv-spin {
  to { transform: rotate(360deg); }
}

.pv-error-banner {
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

.pv-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-12);
  text-align: center;
  color: var(--text-muted);
}
.pv-empty h3 {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0;
}
.pv-empty p {
  font-size: var(--text-sm);
  margin: 0;
  max-width: 480px;
}
.pv-empty-hint {
  font-size: var(--text-xs) !important;
  font-family: var(--font-mono, monospace);
  word-break: break-all;
}

.pv-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(380px, 1fr));
  gap: var(--space-3);
}

.pv-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  transition: border-color var(--transition-fast);
}
.pv-card.errored {
  border-color: #f59e0b;
}

.pv-card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.pv-card-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
}
.pv-version {
  font-size: var(--text-xs);
  color: var(--accent);
  background: rgba(0, 122, 255, 0.08);
  padding: 1px 6px;
  border-radius: 10px;
  font-weight: 500;
}
.pv-author {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.pv-card-desc {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  margin: 0;
}

.pv-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.pv-runtime-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.pv-badge {
  display: inline-flex;
  align-items: center;
  padding: 1px 8px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg-window);
  color: var(--text-muted);
  font-weight: 500;
}
.pv-badge-active {
  color: #16a34a;
  border-color: rgba(22, 163, 74, 0.35);
  background: rgba(22, 163, 74, 0.08);
}
.pv-badge-failed {
  color: #ef4444;
  border-color: rgba(239, 68, 68, 0.35);
  background: rgba(239, 68, 68, 0.08);
}

.pv-permissions {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
.pv-permissions-title {
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.pv-permission-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
}
.pv-permission {
  padding: 1px 6px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--bg-window);
  font-size: var(--text-xs);
  font-family: var(--font-mono, monospace);
  color: var(--text-secondary);
}
.pv-permission-empty,
.pv-command-list small {
  font-size: var(--text-xs);
  color: var(--text-muted);
}

.pv-command-list {
  margin: 0;
  padding: 0 0 0 18px;
  font-size: var(--text-sm);
  color: var(--text-secondary);
}
.pv-meta-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  word-break: break-all;
}

.pv-card-error {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-xs);
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.08);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
}

/* 插件设置（#29）：宿主按插件声明的 schema 渲染 */
.pv-settings {
  margin-top: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: var(--bg-secondary, rgba(255, 255, 255, 0.03));
  border-radius: var(--radius-sm);
}
.pv-settings-title {
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.pv-setting-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-1);
  font-size: var(--text-xs);
}
.pv-setting-row label {
  flex: 0 0 auto;
  color: var(--text-secondary);
}
.pv-setting-input {
  flex: 1;
  min-width: 0;
  font-size: var(--text-xs);
  background: transparent;
  color: var(--text-primary);
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.15));
  border-radius: var(--radius-sm);
  padding: 2px 6px;
}
.pv-setting-row small {
  flex-basis: 100%;
  color: var(--text-muted);
}

/* 信任等级：full-trust 是高风险操作，用红色系与权限区域区分开 */
.pv-trust {
  margin-top: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: rgba(239, 68, 68, 0.06);
  border: 1px solid rgba(239, 68, 68, 0.2);
  border-radius: var(--radius-sm);
}
.pv-trust-title {
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.pv-trust-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-1);
}
.pv-trust-badge {
  font-size: var(--text-xs);
  padding: 2px 8px;
  border-radius: var(--radius-sm);
}
.pv-trust-badge.is-pending {
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.12);
}
.pv-trust-badge.is-granted {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.12);
}
.pv-trust-note {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--text-secondary);
  line-height: 1.5;
}
.pv-btn-warn {
  color: #ef4444;
  border-color: rgba(239, 68, 68, 0.4);
}
.pv-btn-warn:hover {
  background: rgba(239, 68, 68, 0.1);
}
.pv-trust-confirm {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-2);
  padding: var(--space-2);
  background: rgba(239, 68, 68, 0.1);
  border-radius: var(--radius-sm);
  font-size: var(--text-xs);
  line-height: 1.6;
  color: #fca5a5;
}
.pv-trust-confirm-body {
  flex: 1;
}
.pv-trust-confirm-body strong {
  display: block;
  color: #fecaca;
}
.pv-trust-confirm-body ul {
  margin: var(--space-1) 0 0;
  padding-left: 18px;
}
.pv-trust-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-2);
}

.pv-source {
  margin-top: var(--space-1);
}
.pv-source summary {
  font-size: var(--text-xs);
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
}
.pv-source summary:hover {
  color: var(--accent);
}
.pv-source pre {
  margin: var(--space-2) 0 0;
  padding: var(--space-3);
  background: var(--bg-sidebar, #1e1e1e);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: var(--text-xs);
  font-family: var(--font-mono, monospace);
  color: var(--text-primary);
  max-height: 240px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 切换开关（与 SettingsView 一致） */
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

@media (max-width: 720px) {
  .pv-list {
    grid-template-columns: 1fr;
  }
}
</style>
