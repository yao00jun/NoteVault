<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, FileText, PanelLeft, Search } from '@lucide/vue'
import type { ThemeType } from '@/types'

const props = withDefaults(
  defineProps<{
    theme: ThemeType
    selected?: boolean
  }>(),
  { selected: false }
)

const emit = defineEmits<{ select: [theme: ThemeType] }>()
const { t } = useI18n()
const themes: Record<ThemeType, { label: string; description: string }> = {
  macos: { label: 'macOS', description: 'titlebar.themes.macos' },
  winui: { label: 'WinUI', description: 'titlebar.themes.winui' },
  'islands-dark': { label: 'Islands Dark', description: 'titlebar.themes.islandsDark' },
}
const appearance = computed(() => themes[props.theme])
</script>

<template>
  <button
    type="button"
    class="theme-preview"
    :class="{ selected }"
    :aria-pressed="selected"
    :aria-label="`${appearance.label} — ${t(appearance.description)}`"
    @click="emit('select', theme)"
  >
    <span
      class="preview-scene"
      :data-theme="theme"
      aria-hidden="true"
    >
      <span class="preview-toolbar">
        <PanelLeft :size="12" />
        <span class="preview-brand">NoteVault</span>
        <Search :size="12" />
      </span>
      <span class="preview-layout">
        <span class="preview-navigation">
          <span class="preview-nav-item current"><PanelLeft :size="13" /></span>
          <span class="preview-nav-item"><FileText :size="13" /></span>
          <span class="preview-nav-item"><Search :size="13" /></span>
        </span>
        <span class="preview-content">
          <span class="preview-heading">
            <span class="preview-line" />
            <span class="preview-action" />
          </span>
          <span class="preview-tabs">
            <span class="preview-tab current" />
            <span class="preview-tab" />
            <span class="preview-tab" />
          </span>
          <span class="preview-task">
            <span class="preview-checkbox"><Check :size="8" /></span>
            <span class="preview-line" />
          </span>
          <span class="preview-task">
            <span class="preview-checkbox pending" />
            <span class="preview-line short" />
          </span>
        </span>
      </span>
    </span>
    <span class="preview-caption">
      <span class="preview-label">{{ appearance.label }}</span>
      <span
        v-if="selected"
        class="preview-selected"
        aria-hidden="true"
      ><Check :size="14" /></span>
    </span>
    <span class="preview-description">{{ t(appearance.description) }}</span>
  </button>
</template>

<style scoped>
.theme-preview {
  display: flex;
  flex-direction: column;
  min-width: 0;
  width: 100%;
  padding: 10px;
  text-align: left;
  color: var(--text-primary);
  background: var(--surface-panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  transition:
    border-color var(--transition-fast),
    box-shadow var(--transition-fast);
}

.theme-preview:hover {
  border-color: var(--border-accent);
  box-shadow: var(--shadow-sm);
}

.theme-preview.selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent);
}

.theme-preview:focus-visible {
  outline: 2px solid var(--focus-color);
  outline-offset: 4px;
}

.preview-scene {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 132px;
  overflow: hidden;
  color: var(--text-secondary);
  background: var(--bg-desktop);
  border: 1px solid var(--border);
  border-radius: 7px;
  font-family: var(--font-sans);
}

.preview-toolbar {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 26px;
  padding: 3px 8px;
  background: var(--surface-chrome);
}

.preview-brand {
  flex: 1;
  text-align: left;
  font-size: 12px;
  font-weight: 550;
  line-height: 1;
}

.preview-layout {
  display: flex;
  flex: 1;
  min-height: 0;
}

.preview-navigation {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  gap: 3px;
  width: 33px;
  padding: 4px;
  background: var(--surface-chrome);
}

.preview-nav-item {
  display: flex;
  position: relative;
  align-items: center;
  justify-content: center;
  min-height: 23px;
  border-radius: 3px;
}

.preview-nav-item.current {
  color: var(--selection-text);
  background: var(--selection-bg);
}

.preview-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  gap: 5px;
  min-width: 0;
  padding: 8px;
  background: var(--bg-content);
}

.preview-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-height: 10px;
}

.preview-line {
  display: block;
  width: 66%;
  height: 4px;
  background: var(--text-muted);
  border-radius: 2px;
  opacity: 0.5;
}

.preview-heading > .preview-line {
  width: 48%;
  height: 5px;
  background: var(--text-primary);
  opacity: 0.75;
}

.preview-line.short {
  width: 44%;
}

.preview-action {
  width: 22px;
  height: 10px;
  border-radius: var(--control-radius);
  background: var(--accent);
}

.preview-tabs {
  display: flex;
  gap: 3px;
  min-height: 13px;
  margin-bottom: 2px;
  border-bottom: 1px solid var(--border);
}

.preview-tab {
  flex: 1;
  min-width: 0;
  border-bottom: 2px solid transparent;
}

.preview-tab.current {
  border-bottom-color: var(--accent);
}

.preview-task {
  display: flex;
  align-items: center;
  gap: 5px;
  min-height: 19px;
  padding: 4px;
  background: var(--surface-panel);
  border: 1px solid var(--border-light);
  border-radius: 3px;
}

.preview-checkbox {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 9px;
  height: 9px;
  flex-shrink: 0;
  color: var(--accent-contrast);
  background: var(--accent);
  border: 1px solid var(--accent);
  border-radius: 2px;
}

.preview-checkbox.pending {
  background: transparent;
  border-color: var(--text-muted);
}

.preview-scene[data-theme='macos'] {
  background: linear-gradient(135deg, #d7e5f5, #eae4f5);
  border-color: #d2d8e2;
}

.preview-scene[data-theme='macos'] .preview-content {
  margin: 1px 4px 4px 0;
  border-radius: 7px;
  box-shadow: 0 2px 5px #35425f14;
}

.preview-scene[data-theme='macos'] .preview-nav-item {
  border-radius: 7px;
}

.preview-scene[data-theme='macos'] .preview-tabs {
  padding: 2px;
  border: 0;
  border-radius: 5px;
  background: var(--surface-inset);
}

.preview-scene[data-theme='macos'] .preview-tab.current {
  border: 0;
  border-radius: 3px;
  background: #ffffff;
  box-shadow: 0 1px 3px #0003;
}

.preview-scene[data-theme='winui'] .preview-content {
  border-top: 1px solid var(--border);
  border-left: 1px solid var(--border);
  border-top-left-radius: 5px;
}

.preview-scene[data-theme='winui'] .preview-nav-item.current::before {
  content: '';
  position: absolute;
  left: 0;
  width: 2px;
  height: 11px;
  border-radius: 1px;
  background: var(--accent);
}

.preview-scene[data-theme='islands-dark'] {
  gap: 4px;
  padding: 4px;
}

.preview-scene[data-theme='islands-dark'] .preview-toolbar,
.preview-scene[data-theme='islands-dark'] .preview-navigation,
.preview-scene[data-theme='islands-dark'] .preview-content {
  border: 1px solid var(--border-light);
  border-radius: 5px;
}

.preview-scene[data-theme='islands-dark'] .preview-toolbar {
  min-height: 22px;
}

.preview-scene[data-theme='islands-dark'] .preview-brand {
  font-family: var(--font-metadata);
}

.preview-scene[data-theme='islands-dark'] .preview-layout {
  gap: 4px;
}

.preview-scene[data-theme='islands-dark'] .preview-content {
  gap: 4px;
  padding: 6px;
}

.preview-scene[data-theme='islands-dark'] .preview-tabs {
  border: 0;
}

.preview-scene[data-theme='islands-dark'] .preview-tab.current {
  border-bottom: 0;
  border-top: 2px solid var(--accent);
  border-radius: 3px;
  background: var(--bg-card);
}

.preview-caption {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 22px;
  margin-top: 10px;
}

.preview-label {
  font-size: var(--text-sm);
  font-weight: 600;
}

.preview-selected {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  color: var(--accent-contrast);
  background: var(--accent);
  border-radius: 50%;
}

.preview-description {
  margin-top: 3px;
  color: var(--text-secondary);
  font-size: var(--text-xs);
  line-height: 1.5;
  overflow-wrap: anywhere;
}

@media (prefers-reduced-motion: reduce) {
  .theme-preview {
    transition: none;
  }
}

@media (forced-colors: active) {
  .theme-preview.selected {
    outline: 2px solid Highlight;
    outline-offset: -3px;
  }
}
</style>
