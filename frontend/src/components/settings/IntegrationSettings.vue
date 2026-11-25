<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { useWorkspaceStore } from '@/stores/workspace'
import { ClipperService } from '@/api'
import type { ClipperStatus } from '@/api'
import { useToast } from '@/composables/useToast'

// 设置页的「集成」分组：错误监控 / MCP 接入 / Web Clipper。
// 这三块共享一种形态——读后端状态或生成配置文本、复制到剪贴板，与设置页其余
// 区块（外观、编辑器、AI）没有数据往来，所以整体搬成子组件而不是用 props 传参。
//
// 与父视图的契约只有两条，改这里时必须保住：
// 1. 每个区块根元素保留 `id="settings-section-<key>"`——父视图的左侧导航用
//    document.getElementById 滚动定位，key 必须与 sectionGroups 里的 children 一致；
// 2. 设置项仍然直接读写 settingsStore（pinia 单例），不要改成 props/emit 透传，
//    否则父视图又得把整棵设置树接回来。
//
// 样式来自 settings-section.css（与父视图各自 <style scoped src> 引入同一份定义）。

const { t } = useI18n()
const toast = useToast()
const settingsStore = useSettingsStore()
const workspaceStore = useWorkspaceStore()

// MCP 接入：为 notevault-mcp 生成客户端接入配置，工作区路径按当前工作区填入
const mcpCliCommand = computed(() => {
  const ws = workspaceStore.currentWorkspace?.path || t('settings.mcp.workspacePlaceholder')
  const write = settingsStore.settings.mcp.enableWrite ? ' --enable-write' : ''
  return `claude mcp add notevault -- notevault-mcp --workspace "${ws}"${write}`
})
const mcpJsonConfig = computed(() => {
  const ws = workspaceStore.currentWorkspace?.path || t('settings.mcp.workspacePlaceholder')
  const args = ['--workspace', ws]
  if (settingsStore.settings.mcp.enableWrite) args.push('--enable-write')
  return JSON.stringify({ mcpServers: { notevault: { command: 'notevault-mcp', args } } }, null, 2)
})
async function copyMcpConfig(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(t('settings.mcp.copied'))
  } catch {
    toast.error(t('settings.mcp.copyFailed'))
  }
}

// Web Clipper：读取后端运行状态与 Token（后端 Status() 已暴露，Token 供填入浏览器扩展）
const clipperStatus = ref<ClipperStatus | null>(null)
const clipperLoading = ref(false)
async function loadClipperStatus() {
  clipperLoading.value = true
  try {
    clipperStatus.value = await ClipperService.Status()
  } catch (e) {
    clipperStatus.value = null
    toast.error(t('settings.clipper.loadFailed', { msg: (e as Error).message }))
  } finally {
    clipperLoading.value = false
  }
}
async function copyClipperToken() {
  const token = clipperStatus.value?.token
  if (!token) return
  try {
    await navigator.clipboard.writeText(token)
    toast.success(t('settings.clipper.tokenCopied'))
  } catch {
    toast.error(t('settings.clipper.copyFailed'))
  }
}
const clipperEndpoint = computed(() =>
  clipperStatus.value ? `http://127.0.0.1:${clipperStatus.value.port}/api/clip` : ''
)

onMounted(() => {
  void loadClipperStatus()
})
</script>

<template>
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

  <div
    id="settings-section-mcp"
    class="settings-section"
  >
    <h3 class="section-title">
      {{ t('settings.mcp.title') }}
    </h3>
    <p class="section-desc">
      {{ t('settings.mcp.desc') }}
    </p>
    <div class="setting-item">
      <div class="setting-info">
        <span class="setting-label">{{ t('settings.mcp.enableWrite') }}</span>
        <span class="setting-desc">{{ t('settings.mcp.enableWriteDesc') }}</span>
      </div>
      <label class="toggle-switch">
        <input
          v-model="settingsStore.settings.mcp.enableWrite"
          type="checkbox"
        >
        <span class="toggle-slider" />
      </label>
    </div>
    <div class="setting-item">
      <div class="setting-info">
        <span class="setting-label">{{ t('settings.mcp.cli') }}</span>
        <span class="setting-desc">{{ t('settings.mcp.cliDesc') }}</span>
      </div>
      <button
        type="button"
        class="preset-btn"
        data-testid="mcp-copy-cli"
        @click="copyMcpConfig(mcpCliCommand)"
      >
        {{ t('settings.mcp.copy') }}
      </button>
    </div>
    <pre
      class="mcp-snippet"
      data-testid="mcp-cli-snippet"
    >{{ mcpCliCommand }}</pre>
    <div class="setting-item">
      <div class="setting-info">
        <span class="setting-label">{{ t('settings.mcp.json') }}</span>
        <span class="setting-desc">{{ t('settings.mcp.jsonDesc') }}</span>
      </div>
      <button
        type="button"
        class="preset-btn"
        data-testid="mcp-copy-json"
        @click="copyMcpConfig(mcpJsonConfig)"
      >
        {{ t('settings.mcp.copy') }}
      </button>
    </div>
    <pre
      class="mcp-snippet"
      data-testid="mcp-json-snippet"
    >{{ mcpJsonConfig }}</pre>
    <p class="section-desc">
      {{ t('settings.mcp.buildHint') }}
    </p>
  </div>

  <div
    id="settings-section-clipper"
    class="settings-section"
  >
    <h3 class="section-title">
      {{ t('settings.clipper.title') }}
    </h3>
    <p class="section-desc">
      {{ t('settings.clipper.desc') }}
    </p>
    <div class="setting-item">
      <div class="setting-info">
        <span class="setting-label">{{ t('settings.clipper.status') }}</span>
        <span class="setting-desc">{{ t('settings.clipper.statusDesc') }}</span>
      </div>
      <span
        class="clipper-status-badge"
        :class="{ running: clipperStatus?.running }"
        data-testid="clipper-status"
      >{{ clipperStatus?.running ? t('settings.clipper.running') : t('settings.clipper.stopped') }}</span>
    </div>
    <div class="setting-item">
      <div class="setting-info">
        <span class="setting-label">{{ t('settings.clipper.endpoint') }}</span>
        <span class="setting-desc">{{ t('settings.clipper.endpointDesc') }}</span>
      </div>
      <code
        class="mcp-snippet"
        data-testid="clipper-endpoint"
      >{{ clipperEndpoint || '—' }}</code>
    </div>
    <div class="setting-item">
      <div class="setting-info">
        <span class="setting-label">{{ t('settings.clipper.token') }}</span>
        <span class="setting-desc">{{ t('settings.clipper.tokenDesc') }}</span>
      </div>
      <button
        type="button"
        class="preset-btn"
        data-testid="clipper-copy-token"
        :disabled="!clipperStatus?.token"
        @click="copyClipperToken"
      >
        {{ t('settings.clipper.copyToken') }}
      </button>
    </div>
    <code
      class="mcp-snippet"
      data-testid="clipper-token"
    >{{ clipperStatus?.token || (clipperLoading ? '…' : '—') }}</code>
    <p class="section-desc">
      {{ t('settings.clipper.hint') }}
    </p>
  </div>
</template>

<style scoped src="./settings-section.css"></style>
