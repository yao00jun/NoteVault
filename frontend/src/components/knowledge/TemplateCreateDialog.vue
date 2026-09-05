<template>
  <Teleport to="body">
    <div
      class="tcd-overlay"
      @click.self="emit('close')"
    >
      <div class="tcd-dialog">
        <div class="tcd-header">
          <FileText :size="16" />
          <span>{{ t('templates.createTitle') }}</span>
        </div>

        <!-- 加载中 -->
        <div
          v-if="loading"
          class="tcd-hint"
        >
          {{ t('templates.loading') }}
        </div>

        <!-- 空状态：引导用户创建 Templates 目录 -->
        <template v-else-if="templates.length === 0">
          <div class="tcd-hint">
            {{ t('templates.empty') }}
          </div>
          <div class="tcd-path">
            {{ workspacePath }}/Templates/
          </div>
          <div class="tcd-actions">
            <button
              class="tcd-btn"
              @click="emit('close')"
            >
              {{ t('templates.close') }}
            </button>
          </div>
        </template>

        <!-- 正常表单 -->
        <template v-else>
          <div class="tcd-field">
            <label>{{ t('templates.selectLabel') }}</label>
            <select
              v-model="selectedName"
              @change="onTemplateChange"
            >
              <option
                v-for="tpl in templates"
                :key="tpl.name"
                :value="tpl.name"
              >
                {{ tpl.name }}
              </option>
            </select>
          </div>

          <!-- 自定义变量（模板里出现且非内置的占位符） -->
          <div
            v-if="currentVariables.length"
            class="tcd-vars"
          >
            <div
              v-for="name in currentVariables"
              :key="name"
              class="tcd-field"
            >
              <label>{{ t('templates.variableLabel', { name }) }}</label>
              <input
                v-model="variableValues[name]"
                type="text"
                class="tcd-input"
                @keyup.enter="doCreate"
              >
            </div>
          </div>

          <div class="tcd-field">
            <label>{{ t('templates.targetLabel') }}</label>
            <input
              v-model="targetPath"
              type="text"
              class="tcd-input tcd-target"
              @keyup.enter="doCreate"
            >
          </div>

          <div class="tcd-note">
            {{ t('templates.builtinHint') }}
          </div>

          <div class="tcd-actions">
            <button
              class="tcd-btn"
              @click="emit('close')"
            >
              {{ t('templates.cancel') }}
            </button>
            <button
              class="tcd-btn tcd-btn-primary"
              :disabled="creating || !selectedName"
              @click="doCreate"
            >
              <Loader2
                v-if="creating"
                :size="14"
                class="spin"
              />
              <span>{{ t('templates.create') }}</span>
            </button>
          </div>
        </template>

        <!-- 错误提示在所有分支下都可见（含空态时的列表加载失败） -->
        <div
          v-if="errorMsg"
          class="tcd-error"
        >
          {{ errorMsg }}
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileText, Loader2 } from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
import { TemplateService } from '@bindings/github.com/notevault/notevault/index.js'
import type { TemplateInfo } from '@bindings/github.com/notevault/notevault/models.js'

const emit = defineEmits<{
  close: []
  created: [path: string]
}>()

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()

const templates = ref<TemplateInfo[]>([])
const selectedName = ref('')
const variableValues = ref<Record<string, string>>({})
const targetPath = ref('')
const loading = ref(true)
const creating = ref(false)
const errorMsg = ref('')

const workspacePath = computed(() => workspaceStore.currentWorkspace?.path ?? '')
const currentVariables = computed(() => {
  const tpl = templates.value.find(tp => tp.name === selectedName.value)
  return tpl?.variables ?? []
})

/** 选中模板时：重置变量值，目标路径默认为「模板名.md」。 */
function syncSelection(): void {
  variableValues.value = {}
  targetPath.value = selectedName.value ? `${selectedName.value}.md` : ''
}

onMounted(async () => {
  if (!workspacePath.value) {
    loading.value = false
    return
  }
  try {
    // 绑定层对切片模型统一标可空，先过滤再入表
    templates.value = ((await TemplateService.ListTemplates(workspacePath.value)) ?? []).filter(
      (tp): tp is TemplateInfo => tp !== null,
    )
    if (templates.value.length > 0) {
      selectedName.value = templates.value[0]!.name
      syncSelection()
    }
  } catch (e) {
    errorMsg.value = t('templates.loadFailed', { msg: (e as Error).message })
  } finally {
    loading.value = false
  }
})

function onTemplateChange(): void {
  syncSelection()
  errorMsg.value = ''
}

async function doCreate(): Promise<void> {
  if (!workspacePath.value || !selectedName.value || creating.value) return
  creating.value = true
  errorMsg.value = ''
  try {
    const node = await TemplateService.CreateFromTemplate(
      workspacePath.value,
      selectedName.value,
      targetPath.value.trim(),
      { ...variableValues.value },
    )
    if (node) {
      emit('created', node.path)
    }
  } catch (e) {
    errorMsg.value = t('templates.createFailed', { msg: (e as Error).message })
  } finally {
    creating.value = false
  }
}
</script>

<style scoped>
.tcd-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.4);
  padding: var(--space-4);
}
.tcd-dialog {
  width: 420px;
  max-width: 100%;
  max-height: 80vh;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
}
.tcd-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-base);
  font-weight: 700;
  color: var(--text-primary);
}
.tcd-header > svg {
  color: var(--accent);
}
.tcd-hint {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  line-height: 1.6;
}
.tcd-path {
  font-size: var(--text-xs);
  color: var(--accent);
  word-break: break-all;
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}
.tcd-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.tcd-field label {
  font-size: var(--text-xs);
  color: var(--text-muted);
}
.tcd-field select,
.tcd-input {
  height: 32px;
  padding: 0 var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: var(--text-sm);
  outline: none;
}
.tcd-field select:focus,
.tcd-input:focus {
  border-color: var(--accent);
}
.tcd-vars {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3);
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.tcd-note {
  font-size: var(--text-xs);
  color: var(--text-muted);
  line-height: 1.5;
}
.tcd-error {
  font-size: var(--text-sm);
  color: #ef4444;
  word-break: break-all;
}
.tcd-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
.tcd-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  cursor: pointer;
}
.tcd-btn:hover:not(:disabled) {
  background: var(--bg-hover);
}
.tcd-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.tcd-btn-primary {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--inverse-text, white);
}
.spin {
  animation: tcd-spin 0.8s linear infinite;
}
@keyframes tcd-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
