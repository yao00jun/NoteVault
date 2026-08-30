<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { Clock, Plus, Trash2, Check, Bell, FileText } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '@/stores/workspace'
import { useI18n } from 'vue-i18n'
import { ReminderService } from '@bindings/github.com/notevault/notevault/index.js'

interface Reminder {
  id: string
  filePath: string
  fileName: string
  content: string
  remindAt: string
  createdAt: string
  completed: boolean
}

const router = useRouter()
const { t, locale } = useI18n()
const workspaceStore = useWorkspaceStore()

const reminders = ref<Reminder[]>([])
const isLoading = ref(false)
const showAddDialog = ref(false)
const newReminder = ref({ content: '', remindAt: '' })
const currentWorkspace = computed(() => workspaceStore.currentWorkspace)

async function loadReminders() {
  if (!currentWorkspace.value?.path) return
  isLoading.value = true
  try {
    const data = await ReminderService.GetAllReminders(currentWorkspace.value.path)
    reminders.value = data as Reminder[]
  } catch (e) {
    console.error('Failed to load reminders:', e)
    reminders.value = []
  } finally {
    isLoading.value = false
  }
}

async function addReminder() {
  if (!currentWorkspace.value?.path || !newReminder.value.content || !newReminder.value.remindAt) return
  try {
    await ReminderService.AddReminder(currentWorkspace.value.path, '', newReminder.value.content, newReminder.value.remindAt)
    newReminder.value = { content: '', remindAt: '' }
    showAddDialog.value = false
    await loadReminders()
  } catch (e) {
    console.error('Failed to add reminder:', e)
  }
}

async function toggleReminder(reminder: Reminder) {
  if (!currentWorkspace.value?.path) return
  try {
    await ReminderService.ToggleReminder(currentWorkspace.value.path, reminder.id)
    reminder.completed = !reminder.completed
  } catch (e) {
    console.error('Failed to toggle reminder:', e)
  }
}

async function deleteReminder(id: string) {
  if (!currentWorkspace.value?.path) return
  if (!confirm(t('reminders.confirmDelete'))) return
  try {
    await ReminderService.DeleteReminder(currentWorkspace.value.path, id)
    await loadReminders()
  } catch (e) {
    console.error('Failed to delete reminder:', e)
  }
}

function formatDateTime(iso: string): string {
  try {
    const d = new Date(iso)
    return d.toLocaleString(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  } catch { return iso }
}

function isOverdue(reminder: Reminder): boolean {
  if (reminder.completed) return false
  try {
    return new Date(reminder.remindAt) < new Date()
  } catch { return false }
}

onMounted(loadReminders)
watch(() => currentWorkspace.value?.id, loadReminders)
</script>

<template>
  <div class="reminders-view">
    <div class="reminders-header">
      <h2 class="reminders-title">
        <Bell :size="20" /> {{ t('reminders.title') }}
      </h2>
      <button
        class="add-btn"
        @click="showAddDialog = true"
      >
        <Plus :size="16" /> {{ t('reminders.create') }}
      </button>
    </div>

    <div class="reminders-content">
      <div
        v-if="isLoading"
        class="reminders-state"
      >
        <div class="spinner" /><p>{{ t('common.loading') }}</p>
      </div>
      <div
        v-else-if="reminders.length === 0"
        class="reminders-state"
      >
        <div class="empty-icon">
          🔔
        </div>
        <h3>{{ t('reminders.emptyTitle') }}</h3>
        <p>{{ t('reminders.emptyDesc') }}</p>
      </div>
      <div
        v-else
        class="reminders-list"
      >
        <div
          v-for="r in reminders"
          :key="r.id"
          class="reminder-item"
          :class="{ completed: r.completed, overdue: isOverdue(r) }"
        >
          <button
            class="reminder-check"
            @click="toggleReminder(r)"
          >
            <Check
              v-if="r.completed"
              :size="16"
            />
          </button>
          <div class="reminder-info">
            <div class="reminder-content">
              {{ r.content }}
            </div>
            <div class="reminder-meta">
              <Clock :size="12" />
              <span :class="{ overdue: isOverdue(r) }">{{ formatDateTime(r.remindAt) }}</span>
              <span
                v-if="isOverdue(r)"
                class="overdue-tag"
              >{{ t('reminders.overdue') }}</span>
            </div>
          </div>
          <button
            class="delete-btn"
            @click="deleteReminder(r.id)"
          >
            <Trash2 :size="16" />
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="showAddDialog"
      class="dialog-overlay"
      @click.self="showAddDialog = false"
    >
      <div class="dialog">
        <h3>{{ t('reminders.formTitle') }}</h3>
        <div class="form-group">
          <label>{{ t('reminders.contentLabel') }}</label>
          <input
            v-model="newReminder.content"
            type="text"
            :placeholder="t('reminders.contentPlaceholder')"
          >
        </div>
        <div class="form-group">
          <label>{{ t('reminders.timeLabel') }}</label>
          <input
            v-model="newReminder.remindAt"
            type="datetime-local"
          >
        </div>
        <div class="dialog-actions">
          <button
            class="cancel-btn"
            @click="showAddDialog = false"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="confirm-btn"
            @click="addReminder"
          >
            {{ t('reminders.createBtn') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.reminders-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.reminders-header { display: flex; align-items: center; justify-content: space-between; padding: var(--space-6) var(--space-8); border-bottom: 1px solid var(--border); background: var(--bg-window); }
.reminders-title { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xl); font-weight: 700; margin: 0; color: var(--text-primary); }
.add-btn { display: flex; align-items: center; gap: var(--space-1); padding: var(--space-2) var(--space-3); border: none; border-radius: var(--radius-md); background: var(--accent); color: white; font-size: var(--text-sm); font-weight: 600; cursor: pointer; }
.reminders-content { flex: 1; overflow-y: auto; padding: var(--space-6) var(--space-8); }
.reminders-state { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: var(--space-12) 0; color: var(--text-muted); gap: var(--space-3); }
.reminders-state h3 { font-size: var(--text-lg); font-weight: 600; color: var(--text-secondary); margin: 0; }
.empty-icon { font-size: 48px; opacity: 0.5; }
.spinner { width: 32px; height: 32px; border: 3px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.reminders-list { display: flex; flex-direction: column; gap: var(--space-2); max-width: 700px; margin: 0 auto; }
.reminder-item { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--bg-card); }
.reminder-item.completed .reminder-content { text-decoration: line-through; opacity: 0.6; }
.reminder-item.overdue { border-color: var(--danger, #ef4444); }
.reminder-check { width: 24px; height: 24px; border: 2px solid var(--border); border-radius: 50%; display: flex; align-items: center; justify-content: center; cursor: pointer; color: var(--accent); flex-shrink: 0; }
.reminder-info { flex: 1; min-width: 0; }
.reminder-content { font-size: var(--text-base); color: var(--text-primary); margin-bottom: 4px; }
.reminder-meta { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xs); color: var(--text-muted); }
.reminder-meta .overdue { color: var(--danger, #ef4444); font-weight: 600; }
.overdue-tag { background: var(--danger, #ef4444); color: white; padding: 1px 6px; border-radius: 8px; font-size: 10px; }
.delete-btn { color: var(--text-muted); padding: 6px; border-radius: 4px; cursor: pointer; }
.delete-btn:hover { color: var(--danger, #ef4444); background: var(--bg-hover); }
.dialog-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.dialog { background: var(--bg-window); border: 1px solid var(--border); border-radius: var(--radius-lg); padding: var(--space-6); width: 400px; max-width: 90vw; }
.dialog h3 { margin: 0 0 var(--space-4) 0; font-size: var(--text-lg); color: var(--text-primary); }
.form-group { margin-bottom: var(--space-4); }
.form-group label { display: block; font-size: var(--text-sm); color: var(--text-secondary); margin-bottom: var(--space-1); }
.form-group input { width: 100%; padding: var(--space-2) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-input); color: var(--text-primary); font-size: var(--text-sm); }
.dialog-actions { display: flex; justify-content: flex-end; gap: var(--space-2); }
.cancel-btn { padding: var(--space-2) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg-card); color: var(--text-secondary); cursor: pointer; }
.confirm-btn { padding: var(--space-2) var(--space-4); border: none; border-radius: var(--radius-sm); background: var(--accent); color: white; cursor: pointer; }
</style>
