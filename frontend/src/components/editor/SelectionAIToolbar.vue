<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Sparkles, Wand2, Languages, Lightbulb, PenLine } from '@lucide/vue'

defineProps<{ floatingStyle: { left: string; top: string } }>()
const emit = defineEmits<{ action: [kind: 'polish' | 'translate' | 'explain' | 'continue'] }>()
const { t } = useI18n()

const actions = [
  { kind: 'polish' as const, icon: Wand2, label: t('copilot.sel.polish') },
  { kind: 'translate' as const, icon: Languages, label: t('copilot.sel.translate') },
  { kind: 'explain' as const, icon: Lightbulb, label: t('copilot.sel.explain') },
  { kind: 'continue' as const, icon: PenLine, label: t('copilot.sel.continue') },
]
</script>

<template>
  <div
    class="selection-ai-toolbar"
    :style="floatingStyle"
  >
    <span class="selection-ai-brand"><Sparkles :size="12" />AI</span>
    <button
      v-for="action in actions"
      :key="action.kind"
      type="button"
      class="selection-ai-btn"
      :data-testid="`selection-ai-${action.kind}`"
      :title="action.label"
      @click="emit('action', action.kind)"
    >
      <component
        :is="action.icon"
        :size="13"
      />{{ action.label }}
    </button>
  </div>
</template>

<style scoped>
.selection-ai-toolbar {
  position: fixed;
  z-index: 60;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 3px 6px;
  border: 1px solid var(--border-accent, var(--accent, #eab308));
  border-radius: var(--radius-md, 8px);
  background: var(--bg-card, #232323);
  box-shadow: 0 6px 20px rgb(0 0 0 / 25%);
}
.selection-ai-brand {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 0 4px 0 2px;
  color: var(--accent, #eab308);
  font-size: 10px;
  font-weight: 700;
}
.selection-ai-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 3px 7px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text-secondary, #ccc);
  font-size: 11px;
  cursor: pointer;
  white-space: nowrap;
}
.selection-ai-btn:hover {
  background: color-mix(in srgb, var(--accent, #eab308) 14%, transparent);
  color: var(--accent, #eab308);
}
</style>
