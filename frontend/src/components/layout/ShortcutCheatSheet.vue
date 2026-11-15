<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { X } from '@lucide/vue'
import { useShortcuts } from '@/composables/useShortcuts'
import { isImeComposing } from '@/utils/ime'

defineProps<{ visible: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { groups } = useShortcuts()

function close() {
  emit('close')
}
function onKeydown(event: KeyboardEvent) {
  if (isImeComposing(event)) return
  if (event.key === 'Escape') {
    event.preventDefault()
    close()
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible"
      class="cheatsheet-overlay"
      role="dialog"
      aria-modal="true"
      :aria-label="t('cheatsheet.title')"
      @click.self="close"
      @keydown.esc.stop="close"
    >
      <section class="cheatsheet-panel">
        <header class="cheatsheet-header">
          <div>
            <h2>{{ t('cheatsheet.title') }}</h2>
            <p>{{ t('cheatsheet.hint') }}</p>
          </div>
          <button
            type="button"
            class="cheatsheet-close"
            :aria-label="t('common.close')"
            @click="close"
          >
            <X :size="16" />
          </button>
        </header>
        <div class="cheatsheet-body">
          <section
            v-for="group in groups"
            :key="group.id"
            class="cheatsheet-group"
          >
            <h3>{{ group.label }}</h3>
            <ul>
              <li
                v-for="entry in group.shortcuts"
                :key="entry.keys + entry.desc"
              >
                <kbd>{{ entry.keys }}</kbd>
                <span>{{ entry.desc }}</span>
              </li>
            </ul>
          </section>
        </div>
        <footer class="cheatsheet-footer">
          <button
            type="button"
            class="cheatsheet-close-btn"
            @click="close"
          >
            {{ t('common.close') }}
          </button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.cheatsheet-overlay { position: fixed; inset: 0; z-index: 996; display: grid; place-items: center; padding: 24px; background: var(--overlay-bg, rgb(15 20 24 / .45)); backdrop-filter: blur(4px); }
.cheatsheet-panel { width: min(560px, 100%); max-height: calc(100dvh - 64px); display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--radius-lg, 14px); background: var(--bg-card); color: var(--text-primary); box-shadow: 0 20px 60px rgb(0 0 0 / .25); overflow: hidden; }
.cheatsheet-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 18px 20px 12px; }
.cheatsheet-header h2 { margin: 0; font-size: 17px; font-weight: 650; }
.cheatsheet-header p { margin: 4px 0 0; font-size: 12px; color: var(--text-secondary); }
.cheatsheet-close { display: grid; place-items: center; width: 28px; height: 28px; border: none; border-radius: var(--radius-sm, 6px); background: transparent; color: var(--text-muted); cursor: pointer; }
.cheatsheet-close:hover { background: var(--bg-hover); color: var(--text-primary); }
.cheatsheet-body { padding: 0 20px; overflow: auto; }
.cheatsheet-group + .cheatsheet-group { margin-top: 14px; }
.cheatsheet-group h3 { margin: 0 0 8px; font-size: 12px; font-weight: 600; color: var(--text-secondary); }
.cheatsheet-group ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
.cheatsheet-group li { display: flex; align-items: center; gap: 10px; font-size: 12px; color: var(--text-secondary); }
.cheatsheet-group kbd { flex-shrink: 0; min-width: 72px; text-align: center; padding: 3px 8px; border: 1px solid var(--border); border-bottom-width: 2px; border-radius: 6px; background: var(--bg-content); font-family: inherit; font-size: 11px; color: var(--text-primary); }
.cheatsheet-footer { padding: 10px 20px 16px; display: flex; justify-content: flex-end; }
.cheatsheet-close-btn { padding: 6px 14px; border: 1px solid var(--border); border-radius: var(--radius-sm, 6px); background: transparent; color: var(--text-secondary); font-size: 12px; cursor: pointer; }
.cheatsheet-close-btn:hover { color: var(--text-primary); }
</style>
