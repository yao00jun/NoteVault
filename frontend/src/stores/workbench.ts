import { computed, onScopeDispose, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { ReminderService, WorkbenchService } from '@/api'
import type { InterviewCard, WorkbenchSnapshot, WorkbenchTask } from '@/api/workbench'
import { useWorkspaceStore } from './workspace'
import { localDateKey, selectReviewSession, selectTodayTasks } from '@/utils/workbench'

interface DueReminder { id: string; content: string; filePath: string; remindAt: string; completed: boolean }

/** A disposable, read-only projection of the active workspace's Markdown files. */
export const useWorkbenchStore = defineStore('workbench', () => {
  const workspace = useWorkspaceStore()
  const snapshot = ref<WorkbenchSnapshot | null>(null)
  const today = ref(localDateKey())
  const loading = ref(false)
  const busy = ref(false)
  const error = ref('')
  const reminderError = ref('')
  const reminders = ref<DueReminder[]>([])
  let workspaceGeneration = 0
  let requestID = 0
  let disposed = false
  let scheduled: ReturnType<typeof setTimeout> | undefined

  const tasks = computed(() => snapshot.value?.tasks ?? [])
  const projects = computed(() => snapshot.value?.projects ?? [])
  const books = computed(() => snapshot.value?.books ?? [])
  const cards = computed(() => snapshot.value?.cards ?? [])
  const documents = computed(() => snapshot.value?.documents ?? [])
  const progress = computed(() => (snapshot.value?.progress ?? []).filter(entry => entry.at.slice(0, 10) === today.value).slice().sort((a, b) => b.at.localeCompare(a.at)))
  const radar = computed(() => snapshot.value?.radar ?? { path: 'Learning/技术雷达.md', content: '' })
  const todayTasks = computed(() => selectTodayTasks(tasks.value, today.value))
  const blockers = computed(() => tasks.value.filter(task => !task.completed && (task.blocker || task.status === 'blocked')))
  const reviewSession = computed(() => selectReviewSession(cards.value, today.value))
  const reviewQueue = computed(() => reviewSession.value.queue)
  const reviewedToday = computed(() => reviewSession.value.reviewed)
  const reviewTarget = computed(() => reviewSession.value.target)
  const dueCards = computed(() => reviewSession.value.dueCount)
  const lastUpdated = computed(() => snapshot.value?.indexedAt ?? '')
  const warnings = computed(() => snapshot.value?.warnings ?? [])
  const dueReminders = computed(() => reminders.value.filter(reminder => {
    const time = new Date(reminder.remindAt)
    return !reminder.completed && !Number.isNaN(time.getTime()) && localDateKey(time) <= today.value
  }).sort((a, b) => a.remindAt.localeCompare(b.remindAt)))

  async function refresh(): Promise<void> {
    clearTimeout(scheduled)
    const path = workspace.currentWorkspace?.path
    if (!path || disposed) return
    const generation = workspaceGeneration
    const request = ++requestID
    const day = today.value
    loading.value = true
    const [data, reminderData] = await Promise.allSettled([
      WorkbenchService.GetWorkbench(path, day),
      ReminderService.GetAllReminders(path),
    ])
    if (disposed || generation !== workspaceGeneration || request !== requestID || path !== workspace.currentWorkspace?.path) return
    if (data.status === 'fulfilled') {
      snapshot.value = data.value
      error.value = ''
    } else {
      error.value = `工作台读取失败：${data.reason instanceof Error ? data.reason.message : String(data.reason)}`
    }
    if (reminderData.status === 'fulfilled') {
      reminders.value = (reminderData.value ?? []).filter((entry): entry is NonNullable<typeof entry> => !!entry)
      reminderError.value = ''
    } else {
      reminderError.value = '提醒暂时无法读取，请刷新重试'
    }
    loading.value = false
  }

  function scheduleRefresh() {
    clearTimeout(scheduled)
    scheduled = setTimeout(() => { void refresh() }, 250)
  }

  watch(() => workspace.currentWorkspace?.path, () => {
    workspaceGeneration++
    requestID++
    snapshot.value = null
    reminders.value = []
    error.value = ''
    reminderError.value = ''
    loading.value = false
    busy.value = false
    today.value = localDateKey()
    void refresh()
  }, { immediate: true, flush: 'sync' })
  watch(() => workspace.fileTreeVersion, scheduleRefresh, { flush: 'sync' })

  async function mutate(operation: (path: string, day: string) => Promise<void>) {
    const path = workspace.currentWorkspace?.path
    if (!path) throw new Error('请先打开工作区')
    if (busy.value) throw new Error('上一项操作正在保存，请稍候')
    const generation = workspaceGeneration
    busy.value = true
    try {
      await operation(path, today.value)
      if (generation !== workspaceGeneration || disposed) return
      workspace.incrementFileTreeVersion()
      await refresh()
    } finally {
      if (generation === workspaceGeneration) busy.value = false
    }
  }

  function updateTask(task: WorkbenchTask, action: 'toggle' | 'progress' | 'blocker', value = '') {
    return mutate((path, day) => WorkbenchService.UpdateWorkbenchTask(path, task.filePath, task.lineIndex, task.sourceLine, action, value, day))
  }
  const toggleTask = (task: WorkbenchTask) => updateTask(task, 'toggle')
  const recordProgress = (task: WorkbenchTask, note: string) => updateTask(task, 'progress', note.trim())
  const setBlocker = (task: WorkbenchTask, reason: string) => updateTask(task, 'blocker', reason.trim())
  function reviewCard(card: InterviewCard, level: '掌握' | '模糊' | '不会') {
    return mutate((path, day) => WorkbenchService.ReviewInterviewCard(path, card.filePath, card.lineIndex, card.comment, level, day))
  }
  function addTask(projectFolder: string, title: string, kind: string, due: string) {
    return mutate((path, day) => WorkbenchService.AddWorkbenchTask(path, projectFolder, title.trim(), kind, due, day))
  }

  function updateDay() {
    const day = localDateKey()
    if (day !== today.value) {
      today.value = day
      void refresh()
    }
  }
  function onFocus() { updateDay(); scheduleRefresh() }
  const clock = typeof window !== 'undefined' ? setInterval(updateDay, 60_000) : undefined
  if (typeof window !== 'undefined') window.addEventListener('focus', onFocus)
  onScopeDispose(() => {
    disposed = true
    requestID++
    clearInterval(clock)
    clearTimeout(scheduled)
    if (typeof window !== 'undefined') window.removeEventListener('focus', onFocus)
  })

  return { snapshot, today, loading, busy, error, reminderError, tasks, projects, books, cards, documents, progress, radar, todayTasks, blockers, reviewQueue, reviewedToday, reviewTarget, dueCards, reminders, dueReminders, lastUpdated, warnings, refresh, scheduleRefresh, toggleTask, recordProgress, setBlocker, reviewCard, addTask }
})
