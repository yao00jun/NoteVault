<script setup lang="ts">
import { computed, ref } from 'vue'
import { Tag, Plus, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  tags: string[]
  /** 无打开文档时隐藏 */
  visible: boolean
}>()

const emit = defineEmits<{
  'update:tags': [tags: string[]]
}>()

const { t } = useI18n()
const input = ref('')
const inputVisible = ref(false)

const normalizedTags = computed(() => props.tags)

function removeTag(tag: string) {
  emit('update:tags', normalizedTags.value.filter((item) => item !== tag))
}

function commitInput() {
  const value = input.value.trim()
  if (!value) {
    inputVisible.value = false
    return
  }
  // 支持一次粘贴多个（逗号/分号分隔）
  const parts = value.split(/[,，;；]/).map((s) => s.trim()).filter(Boolean)
  const next = [...normalizedTags.value]
  for (const part of parts) {
    if (!next.includes(part)) next.push(part)
  }
  emit('update:tags', next)
  input.value = ''
  inputVisible.value = false
}

function showInput() {
  inputVisible.value = true
}
</script>

<template>
  <div
    v-if="visible"
    class="doc-properties"
    data-testid="doc-properties"
  >
    <Tag
      :size="13"
      class="properties-icon"
    />
    <span class="properties-label">{{ t('editor.properties.tags') }}</span>

    <div class="tags-chips">
      <span
        v-for="tag in normalizedTags"
        :key="tag"
        class="tag-chip"
        data-testid="doc-tag-chip"
      >
        <span class="chip-text">{{ tag }}</span>
        <button
          class="chip-remove"
          :title="t('editor.properties.removeTag')"
          @click="removeTag(tag)"
        >
          <X :size="11" />
        </button>
      </span>

      <button
        v-if="!inputVisible"
        class="chip-add"
        data-testid="doc-tag-add"
        :title="t('editor.properties.addTag')"
        @click="showInput"
      >
        <Plus :size="11" />
      </button>
      <input
        v-else
        v-model="input"
        class="chip-input"
        data-testid="doc-tag-input"
        type="text"
        :placeholder="t('editor.properties.tagPlaceholder')"
        autofocus
        @keydown.enter.prevent="commitInput"
        @keydown.esc="inputVisible = false"
        @blur="commitInput"
      >
    </div>
  </div>
</template>

<style scoped>
.doc-properties {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-3);
  border-bottom: 1px solid var(--border-light);
  background: var(--bg-sidebar);
  min-height: 30px;
}

.properties-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.properties-label {
  font-size: var(--text-xs);
  color: var(--text-muted);
  flex-shrink: 0;
}

.tags-chips {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-1);
  min-width: 0;
}

.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 1px 4px 1px 8px;
  border-radius: 10px;
  background: var(--bg-active);
  color: var(--accent);
  font-size: var(--text-xs);
  max-width: 200px;
}

.chip-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chip-remove {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  color: var(--text-muted);
  flex-shrink: 0;
  transition: background var(--transition-fast), color var(--transition-fast);
}

.chip-remove:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.chip-add {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  border: 1px dashed var(--border);
  color: var(--text-muted);
  flex-shrink: 0;
  transition: border-color var(--transition-fast), color var(--transition-fast);
}

.chip-add:hover {
  border-color: var(--accent);
  color: var(--accent);
}

.chip-input {
  width: 120px;
  height: 20px;
  padding: 0 8px;
  border: 1px solid var(--accent);
  border-radius: 10px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-xs);
  outline: none;
}
</style>
