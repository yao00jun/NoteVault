<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { Plus, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { COMMON_FOLDER_PATHS, normalizeFolderDisplayNames, normalizeFolderDisplayPath } from '@/utils/folderDisplayNames'

const props = defineProps<{ modelValue: Record<string, string> }>()
const emit = defineEmits<{ 'update:modelValue': [names: Record<string, string>] }>()
const { t } = useI18n()
const headingId = useId()
const draftPath = ref('')
const draftName = ref('')
const names = computed(() => normalizeFolderDisplayNames(props.modelValue))
const commonPaths: readonly string[] = COMMON_FOLDER_PATHS
const paths = computed(() => [
  ...commonPaths,
  ...Object.keys(names.value).filter(path => !commonPaths.includes(path)).sort(),
])
const validPath = computed(() => normalizeFolderDisplayPath(draftPath.value))
const canAdd = computed(() => !!validPath.value && !!draftName.value.trim())

function updateName(path: string, name: string) {
  emit('update:modelValue', normalizeFolderDisplayNames({ ...names.value, [path]: name }))
}

function changeName(path: string, event: Event) {
  updateName(path, (event.target as HTMLInputElement).value)
}

function addMapping() {
  if (!canAdd.value) return
  updateName(validPath.value, draftName.value)
  draftPath.value = ''
  draftName.value = ''
}

function useLocalizedNames() {
  const defaults = Object.fromEntries(COMMON_FOLDER_PATHS.map(path => [path, t(`settings.editor.folderDisplayNames.defaults.${path}`)]))
  emit('update:modelValue', { ...names.value, ...defaults })
}
</script>

<template>
  <section
    class="folder-display-names"
    data-testid="folder-display-names"
    :aria-labelledby="headingId"
  >
    <h4 :id="headingId">
      {{ t('settings.editor.folderDisplayNames.title') }}
    </h4>
    <p class="mapping-description">
      {{ t('settings.editor.folderDisplayNames.description') }}
    </p>
    <div class="mapping-actions">
      <button
        type="button"
        data-testid="folder-alias-preset"
        @click="useLocalizedNames"
      >
        {{ t('settings.editor.folderDisplayNames.useLocalized') }}
      </button>
      <button
        type="button"
        data-testid="folder-alias-reset"
        :disabled="Object.keys(names).length === 0"
        @click="emit('update:modelValue', {})"
      >
        {{ t('settings.editor.folderDisplayNames.reset') }}
      </button>
    </div>
    <div class="mapping-list">
      <div
        v-for="path in paths"
        :key="path"
        class="mapping-row"
        :data-folder-path="path"
      >
        <label class="mapping-field">
          <code :title="path">{{ path }}</code>
          <input
            type="text"
            :value="names[path] || ''"
            :placeholder="path.split('/').pop()"
            :aria-label="t('settings.editor.folderDisplayNames.nameFor', { path })"
            @change="changeName(path, $event)"
          >
        </label>
        <button
          v-if="!commonPaths.includes(path)"
          type="button"
          class="mapping-remove"
          :title="t('settings.editor.folderDisplayNames.remove', { path })"
          @click="updateName(path, '')"
        >
          <X :size="14" />
        </button>
      </div>
    </div>
    <form
      class="mapping-form"
      data-testid="folder-alias-form"
      @submit.prevent="addMapping"
    >
      <label>
        <span>{{ t('settings.editor.folderDisplayNames.path') }}</span>
        <input
          v-model="draftPath"
          type="text"
          data-testid="folder-alias-path"
          :placeholder="t('settings.editor.folderDisplayNames.pathPlaceholder')"
          :aria-invalid="!!draftPath.trim() && !validPath"
        >
      </label>
      <label>
        <span>{{ t('settings.editor.folderDisplayNames.name') }}</span>
        <input
          v-model="draftName"
          type="text"
          data-testid="folder-alias-name"
        >
      </label>
      <button
        type="submit"
        data-testid="folder-alias-add"
        :disabled="!canAdd"
      >
        <Plus :size="14" />
        {{ t('settings.editor.folderDisplayNames.add') }}
      </button>
    </form>
    <p
      v-if="draftPath.trim() && !validPath"
      class="mapping-error"
      role="status"
    >
      {{ t('settings.editor.folderDisplayNames.invalidPath') }}
    </p>
  </section>
</template>

<style scoped>
.folder-display-names {
  margin: var(--space-4) 0;
  padding: var(--space-4) 0;
  border-block: 1px solid var(--border-light);
  min-width: 0;
}
h4 {
  margin: 0 0 var(--space-2);
  color: var(--text-primary);
  font-size: var(--text-sm);
  font-weight: 600;
}
.mapping-description,
.mapping-error {
  margin: 0 0 var(--space-3);
  color: var(--text-secondary);
  font-size: var(--text-xs);
  line-height: 1.6;
}
.mapping-actions,
.mapping-row,
.mapping-form {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.mapping-actions { flex-wrap: wrap; margin-bottom: var(--space-3); }
.mapping-list { display: grid; gap: var(--space-2); }
.mapping-field {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.mapping-field code {
  flex: 1 1 120px;
  min-width: 0;
  color: var(--text-secondary);
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
input {
  flex: 1 1 160px;
  width: 100%;
  min-width: 0;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text-primary);
  font: inherit;
  font-size: var(--text-sm);
}
input::placeholder { color: var(--text-secondary); }
button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-1);
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-primary);
  font-size: var(--text-xs);
  cursor: pointer;
}
button:hover:not(:disabled) { background: var(--bg-hover); }
button:disabled { opacity: .5; cursor: default; }
button:focus-visible,
input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.mapping-remove { flex-shrink: 0; padding: 6px; }
.mapping-form { flex-wrap: wrap; align-items: end; margin-top: var(--space-3); }
.mapping-form label {
  flex: 1 1 140px;
  display: grid;
  gap: var(--space-1);
  min-width: 0;
  color: var(--text-secondary);
  font-size: var(--text-xs);
}
.mapping-error { margin: var(--space-2) 0 0; color: var(--text-primary); }
</style>
