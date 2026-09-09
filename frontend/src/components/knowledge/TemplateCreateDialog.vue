<template>
  <Teleport to="body">
    <div
      class="tcd-overlay"
      @click.self="emit('close')"
    >
      <div
        class="tcd-dialog"
        role="dialog"
        aria-modal="true"
        :aria-label="t('templates.createTitle')"
      >
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
            <label for="template-choice">{{ t('templates.selectLabel') }}</label>
            <select
              id="template-choice"
              v-model="selectedName"
              @change="onTemplateChange"
            >
              <optgroup
                v-for="group in templateGroups"
                :key="group.category"
                :label="group.category"
              >
                <option
                  v-for="tpl in group.templates"
                  :key="tpl.name"
                  :value="tpl.name"
                >
                  {{ tpl.name }}{{ tpl.builtin ? t('templates.builtinTag') : '' }}
                </option>
              </optgroup>
            </select>
          </div>

          <div class="tcd-purpose">
            <p>{{ currentTemplate?.description || t('templates.customDescription') }}</p>
            <p
              v-if="currentTemplate?.destination"
              class="tcd-destination"
            >
              {{ t('templates.destinationHint') }}
              <code>{{ recommendedTarget }}</code>
            </p>
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
              <label :for="`template-variable-${name}`">{{ varLabel(name) }}</label>
              <input
                :id="`template-variable-${name}`"
                v-model="variableValues[name]"
                type="text"
                class="tcd-input"
                :placeholder="name"
                @keyup.enter="onEnterCreate"
              >
            </div>
          </div>

          <div class="tcd-field">
            <label for="template-target">{{ t('templates.targetLabel') }}</label>
            <input
              id="template-target"
              v-model="targetPath"
              type="text"
              class="tcd-input tcd-target"
              @input="targetEdited = true"
              @keyup.enter="onEnterCreate"
            >
            <span
              v-if="hasUnfilledTarget"
              class="tcd-note"
            >{{ t('templates.fillDestination') }}</span>
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
              :disabled="!canCreate"
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
import { TemplateService, type TemplateInfo } from '@/api'
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isImeComposing } from '@/utils/ime'
import { FileText, Loader2 } from '@lucide/vue'
import { useWorkspaceStore } from '@/stores/workspace'
const props = defineProps<{ defaultFolder?: string }>()

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
const targetEdited = ref(false)

const workspacePath = computed(() => workspaceStore.currentWorkspace?.path ?? '')
const initialWorkspace = workspacePath.value
const selectionDate = ref(new Date())
let workspaceChanged = false
watch(workspacePath, value => {
  if (value !== initialWorkspace && !workspaceChanged) {
    workspaceChanged = true
    emit('close')
  }
}, { flush: 'sync' })

// 内置模板自定义变量的中文标签；未映射的变量（用户自建模板）原样显示
const VAR_LABEL_KEYS: Record<string, string> = {
  author: 'varAuthor',
  attendees: 'varAttendees',
  deadline: 'varDeadline',
  project: 'varProject',
  book: 'varBook',
  people: 'varPeople',
  question: 'varQuestion',
}
function varLabel(name: string): string {
  const key = VAR_LABEL_KEYS[name]
  return key ? t(`templates.${key}`) : t('templates.variableLabel', { name })
}

const currentTemplate = computed(() => templates.value.find(tp => tp.name === selectedName.value))
const currentVariables = computed(() => currentTemplate.value?.variables ?? [])
const templateGroups = computed(() => {
  const groups = new Map<string, TemplateInfo[]>()
  for (const template of templates.value) {
    const category = template.category || t('templates.customCategory')
    const entries = groups.get(category) ?? []
    entries.push(template)
    groups.set(category, entries)
  }
  return Array.from(groups, ([category, entries]) => ({ category, templates: entries }))
})

const placeholderPattern = /\{\{([a-zA-Z][a-zA-Z0-9_-]*)\}\}/g
const recommendedTarget = computed(() => {
  const template = currentTemplate.value
  if (!template) return ''
  const now = selectionDate.value
  const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const time = `${String(now.getHours()).padStart(2, '0')}-${String(now.getMinutes()).padStart(2, '0')}`
  const values: Record<string, string> = { ...variableValues.value, title: template.name, date, time, datetime: `${date} ${time}` }
  return (template.destination || `${template.name}.md`).replace(placeholderPattern, (placeholder, name: string) => values[name]?.trim() || placeholder)
})
const suggestedTarget = computed(() => {
  const folder = (props.defaultFolder ?? '').replaceAll('\\', '/').replace(/\/+$/, '')
  if (!folder || recommendedTarget.value.startsWith(`${folder}/`)) return recommendedTarget.value
  const filename = recommendedTarget.value.slice(recommendedTarget.value.lastIndexOf('/') + 1)
  return `${folder}/${filename}`
})
watch(suggestedTarget, value => {
  if (!targetEdited.value) targetPath.value = value
})
const hasUnfilledTarget = computed(() => /\{\{[a-zA-Z][a-zA-Z0-9_-]*\}\}/.test(targetPath.value))
const canCreate = computed(() => !loading.value && !creating.value && !workspaceChanged && Boolean(selectedName.value) && Boolean(targetPath.value.trim()) && !hasUnfilledTarget.value)

/** Keep a hand-written path when filling variables; changing templates starts a fresh form. */
function syncSelection(): void {
  targetEdited.value = false
  variableValues.value = {}
  selectionDate.value = new Date()
  targetPath.value = suggestedTarget.value
}

onMounted(async () => {
  if (!initialWorkspace) {
    loading.value = false
    return
  }
  try {
    // 绑定层对切片模型统一标可空，先过滤再入表
    const listed = await TemplateService.ListTemplates(initialWorkspace)
    if (workspaceChanged) return
    templates.value = (listed ?? []).filter(
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

function onEnterCreate(e: KeyboardEvent) {
  // IME 守卫：合成态 Enter 是确认候选字
  if (isImeComposing(e)) return
  void doCreate()
}

async function doCreate(): Promise<void> {
  if (!initialWorkspace || workspacePath.value !== initialWorkspace || !canCreate.value) return
  creating.value = true
  errorMsg.value = ''
  try {
    const node = await TemplateService.CreateFromTemplate(
      initialWorkspace,
      selectedName.value,
      targetPath.value.trim(),
      { ...variableValues.value },
    )
    if (node && !workspaceChanged) {
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
  width: 480px;
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
.tcd-purpose {
  padding: var(--space-3);
  background: var(--bg-window);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: var(--text-sm);
  line-height: 1.6;
}
.tcd-purpose p {
  margin: 0;
}
.tcd-purpose .tcd-destination {
  margin-top: var(--space-2);
  font-size: var(--text-xs);
}
.tcd-destination code {
  color: var(--accent);
  overflow-wrap: anywhere;
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
